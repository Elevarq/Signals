package db

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
)

const compressThreshold = 4096

// EncodeNDJSON encodes rows as newline-delimited JSON.
// If the result exceeds compressThreshold bytes, it is gzip-compressed.
// Returns (data, compressed, uncompressedSize, error).
func EncodeNDJSON(rows []map[string]any) ([]byte, bool, int, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			return nil, false, 0, fmt.Errorf("encode row: %w", err)
		}
	}

	raw := buf.Bytes()
	if raw == nil {
		raw = []byte{} // ensure non-nil for NOT NULL constraint
	}
	uncompSize := len(raw)

	if uncompSize <= compressThreshold {
		return raw, false, uncompSize, nil
	}

	var comp bytes.Buffer
	gz := gzip.NewWriter(&comp)
	if _, err := gz.Write(raw); err != nil {
		return nil, false, 0, fmt.Errorf("gzip write: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, false, 0, fmt.Errorf("gzip close: %w", err)
	}

	return comp.Bytes(), true, uncompSize, nil
}

// DecodeNDJSON decodes NDJSON data, decompressing if compressed is true.
func DecodeNDJSON(data []byte, compressed bool) ([]map[string]any, error) {
	var reader io.Reader = bytes.NewReader(data)

	if compressed {
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return nil, fmt.Errorf("gzip open: %w", err)
		}
		defer func() { _ = gz.Close() }()
		reader = gz
	}

	var rows []map[string]any
	dec := json.NewDecoder(reader)
	for dec.More() {
		var row map[string]any
		if err := dec.Decode(&row); err != nil {
			return nil, fmt.Errorf("decode row: %w", err)
		}
		rows = append(rows, row)
	}

	return rows, nil
}
