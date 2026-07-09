// gen-collector-inventory regenerates the canonical collector
// inventory committed at
// specifications/collectors/collector-inventory.json from the query
// registry.
//
// Spec: specifications/collector-inventory.md (R119–R122). Run from
// the repository root:
//
//	go run ./cmd/gen-collector-inventory
//
// The R121 CI gate (internal/pgqueries/inventory_test.go) fails any
// commit whose registry and committed inventory diverge, so this
// command must be run — and its output committed — together with any
// change that registers, renames, or removes a collector.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/elevarq/signals/internal/pgqueries"
)

func main() {
	out := flag.String("out", filepath.FromSlash("specifications/collectors/collector-inventory.json"),
		"output path (relative to the repository root)")
	flag.Parse()

	raw, err := pgqueries.CollectorInventoryJSON()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen-collector-inventory: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, raw, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "gen-collector-inventory: write %s: %v\n", *out, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "gen-collector-inventory: wrote %s\n", *out)
}
