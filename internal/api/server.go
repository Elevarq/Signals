package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/config"
	"github.com/elevarq/arq-signals/internal/db"
	"github.com/elevarq/arq-signals/internal/export"
	"github.com/elevarq/arq-signals/internal/metrics"
	"github.com/elevarq/arq-signals/internal/safety"
)

// Deps holds handler dependencies.
type Deps struct {
	DB        *db.DB
	Collector *collector.Collector
	Exporter  *export.Builder
	Targets   []config.TargetConfig
	// ConfigPath is the absolute path of the YAML config file the
	// daemon loaded at startup. Used by POST /reload (R100) to
	// re-read the same file. Empty when the daemon was started
	// with built-in defaults.
	ConfigPath string
	// Metrics is the optional Prometheus registry. When nil the
	// /metrics endpoint is not registered. Pass non-nil only when
	// signals.metrics_enabled is true.
	Metrics *metrics.Registry
	// MetricsPath is the URL path the /metrics endpoint is mounted on.
	// Ignored when Metrics is nil.
	MetricsPath string
	// ArqControlPlaneTokenFn returns the current Arq control-plane
	// bearer token (R083), or empty string when control-plane auth
	// is disabled. The closure is invoked once per authenticated
	// request so token-file rotation takes effect on the next call
	// without restarting the daemon. nil is equivalent to a function
	// that always returns "" — the standalone-mode default.
	ArqControlPlaneTokenFn func() string
}

// Server is the Arq Signals HTTP API server.
type Server struct {
	httpServer *http.Server
	deps       *Deps
}

// NewServer creates a new API Server with signals-only endpoints.
func NewServer(addr string, readTimeout, writeTimeout time.Duration, apiToken string, deps *Deps) *Server {
	mux := http.NewServeMux()

	// Register signals-only routes.
	mux.HandleFunc("GET /health", handleHealth(deps))
	mux.HandleFunc("GET /status", handleStatus(deps))
	mux.HandleFunc("POST /collect/now", handleCollectNow(deps))
	mux.HandleFunc("POST /collect/pause", handleCollectPause(deps))
	mux.HandleFunc("POST /collect/resume", handleCollectResume(deps))
	mux.HandleFunc("POST /reload", handleConfigReload(deps, deps.ConfigPath))
	mux.HandleFunc("GET /export", handleExport(deps))

	// Optional Prometheus /metrics endpoint (R079). Off unless an
	// explicit Metrics registry is supplied. Inherits the same bearer
	// token auth as the rest of the API; operators that want
	// unauthenticated scraping should bind locally and use
	// network-level controls.
	if deps.Metrics != nil && deps.MetricsPath != "" {
		mux.Handle("GET "+deps.MetricsPath, promhttp.HandlerFor(
			deps.Metrics.Gatherer(),
			promhttp.HandlerOpts{
				ErrorLog:      slog.NewLogLogger(slog.Default().Handler(), slog.LevelError),
				ErrorHandling: promhttp.ContinueOnError,
			},
		))
	}

	// Wrap with middleware: recovery -> logging -> token auth.
	tokenLimiter := newTokenRateLimiter()
	handler := recoveryMiddleware(loggingMiddleware(
		tokenAuthMiddleware(apiToken, deps.ArqControlPlaneTokenFn, tokenLimiter)(mux),
	))

	return &Server{
		httpServer: &http.Server{
			Addr:         addr,
			Handler:      handler,
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
		},
		deps: deps,
	}
}

// Handler returns the HTTP handler for testing.
func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

// Start begins listening and serving. It blocks until the server stops.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}
	slog.Info("API server listening", "addr", s.httpServer.Addr)
	return s.httpServer.Serve(ln)
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("API server shutting down")
	return s.httpServer.Shutdown(ctx)
}

// --- Handlers ---

func handleHealth(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"version": safety.Version,
		})
	}
}

