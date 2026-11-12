package config

import (
	"strings"
	"testing"
)

// #808 — the per-run query_results payload cap: default is 32 MiB and
// ValidateStrict rejects a negative value (FC-808). 0 disables the cap.
func TestMaxQueryResultBytes_Default(t *testing.T) {
	if got := DefaultConfig().Signals.MaxQueryResultBytes; got != 32<<20 {
		t.Fatalf("DefaultConfig MaxQueryResultBytes = %d, want %d (32 MiB)", got, 32<<20)
	}
}

func TestMaxQueryResultBytes_ValidateStrict(t *testing.T) {
	cases := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{"default 32 MiB is valid", 32 << 20, false},
		{"zero disables the cap and is valid", 0, false},
		{"positive is valid", 1 << 20, false},
		{"negative is rejected (FC-808)", -1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseValidConfig(baseValidTarget())
			cfg.Signals.MaxQueryResultBytes = tc.value
			_, err := ValidateStrict(cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateStrict(%d) = nil error, want a hard error", tc.value)
				}
				if !strings.Contains(err.Error(), "max_query_result_bytes") {
					t.Fatalf("error %q does not mention max_query_result_bytes", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateStrict(%d) = %v, want nil", tc.value, err)
			}
		})
	}
}
