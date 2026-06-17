package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/elevarq/arq-signals/internal/conntest"
	"github.com/elevarq/arq-signals/internal/guidedconnect"
)

// connectCmd is the `signalsctl connect` parent for connection diagnostics
// (R096). It exposes a `test` subcommand today; a future expansion
// (e.g. `signalsctl connect probe` for traffic-shape checks) drops in
// alongside without crowding the root namespace.
func connectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Connection diagnostics",
	}
	cmd.AddCommand(connectTestCmd())
	cmd.AddCommand(connectAutoCmd())
	return cmd
}

// connectAutoCmd is the `signalsctl connect --auto` guided-onboarding
// subcommand (#99). It auto-detects the cloud + ambient identity, proposes
// an auth_method, resolves the credential, runs the connection diagnostic
// over verify-full, validates the role is read-only, and emits either a
// ready-to-use (secret-free) target config block or a copy-pasteable fix.
// Spec: features/arq-signals/guided-connect.md.
func connectAutoCmd() *cobra.Command {
	var opts guidedconnect.Options

	cmd := &cobra.Command{
		Use:   "auto",
		Short: "Guided onboarding: detect, connect, verify, and emit a ready target config",
		Long: "Guided onboarding for a single PostgreSQL target.\n" +
			"\n" +
			"Auto-detects the cloud platform and ambient identity, selects the\n" +
			"right auth_method, resolves the credential (no secret is ever\n" +
			"printed), runs the connection diagnostic with sslmode=verify-full,\n" +
			"validates the role is read-only, and prints a ready-to-use target\n" +
			"config block on success — or the exact grant / IAM binding to apply\n" +
			"on a fixable gap. Spec: ARQ-SIGNALS-CONNECT.\n" +
			"\n" +
			"Default is dry-run (print only). Pass --write <path> to append the\n" +
			"verified, secret-free block to a config file's targets: list.\n" +
			"\n" +
			"Exits 0 on a verified connection, 1 on a fixable gap, 2 on usage errors.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Getenv = os.Getenv
			// Prompt for a password only on the password fallback and only
			// when stdin is a TTY (resolved decision 1).
			if term.IsTerminal(int(os.Stdin.Fd())) {
				opts.PasswordPrompt = func() (string, error) {
					return promptPassword(cmd.OutOrStdout())
				}
			}

			outcome, err := guidedconnect.Run(context.Background(), opts, guidedconnect.Deps{})
			if err != nil {
				var ue *guidedconnect.UsageError
				if errors.As(err, &ue) {
					return usageError{msg: ue.Msg}
				}
				return usageError{msg: err.Error()}
			}

			if werr := writeAutoOutcome(cmd.OutOrStdout(), outcome); werr != nil {
				return werr
			}
			if !outcome.Success {
				return failError{}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Host, "host", "", "Target host (required)")
	cmd.Flags().IntVar(&opts.Port, "port", 5432, "Target port")
	cmd.Flags().StringVar(&opts.DBName, "dbname", "", "Target database name")
	cmd.Flags().StringVar(&opts.User, "user", "", "Role to authenticate as (required)")
	cmd.Flags().StringVar(&opts.AuthMethod, "auth-method", "", "Override detection: password|aws_rds_iam|azure_entra|gcp_cloudsql_iam|secret_store|mtls")
	cmd.Flags().StringVar(&opts.Region, "region", "", "AWS region (aws_rds_iam)")
	cmd.Flags().StringVar(&opts.AzureClientID, "azure-client-id", "", "User-assigned managed identity client ID (azure_entra)")
	cmd.Flags().StringVar(&opts.GCPImpersonateServiceAccount, "gcp-impersonate-service-account", "", "Service account to impersonate (gcp_cloudsql_iam)")
	cmd.Flags().StringVar(&opts.SecretRef, "secret-ref", "", "Cloud secret-store reference (secret_store)")
	cmd.Flags().StringVar(&opts.SSLCert, "sslcert", "", "Client certificate path (mtls)")
	cmd.Flags().StringVar(&opts.SSLKey, "sslkey", "", "Client private key path (mtls)")
	cmd.Flags().StringVar(&opts.SSLRootCert, "sslrootcert-file", "", "Server CA certificate path (sslrootcert)")
	cmd.Flags().StringVar(&opts.Name, "name", "", "Target name for the emitted block (default: derived from host)")
	cmd.Flags().StringVar(&opts.WritePath, "write", "", "Append the verified target block to this config file (default: dry-run)")
	return cmd
}

// promptPassword reads a password from the terminal without echoing it.
func promptPassword(w io.Writer) (string, error) {
	_, _ = fmt.Fprint(w, "Password: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(w)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// writeAutoOutcome renders the guided-connect outcome. On success it prints
// the confirmation and the ready target block; on a fixable gap it prints
// the redacted, actionable message. No secret is ever printed
// (ARQ-SIGNALS-CONNECT-INV001).
func writeAutoOutcome(w io.Writer, o guidedconnect.Outcome) error {
	if o.Success {
		header := "Add this target to your config (targets:):"
		if o.Wrote {
			header = "Appended the target block to the config file:"
		}
		_, err := fmt.Fprintf(w, "OK   %s\n\n%s\n\n%s", o.Message, header, o.ConfigBlock)
		return err
	}
	_, err := fmt.Fprintf(w, "FAIL [%s]\n\n%s\n", o.Category, o.Message)
	return err
}

// connectTestCmd is the `signalsctl connect test` subcommand (R096).
// Tests one target / every enabled target / an ad-hoc DSN and returns
// classified failure categories.
func connectTestCmd() *cobra.Command {
	var (
		configPath string
		jsonOut    bool
		dsnArg     string
	)

	cmd := &cobra.Command{
		Use:   "test [<target-name>]",
		Short: "Test PostgreSQL connectivity with classified diagnostics",
		Long: "Tests one or more PostgreSQL connections and classifies any\n" +
			"failure into ok / dns / tcp / tls / auth / startup / role /\n" +
			"password_resolve / config. Spec: R096.\n" +
			"\n" +
			"Modes:\n" +
			"  signalsctl connect test             test every enabled target in config\n" +
			"  signalsctl connect test <name>      test one target from config\n" +
			"  signalsctl connect test --dsn '...' test an ad-hoc DSN without config\n" +
			"\n" +
			"Ad-hoc --dsn fields (space-separated key=value):\n" +
			"  host=<host>            required\n" +
			"  port=<int>             required\n" +
			"  dbname=<db>            required\n" +
			"  user=<role>            required\n" +
			"  sslmode=<mode>         default 'prefer'\n" +
			"  password_env=<envvar>  optional\n" +
			"  password_file=<path>   optional\n" +
			"  pgpass_file=<path>     optional\n" +
			"\n" +
			"Exits 0 when every attempt is ok, 1 when any attempt fails, 2 on\n" +
			"usage errors (mutually exclusive flags, bad --dsn, unknown target).",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var targetName string
			if len(args) == 1 {
				targetName = args[0]
			}
			var adhoc map[string]string
			if dsnArg != "" {
				if targetName != "" {
					return usageError{msg: "<target-name> and --dsn are mutually exclusive"}
				}
				parsed, err := parseDSNArg(dsnArg)
				if err != nil {
					return usageError{msg: err.Error()}
				}
				adhoc = parsed
			}

			report, runErr := conntest.Run(context.Background(), configPath, targetName, adhoc, conntest.Options{})
			if runErr != nil {
				fmt.Fprintln(os.Stderr, "connect test:", runErr)
				return usageError{msg: runErr.Error()}
			}

			if jsonOut {
				if err := writeConnectJSON(cmd.OutOrStdout(), report); err != nil {
					return err
				}
			} else {
				if err := writeConnectText(cmd.OutOrStdout(), report); err != nil {
					return err
				}
			}

			if report.Summary.Fail > 0 {
				return failError{}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", defaultConfigPath(), "Path to config file (ignored when --dsn is supplied)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit a JSON report instead of human-readable text")
	cmd.Flags().StringVar(&dsnArg, "dsn", "", "Ad-hoc DSN fields (e.g. \"host=db port=5432 dbname=app user=arq sslmode=disable\")")
	return cmd
}

// parseDSNArg splits a space-separated key=value DSN string into a
// map[string]string for conntest.Run's adhoc parameter. Empty values
// are rejected so an operator typo like `host= port=5432` fails fast
// rather than silently dialing empty host.
func parseDSNArg(s string) (map[string]string, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil, fmt.Errorf("--dsn is empty")
	}
	out := make(map[string]string, len(fields))
	for _, f := range fields {
		eq := strings.IndexByte(f, '=')
		if eq <= 0 || eq == len(f)-1 {
			return nil, fmt.Errorf("--dsn: malformed field %q (want key=value)", f)
		}
		key := f[:eq]
		val := f[eq+1:]
		out[key] = val
	}
	return out, nil
}

// writeConnectJSON emits the report's wire shape. Fail-gate lives
// in RunE; this is a pure formatter.
func writeConnectJSON(w io.Writer, report conntest.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

// writeConnectText renders a human-readable one-line-per-attempt
// report. The category is column-aligned to 16 chars so wider
// categories (password_resolve) line up with shorter ones (ok).
func writeConnectText(w io.Writer, report conntest.Report) error {
	for _, a := range report.Attempts {
		mark := "OK  "
		if a.Category != conntest.CategoryOK {
			mark = "FAIL"
		}
		cat := string(a.Category)
		paddedCat := cat + strings.Repeat(" ", maxCategoryWidth-len(cat))
		if _, err := fmt.Fprintf(w, "%s %-24s %s %s\n", mark, a.Target, paddedCat, a.Detail); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "\nSummary: %d OK, %d FAIL\n", report.Summary.OK, report.Summary.Fail)
	return err
}

const maxCategoryWidth = 16 // long enough for "password_resolve"
