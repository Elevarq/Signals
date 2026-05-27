package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

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
