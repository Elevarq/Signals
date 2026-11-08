// Settings + HBA credential post-processors (#708 / F01, INV-SIGNALS-07).
//
// Two general-catalog collectors carry operator credentials that the
// sensitivity opt-out does NOT exclude and no existing scrubber touched, so
// cleartext survived into the SQLite store, the production ZIP export, and any
// configured S3 output:
//
//   - pg_settings_v1 returns setting/reset_val/boot_val for EVERY GUC,
//     including `primary_conninfo` (a libpq conninfo with `password=`) and the
//     archive/restore *_command GUCs (arbitrary operator shell that can embed
//     credentials). The dedicated WAL collector carefully emits only
//     command-PRESENCE booleans; the general settings collector defeated that.
//   - pg_hba_file_rules_v1 preserves `options` such as `ldapbindpasswd` /
//     `radiussecret` when the optional HBA grants are installed.
//
// These post-processors run after queryToMaps and BEFORE NDJSON encoding, in
// place, so the cleartext never reaches the snapshot writer. Both are no-ops
// for every other collector.
//
// Spec: specifications/collectors/pg_settings_v1.md (REDACT-R001),
//       specifications/collectors/pg_hba_file_rules_v1.md (REDACT-R001).

package collector

import "github.com/elevarq/signals/internal/pgqueries"

// credentialBearingGUCs is the closed set of pg_settings `name` values whose
// setting/reset_val/boot_val must never reach a persisted snapshot. Keep it
// small + append-only: add a GUC only when it is confirmed to carry a
// credential or arbitrary operator command.
var credentialBearingGUCs = map[string]bool{
	"primary_conninfo":        true, // libpq conninfo — carries password=
	"archive_command":         true, // arbitrary shell — can embed creds
	"restore_command":         true,
	"archive_cleanup_command": true,
	"recovery_end_command":    true,
	"ssl_passphrase_command":  true, // runs to obtain the TLS key passphrase
}

const settingsRedactedSentinel = "<redacted>"

// redactSettingsSecretsIfNeeded replaces the value columns
// (setting/reset_val/boot_val) of a credential-bearing GUC with "<redacted>".
// An empty value stays empty so "set vs unset" is still distinguishable — the
// diagnostic signal the WAL collector preserves — without leaking the content.
// No-op for any collector other than pg_settings_v1.
func redactSettingsSecretsIfNeeded(queryID string, rows []map[string]any) {
	if queryID != "pg_settings_v1" {
		return
	}
	for _, row := range rows {
		name, _ := row["name"].(string)
		if !credentialBearingGUCs[name] {
			continue
		}
		for _, col := range []string{"setting", "reset_val", "boot_val"} {
			if v, ok := row[col].(string); ok && v != "" {
				row[col] = settingsRedactedSentinel
			}
		}
	}
}

// redactHBAOptionsIfNeeded redacts sensitive entries of the pg_hba_file_rules_v1
// `options` text[] (e.g. ldapbindpasswd, radiussecret) in place while preserving
// the array shape (RedactOptionsArray — key-name based). No-op for any other
// collector; defensive on an absent / unexpectedly-typed options column.
func redactHBAOptionsIfNeeded(queryID string, rows []map[string]any) {
	if queryID != "pg_hba_file_rules_v1" {
		return
	}
	for _, row := range rows {
		raw, present := row["options"]
		if !present || raw == nil {
			continue
		}
		arr, ok := raw.([]any)
		if !ok {
			continue
		}
		opts := make([]string, 0, len(arr))
		for _, v := range arr {
			if s, ok := v.(string); ok {
				opts = append(opts, s)
			}
		}
		row["options"] = pgqueries.RedactOptionsArray(opts)
	}
}
