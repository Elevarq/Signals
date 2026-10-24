package singlestore

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/elevarq/signals/snapshot"
)

// BuildInfo carries the collection context stamped into the snapshot metadata.
type BuildInfo struct {
	CollectorVersion string
	CollectorCommit  string
	CollectedAt      string
	Target           snapshot.TargetInfo
}

// Snapshot is an assembled SingleStore snapshot: the envelope metadata plus the
// per-collector results. For the POC this is a standalone artifact; it reuses
// snapshot.Metadata so the engine-identity contract is shared with the
// PostgreSQL path.
type Snapshot struct {
	Metadata   snapshot.Metadata
	Collectors []Result
}

// BuildSnapshot assembles a Snapshot, applying engine identity via
// SetEngineIdentity (SSA-R001): a SingleStore engine sets Engine/EngineVersion;
// a PostgreSQL/empty engine leaves them empty (v1-compatible).
func BuildSnapshot(info BuildInfo, engine, version string, results []Result) Snapshot {
	m := snapshot.Metadata{
		SchemaVersion:    snapshot.SchemaVersion,
		CollectorVersion: info.CollectorVersion,
		CollectorCommit:  info.CollectorCommit,
		CollectedAt:      info.CollectedAt,
		Target:           info.Target,
	}
	m.SetEngineIdentity(engine, version)
	return Snapshot{Metadata: m, Collectors: results}
}

// MetadataJSON returns the metadata.json bytes.
func (s Snapshot) MetadataJSON() ([]byte, error) {
	return json.MarshalIndent(s.Metadata, "", "  ")
}

// ndjsonRow is one query_results.ndjson object per collector.
type ndjsonRow struct {
	CollectorID string           `json:"collector_id"`
	Category    string           `json:"category"`
	Rows        []map[string]any `json:"rows"`
	Err         string           `json:"error,omitempty"`
}

// QueryResultsNDJSON returns one JSON object per collector result, in order
// (SSA-R002), newline-delimited.
func (s Snapshot) QueryResultsNDJSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, r := range s.Collectors {
		// ndjsonRow mirrors Result's fields but carries the JSON tags for the
		// query_results.ndjson shape; a direct conversion keeps them in sync.
		if err := enc.Encode(ndjsonRow(r)); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// WriteZIP emits metadata.json, query_results.ndjson, and a manifest.json of
// SHA-256 hashes (SSA-R003) over the exact bytes written.
func (s Snapshot) WriteZIP(w io.Writer) error {
	meta, err := s.MetadataJSON()
	if err != nil {
		return err
	}
	results, err := s.QueryResultsNDJSON()
	if err != nil {
		return err
	}

	files := []struct {
		name string
		data []byte
	}{
		{"metadata.json", meta},
		{"query_results.ndjson", results},
	}

	manifest := map[string]string{}
	for _, f := range files {
		sum := sha256.Sum256(f.data)
		manifest[f.name] = hex.EncodeToString(sum[:])
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	zw := zip.NewWriter(w)
	for _, f := range files {
		fw, err := zw.Create(f.name)
		if err != nil {
			return err
		}
		if _, err := fw.Write(f.data); err != nil {
			return err
		}
	}
	mw, err := zw.Create("manifest.json")
	if err != nil {
		return err
	}
	if _, err := mw.Write(manifestJSON); err != nil {
		return err
	}
	return zw.Close()
}
