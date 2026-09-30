// Copyright (c) 2026 Scantr LLC. All rights reserved.
// Elevarq is a trade name of Scantr LLC.
// This file is part of Elevarq Signals. Use is governed by the
// commercial license at LICENSE in the repository root.

// Guards the Helm chart's version-lockstep policy (Signals#477): Chart.yaml
// `version`, Chart.yaml `appVersion`, and values.yaml `image.tag` must all
// match, because they are bumped together at release time and a mismatch would
// ship a chart that installs the wrong image. Runs in the normal `go test`
// job, so drift fails CI on any PR — #476 shipped green with Chart 1.5.1 but
// image.tag 1.5.0 because nothing enforced this.
package chart_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func find(t *testing.T, path, pattern string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	m := regexp.MustCompile(pattern).FindSubmatch(b)
	if m == nil {
		t.Fatalf("%s: pattern %q not found", path, pattern)
	}
	return string(m[1])
}

func TestChartVersionLockstep(t *testing.T) {
	root := filepath.Join("..", "..", "deploy", "helm", "signals")
	chart := filepath.Join(root, "Chart.yaml")
	values := filepath.Join(root, "values.yaml")

	version := find(t, chart, `(?m)^version:\s*"?([0-9]+\.[0-9]+\.[0-9]+)"?`)
	appVersion := find(t, chart, `(?m)^appVersion:\s*"?([0-9]+\.[0-9]+\.[0-9]+)"?`)
	imageTag := find(t, values, `(?m)^\s*tag:\s*"?([0-9]+\.[0-9]+\.[0-9]+)"?`)

	if version != appVersion || version != imageTag {
		t.Fatalf("chart version lockstep violated: Chart.yaml version=%q appVersion=%q, values.yaml image.tag=%q — bump all three together (Signals#477)",
			version, appVersion, imageTag)
	}
}
