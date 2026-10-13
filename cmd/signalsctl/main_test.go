package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

// #492 — signalsctl --help must NOT render $SIGNALS_API_TOKEN in cleartext.
// The regression: the --api-token flag used the resolved env value as its
// Cobra default, which --help prints as `(default "...")`.
func TestHelpDoesNotLeakAPIToken(t *testing.T) {
	const secret = "super-secret-token-value"
	t.Setenv("SIGNALS_API_TOKEN", secret)

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("--help should exit cleanly, got %v", err)
	}
	if strings.Contains(out.String(), secret) {
		t.Errorf("--help output leaked the API token in cleartext:\n%s", out.String())
	}
}

// #492 — env-based auth still works when no --api-token flag is given:
// effectiveAPIToken falls back to $SIGNALS_API_TOKEN, and the explicit flag
// still wins over the env.
func TestEffectiveAPITokenResolution(t *testing.T) {
	t.Setenv("SIGNALS_API_TOKEN", "from-env")

	apiToken = "" // no flag set
	if got := effectiveAPIToken(); got != "from-env" {
		t.Errorf("empty flag should fall back to env; got %q, want %q", got, "from-env")
	}

	apiToken = "from-flag"
	if got := effectiveAPIToken(); got != "from-flag" {
		t.Errorf("explicit flag must win over env; got %q, want %q", got, "from-flag")
	}
	apiToken = "" // restore package state for other tests
}

func newResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}
}

func TestCheckSuccessAllows2xx(t *testing.T) {
	for _, code := range []int{200, 201, 202, 204, 299} {
		if err := checkSuccess(newResp(code, ""), "op"); err != nil {
			t.Errorf("HTTP %d should succeed, got %v", code, err)
		}
	}
}

func TestCheckSuccessRejectsNon2xx(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{401, `{"error":"missing or invalid Authorization header"}`, "HTTP 401"},
		{403, ``, "HTTP 403"},
		{500, `internal server error`, "HTTP 500"},
		{503, "  \n", "HTTP 503"}, // whitespace-only body trimmed away
		{429, `{"error":"too many invalid token attempts"}`, "HTTP 429"},
	}

	for _, tc := range cases {
		err := checkSuccess(newResp(tc.status, tc.body), "test")
		if err == nil {
			t.Errorf("HTTP %d should fail", tc.status)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("HTTP %d: expected error to contain %q, got %v", tc.status, tc.want, err)
		}
		if !strings.HasPrefix(err.Error(), "test failed:") {
			t.Errorf("HTTP %d: expected error to start with op name, got %v", tc.status, err)
		}
	}
}

func TestCheckSuccessIncludesBodyWhenPresent(t *testing.T) {
	body := `{"error":"bad target_id"}`
	err := checkSuccess(newResp(400, body), "export")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), body) {
		t.Errorf("expected error to include body %q, got %v", body, err)
	}
}

func TestCheckSuccessOmitsEmptyBody(t *testing.T) {
	err := checkSuccess(newResp(404, ""), "status")
	if err == nil {
		t.Fatal("expected error")
	}
	// No trailing colon-space-empty.
	if strings.HasSuffix(err.Error(), ": ") {
		t.Errorf("error should not have empty body suffix: %q", err.Error())
	}
}
