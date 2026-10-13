package collector

import (
	"testing"
	"time"
)

// #489 — the "collector starting" log must render the poll interval as a
// compact human-readable duration, not slog's raw int64 nanosecond count.
func TestFormatInterval(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want string
	}{
		{"one hour", time.Hour, "1h"},
		{"ninety minutes", 90 * time.Minute, "1h30m"},
		{"thirty seconds", 30 * time.Second, "30s"},
		{"five minutes", 5 * time.Minute, "5m"},
		{"hours minutes seconds", 2*time.Hour + 15*time.Minute + 30*time.Second, "2h15m30s"},
		{"hour and seconds, no minutes", time.Hour + 30*time.Second, "1h30s"},
		{"one second", time.Second, "1s"},
		// Sub-second: nanosecond precision is meaningful, fall back to String().
		{"five hundred millis", 500 * time.Millisecond, "500ms"},
		{"zero", 0, "0s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatInterval(tc.in); got != tc.want {
				t.Errorf("formatInterval(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
