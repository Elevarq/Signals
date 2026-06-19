package tests

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/config"
)

// strongTokenForTest returns a freshly-minted high-entropy token of
// the documented shape (`encoding == "base64url"` or `"hex"`). Used
// instead of hard-coded literals so the source tree never contains
// a high-entropy string that gitleaks (or any secret scanner) could
// reasonably flag as a leaked credential.
func strongTokenForTest(t *testing.T, encoding string) string {
	t.Helper()
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	switch encoding {
	case "base64url":
		return base64.RawURLEncoding.EncodeToString(buf)
	case "hex":
		return hex.EncodeToString(buf)
	default:
		t.Fatalf("strongTokenForTest: unknown encoding %q", encoding)
		return ""
	}
}

// #135 — WeakAPITokenReason rejects short tokens.
func TestWeakAPITokenReason_RejectsShortTokens(t *testing.T) {
	cases := []string{
		"",
		"x",
		"my-token",
		"dev-token",
		"test-token",
		"abcdef0123456789", // 16 hex chars — under the 32 minimum
		strings.Repeat("a", 31),
	}
	for _, tok := range cases {
		reason := config.WeakAPITokenReason(tok)
		if reason == "" {
			t.Errorf("token %q (len=%d) should be rejected as too short", tok, len(tok))
			continue
		}
		if !strings.Contains(reason, "too short") {
			t.Errorf("token %q: reason %q, expected 'too short'", tok, reason)
		}
		// AC: reason MUST NOT contain the token itself.
		if tok != "" && strings.Contains(reason, tok) {
			t.Errorf("token leaked into reason: %q in %q", tok, reason)
		}
	}
}

// #135 — WeakAPITokenReason rejects 32+ char tokens with low entropy.
func TestWeakAPITokenReason_RejectsLowEntropy(t *testing.T) {
	cases := map[string]bool{
		strings.Repeat("a", 32):            true, // 1 distinct char
		strings.Repeat("ab", 16):           true, // 2 distinct chars
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaabcde": true, // 5 distinct chars over 32 len
	}
	for tok, shouldReject := range cases {
		reason := config.WeakAPITokenReason(tok)
		if shouldReject && reason == "" {
			t.Errorf("token %q (distinct=%d) should be rejected as low-entropy", tok, distinct(tok))
		}
		if shouldReject && !strings.Contains(reason, "entropy too low") {
			t.Errorf("token %q: reason %q, expected 'entropy too low'", tok, reason)
		}
	}
}

// #135 — WeakAPITokenReason accepts well-formed strong tokens.
// Tokens are generated at runtime via crypto/rand so the source
// tree never contains a literal high-entropy string a secret
// scanner could flag.
func TestWeakAPITokenReason_AcceptsStrongTokens(t *testing.T) {
	cases := []string{
		strongTokenForTest(t, "base64url"),
		strongTokenForTest(t, "hex"),
		// Mixed-case constructed string with documented diversity —
		// not a credential, just a length-and-uniqueness test fixture.
		"my-very-strong-deployment-secret-abcdef-1234567890",
	}
	for _, tok := range cases {
		if reason := config.WeakAPITokenReason(tok); reason != "" {
			t.Errorf("strong token (%d chars) rejected with reason: %s", len(tok), reason)
		}
	}
}

// #135 — ValidateStrict in prod env returns a HARD error for weak tokens.
func TestValidateStrict_WeakTokenIsProdHardError(t *testing.T) {
	cfg := strictBaseConfig(t)
	cfg.Env = "prod"
	cfg.API.APIToken = "dev-token"

	warns, err := config.ValidateStrict(cfg)
	if err == nil {
		t.Fatalf("expected prod weak-token to be a hard error, got nil")
	}
	if !strings.Contains(err.Error(), "api.api_token") {
		t.Errorf("hard error should mention api.api_token; got: %v", err)
	}
	if strings.Contains(err.Error(), "dev-token") {
		t.Errorf("error leaked the token value: %v", err)
	}
	// Warnings list should NOT also carry it (we hard-errored).
	for _, w := range warns {
		if strings.Contains(w, "api.api_token") {
			t.Errorf("prod weak token should hard-error, not warn — got warning: %s", w)
		}
	}
}

// #135 — ValidateStrict in non-prod env emits a WARNING but does not fail.
func TestValidateStrict_WeakTokenIsDevWarning(t *testing.T) {
	cfg := strictBaseConfig(t)
	cfg.Env = "dev"
	cfg.API.APIToken = "dev-token"

	warns, err := config.ValidateStrict(cfg)
	if err != nil {
		t.Fatalf("expected dev weak-token to be a warning, got hard error: %v", err)
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w, "api.api_token") {
			found = true
			if strings.Contains(w, "dev-token") {
				t.Errorf("warning leaked the token value: %s", w)
			}
		}
	}
	if !found {
		t.Errorf("expected an api.api_token warning in dev; got: %v", warns)
	}
}

// #135 — ValidateStrict accepts strong tokens in prod without complaint.
func TestValidateStrict_StrongTokenIsLegalInProd(t *testing.T) {
	cfg := strictBaseConfig(t)
	cfg.Env = "prod"
	cfg.API.APIToken = strongTokenForTest(t, "base64url")

	warns, err := config.ValidateStrict(cfg)
	if err != nil {
		// Only fail if the api.api_token rule was the cause.
		if strings.Contains(err.Error(), "api.api_token") {
			t.Errorf("strong prod token rejected: %v", err)
		}
	}
	for _, w := range warns {
		if strings.Contains(w, "api.api_token") {
			t.Errorf("strong prod token emitted unexpected warning: %s", w)
		}
	}
}

// #135 — empty APIToken (auto-generation path) is legal and does
// not trigger the rule. Auto-generated tokens are always 32 random
// bytes from crypto/rand and don't pass through this validator.
func TestValidateStrict_EmptyTokenAllowed(t *testing.T) {
	cfg := strictBaseConfig(t)
	cfg.Env = "prod"
	cfg.API.APIToken = "" // empty → cmd/signals/main.go auto-generates

	warns, err := config.ValidateStrict(cfg)
	if err != nil && strings.Contains(err.Error(), "api.api_token") {
		t.Errorf("empty token unexpectedly tripped the api_token rule: %v", err)
	}
	for _, w := range warns {
		if strings.Contains(w, "api.api_token") {
			t.Errorf("empty token unexpectedly tripped the api_token rule: %s", w)
		}
	}
}

// strictBaseConfig returns a minimal-but-valid Config for the
// strict-validator tests; callers tweak specific fields per test.
func strictBaseConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Database.Path = "/tmp/test.db"
	cfg.API.ListenAddr = "127.0.0.1:8080"
	return cfg
}

func distinct(s string) int {
	u := make(map[rune]struct{})
	for _, r := range s {
		u[r] = struct{}{}
	}
	return len(u)
}
