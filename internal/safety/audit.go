package safety

import (
	"log/slog"
	"strings"
)

// AuditEvent is the slog key used for every audit-event log line. SIEMs and
// log shippers can filter on it to extract the audit trail.
const AuditEvent = "audit_event"

// auditAttrDenyPrefixes lists attribute key prefixes that must never appear
// on an audit event. Filtering happens centrally so individual call sites
// cannot accidentally widen the contract by adding a new key with a
// sensitive name.
var auditAttrDenyPrefixes = []string{
	"password",
	"secret",
	"api_token",
	"token",
	"dsn",
	"connection_string",
	"payload",
	"query_result",
}

// auditAttrAllowList is a small, hand-curated set of audit-attribute
// keys that contain a denylist substring (e.g. "token") but only ever
// carry boolean / metadata values about the configured value, never
// the secret value itself. Without this allow-list the substring
// match in auditAttrDenyPrefixes would silently filter out useful
// diagnostics like `arq_control_plane_token_configured=true` from
// the R083 startup `mode_configured` audit event.
//
// Entries here must be reviewed: each key may carry only metadata
// (boolean, count, fingerprint), never the underlying secret value.
var auditAttrAllowList = map[string]bool{
	"arq_control_plane_token_configured": true,
}

// AuditLog emits a structured info-level audit event. The first attr pair
// must be the event name as a key/value, e.g.
//
//	safety.AuditLog("config_validated", "status", "ok", "warnings", 0)
//
// Attributes whose key matches a denylist prefix are silently dropped to
// guarantee R078's "no secrets in audit events" invariant even if a future
// caller forgets the contract.
func AuditLog(event string, attrs ...any) {
	filtered := filterAuditAttrs(attrs)
	args := make([]any, 0, len(filtered)+2)
	args = append(args, AuditEvent, event)
	args = append(args, filtered...)
	slog.Info("audit", args...)
}

func filterAuditAttrs(attrs []any) []any {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]any, 0, len(attrs))
	for i := 0; i+1 < len(attrs); i += 2 {
		key, ok := attrs[i].(string)
		if !ok {
			// Non-string keys are not part of the audit contract; drop.
			continue
		}
		if isDeniedAuditKey(key) {
			continue
		}
		out = append(out, attrs[i], attrs[i+1])
	}
	return out
}

func isDeniedAuditKey(key string) bool {
	lower := strings.ToLower(key)
	if auditAttrAllowList[lower] {
		return false
	}
	for _, p := range auditAttrDenyPrefixes {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}
