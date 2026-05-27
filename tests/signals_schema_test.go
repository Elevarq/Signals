package tests

import (
	"reflect"
	"testing"

	"github.com/elevarq/arq-signals/snapshot"
)

// TestSnapshotSchemaVersion verifies the schema version constant.
// Traces: TC-SIG-023
func TestSnapshotSchemaVersion(t *testing.T) {
	expected := "arq-snapshot.v1"
	if snapshot.SchemaVersion != expected {
		t.Fatalf("SchemaVersion = %q, want %q", snapshot.SchemaVersion, expected)
	}
}

// TestMetadataFields verifies that snapshot.Metadata has all required
// fields using reflection.
// Traces: TC-SIG-023
func TestMetadataFields(t *testing.T) {
	requiredFields := []string{
		"SchemaVersion",
		"CollectorVersion",
		"CollectorCommit",
		"CollectedAt",
		"PGVersion",
		"Target",
	}

	metaType := reflect.TypeOf(snapshot.Metadata{})
	for _, name := range requiredFields {
		field, ok := metaType.FieldByName(name)
		if !ok {
			t.Errorf("Metadata struct is missing required field %q", name)
			continue
		}
		// Verify each field has a json tag.
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" {
			t.Errorf("Metadata.%s has no json tag", name)
		}
	}
}

// TestTargetInfoFields verifies that snapshot.TargetInfo has the expected
// fields.
// Traces: TC-SIG-023
func TestTargetInfoFields(t *testing.T) {
	requiredFields := []string{
		"Name",
		"Host",
		"Port",
		"DBName",
	}

	tiType := reflect.TypeOf(snapshot.TargetInfo{})
	for _, name := range requiredFields {
		field, ok := tiType.FieldByName(name)
		if !ok {
			t.Errorf("TargetInfo struct is missing required field %q", name)
			continue
		}
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" {
			t.Errorf("TargetInfo.%s has no json tag", name)
		}
	}
}
