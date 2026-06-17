package metrics

// FailureReasonCode is the closed enum of operator-facing failure
// categories (#138 / docs/observability/operational-readiness.md).
// Every collection or startup failure resolves to exactly one of
// these values; new failure modes MUST be added here via a
// deliberate spec amendment rather than emitted as ad-hoc strings.
//
// The wire strings (the const values) are stable contract — they
// appear in `/status` JSON, in `signalsctl doctor --json`, in
// Prometheus metric labels, and in support-bundle templates.
type FailureReasonCode string

const (
	// ReasonTargetUnreachable: TCP-level connection refused / timeout.
	ReasonTargetUnreachable FailureReasonCode = "target_unreachable"

	// ReasonTargetTLSInvalid: cert verification failed under
	// `verify-full` or `verify-ca`.
	ReasonTargetTLSInvalid FailureReasonCode = "target_tls_invalid"

	// ReasonAuthFailed: Postgres rejected credentials.
	ReasonAuthFailed FailureReasonCode = "auth_failed"

	// ReasonRoleInsufficient: credentials accepted but role lacks
	// permission for required catalogs (typically `pg_monitor`).
	ReasonRoleInsufficient FailureReasonCode = "role_insufficient"

	// ReasonCollectorPGVersionUnsupported: target PG major outside
	// the SupportedMajors window.
	ReasonCollectorPGVersionUnsupported FailureReasonCode = "collector_pg_version_unsupported"

	// ReasonCollectorExtensionMissing: required extension absent.
	ReasonCollectorExtensionMissing FailureReasonCode = "collector_extension_missing"

	// ReasonCollectorQueryTimeout: collector query exceeded the
	// configured `query_timeout`.
	ReasonCollectorQueryTimeout FailureReasonCode = "collector_query_timeout"

	// ReasonCollectorCircuitOpen: per-target circuit-breaker open
	// after the configured failure-threshold cooldown.
	ReasonCollectorCircuitOpen FailureReasonCode = "collector_circuit_open"

	// ReasonStorageWriteFailed: SQLite write failed (disk full,
	// permission, FS unmounted).
	ReasonStorageWriteFailed FailureReasonCode = "storage_write_failed"

	// ReasonStorageBusy: SQLite returned SQLITE_BUSY after the
	// configured retry budget.
	ReasonStorageBusy FailureReasonCode = "storage_busy"

	// ReasonConfigInvalid: ValidateStrict returned a hard error.
	ReasonConfigInvalid FailureReasonCode = "config_invalid"

	// ReasonUnknown: catch-all bucket. Firing this signals an
	// incomplete taxonomy; the operator should file a bug with the
	// `signalsctl doctor --json` output and the failing log line so
	// the closed list grows to cover the case.
	ReasonUnknown FailureReasonCode = "unknown"
)

// AllFailureReasonCodes is the closed list used by validators and
// drift-gate tests. Tests assert this list matches the wire-string
// taxonomy documented in
// `docs/observability/operational-readiness.md`.
var AllFailureReasonCodes = []FailureReasonCode{
	ReasonTargetUnreachable,
	ReasonTargetTLSInvalid,
	ReasonAuthFailed,
	ReasonRoleInsufficient,
	ReasonCollectorPGVersionUnsupported,
	ReasonCollectorExtensionMissing,
	ReasonCollectorQueryTimeout,
	ReasonCollectorCircuitOpen,
	ReasonStorageWriteFailed,
	ReasonStorageBusy,
	ReasonConfigInvalid,
	ReasonUnknown,
}

// IsValid reports whether c is a recognised closed-enum value.
func (c FailureReasonCode) IsValid() bool {
	for _, v := range AllFailureReasonCodes {
		if c == v {
			return true
		}
	}
	return false
}
