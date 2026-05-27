package api

import (
	"fmt"
	"net/http"

	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/config"
	"github.com/elevarq/arq-signals/internal/safety"
)

// handleConfigReload implements POST /reload (R100). Re-reads the
// config from disk via the supplied path, validates it, and (on
// success) swaps the collector's target list in place. Other
// runtime fields are out of v1 scope — see collector.Reload.
//
// configPath is captured at server construction so the handler
// has a stable source even after multiple reloads (the file path
// itself doesn't change during the daemon's lifetime).
func handleConfigReload(deps *Deps, configPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromCtx(r.Context())
		safety.AuditLog("config_reload_requested", "actor", actor, "path", configPath)

		newCfg, err := config.Load(configPath)
		if err != nil {
			// Load errors can quote arbitrary config-file content
			// (YAML-parse errors echo offending lines). Redact via
			// RedactDSN to scrub any postgres:// strings and cap
			// the message length so a runaway parser can't push
			// large payloads into the audit stream.
			redacted := redactReloadErr(err)
			safety.AuditLog("config_reload_rejected",
				"actor", actor, "reason", "load_failed", "error", redacted)
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": fmt.Sprintf("load %s: %s", configPath, redacted),
			})
			return
		}
		if _, err := config.ValidateStrict(newCfg); err != nil {
			redacted := redactReloadErr(err)
			safety.AuditLog("config_reload_rejected",
				"actor", actor, "reason", "validate_failed", "error", redacted)
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": fmt.Sprintf("validate %s: %s", configPath, redacted),
			})
			return
		}

		deps.Collector.Reload(newCfg.Targets)

		safety.AuditLog("config_reload_applied",
			"actor", actor, "target_count", len(newCfg.Targets))
		writeJSON(w, http.StatusOK, map[string]any{
			"reloaded":     true,
			"target_count": len(newCfg.Targets),
		})
	}
}

// reloadErrMaxLen caps the size of an error message emitted into
// audit / HTTP body. YAML-parse errors can quote multi-KB chunks
// of offending input; truncating bounds the audit payload to a
// reasonable size without losing the first useful frame.
const reloadErrMaxLen = 512

// redactReloadErr scrubs a load / validate error before it lands in
// the audit stream or HTTP body. Two defenses:
//
//  1. collector.RedactDSN replaces any embedded postgres:// URL
//     with the redacted form, in case a YAML-parse error echoed a
//     config block that contained a DSN.
//  2. Cap at reloadErrMaxLen so a pathological config file (huge
//     line, runaway nested structure) cannot push large payloads
//     into the audit stream.
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
