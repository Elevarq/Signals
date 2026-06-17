package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/elevarq/arq-signals/internal/doctor"
)

// usageError signals an argument-parsing problem from a RunE handler
// (e.g. an unknown --check name). main() maps it to exit code 2.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

// failError signals that a command ran to completion but reported at
// least one FAIL outcome. main() maps it to exit code 1. The empty
// message is intentional — the failing detail is already on stdout
// (text or JSON report) so cobra has no useful summary to print.
type failError struct{}

func (failError) Error() string { return "" }

// doctorCmd is the `signalsctl doctor` subcommand (R095). It runs the
// read-only operator pre-flight checks defined in
// specifications/doctor.md and signals 0 / 1 / 2 exit codes via
// typed errors handled in main().
//
// Unlike status/collect/export, doctor does NOT call the daemon's
// HTTP API. It reads the config from disk and probes the configured
// store path and targets directly.
func doctorCmd() *cobra.Command {
	var configPath string
	var jsonOut bool
	var checks []string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run pre-flight readiness checks against config and targets",
		Long: "Runs read-only operator pre-flight checks (R095):\n" +
			"  C1 config_valid             - Config file parses + passes ValidateStrict\n" +
			"  C2 store_writable           - SQLite store path is writable\n" +
			"  C3 target_reachable         - Each enabled target accepts TCP\n" +
			"  C4 role_safe                - Each reachable target's role is not superuser/replication/bypassrls\n" +
			"  C5 collector_prerequisites  - Each target's enabled collectors classified (available/missing/unsupported)\n" +
			"  C6 snapshot_freshness       - Each target's latest snapshot is within 2x poll_interval\n" +
			"\n" +
			"Exits 0 when every check is OK, 1 when any check FAILs,\n" +
			"and 2 on usage errors (unknown --check name).",
		// SilenceUsage prevents cobra from printing the long usage
		// dump on every RunE-returned error. We surface errors
		// ourselves via stderr + exit code; the long help is for
		// `--help`, not failure paths.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := doctor.Run(context.Background(), configPath, checks)
			if err != nil {
				fmt.Fprintln(os.Stderr, "doctor:", err)
				return usageError{msg: err.Error()}
			}

			if jsonOut {
				if writeErr := writeJSONReport(cmd.OutOrStdout(), report); writeErr != nil {
					return writeErr
				}
			} else {
				if writeErr := writeTextReport(cmd.OutOrStdout(), report); writeErr != nil {
					return writeErr
				}
			}

			if report.Summary.Fail > 0 {
				return failError{}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", defaultConfigPath(), "Path to config file")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit a JSON report instead of human-readable text")
	cmd.Flags().StringSliceVar(&checks, "check", nil, "Run only the named checks (e.g. --check=C1,C3)")
	return cmd
}

func defaultConfigPath() string {
	if v := os.Getenv("ARQ_SIGNALS_CONFIG"); v != "" {
		return v
	}
	return "/etc/arq/signals.yaml"
}

// writeJSONReport encodes the report to w. The fail-gate lives in
// RunE; this function is a pure formatter.
func writeJSONReport(w io.Writer, report doctor.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

// writeTextReport renders a column-aligned human-readable report to
// w. Like writeJSONReport, the fail-gate lives in RunE; this function
// only formats.
func writeTextReport(w io.Writer, report doctor.Report) error {
	for _, c := range report.Checks {
		status := strings.ToUpper(string(c.Status))
		// Pad status to 4 chars for column alignment.
		paddedStatus := status + strings.Repeat(" ", 4-len(status))
		nameAndTarget := c.Name
		if c.Target != "" {
			nameAndTarget = c.Name + " " + c.Target
		}
		if _, err := fmt.Fprintf(w, "%s %s %-30s %s\n", paddedStatus, c.ID, nameAndTarget, c.Detail); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "\nSummary: %d OK, %d WARN, %d FAIL\n",
		report.Summary.OK, report.Summary.Warn, report.Summary.Fail)
	return err
}
