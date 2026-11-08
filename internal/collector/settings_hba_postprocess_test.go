package collector

import "testing"

// #708 / F01: pg_settings_v1 credential-bearing GUC values are redacted in the
// setting/reset_val/boot_val columns; non-sensitive GUCs are untouched; empties
// stay empty; other collectors are a no-op.
func TestRedactSettingsSecrets(t *testing.T) {
	rows := []map[string]any{
		{"name": "primary_conninfo", "setting": "host=p user=rep password=s3cr3t sslmode=require", "reset_val": "host=p password=s3cr3t", "boot_val": ""},
		{"name": "archive_command", "setting": "aws s3 cp %p s3://b --sse --secret HUNTER2", "reset_val": "", "boot_val": ""},
		{"name": "shared_buffers", "setting": "128MB", "reset_val": "128MB", "boot_val": "16MB"},
	}
	redactSettingsSecretsIfNeeded("pg_settings_v1", rows)

	if got := rows[0]["setting"]; got != settingsRedactedSentinel {
		t.Errorf("primary_conninfo setting not redacted: %q", got)
	}
	if got := rows[0]["reset_val"]; got != settingsRedactedSentinel {
		t.Errorf("primary_conninfo reset_val not redacted: %q", got)
	}
	if got := rows[0]["boot_val"]; got != "" {
		t.Errorf("empty boot_val should stay empty, got %q", got)
	}
	if got := rows[1]["setting"]; got != settingsRedactedSentinel {
		t.Errorf("archive_command setting not redacted: %q", got)
	}
	if got := rows[2]["setting"]; got != "128MB" {
		t.Errorf("non-sensitive GUC shared_buffers must be untouched, got %q", got)
	}
	if got := rows[2]["boot_val"]; got != "16MB" {
		t.Errorf("non-sensitive GUC boot_val must be untouched, got %q", got)
	}
}

func TestRedactSettingsSecrets_NoOpForOtherCollector(t *testing.T) {
	rows := []map[string]any{{"name": "primary_conninfo", "setting": "password=s3cr3t"}}
	redactSettingsSecretsIfNeeded("pg_stat_activity_v1", rows)
	if got := rows[0]["setting"]; got != "password=s3cr3t" {
		t.Errorf("non-pg_settings collector must be a no-op, got %q", got)
	}
}

// #708 / F01: pg_hba_file_rules_v1 options redact ldapbindpasswd / radiussecret
// while preserving the array shape and non-sensitive entries.
func TestRedactHBAOptions(t *testing.T) {
	rows := []map[string]any{
		{"options": []any{"ldapserver=ldap.example.com", "ldapbindpasswd=s3cr3t", "radiussecret=hunter2"}},
	}
	redactHBAOptionsIfNeeded("pg_hba_file_rules_v1", rows)

	out, ok := rows[0]["options"].([]string)
	if !ok {
		t.Fatalf("options should remain a []string array, got %T", rows[0]["options"])
	}
	want := []string{"ldapserver=ldap.example.com", "ldapbindpasswd=<redacted>", "radiussecret=<redacted>"}
	if len(out) != len(want) {
		t.Fatalf("got %d options, want %d: %v", len(out), len(want), out)
	}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("option[%d] = %q, want %q", i, out[i], want[i])
		}
	}
}

func TestRedactHBAOptions_NoOpForOtherCollector(t *testing.T) {
	rows := []map[string]any{{"options": []any{"ldapbindpasswd=s3cr3t"}}}
	redactHBAOptionsIfNeeded("pg_settings_v1", rows)
	if arr, _ := rows[0]["options"].([]any); len(arr) != 1 || arr[0] != "ldapbindpasswd=s3cr3t" {
		t.Errorf("non-hba collector must be a no-op, got %v", rows[0]["options"])
	}
}
