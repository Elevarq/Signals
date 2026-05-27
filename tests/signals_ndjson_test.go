package tests

import (
	"fmt"
	"testing"

	"github.com/elevarq/arq-signals/internal/db"
)

// TestNDJSONEncodeSmall verifies that a small payload is NOT compressed
// and round-trips correctly.
// Traces: ARQ-SIGNALS-R004 / TC-SIG-006
func TestNDJSONEncodeSmall(t *testing.T) {
	rows := []map[string]any{
		{"a": "one", "b": 1},
		{"a": "two", "b": 2},
		{"a": "three", "b": 3},
	}

	data, compressed, size, err := db.EncodeNDJSON(rows)
	if err != nil {
		t.Fatalf("EncodeNDJSON: %v", err)
	}
	if compressed {
		t.Fatal("expected small payload to NOT be compressed")
	}
	if size == 0 {
		t.Fatal("expected non-zero uncompressed size")
	}

	decoded, err := db.DecodeNDJSON(data, compressed)
	if err != nil {
		t.Fatalf("DecodeNDJSON: %v", err)
	}
	if len(decoded) != len(rows) {
		t.Fatalf("expected %d rows, got %d", len(rows), len(decoded))
	}
	for i, row := range decoded {
		if row["a"] != rows[i]["a"] {
			t.Errorf("row %d: expected a=%v, got %v", i, rows[i]["a"], row["a"])
		}
	}
}

// TestNDJSONEncodeLarge verifies that a payload exceeding 4 KB IS compressed
// and round-trips correctly.
// Traces: ARQ-SIGNALS-R004 / TC-SIG-007
func TestNDJSONEncodeLarge(t *testing.T) {
	rows := make([]map[string]any, 250)
	for i := range rows {
		rows[i] = map[string]any{
			"index":       i,
			"description": fmt.Sprintf("row number %d with enough padding to ensure we exceed the threshold easily", i),
			"value":       float64(i) * 1.5,
		}
	}

	data, compressed, _, err := db.EncodeNDJSON(rows)
	if err != nil {
		t.Fatalf("EncodeNDJSON: %v", err)
	}
	if !compressed {
		t.Fatal("expected large payload to be compressed")
	}

	decoded, err := db.DecodeNDJSON(data, compressed)
	if err != nil {
		t.Fatalf("DecodeNDJSON: %v", err)
	}
	if len(decoded) != len(rows) {
		t.Fatalf("expected %d rows, got %d", len(rows), len(decoded))
	}
}

// TestNDJSONRoundtrip verifies that various data types survive encode/decode.
// Traces: ARQ-SIGNALS-R004 / TC-SIG-006
func TestNDJSONRoundtrip(t *testing.T) {
	rows := []map[string]any{
		{"str": "hello", "num": float64(42), "flt": 3.14, "nul": nil, "bool": true},
		{"str": "world", "num": float64(0), "flt": -1.0, "nul": nil, "bool": false},
	}

	data, compressed, _, err := db.EncodeNDJSON(rows)
	if err != nil {
		t.Fatalf("EncodeNDJSON: %v", err)
	}

	decoded, err := db.DecodeNDJSON(data, compressed)
	if err != nil {
		t.Fatalf("DecodeNDJSON: %v", err)
	}
	if len(decoded) != len(rows) {
		t.Fatalf("expected %d rows, got %d", len(rows), len(decoded))
	}

	// Check first row types.
	r := decoded[0]
	if s, ok := r["str"].(string); !ok || s != "hello" {
		t.Errorf("str: expected \"hello\", got %v", r["str"])
	}
	if n, ok := r["num"].(float64); !ok || n != 42 {
		t.Errorf("num: expected 42, got %v", r["num"])
	}
	if r["nul"] != nil {
		t.Errorf("nul: expected nil, got %v", r["nul"])
	}
	if b, ok := r["bool"].(bool); !ok || b != true {
		t.Errorf("bool: expected true, got %v", r["bool"])
	}
}

// TestNDJSONEncodeEmpty verifies encoding an empty slice succeeds.
// Traces: ARQ-SIGNALS-R004 / TC-SIG-006
func TestNDJSONEncodeEmpty(t *testing.T) {
	data, compressed, size, err := db.EncodeNDJSON(nil)
	if err != nil {
		t.Fatalf("EncodeNDJSON(nil): %v", err)
	}
	if compressed {
		t.Error("empty payload should not be compressed")
	}
	if size != 0 {
		t.Errorf("expected size 0 for empty payload, got %d", size)
	}
	_ = data
}
