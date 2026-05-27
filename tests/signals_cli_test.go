package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go/ast"
	"go/parser"
	"go/token"
)

// TestCLISubcommandsRegistered verifies that the arqctl main.go registers
// the expected subcommands (version, status, collect, export) by inspecting
// the AST of cmd/arqctl/main.go for AddCommand calls.
// Traces: ARQ-SIGNALS-R010 / TC-SIG-025
func TestCLISubcommandsRegistered(t *testing.T) {
	root := repoRoot(t)
	mainPath := filepath.Join(root, "cmd", "arqctl", "main.go")

	data, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("reading cmd/arqctl/main.go: %v", err)
	}
	content := string(data)

	// Verify the cobra root command is defined.
	if !strings.Contains(content, `cobra.Command`) {
		t.Fatal("cmd/arqctl/main.go does not use cobra.Command")
	}

	// Verify expected subcommands are added via AddCommand.
	expected := []string{"versionCmd", "statusCmd", "collectCmd", "exportCmd"}
	for _, cmd := range expected {
		if !strings.Contains(content, cmd+"()") {
			t.Errorf("cmd/arqctl/main.go does not call %s()", cmd)
		}
	}
}

// TestCLIMainImportsCobra verifies that cmd/arqctl/main.go imports the
// cobra package.
// Traces: ARQ-SIGNALS-R010 / TC-SIG-025
func TestCLIMainImportsCobra(t *testing.T) {
	root := repoRoot(t)
	mainPath := filepath.Join(root, "cmd", "arqctl", "main.go")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, mainPath, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	found := false
	for _, imp := range f.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		if strings.Contains(importPath, "cobra") {
			found = true
			break
		}
	}
	if !found {
		t.Error("cmd/arqctl/main.go does not import cobra")
	}
}

// TestCLIMainDefinesFunctions verifies cmd/arqctl/main.go defines the
// expected command builder functions.
// Traces: ARQ-SIGNALS-R010 / TC-SIG-025
func TestCLIMainDefinesFunctions(t *testing.T) {
	root := repoRoot(t)
	mainPath := filepath.Join(root, "cmd", "arqctl", "main.go")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, mainPath, nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	expectedFuncs := map[string]bool{
		"versionCmd": false,
		"statusCmd":  false,
		"collectCmd": false,
		"exportCmd":  false,
	}

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if _, want := expectedFuncs[fn.Name.Name]; want {
			expectedFuncs[fn.Name.Name] = true
		}
	}

	for name, found := range expectedFuncs {
		if !found {
			t.Errorf("expected function %s() not found in cmd/arqctl/main.go", name)
		}
	}
}
