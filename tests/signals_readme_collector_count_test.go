package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// Anti-drift guard for #161. The collector count advertised in README.md
// must equal the runtime registry (`len(pgqueries.All())`). The README
// lagged at 73 while the registry already had 99; this test pins every
// count claim on the public landing page to the single source of truth so
// it cannot silently drift again.
//
// When README phrasing introduces a new count claim, add its pattern to
// countPatterns below — that is the deliberate-change checkpoint.
func TestREADME_CollectorCountMatchesRegistry(t *testing.T) {
	want := len(pgqueries.All())

	data, err := os.ReadFile(filepath.Join(repoRoot(t), "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	body := string(data)

	countPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(\d+) read-only diagnostic collectors`),
		regexp.MustCompile(`includes (\d+) read-only collectors`),
		regexp.MustCompile(`all (\d+) collectors`),
	}

	for _, re := range countPatterns {
		matches := re.FindAllStringSubmatch(body, -1)
		if matches == nil {
			t.Errorf("README.md: no match for %q — phrasing changed? update this guard (#161)", re)
			continue
		}
		for _, m := range matches {
			got, err := strconv.Atoi(m[1])
			if err != nil {
				t.Fatalf("parse count from %q: %v", m[0], err)
			}
			if got != want {
				t.Errorf("README.md says %d collectors (%q) but len(pgqueries.All()) = %d — update README.md", got, m[0], want)
			}
		}
	}
}
