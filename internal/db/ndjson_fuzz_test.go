package db

import "testing"

// FuzzDecodeNDJSON feeds arbitrary bytes into the NDJSON decoder and
// asserts no panic. DecodeNDJSON consumes data that originates from
// the database (and could be partly attacker-influenced through
// collected PostgreSQL rows), so robustness against malformed input
// is a hard requirement. Errors are fine; panics are not.
//
// Compressed=false is exercised here; compressed=true wraps gzip
// whose own decoder has its own fuzzing in the standard library.
//
// Closes Scorecard FuzzingID alongside FuzzRedactDSN (issue #32).
func FuzzDecodeNDJSON(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("\n"))
	f.Add([]byte("{}\n"))
	f.Add([]byte("{\"a\":1}\n{\"b\":2}\n"))
	f.Add([]byte("garbage\n"))
	f.Add([]byte("{\"a\":"))
	f.Add([]byte("{\"a\":\"\\u\"}\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		// The decoder is allowed to return an error for malformed input.
		// What it MUST NOT do is panic. The function's return value is
		// otherwise irrelevant for this property.
		_, _ = DecodeNDJSON(data, false)
	})
}
