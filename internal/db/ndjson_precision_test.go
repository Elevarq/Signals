package db

import (
	"encoding/json"
	"testing"
)

// TestNDJSONPreservesLargeIntPrecision is the #444 regression: a bigint
// (int8) above 2^53 must round-trip through EncodeNDJSON/DecodeNDJSON and a
// subsequent re-encode (as the export path does) without losing precision.
// Before dec.UseNumber() the decode went through float64 and silently
// truncated the low bits.
func TestNDJSONPreservesLargeIntPrecision(t *testing.T) {
	const big = int64(9007199254740993) // 2^53 + 1 — not representable as float64

	payload, compressed, _, err := EncodeNDJSON([]map[string]any{{"n": big}})
	if err != nil {
		t.Fatalf("EncodeNDJSON: %v", err)
	}

	decoded, err := DecodeNDJSON(payload, compressed)
	if err != nil {
		t.Fatalf("DecodeNDJSON: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("got %d rows, want 1", len(decoded))
	}

	// Values decode as json.Number, preserving the exact literal.
	num, ok := decoded[0]["n"].(json.Number)
	if !ok {
		t.Fatalf("decoded n is %T, want json.Number", decoded[0]["n"])
	}
	got, err := num.Int64()
	if err != nil {
		t.Fatalf("num.Int64(): %v", err)
	}
	if got != big {
		t.Errorf("large int precision lost: got %d, want %d", got, big)
	}

	// The export re-encodes decoded rows; the re-encoded JSON must still
	// carry the exact literal, not a float64 approximation.
	reencoded, err := json.Marshal(decoded[0])
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if want := `{"n":9007199254740993}`; string(reencoded) != want {
		t.Errorf("re-encoded payload lost precision: got %s, want %s", reencoded, want)
	}
}

// TestNDJSONPreservesNumericDigits is the #444 regression for exact
// numeric fractional digits (which float64 would round).
func TestNDJSONPreservesNumericDigits(t *testing.T) {
	// A high-precision decimal as PostgreSQL numeric text would arrive.
	const exact = "12345678901234567890.123456789"

	payload, compressed, _, err := EncodeNDJSON([]map[string]any{{"d": json.Number(exact)}})
	if err != nil {
		t.Fatalf("EncodeNDJSON: %v", err)
	}
	decoded, err := DecodeNDJSON(payload, compressed)
	if err != nil {
		t.Fatalf("DecodeNDJSON: %v", err)
	}
	num, ok := decoded[0]["d"].(json.Number)
	if !ok {
		t.Fatalf("decoded d is %T, want json.Number", decoded[0]["d"])
	}
	if num.String() != exact {
		t.Errorf("numeric digits lost: got %s, want %s", num.String(), exact)
	}
}
