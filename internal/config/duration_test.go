package config

import (
	"testing"
	"time"
)

// #490 — parseFlexDuration adds `d` (24h) and `w` (168h) to the standard Go
// duration units while parsing every existing value identically.
func TestParseFlexDuration_Valid(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		// New units.
		{"1d", 24 * time.Hour},
		{"1w", 168 * time.Hour},
		{"7d", 168 * time.Hour},
		{"2w", 2 * 168 * time.Hour},
		// Combinations.
		{"2d12h", 2*24*time.Hour + 12*time.Hour},
		{"1w2d", 168*time.Hour + 2*24*time.Hour},
		{"1d30m", 24*time.Hour + 30*time.Minute},
		{"0.5d", 12 * time.Hour},
		// Backward compatibility — must be byte-for-byte identical to
		// time.ParseDuration.
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{"1m", time.Minute}, // 1m stays ONE MINUTE, not one month.
		{"1h", time.Hour},
		{"1h30m", time.Hour + 30*time.Minute},
		{"1.5h", 90 * time.Minute},
		{"90m", 90 * time.Minute},
		{"500ms", 500 * time.Millisecond},
		{"-5m", -5 * time.Minute},
		{"0s", 0},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseFlexDuration(tc.in)
			if err != nil {
				t.Fatalf("parseFlexDuration(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("parseFlexDuration(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// Existing values must parse identically to the stock parser — proves no
// regression for any pre-#490 duration.
func TestParseFlexDuration_MatchesStdlibForStandardUnits(t *testing.T) {
	for _, in := range []string{"30s", "5m", "1m", "1h", "1h30m", "1.5h", "90m", "250ms", "-2h", "0s"} {
		std, err := time.ParseDuration(in)
		if err != nil {
			t.Fatalf("time.ParseDuration(%q): %v", in, err)
		}
		got, err := parseFlexDuration(in)
		if err != nil {
			t.Fatalf("parseFlexDuration(%q): %v", in, err)
		}
		if got != std {
			t.Errorf("parseFlexDuration(%q) = %v, time.ParseDuration = %v", in, got, std)
		}
	}
}

// Calendar-ambiguous and bogus units are rejected with an error — months
// and years are deliberately out of scope (#490 decision).
func TestParseFlexDuration_RejectsUnsupportedUnits(t *testing.T) {
	for _, in := range []string{"1M", "1mo", "1y", "1x", "d", "w", "1week", "", "abc", "1w1mo"} {
		if got, err := parseFlexDuration(in); err == nil {
			t.Errorf("parseFlexDuration(%q) = %v, want error", in, got)
		}
	}
}
