package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/elevarq/arq-signals/internal/safety"
)

var (
	apiAddr  string
	apiToken string
)

func main() {
	root := &cobra.Command{
		Use:   "arqctl",
		Short: "CLI for Elevarq Signals",
	}

	defaultToken := os.Getenv("ARQ_SIGNALS_API_TOKEN")
	root.PersistentFlags().StringVar(&apiAddr, "api-addr", "http://127.0.0.1:8081", "Elevarq Signals API address")
	root.PersistentFlags().StringVar(&apiToken, "api-token", defaultToken, "API bearer token (default: $ARQ_SIGNALS_API_TOKEN)")

	root.AddCommand(versionCmd())
	root.AddCommand(statusCmd())
	root.AddCommand(collectCmd())
	root.AddCommand(exportCmd())
	root.AddCommand(doctorCmd())
	root.AddCommand(connectCmd())

	if err := root.Execute(); err != nil {
		// Subcommands signal usage errors (exit 2) vs runtime failures
		// (exit 1) via typed sentinels. Anything else falls through
		// to the generic exit-1 path. Keeps RunE handlers pure —
		// they return errors instead of calling os.Exit themselves
		// (see internal/doctor and cmd/arqctl/doctor.go for the
		// usageError / failError types).
		switch err.(type) {
		case usageError:
			os.Exit(2)
		case failError:
			os.Exit(1)
		default:
			os.Exit(1)
		}
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("arqctl %s (%s) built %s\n", safety.Version, safety.Commit, safety.BuildDate)
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show collector status",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := apiGet("/status")
			if err != nil {
				return fmt.Errorf("status request failed: %w", err)
			}
			defer resp.Body.Close()

			if err := checkSuccess(resp, "status"); err != nil {
				return err
			}

			var data map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}

			out, _ := json.MarshalIndent(data, "", "  ")
			fmt.Println(string(out))
			return nil
		},
	}
}

func collectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collect",
		Short: "Collection management",
	}

	var force bool
	now := &cobra.Command{
		Use:   "now",
		Short: "Trigger immediate collection",
		Long: `Trigger an immediate collection cycle.

By default the daemon respects 'signals.min_snapshot_interval' (R091)
— a target collected within the window is skipped with reason
min_interval_not_elapsed. The --force flag bypasses that check for
this one cycle (R092). Audit events for forced cycles carry
forced=true.

Spec: features/arq-signals/specification.md (R091, R092).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var body io.Reader
			if force {
				// R092: opt-in interval bypass via JSON body field.
				body = bytes.NewBufferString(`{"force":true}`)
			}
			resp, err := apiRequestWithTimeout("POST", "/collect/now", body, 10*time.Second)
			if err != nil {
				return fmt.Errorf("collect request failed: %w", err)
			}
			defer resp.Body.Close()

			if err := checkSuccess(resp, "collect"); err != nil {
				return err
			}

			var result map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}
			out, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(out))
			return nil
		},
	}
	now.Flags().BoolVar(&force, "force", false, "bypass signals.min_snapshot_interval for this one cycle (R092)")
	cmd.AddCommand(now)
	cmd.AddCommand(collectPauseCmd())
	cmd.AddCommand(collectResumeCmd())

	return cmd
}

// collectPauseCmd is `arqctl collect pause` (R097). Sets a target's
// circuit state to `paused` via the daemon API.
func collectPauseCmd() *cobra.Command {
	var target, reason string
	cmd := &cobra.Command{
		Use:   "pause",
		Short: "Pause collection for a target (or all enabled targets when --target is omitted)",
		Long: `Pause collection for one target or every enabled target.

A paused target stays paused until 'arqctl collect resume' is run.
State is in-memory only — a daemon restart resumes all targets.
The pause/resume events live in the audit log so the operator trail
survives the restart.

Spec: features/arq-signals/specification.md (R097).`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]string{}
			if target != "" {
				body["target"] = target
			}
			if reason != "" {
				body["reason"] = reason
			}
			payload, _ := json.Marshal(body)
			resp, err := apiRequestWithTimeout("POST", "/collect/pause", bytes.NewReader(payload), 10*time.Second)
			if err != nil {
				return fmt.Errorf("pause request failed: %w", err)
			}
			defer resp.Body.Close()
			if err := checkSuccess(resp, "pause"); err != nil {
				return err
			}
			out, _ := io.ReadAll(resp.Body)
			fmt.Println(string(out))
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "target name (empty = every enabled target)")
	cmd.Flags().StringVar(&reason, "reason", "", "operator-supplied reason (<=256 chars)")
	return cmd
}

// collectResumeCmd is `arqctl collect resume` (R097).
func collectResumeCmd() *cobra.Command {
	var target string
	cmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume collection for a paused target (or all enabled targets when --target is omitted)",
		Long: `Resume collection for one target or every enabled target.

Resume clears the paused state and resets the consecutive-failure
counter — a previously auto-opened target also returns to closed.
Unknown target names are rejected (FC-CIRC-02).

Spec: features/arq-signals/specification.md (R097).`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]string{}
			if target != "" {
				body["target"] = target
			}
			payload, _ := json.Marshal(body)
			resp, err := apiRequestWithTimeout("POST", "/collect/resume", bytes.NewReader(payload), 10*time.Second)
			if err != nil {
				return fmt.Errorf("resume request failed: %w", err)
			}
			defer resp.Body.Close()
			if err := checkSuccess(resp, "resume"); err != nil {
				return err
			}
			out, _ := io.ReadAll(resp.Body)
			fmt.Println(string(out))
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "target name (empty = every enabled target)")
	return cmd
}

func exportCmd() *cobra.Command {
	var (
		output     string
		all        bool
		snapshotID string
		since      string
		until      string
		targetID   int64
	)
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export collected data as ZIP",
		Long: `Export the daemon's collected data as a ZIP archive.

By default ('arqctl export' with no flags), the export contains only the
LATEST completed snapshot per active target — one snapshot per target,
not the daemon's accumulated history. This is the unit downstream
consumers (Elevarq Analyzer, third-party integrations) ingest as a single
analysis.

Use the selector flags below to widen or narrow the scope:

  --all                       include every snapshot in local storage
                              (the pre-R084 forensic full-history mode).
                              Mutually exclusive with --snapshot-id.
  --snapshot-id <id>          include exactly one snapshot. Mutually
                              exclusive with --all. Unknown id → 404.
  --since/--until <RFC3339>   restrict to a half-open time window.
  --target-id <int>           narrow any of the above to a single target.

Spec: features/arq-signals/specification.md (R084..R086).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if all && snapshotID != "" {
				return fmt.Errorf("--all and --snapshot-id are mutually exclusive")
			}

			// Build query string from the flags. Empty values are
			// omitted so the server falls through to the R084 default.
			q := url.Values{}
			if all {
				q.Set("all", "true")
			}
			if snapshotID != "" {
				q.Set("snapshot_id", snapshotID)
			}
			if since != "" {
				q.Set("since", since)
			}
			if until != "" {
				q.Set("until", until)
			}
			if targetID > 0 {
				q.Set("target_id", strconv.FormatInt(targetID, 10))
			}
			path := "/export"
			if encoded := q.Encode(); encoded != "" {
				path += "?" + encoded
			}

			resp, err := apiGet(path)
			if err != nil {
				return fmt.Errorf("export request failed: %w", err)
			}
			defer resp.Body.Close()

			if err := checkSuccess(resp, "export"); err != nil {
				return err
			}

			if output == "" {
				output = fmt.Sprintf("arq-export-%s.zip", time.Now().UTC().Format("20060102-150405"))
			}

			f, err := os.Create(output)
			if err != nil {
				return fmt.Errorf("create output file: %w", err)
			}
			defer f.Close()

			n, err := io.Copy(f, resp.Body)
			if err != nil {
				return fmt.Errorf("write export: %w", err)
			}

			fmt.Printf("Export saved to %s (%d bytes)\n", output, n)
			return nil
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "output file path (default: arq-export-<timestamp>.zip)")
	cmd.Flags().BoolVar(&all, "all", false, "include every snapshot in local storage (forensic full-history)")
	cmd.Flags().StringVar(&snapshotID, "snapshot-id", "", "include exactly one snapshot by id")
	cmd.Flags().StringVar(&since, "since", "", "include snapshots collected at or after this RFC3339 timestamp")
	cmd.Flags().StringVar(&until, "until", "", "include snapshots collected at or before this RFC3339 timestamp")
	cmd.Flags().Int64Var(&targetID, "target-id", 0, "narrow the export to a single target (0 = all targets)")
	return cmd
}

// checkSuccess returns an error if resp's status is not 2xx, including the
// response body in the error message so the operator sees what the server
// actually said. Callers must still close resp.Body.
func checkSuccess(resp *http.Response, op string) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return fmt.Errorf("%s failed: HTTP %d %s", op, resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	return fmt.Errorf("%s failed: HTTP %d %s: %s", op, resp.StatusCode, http.StatusText(resp.StatusCode), body)
}

func apiRequestWithTimeout(method, path string, body io.Reader, timeout time.Duration) (*http.Response, error) {
	req, err := http.NewRequest(method, apiAddr+path, body)
	if err != nil {
		return nil, err
	}
	if apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+apiToken)
	}
	client := &http.Client{Timeout: timeout}
	return client.Do(req)
}

func apiGet(path string) (*http.Response, error) {
	return apiRequestWithTimeout("GET", path, nil, 30*time.Second)
}
