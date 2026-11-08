package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #725 / F18: the Kubernetes production runbook's values example must use the
// EXACT (case-sensitive) chart key the template reads — .Values.target.
// sslRootCertFile. The prior example used "sslrootcertFile", which Helm
// silently drops (unknown key), so a verify-full target rendered without a CA
// and startup failed closed. Guard the docs example against that typo.
func TestDocs_ProductionHelmCAExampleUsesChartKey(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "docs", "install", "kubernetes-production.md"))
	if err != nil {
		t.Fatalf("read kubernetes-production.md: %v", err)
	}
	doc := string(b)
	if strings.Contains(doc, "sslrootcertFile") {
		t.Error("kubernetes-production.md uses the wrong CA key 'sslrootcertFile'; the chart value is 'sslRootCertFile' (case-sensitive) — Helm silently drops the CA otherwise (#725)")
	}
	if !strings.Contains(doc, "sslRootCertFile") {
		t.Error("kubernetes-production.md no longer shows the sslRootCertFile CA example (#725)")
	}
}
