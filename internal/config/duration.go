package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// parseFlexDuration parses a duration string with the standard Go units
// (ns, us/µs, ms, s, m, h) plus two fixed calendar-flat extensions: `d`
// (day = 24h) and `w` (week = 168h). It exists because time.ParseDuration
// supports only ns..h, so an operator could not write `poll_interval: 1d`
// (#490).
//
// Design:
//   - Any string without a `d` or `w` unit is handed straight to
//     time.ParseDuration, so every existing value (`30s`, `5m`, `1h`,
//     `1h30m`, `1.5h`, `-5m`, `1m` = one minute) parses byte-for-byte as
//     before — full backward compatibility.
//   - `d`/`w` are FIXED durations (24h / 168h). They are not
//     calendar-aware; DST / "the 1st of the month" semantics belong to
//     calendar scheduling (#491), not a flat interval. Months and years
//     are deliberately unsupported (calendar-ambiguous).
//
// Combinations are allowed (`1w`, `2d12h`, `1w2d`), mirroring
// time.ParseDuration's own multi-unit behaviour.
var flexDurationRe = regexp.MustCompile(`^[+-]?(\d+(\.\d+)?(ns|us|µs|μs|ms|s|m|h|d|w))+$`)

var flexDurationGroupRe = regexp.MustCompile(`(\d+(?:\.\d+)?)(ns|us|µs|μs|ms|s|m|h|d|w)`)

func parseFlexDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	// Fast path: no day/week unit → stock parser, identical to before.
	// No standard unit contains 'd' or 'w', so this never diverts an
	// existing value.
	if !strings.ContainsAny(s, "dw") {
		return time.ParseDuration(s)
	}

	if !flexDurationRe.MatchString(s) {
		return 0, fmt.Errorf("invalid duration %q (supported units: ns, us, ms, s, m, h, d, w; "+
			"months and years are not supported — use calendar scheduling instead)", s)
	}

	body := s
	neg := false
	switch body[0] {
	case '+':
		body = body[1:]
	case '-':
		neg = true
		body = body[1:]
	}

	var total time.Duration
	var residual strings.Builder
	for _, m := range flexDurationGroupRe.FindAllStringSubmatch(body, -1) {
		num, unit := m[1], m[2]
		switch unit {
		case "d", "w":
			f, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid duration %q: %w", s, err)
			}
			hours := f * 24
			if unit == "w" {
				hours = f * 168
			}
			total += time.Duration(hours * float64(time.Hour))
		default:
			// Let time.ParseDuration handle every standard unit so their
			// semantics (including `m` = minute) are exactly preserved.
			residual.WriteString(num)
			residual.WriteString(unit)
		}
	}
	if residual.Len() > 0 {
		d, err := time.ParseDuration(residual.String())
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		total += d
	}
	if neg {
		total = -total
	}
	return total, nil
}