func handleStatus(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Codex post-0.3.1 M-002: surface DB read failures as 500
		// instead of swallowing them with `_`. A silent fallback to
		// "0 snapshots / 0 targets" makes a wedged SQLite look like
		// a healthy empty system.
		snapCount, err := deps.DB.CountSnapshots()
		if err != nil {
			slog.Error("status: CountSnapshots failed", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "status read failed"})
			return
		}
		targets, err := deps.DB.GetTargets()
		if err != nil {
			slog.Error("status: GetTargets failed", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "status read failed"})
			return
		}
		instanceID, err := deps.DB.GetMeta("instance_id")
		if err != nil {
			slog.Error("status: GetMeta(instance_id) failed", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "status read failed"})
			return
		}

		var targetInfo []map[string]any
		for _, t := range targets {
			// R109 / INV-SIGNALS-14: /status lists only enabled targets so
			// it agrees with the default export's active-target set. A
			// disabled or removed target is reconciled to enabled=0 on
			// startup/reload and is excluded here.
			if !t.Enabled {
				continue
			}
			tInfo := map[string]any{
				"id":      t.ID,
				"name":    t.Name,
				"host":    t.Host,
				"port":    t.Port,
				"dbname":  t.DBName,
				"user":    t.Username,
				"sslmode": t.SSLMode,
				"enabled": t.Enabled,
			}
			// secret_type and secret_ref are intentionally omitted from
			// /status to avoid revealing credential source details.

			if lc := deps.DB.GetTargetLastCollected(t.ID); lc != "" {
				tInfo["last_collected"] = lc
			}

			targetInfo = append(targetInfo, tInfo)
		}

		queryCatalog, err := deps.DB.GetQueryCatalog()
		if err != nil {
			slog.Error("status: GetQueryCatalog failed", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "status read failed"})
			return
		}

		resp := map[string]any{
			"instance_id": instanceID,
			"version":     safety.Version,
			// R109 / INV-SIGNALS-14: target_count must match the
			// active-target set surfaced in targetInfo (which already
			// filters out enabled=0). Counting the unfiltered
			// GetTargets() result would let a disabled or removed
			// target still bump the count.
			"target_count":        len(targetInfo),
			"targets":             targetInfo,
			"snapshot_count":      snapCount,
			"query_catalog_count": len(queryCatalog),
			"last_collected":      deps.Collector.LastCollected(),
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// requestIDPattern bounds R082 Phase 2's `request_id` correlation
// identifier. ASCII alphanumerics, underscore, dash; up to 32 chars.
// Restricting the charset keeps audit logs greppable and prevents
// log-injection via control bytes. ULIDs satisfy this regex.
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// reasonPattern bounds R082 Phase 2's `reason` label. Shares the
// request_id charset so neither field can carry log-injection
// payloads or unbounded whitespace. Up to 64 chars.
var reasonPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// handleCollectNow handles POST /collect/now with the full R082
// Phase 1 + Phase 2 contract.
//
// Body is optional. The historical empty-body path keeps collecting
// every enabled target and is unchanged at the HTTP level. Phase 2
// adds a single new behaviour: every request — empty body included —
// now emits an `audit_event=collect_now_requested` slog record with
// actor, requested_targets, accepted_targets, request_id, and (when
// supplied) reason. R078's audit-attribute denylist remains in
// force; secrets and SQL payloads never reach the audit log.
//
// Body shape (all fields optional):
//
//	{
//	  "targets": ["a", "b"],
//	  "request_id": "01J5K…",
//	  "reason": "scheduled_arq_cycle"
//	}
//
// Validation:
//   - targets empty array, unknown name, or disabled target → 400
//     with `accepted_targets` + per-name `rejected_targets`. Emits
//     `collect_now_rejected` audit event. (Phase 1 contract,
//     unchanged.)
//   - request_id present but doesn't match `^[A-Za-z0-9_-]{1,32}$`
//     → 400, emits `collect_now_rejected`.
//   - reason present but doesn't match `^[A-Za-z0-9_-]{1,64}$`
//     → 400, emits `collect_now_rejected`.
//   - Invalid JSON → 400, emits `collect_now_rejected`.
//
// When the channel buffer is full because a previous on-demand cycle
// is still queued, the request returns 202 (R032: no overlapping
// cycles, the in-flight filter wins) and emits a
// `collect_now_dropped` audit event so the request_id stays in the
// trail even when the cycle never fires.
//
// actor is always `local_operator` in Phase 2. The
// `arq_control_plane` actor value is reserved for Phase 3.
// collectNowMaxBodyBytes caps the /collect/now request body. The legal
// payload is three short fields (targets array, request_id ≤32 chars,
// reason ≤64 chars) — even a few hundred targets stays well under
// 64 KiB. Bigger requests are either malformed or hostile; reject
// before allocating. Codex post-0.3.1 L-001.
const collectNowMaxBodyBytes = 64 * 1024

func handleCollectNow(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Enforce the body-size cap before reading. A MaxBytesError
		// from io.ReadAll → 413 with bounded JSON; the audit event
		// still fires so the request is not silently lost.
		r.Body = http.MaxBytesReader(w, r.Body, collectNowMaxBodyBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				safety.AuditLog("collect_now_rejected",
					"actor", actorFromCtx(r.Context()),
					"error", "body_too_large",
				)
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
					"error": fmt.Sprintf("request body exceeds %d bytes", collectNowMaxBodyBytes),
				})
				return
			}
			safety.AuditLog("collect_now_rejected",
				"actor", actorFromCtx(r.Context()),
				"error", "body_read_error",
			)
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "could not read request body"})
			return
		}
		body = bytes.TrimSpace(body)

		// R100 + #106: read from the collector so SIGHUP / POST /reload
		// is honoured. deps.Targets is the construction-time snapshot;
		// after reload it's stale.
		currentTargets := deps.Collector.Targets()
		var allEnabled []string
		for _, t := range currentTargets {
			if t.Enabled {
				allEnabled = append(allEnabled, t.Name)
			}
		}

		var (
			targetFilter   []string
			requestID      string
			reason         string
			suppliedReqID  bool
			suppliedReason bool
			force          bool
		)

		// Parse body when non-empty. Empty body retains Phase 1 backward
		// compatibility — no targets / no request_id / no reason.
		if len(body) > 0 {
			var req struct {
				Targets   *[]string `json:"targets,omitempty"`
				RequestID *string   `json:"request_id,omitempty"`
				Reason    *string   `json:"reason,omitempty"`
				// R092: per-request override of R091's
				// min_snapshot_interval. Operators set this via
				// `arqctl collect now --force`. Defaults to false.
				Force *bool `json:"force,omitempty"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				safety.AuditLog("collect_now_rejected",
					"actor", actorFromCtx(r.Context()),
					"error", "invalid_json",
				)
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error": "invalid JSON body",
				})
				return
			}

			// Validate request_id format before anything else so we can
			// surface it on subsequent rejection events.
			if req.RequestID != nil {
				if !requestIDPattern.MatchString(*req.RequestID) {
					safety.AuditLog("collect_now_rejected",
						"actor", actorFromCtx(r.Context()),
						"error", "invalid_request_id",
					)
					writeJSON(w, http.StatusBadRequest, map[string]any{
						"error": "request_id must match ^[A-Za-z0-9_-]{1,32}$",
					})
					return
				}
				requestID = *req.RequestID
				suppliedReqID = true
			}

			if req.Reason != nil {
				if !reasonPattern.MatchString(*req.Reason) {
					safety.AuditLog("collect_now_rejected",
						"actor", actorFromCtx(r.Context()),
						"request_id", requestID,
						"error", "invalid_reason",
					)
					writeJSON(w, http.StatusBadRequest, map[string]any{
						"error": "reason must match ^[A-Za-z0-9_-]{1,64}$",
					})
					return
				}
				reason = *req.Reason
				suppliedReason = true
			}

			if req.Targets != nil {
				if len(*req.Targets) == 0 {
					safety.AuditLog("collect_now_rejected",
						"actor", actorFromCtx(r.Context()),
						"request_id", requestID,
						"error", "empty_targets_array",
					)
					writeJSON(w, http.StatusBadRequest, map[string]any{
						"error": "targets must be a non-empty array; omit the field to collect all enabled targets",
					})
					return
				}

				// R100 + #106: configured map reads from the
				// reload-aware accessor for the same reason as
				// allEnabled above.
				configured := make(map[string]config.TargetConfig, len(currentTargets))
				for _, t := range currentTargets {
					configured[t.Name] = t
				}

				type rejection struct {
					Name   string `json:"name"`
					Reason string `json:"reason"`
				}
				seen := make(map[string]bool, len(*req.Targets))
				accepted := make([]string, 0, len(*req.Targets))
				var rejected []rejection

				for _, name := range *req.Targets {
					if seen[name] {
						continue
					}
					seen[name] = true

					cfg, ok := configured[name]
					if !ok {
						rejected = append(rejected, rejection{Name: name, Reason: "unknown_target"})
						continue
					}
					if !cfg.Enabled {
						rejected = append(rejected, rejection{Name: name, Reason: "disabled_target"})
						continue
					}
					accepted = append(accepted, name)
				}

				if len(rejected) > 0 {
					rejectedAttrs := []any{
						"actor", actorFromCtx(r.Context()),
						"requested_targets", *req.Targets,
						"accepted_targets", accepted,
						"rejected_targets", rejected,
					}
					if suppliedReqID {
						rejectedAttrs = append(rejectedAttrs, "request_id", requestID)
					}
					if suppliedReason {
						rejectedAttrs = append(rejectedAttrs, "reason", reason)
					}
					safety.AuditLog("collect_now_rejected", rejectedAttrs...)
					writeJSON(w, http.StatusBadRequest, map[string]any{
						"error":            "one or more targets cannot be collected",
						"accepted_targets": accepted,
						"rejected_targets": rejected,
					})
					return
				}

				targetFilter = accepted
			}

			if req.Force != nil {
				force = *req.Force
			}
		}

		// Generate a ULID when the caller didn't supply a request_id
		// so cycle audit events always carry a correlation id.
		if requestID == "" {
			requestID = newRequestID()
		}

		// Compute the effective response/audit target set.
		responseTargets := targetFilter
		if responseTargets == nil {
			responseTargets = allEnabled
		}

		// Audit: every successful request emits collect_now_requested
		// before queuing. Phase 2 actor invariant: always
		// local_operator until Phase 3 introduces a separate
		// arq_control_plane token (R082).
		requestedAttrs := []any{
			"actor", actorFromCtx(r.Context()),
			"request_id", requestID,
			"requested_targets", requestedTargetsAuditValue(targetFilter, allEnabled),
			"accepted_targets", responseTargets,
		}
		if suppliedReason {
			requestedAttrs = append(requestedAttrs, "reason", reason)
		}
		safety.AuditLog("collect_now_requested", requestedAttrs...)

		queued := deps.Collector.CollectNow(collector.CollectRequest{
			Targets:   targetFilter,
			RequestID: requestID,
			Actor:     actorFromCtx(r.Context()),
			Force:     force,
		})
		if !queued {
			safety.AuditLog("collect_now_dropped",
				"actor", actorFromCtx(r.Context()),
				"request_id", requestID,
				"reason_category", "previous_request_pending",
			)
		}

		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":           "collection triggered",
			"request_id":       requestID,
			"accepted_targets": responseTargets,
		})
	}
}

// exportRejectInvalidTime emits the standard export_completed audit
// + metrics records for an RFC3339 parse failure on since/until and
// writes a 400 response. Codex post-0.3.1 M-003.
func exportRejectInvalidTime(w http.ResponseWriter, deps *Deps, actor string, start time.Time, field, value string) {
	safety.AuditLog("export_completed",
		"actor", actor,
		"status", "failed",
		"duration_ms", time.Since(start).Milliseconds(),
		"size_bytes", 0,
		"error_category", "invalid_time_format",
	)
	deps.Metrics.RecordExport("failed", time.Since(start).Seconds())
	deps.Metrics.RecordExportFailure("invalid_time_format")
	_ = value // value is intentionally not echoed to avoid log-injection / reflection attacks.
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"error": fmt.Sprintf("%s must be RFC3339 (e.g. 2026-01-02T15:04:05Z)", field),
	})
}

// requestedTargetsAuditValue returns either the explicit narrowing
// list or the literal string "all_enabled" so the audit log is
// unambiguous when the caller omitted `targets`. Keeps the audit
// attribute value bounded — R078 forbids unbounded label content.
func requestedTargetsAuditValue(targetFilter, allEnabled []string) any {
	if targetFilter != nil {
		return targetFilter
	}
	return "all_enabled"
}

// newRequestID generates a ULID for the audit-correlation field
// when the caller didn't supply request_id. ULIDs are time-ordered,
// 26 ASCII chars, and naturally satisfy requestIDPattern.
func newRequestID() string {
	return ulid.MustNew(ulid.Now(), rand.Reader).String()
}

func handleExport(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		opts := export.Options{
			Since:      r.URL.Query().Get("since"),
			Until:      r.URL.Query().Get("until"),
			SnapshotID: r.URL.Query().Get("snapshot_id"),
		}
		// `all=true` opts into the pre-R084 full-history scope.
		// Anything other than the literal "true" (case-insensitive)
		// is treated as false — keeps the contract crisp.
		if all := r.URL.Query().Get("all"); all != "" {
			switch strings.ToLower(all) {
			case "true", "1", "yes":
				opts.All = true
			case "false", "0", "no":
				opts.All = false
			default:
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid all=<bool>"})
				return
			}
		}

		actor := actorFromCtx(r.Context())

		if tid := r.URL.Query().Get("target_id"); tid != "" {
			id, err := strconv.ParseInt(tid, 10, 64)
			if err != nil {
				safety.AuditLog("export_requested",
					"actor", actor,
					"source_ip", remoteIP(r),
					"target_id", tid,
					"since", opts.Since,
					"until", opts.Until,
				)
				safety.AuditLog("export_completed",
					"actor", actor,
					"status", "failed",
					"duration_ms", time.Since(start).Milliseconds(),
					"size_bytes", 0,
					"error_category", "invalid_target_id",
				)
				deps.Metrics.RecordExport("failed", time.Since(start).Seconds())
				deps.Metrics.RecordExportFailure("invalid_target_id")
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid target_id"})
				return
			}
			opts.TargetID = id
		}

		// Codex post-0.3.1 M-003: validate since/until as RFC3339 and
		// reject inverted ranges. Without this the strings flow
		// straight into SQLite as text comparisons; a typo silently
		// returns an empty export and the client thinks the time
		// range was simply empty.
		var sinceT, untilT time.Time
		if opts.Since != "" {
			t, err := time.Parse(time.RFC3339, opts.Since)
			if err != nil {
				exportRejectInvalidTime(w, deps, actor, start, "since", opts.Since)
				return
			}
			sinceT = t
		}
		if opts.Until != "" {
			t, err := time.Parse(time.RFC3339, opts.Until)
			if err != nil {
				exportRejectInvalidTime(w, deps, actor, start, "until", opts.Until)
				return
			}
			untilT = t
		}
		if !sinceT.IsZero() && !untilT.IsZero() && sinceT.After(untilT) {
			safety.AuditLog("export_completed",
				"actor", actor,
				"status", "failed",
				"duration_ms", time.Since(start).Milliseconds(),
				"size_bytes", 0,
				"error_category", "invalid_time_range",
			)
			deps.Metrics.RecordExport("failed", time.Since(start).Seconds())
			deps.Metrics.RecordExportFailure("invalid_time_range")
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "since must be <= until"})
			return
		}

		safety.AuditLog("export_requested",
			"actor", actor,
			"source_ip", remoteIP(r),
			"target_id", opts.TargetID,
			"since", opts.Since,
			"until", opts.Until,
		)

		// Buffer the ZIP fully before writing any response headers. If the
		// export fails midway we want to return a 500 with an error body, not
		// a 200 with a truncated/invalid ZIP that the client cannot
		// distinguish from success.
		//
		// Two specific builder errors map to client-side status codes:
		//   * ErrConflictingSelectors → 400 (FC-08; --all + --snapshot-id).
		//   * ErrSnapshotNotFound     → 404 (FC-08; unknown --snapshot-id).
		// Everything else stays as 500 with category=builder_error.
		var buf bytes.Buffer
		if err := deps.Exporter.WriteTo(&buf, opts); err != nil {
			switch {
			case errors.Is(err, export.ErrConflictingSelectors):
				safety.AuditLog("export_completed",
					"actor", actor,
					"status", "failed",
					"duration_ms", time.Since(start).Milliseconds(),
					"size_bytes", 0,
					"error_category", "conflicting_selectors",
				)
				deps.Metrics.RecordExport("failed", time.Since(start).Seconds())
				deps.Metrics.RecordExportFailure("conflicting_selectors")
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			case errors.Is(err, export.ErrSnapshotNotFound):
				safety.AuditLog("export_completed",
					"actor", actor,
					"status", "failed",
					"duration_ms", time.Since(start).Milliseconds(),
					"size_bytes", 0,
					"error_category", "snapshot_not_found",
				)
				deps.Metrics.RecordExport("failed", time.Since(start).Seconds())
				deps.Metrics.RecordExportFailure("snapshot_not_found")
				writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
				return
			}
			slog.Error("export failed", "err", err)
			safety.AuditLog("export_completed",
				"actor", actor,
				"status", "failed",
				"duration_ms", time.Since(start).Milliseconds(),
				"size_bytes", 0,
				"error_category", "builder_error",
			)
			deps.Metrics.RecordExport("failed", time.Since(start).Seconds())
			deps.Metrics.RecordExportFailure("builder_error")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "export failed"})
			return
		}

		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=arq-export-%s.zip",
			time.Now().UTC().Format("20060102-150405")))
		w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
		if _, err := w.Write(buf.Bytes()); err != nil {
			slog.Error("export write failed", "err", err)
			safety.AuditLog("export_completed",
				"actor", actor,
				"status", "failed",
				"duration_ms", time.Since(start).Milliseconds(),
				"size_bytes", buf.Len(),
				"error_category", "write_error",
			)
			deps.Metrics.RecordExport("failed", time.Since(start).Seconds())
			deps.Metrics.RecordExportFailure("write_error")
			return
		}
		safety.AuditLog("export_completed",
			"actor", actor,
			"status", "success",
			"duration_ms", time.Since(start).Milliseconds(),
			"size_bytes", buf.Len(),
		)
		deps.Metrics.RecordExport("success", time.Since(start).Seconds())
	}
}

// --- Middleware ---

// tokenRateLimiter tracks invalid bearer token attempts per IP.
type tokenRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*tokenAttempt
}

type tokenAttempt struct {
	failures    int
	lastAttempt time.Time
}

func newTokenRateLimiter() *tokenRateLimiter {
	return &tokenRateLimiter{attempts: make(map[string]*tokenAttempt)}
}

const (
	tokenMaxFailures   = 5
	tokenLockoutWindow = 5 * time.Minute
)

func (l *tokenRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	a, ok := l.attempts[ip]
	if !ok {
		return true
	}
	if time.Since(a.lastAttempt) > tokenLockoutWindow {
		delete(l.attempts, ip)
		return true
	}
	return a.failures < tokenMaxFailures
}

func (l *tokenRateLimiter) recordFailure(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	a, ok := l.attempts[ip]
	if !ok {
		a = &tokenAttempt{}
		l.attempts[ip] = a
	}
	a.failures++
	a.lastAttempt = now

	// Opportunistic prune: drop entries older than the lockout window.
	// Bounds map growth in long-lived processes where attackers come
	// from many short-lived IPs that never retry.
	cutoff := now.Add(-tokenLockoutWindow)
	for otherIP, other := range l.attempts {
		if other.lastAttempt.Before(cutoff) {
			delete(l.attempts, otherIP)
		}
	}
}

// recordSuccess clears any prior failures for the IP. Called when a
// request authenticates successfully so a near-miss IP does not stay
// near the lockout threshold indefinitely.
func (l *tokenRateLimiter) recordSuccess(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}

// recoveryMiddleware catches panics and returns 500 with a JSON
// body. Codex post-0.3.1 L-002: previously used http.Error which
// sets Content-Type: text/plain even when the body content is JSON,
// breaking clients that branch on the response Content-Type.
func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", "panic", rec, "path", r.URL.Path)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs each request.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Generate request ID. crypto/rand.Read on Go 1.24+ can
		// only fail under exotic conditions (entropy source
		// unavailable); a partial-read here yields a shorter
		// hex-encoded ID, which is still acceptable as a trace
		// correlator.
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		reqID := hex.EncodeToString(b)
		w.Header().Set("X-Request-ID", reqID)

		wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.status,
			"duration", time.Since(start).String(),
			"request_id", reqID,
		)
	})
}

// Audit `actor` values (R078 / R083). Stable wire constants.
const (
	ActorLocalOperator   = "local_operator"
	ActorArqControlPlane = "arq_control_plane"
)

// ctxKey is an unexported type so external packages can never set or
// read context values for the api package's keys.
type ctxKey int

const ctxKeyActor ctxKey = iota

// actorFromCtx returns the audit `actor` value attached to the
// request context by tokenAuthMiddleware. Defaults to local_operator
// when no value is present (e.g. test code that bypasses the
// middleware) — never returns the more privileged
// arq_control_plane value by default.
func actorFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyActor).(string); ok && v != "" {
		return v
	}
	return ActorLocalOperator
}

// tokenAuthMiddleware checks for a valid Bearer token on all requests
// except /health.
//
// R083: in addition to the local API token, the supplied bearer is
// compared (in constant time) to the Arq control-plane token when
// `controlPlaneTokenFn` is non-nil and returns a non-empty value.
// The matched token determines the audit `actor` attached to the
// request context — never inferred from request shape. The
// control-plane token closure is invoked per request so file-based
// rotation takes effect on the next call without a restart.
//
// In standalone mode, controlPlaneTokenFn is either nil or returns
// empty; only the local API token is consulted, and a request that
// would have matched the control-plane token simply gets a 401.
func tokenAuthMiddleware(apiToken string, controlPlaneTokenFn func() string, limiter *tokenRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for health endpoint (loopback health checks).
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}

			ip := remoteIP(r)

			if !limiter.allow(ip) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{"error": "too many invalid token attempts"})
				return
			}

			auth := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(auth, prefix) {
				limiter.recordFailure(ip)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "missing or invalid Authorization header"})
				return
			}

			provided := []byte(auth[len(prefix):])

			// Compare against api.token first.
			actor := ""
			if subtle.ConstantTimeCompare(provided, []byte(apiToken)) == 1 {
				actor = ActorLocalOperator
			} else if controlPlaneTokenFn != nil {
				// Fall through to the control-plane token. The closure
				// is the per-request file/env re-read.
				cpt := controlPlaneTokenFn()
				if cpt != "" && subtle.ConstantTimeCompare(provided, []byte(cpt)) == 1 {
					actor = ActorArqControlPlane
				}
			}

			if actor == "" {
				limiter.recordFailure(ip)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid API token"})
				return
			}

			// Successful authentication clears any prior failure count for this IP.
			limiter.recordSuccess(ip)

			// Propagate the resolved actor into the request context so
			// downstream handlers can attach it to audit events without
			// re-running the auth check.
			r = r.WithContext(context.WithValue(r.Context(), ctxKeyActor, actor))
			next.ServeHTTP(w, r)
		})
	}
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
