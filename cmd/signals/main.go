package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/elevarq/arq-signals/internal/api"
	"github.com/elevarq/arq-signals/internal/circuit"
	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/config"
	"github.com/elevarq/arq-signals/internal/db"
	"github.com/elevarq/arq-signals/internal/export"
	"github.com/elevarq/arq-signals/internal/metrics"
	"github.com/elevarq/arq-signals/internal/pgqueries"
	"github.com/elevarq/arq-signals/internal/safety"
)

// redactReloadErr mirrors the helper in internal/api/reload.go so
// the SIGHUP path applies identical redaction posture to load /
// validate errors (issue #87). Local copy to avoid pulling internal
// api into the daemon main's import graph.
const reloadErrMaxLen = 512

func redactReloadErr(err error) string {
	if err == nil {
		return ""
	}
	msg := collector.RedactDSN(err.Error())
	if len(msg) > reloadErrMaxLen {
		msg = msg[:reloadErrMaxLen] + "...[truncated]"
	}
	return msg
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "signals: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to config file")
	flag.Parse()

	// Load configuration.
	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Setup logging before validation so warnings reach the configured sink.
	safety.SetupLogging(cfg.Signals.LogLevel, cfg.Signals.LogJSON)

	// Strict configuration validation (R076). Hard errors abort startup;
	// warnings are logged and the daemon continues.
	warnings, err := config.ValidateStrict(cfg)
	for _, w := range warnings {
		slog.Warn("config warning", "msg", w)
	}
	if err != nil {
		safety.AuditLog("config_validated",
			"status", "error",
			"warnings", len(warnings),
			"hard_errors", 1,
		)
		return err
	}
	safety.AuditLog("config_validated",
		"status", "ok",
		"warnings", len(warnings),
		"hard_errors", 0,
	)

	// Enforce Postgres TLS policy.
	if err := config.ValidateProdTLS(cfg); err != nil {
		safety.AuditLog("config_validated",
			"status", "error",
			"phase", "tls_policy",
		)
		return fmt.Errorf("TLS policy: %w", err)
	}

	// Audit posture: high-sensitivity gate state and target enable/disable
	// counts. Per R078 these record *what* ran, not *which credentials* —
	// only counts and booleans, no host/user/password leakage.
	enabled, disabled := 0, 0
	for _, t := range cfg.Targets {
		if t.Enabled {
			enabled++
		} else {
			disabled++
		}
	}
	safety.AuditLog("high_sensitivity_collectors",
		"enabled", cfg.Signals.HighSensitivityCollectorsEnabled,
	)
	safety.AuditLog("targets_loaded",
		"enabled", enabled,
		"disabled", disabled,
	)

	slog.Info("signals starting",
		"version", safety.Version,
		"commit", safety.Commit,
		"build_date", safety.BuildDate,
	)

	// Open database.
	store, err := db.Open(cfg.Database.Path, cfg.Database.WAL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = store.Close() }()

	// Run migrations.
	if err := store.Migrate(); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// R109: reconcile persisted target enable-state against config at
	// startup so a target disabled or removed while the daemon was down
	// stops appearing in the default export and /status. Reload does the
	// same on SIGHUP / POST /reload.
	{
		enabledNames := make([]string, 0, len(cfg.Targets))
		for _, t := range cfg.Targets {
			if t.Enabled {
				enabledNames = append(enabledNames, t.Name)
			}
		}
		if err := store.ReconcileEnabledTargets(enabledNames); err != nil {
			return fmt.Errorf("reconcile enabled targets: %w", err)
		}
	}

	// Ensure instance ID.
	instanceID, err := store.EnsureInstanceID()
	if err != nil {
		return fmt.Errorf("instance id: %w", err)
	}
	slog.Info("instance", "id", instanceID)

	// Create context with signal handling.
	ctx, cancel := safety.SignalContext(context.Background())
	defer cancel()

	// Sync query catalog.
	syncQueryCatalog(store)

	// Initialize the optional Prometheus registry. nil when
	// signals.metrics_enabled is false; passing nil downstream is the
	// no-op contract for every recorder.
	var metricsReg *metrics.Registry
	if cfg.Signals.MetricsEnabled {
		metricsReg = metrics.New()
		metricsReg.SetHighSensitivityEnabled(cfg.Signals.HighSensitivityCollectorsEnabled)
	}

	// R097: per-target circuit-breaker manager. Constructed up front
	// so the collector and the API handlers share the same state
	// (both opaque-ly access it via collector.Circuit()).
	circuitMgr := circuit.NewManager(cfg.Signals.Circuit.FailThreshold, cfg.Signals.Circuit.OpenCooldown)

	// Initialize collector (no license gate, no stats engine).
	coll := collector.New(store, cfg.Targets, cfg.Signals.PollInterval, cfg.Signals.RetentionDays,
		collector.WithMaxConcurrentTargets(cfg.Signals.MaxConcurrentTargets),
		collector.WithTargetTimeout(cfg.Signals.TargetTimeout),
		collector.WithQueryTimeout(cfg.Signals.QueryTimeout),
		collector.WithMinSnapshotInterval(cfg.Signals.MinSnapshotInterval),
		collector.WithAllowUnsafeRole(cfg.AllowUnsafeRole),
		collector.WithHighSensitivityCollectors(cfg.Signals.HighSensitivityCollectorsEnabled),
		collector.WithCollectArrayRangeHistograms(cfg.Signals.CollectArrayRangeHistograms),
		collector.WithMetrics(metricsReg),
		collector.WithCircuitManager(circuitMgr),
		collector.WithRetention(cfg.Signals.Retention),
	)

	if cfg.AllowUnsafeRole {
		slog.Warn("UNSAFE MODE ENABLED: collection will proceed with unsafe role attributes — this is NOT recommended for production")
	}

	// Initialize exporter (no license gating).
	exporter := export.NewBuilder(store, instanceID)
	exporter.SetHighSensitivityCollectorsEnabled(cfg.Signals.HighSensitivityCollectorsEnabled)
	exporter.SetExportPerCollectorFiles(cfg.Signals.ExportPerCollectorFiles)
	if cfg.AllowUnsafeRole {
		// Pass a function that returns the actual bypassed checks at export time,
		// so metadata reflects the specific role attributes that were bypassed.
		exporter.SetUnsafeMode(func() []string {
			checks := coll.GetBypassedChecks()
			if len(checks) == 0 {
				return []string{"SIGNALS_ALLOW_UNSAFE_ROLE=true (no role checks bypassed yet)"}
			}
			return checks
		})
	}

	// Start collector in background.
	go coll.Run(ctx)

	// Generate API token if not set.
	if cfg.API.APIToken == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return fmt.Errorf("generate API token: %w", err)
		}
		cfg.API.APIToken = hex.EncodeToString(b)
		fp := fmt.Sprintf("%x", sha256.Sum256([]byte(cfg.API.APIToken)))[:12]
		slog.Info("signals API token generated (auto)", "fingerprint", fp)
	}

	// R083: Mode B opt-in. Resolve the control-plane token at
	// startup so cross-token rules (length floor, distinctness from
	// api.token) are validated before serving any traffic. Per-
	// request rotation is handled by ControlPlaneTokenFn below.
	var signalsControlPlaneTokenFn func() string
	controlPlaneTokenConfigured := false
	if cfg.Signals.Mode == config.ModeManaged {
		startupToken, err := config.ResolveControlPlaneToken(cfg.Signals)
		if err != nil {
			return fmt.Errorf("resolve control_plane_token: %w", err)
		}
		if err := config.ValidateModeBTokens(cfg, cfg.API.APIToken, startupToken); err != nil {
			return fmt.Errorf("R083 cross-token validation: %w", err)
		}
		controlPlaneTokenConfigured = true
		signalsControlPlaneTokenFn = func() string {
			// Re-read the source on every authentication attempt so
			// rotating the file's contents takes effect on the next
			// request without restarting the daemon. A read error
			// here returns "" — the bearer comparison fails closed
			// and the request gets a 401 like any other unknown
			// token.
			tok, err := config.ResolveControlPlaneToken(cfg.Signals)
			if err != nil {
				slog.Warn("control_plane_token resolve failed", "err", err)
				return ""
			}
			if tok == "" {
				// Empty file post-rotation: log so an operator can
				// tell their rotation broke instead of silently
				// turning into 401s for the control plane.
				slog.Warn("control_plane_token resolved to empty value — Mode B authentication is degraded until the source is restored")
			}
			return tok
		}
	}

	// Mode-configured startup audit event (R083). Token VALUE never
	// logged — only the configured/not-configured boolean.
	safety.AuditLog("mode_configured",
		"mode", cfg.Signals.Mode,
		"control_plane_token_configured", controlPlaneTokenConfigured,
	)

	// Start HTTP API server.
	metricsPath := cfg.Signals.MetricsPath
	if metricsReg != nil {
		slog.Info("metrics endpoint enabled", "path", metricsPath)
	}
	deps := &api.Deps{
		DB:                  store,
		Metrics:             metricsReg,
		MetricsPath:         metricsPath,
		Collector:           coll,
		Exporter:            exporter,
		Targets:             cfg.Targets,
		ConfigPath:          *configPath, // R100: needed for POST /reload + SIGHUP handler.
		ControlPlaneTokenFn: signalsControlPlaneTokenFn,
		TLSCertFile:         cfg.API.TLSCertFile, // R113: daemon-terminated TLS (both-or-neither, validated).
		TLSKeyFile:          cfg.API.TLSKeyFile,
	}
	srv := api.NewServer(cfg.API.ListenAddr, cfg.API.ReadTimeout, cfg.API.WriteTimeout, cfg.API.APIToken, deps)

	// Run server in background.
	errCh := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	_ = store.InsertEvent("signals_started", fmt.Sprintf("version=%s instance=%s", safety.Version, instanceID))

	// R100: SIGHUP handler triggers a config reload (re-read +
	// validate + collector.Reload). Runs alongside the existing
	// SIGINT/SIGTERM shutdown handler from safety.SignalContext.
	// Runs in a goroutine for the daemon's full lifetime.
	hupCh := make(chan os.Signal, 1)
	signal.Notify(hupCh, syscall.SIGHUP)
	go func() {
		for {
			select {
			case <-ctx.Done():
				signal.Stop(hupCh)
				return
			case <-hupCh:
				safety.AuditLog("config_reload_requested", "actor", "signal_handler", "trigger", "SIGHUP")
				newCfg, err := config.Load(*configPath)
				if err != nil {
					// Same redaction posture as the HTTP /reload
					// handler — load errors can echo config-file
					// content (issue #87).
					redacted := redactReloadErr(err)
					slog.Error("SIGHUP reload: load failed", "err", redacted)
					safety.AuditLog("config_reload_rejected",
						"actor", "signal_handler", "reason", "load_failed", "error", redacted)
					continue
				}
				if _, err := config.ValidateStrict(newCfg); err != nil {
					redacted := redactReloadErr(err)
					slog.Error("SIGHUP reload: validation failed", "err", redacted)
					safety.AuditLog("config_reload_rejected",
						"actor", "signal_handler", "reason", "validate_failed", "error", redacted)
					continue
				}
				// R109/#16: propagate reconcile failures rather than
				// leaving DB enablement stale. Reload aborts before any
				// in-memory mutation on failure, matching the
				// load/validate-rejected pattern above.
				if err := coll.Reload(newCfg.Targets); err != nil {
					redacted := redactReloadErr(err)
					slog.Error("SIGHUP reload: reconcile failed", "err", redacted)
					safety.AuditLog("config_reload_rejected",
						"actor", "signal_handler", "reason", "reconcile_failed", "error", redacted)
					continue
				}
				slog.Info("SIGHUP reload applied", "target_count", len(newCfg.Targets))
				safety.AuditLog("config_reload_applied",
					"actor", "signal_handler", "target_count", len(newCfg.Targets))
			}
		}
	}()

	// Wait for shutdown signal or server error.
	select {
	case <-ctx.Done():
		slog.Info("shutting down...")
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	// Graceful shutdown.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("API server shutdown error", "err", err)
	}

	_ = store.InsertEvent("signals_stopped", "graceful shutdown")
	slog.Info("signals stopped")
	return nil
}

func syncQueryCatalog(store *db.DB) {
	now := time.Now().UTC().Format(time.RFC3339)
	for _, q := range pgqueries.All() {
		if err := store.UpsertQueryCatalog(db.QueryCatalogRow{
			QueryID:        q.ID,
			Category:       q.Category,
			ResultKind:     string(q.ResultKind),
			RetentionClass: string(q.RetentionClass),
			RegisteredAt:   now,
		}); err != nil {
			slog.Warn("failed to upsert query catalog", "query", q.ID, "err", err)
		}
	}
	slog.Info("query catalog synced", "count", len(pgqueries.All()))
}
