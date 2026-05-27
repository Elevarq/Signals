// FDW option redaction.
//
// Foreign-server / user-mapping / foreign-table options can carry
// connection credentials, tokens, and private keys. Per the FDW
// collector specs (specifications/collectors/fdw_*_v1.md), the
// collectors run their option arrays through this redactor before
// emitting them so downstream consumers (snapshot ZIP, analyzer,
// support bundles) never see the cleartext secret.
//
// Redaction is **key-name based** — we don't try to detect "secret-
// shaped" values. If a key matches a sensitive pattern, the value
// is replaced with the literal "<redacted>". Matching is
// case-insensitive and substring-based so common variants
// (`PASSWORD`, `Password`, `db_password`, `proxy-password`) are all
// caught.
//
// User mappings are the most sensitive class — they typically
// contain `user` + `password` for the remote server. The user-
// mapping collector therefore redacts every option key whose name
// matches ANY of the patterns below; foreign-server options use the
// same set so a misconfigured operator who put a password in the
// SERVER OPTIONS by mistake doesn't leak it.

package pgqueries

import (
	"sort"
	"strings"
)

// fdwRedactPatterns is the closed list of substring patterns
// (case-insensitive) that mark an option key as sensitive. Keep
// this list small and append-only — broadening it does not help
// (legitimate keys get smeared); narrowing it could leak secrets.
//
// Maintenance: add a new pattern only when a real-world FDW driver
// is observed using a new keyword for credentials.
var fdwRedactPatterns = []string{
	"password",
	"pass", // matches `pwd`-style aliases like `dbpass`
	"secret",
	"token",
	"key", // catches `private_key`, `api_key`, `access_key`, `sslkey` (sslkey is a *file path*, but file paths can leak hostnames; redact)
	"credential",
	"connstr",
	"connection_string",
	"conn_string",
	"sslcert",
	"sslrootcert",
	"service", // libpq `service` resolves to a `.pg_service.conf` entry — the resolved value can carry creds
	"auth",
	"bearer",
	// `user` / `username` are intentionally NOT redacted: the local-
	// user side of a user mapping is needed for analyzer reasoning
	// (which local role uses which remote server), and the remote
	// `user` option name is a routine identifier, not a secret.
	// If a future option uses `user` as a secret-bearing alias, add
	// the specific full key (e.g. `user_token`) here rather than
	// shadowing the generic `user`.
}

const fdwRedactedSentinel = "<redacted>"

// RedactFDWOptions accepts a `text[]` array as Postgres returns it
// — `{key=value, key2=value2}` — and returns a deterministic
// `map[string]string` with sensitive values replaced by
// `"<redacted>"`. The output is stable: callers may marshal it
// directly to JSON without worrying about map iteration order
// (Go's encoding/json sorts string keys; tests verify the same
// pattern via SortedFDWOptionKeys for explicit pinning).
//
// Inputs:
//   - opts: each entry is in libpq `key=value` form. Empty entries
//     and entries without `=` are skipped (defensive — Postgres
//     never emits malformed entries, but we don't crash on
//     hypothetical malformed input either).
//
// Outputs:
//   - map[string]string. Empty input → empty map.
//
// Spec: specifications/collectors/fdw_servers_v1.md (REDACT-R001)
//
//	specifications/collectors/fdw_user_mappings_v1.md (REDACT-R002)
//	specifications/collectors/fdw_foreign_tables_v1.md (REDACT-R003)
func RedactFDWOptions(opts []string) map[string]string {
	out := make(map[string]string, len(opts))
	for _, kv := range opts {
		if kv == "" {
			continue
		}
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			// Malformed (no key, no `=`); skip — never reach customer
			// artifacts even as `{key: ""}`.
			continue
		}
		key := kv[:eq]
		val := kv[eq+1:]
		if isFDWSensitiveKey(key) {
			out[key] = fdwRedactedSentinel
			continue
		}
		out[key] = val
	}
	return out
}

// isFDWSensitiveKey returns true when the option key matches any
// pattern in fdwRedactPatterns. Comparison is case-folded and
// substring-based per the file comment.
func isFDWSensitiveKey(key string) bool {
	low := strings.ToLower(key)
	for _, pat := range fdwRedactPatterns {
		if strings.Contains(low, pat) {
			return true
		}
	}
	return false
}

// SortedFDWOptionKeys returns the keys of an options map in stable
// sorted order. Helpful for tests pinning rendered output and for
// any future code path that needs deterministic iteration without
// re-sorting at every call site.
func SortedFDWOptionKeys(opts map[string]string) []string {
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
