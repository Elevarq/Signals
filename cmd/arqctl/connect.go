package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/elevarq/arq-signals/internal/conntest"
)

// connectCmd is the `arqctl connect` parent for connection diagnostics
// (R096). It exposes a `test` subcommand today; a future expansion
// (e.g. `arqctl connect probe` for traffic-shape checks) drops in
// alongside without crowding the root namespace.
func connectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Connection diagnostics",
	}
	cmd.AddCommand(connectTestCmd())
	return cmd
}

// connectTestCmd is the `arqctl connect test` subcommand (R096).
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
			"  arqctl connect test             test every enabled target in config\n" +
			"  arqctl connect test <name>      test one target from config\n" +
			"  arqctl connect test --dsn '...' test an ad-hoc DSN without config\n" +
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
