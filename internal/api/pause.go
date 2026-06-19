package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/elevarq/signals/internal/circuit"
	"github.com/elevarq/signals/internal/safety"
)

// pauseMaxBodyBytes caps the /collect/pause and /collect/resume
// request bodies. Generous given the only fields are target + reason,
// matches the existing /collect/now ceiling pattern.
const pauseMaxBodyBytes = 4 * 1024

// pauseRequest is the wire shape for POST /collect/pause and
// /collect/resume. Both fields optional; an empty target means
// "every enabled target in config" for pause, or rejected (FC-CIRC-02)
// for resume.
type pauseRequest struct {
	Target string `json:"target,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// handleCollectPause implements POST /collect/pause (R097). Sets the
// target's circuit state to `paused` with an operator-supplied
// reason. Empty target → pause every enabled target.
func handleCollectPause(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, ok := readPauseRequest(w, r)
		if !ok {
			return
		}
		if len(req.Reason) > circuit.MaxReasonLength {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": fmt.Sprintf("reason exceeds %d chars", circuit.MaxReasonLength),
			})
			return
		}

		actor := actorFromCtx(r.Context())
		targets := resolvePauseTargets(deps, req.Target)
		if req.Target != "" && len(targets) == 0 {
			// Pause is permissive for unknown targets (spec): echo
			// the request back as paused so the caller sees a
			// definite outcome. Audit-log the no-op separately
			// (issue #95) so an auditor reading the trail sees the
			// accepted-but-not-applied operator action.
			safety.AuditLog("circuit_pause_noop",
				"target", req.Target,
				"actor", actor,
				"reason_category", "unknown_target")
			writeJSON(w, http.StatusOK, map[string]any{"paused": []string{req.Target}})
			return
		}

		paused := make([]string, 0, len(targets))
		for _, name := range targets {
			if err := deps.Collector.PauseTarget(name, req.Reason, actor); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			paused = append(paused, name)
		}
		writeJSON(w, http.StatusOK, map[string]any{"paused": paused})
	}
}

// handleCollectResume implements POST /collect/resume (R097). Clears
// the target's `paused` state, returning it to `closed`. An unknown
// target name is rejected with 400 (FC-CIRC-02 — resume must
// reference a known target).
func handleCollectResume(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, ok := readPauseRequest(w, r)
		if !ok {
			return
		}
		actor := actorFromCtx(r.Context())

		if req.Target == "" {
			// Resume-all is symmetrical with pause-all.
			targets := enabledTargetNames(deps)
			for _, name := range targets {
				deps.Collector.ResumeTarget(name, actor)
			}
			writeJSON(w, http.StatusOK, map[string]any{"resumed": targets})
			return
		}

		// Resume with explicit target — must match a configured
		// target (FC-CIRC-02).
		if !isConfiguredTarget(deps, req.Target) {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":     fmt.Sprintf("unknown target %q", req.Target),
				"available": enabledTargetNames(deps),
			})
			return
		}
		deps.Collector.ResumeTarget(req.Target, actor)
		writeJSON(w, http.StatusOK, map[string]any{"resumed": []string{req.Target}})
	}
}

func readPauseRequest(w http.ResponseWriter, r *http.Request) (pauseRequest, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, pauseMaxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			safety.AuditLog("circuit_request_rejected",
				"actor", actorFromCtx(r.Context()),
				"error", "body_too_large")
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
				"error": fmt.Sprintf("request body exceeds %d bytes", pauseMaxBodyBytes),
			})
			return pauseRequest{}, false
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "could not read request body"})
		return pauseRequest{}, false
	}
	var req pauseRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
			return pauseRequest{}, false
		}
	}
	return req, true
}

func resolvePauseTargets(deps *Deps, requested string) []string {
	if requested != "" {
		if isConfiguredTarget(deps, requested) {
			return []string{requested}
		}
		// Permissive: unknown targets are a no-op (FC-CIRC-02 only
		// applies to resume).
		return nil
	}
	return enabledTargetNames(deps)
}

func enabledTargetNames(deps *Deps) []string {
	// Read from the collector so R100 reload is honoured. The
	// deps.Targets slice is the construction-time snapshot; after
	// a reload it's stale.
	//
	// Initialise as a non-nil empty slice so empty-fleet daemons
	// serialise to JSON `[]` rather than `null` (issue #94).
	out := []string{}
	for _, t := range deps.Collector.Targets() {
		if t.Enabled {
			out = append(out, t.Name)
		}
	}
	return out
}

func isConfiguredTarget(deps *Deps, name string) bool {
	for _, t := range deps.Collector.Targets() {
		if t.Name == name {
			return true
		}
	}
	return false
}
