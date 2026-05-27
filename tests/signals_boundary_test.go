package tests

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot returns the absolute path to the Arq Signals repository root.
func repoRoot(t *testing.T) string {
	t.Helper()
	// Walk up from the test file location to find go.mod.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod)")
		}
		dir = parent
	}
}

// allGoFiles returns all .go files in the repo, excluding vendor/.
func allGoFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && info.Name() == "vendor" {
			return filepath.SkipDir
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking repo: %v", err)
	}
	return files
}

// TestNoAnalyzerImports verifies that no .go file in Arq Signals imports
// packages that belong to the analyzer product boundary.
// Traces: ARQ-SIGNALS-R007, ARQ-SIGNALS-R008, ARQ-SIGNALS-R009 / TC-SIG-010, TC-SIG-011
func TestNoAnalyzerImports(t *testing.T) {
	root := repoRoot(t)
	forbidden := []string{
		"requirements",
		"scoring",
		"stats",
		"report",
		"llm",
		// "doctor" is intentionally NOT in this list. The signals
		// `doctor` package (internal/doctor, R095) is the operator
		// pre-flight surface for arq-signals itself, not an analyzer
		// concern. The original boundary list pre-dated R095 and was
		// reserving the name out of caution; with R095 active the
		// name is in-scope for this product.
		"analyzer",
		"license",
		"auth",
		"dashboard",
	}

	fset := token.NewFileSet()
	for _, path := range allGoFiles(t, root) {
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Logf("warning: could not parse %s: %v", path, err)
			continue
		}
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			for _, kw := range forbidden {
				// Check if the keyword appears as a path segment.
				segments := strings.Split(importPath, "/")
				for _, seg := range segments {
					if seg == kw {
						rel, _ := filepath.Rel(root, path)
						t.Errorf("file %s imports %q which contains forbidden segment %q",
							rel, importPath, kw)
					}
				}
			}
		}
	}
}

// TestNoLLMCode scans all non-test .go files for strings that would
// indicate LLM/report integration code has leaked into Arq Signals.
// Traces: ARQ-SIGNALS-R007 / TC-SIG-012
func TestNoLLMCode(t *testing.T) {
	root := repoRoot(t)
	forbidden := []string{
		"UDSClient",
		"llm.sock",
		"report.LLM",
		"DeterministicStub",
		"GenerateReport",
	}

	for _, path := range allGoFiles(t, root) {
		// Skip test files themselves to avoid false positives.
		if strings.HasSuffix(path, "_test.go") {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		content := string(data)
		for _, kw := range forbidden {
			if strings.Contains(content, kw) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("file %s contains forbidden LLM string %q", rel, kw)
			}
		}
	}
}

// TestNoScoringCode scans all non-test .go files for strings that would
// indicate scoring/analysis code has leaked into Arq Signals.
// Traces: ARQ-SIGNALS-R008 / TC-SIG-013
func TestNoScoringCode(t *testing.T) {
	root := repoRoot(t)
	forbidden := []string{
		"ComputeScore",
		"GradeBand",
		"TopRisk",
		"CategoryBreakdown",
		"RequirementDef",
		"RunAll",
	}

	for _, path := range allGoFiles(t, root) {
		// Skip test files themselves to avoid false positives.
		if strings.HasSuffix(path, "_test.go") {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		content := string(data)
		for _, kw := range forbidden {
			if strings.Contains(content, kw) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("file %s contains forbidden scoring string %q", rel, kw)
			}
		}
	}
}

// TestLicenseFileExists verifies that a LICENSE file exists and contains
// the expected BSD-3-Clause identifier. This test will pass after Phase 7
// (OSS readiness).
// Traces: ARQ-SIGNALS-R009 / TC-SIG-014
func TestLicenseFileExists(t *testing.T) {
	root := repoRoot(t)
	licensePath := filepath.Join(root, "LICENSE")

	data, err := os.ReadFile(licensePath)
	if err != nil {
		t.Skipf("LICENSE file not found (expected after Phase 7): %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "BSD") || !strings.Contains(content, "3-Clause") {
		t.Error("LICENSE file does not contain BSD 3-Clause license text")
	}
}

// TestNoProprietaryContent scans all files for proprietary markers that
// must not appear in an open-source project.
// Traces: ARQ-SIGNALS-R009 / TC-SIG-014
func TestNoProprietaryContent(t *testing.T) {
	root := repoRoot(t)
	forbidden := []string{
		"PROPRIETARY",
		"CONFIDENTIAL",
		"trade secret",
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "vendor" || info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip test files to avoid false positives from the marker literals.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// Only scan text files (skip binaries by extension).
		ext := strings.ToLower(filepath.Ext(path))
		textExts := map[string]bool{
			".go": true, ".mod": true, ".sum": true, ".md": true,
			".yaml": true, ".yml": true, ".json": true, ".toml": true,
			".txt": true, ".sql": true, ".sh": true, "": true,
		}
		if !textExts[ext] {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil // skip unreadable files
		}
		content := string(data)
		for _, kw := range forbidden {
			if strings.Contains(content, kw) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("file %s contains proprietary marker %q", rel, kw)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking repo: %v", err)
	}
}
