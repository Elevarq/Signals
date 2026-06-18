package tests

import (
	"strings"
	"testing"
)

// #149 — the chart's signals.yaml ConfigMap was previously rendered
// but never mounted into the pod. The Deployment now mounts it at
// /etc/signals, so the daemon's default config-file search picks up
// /etc/signals/signals.yaml.
//
// These tests verify the chart renders both halves of that contract
// every install: the ConfigMap resource is present, the volume is
// declared, the volumeMount targets /etc/signals, and the mount is
// read-only (defence in depth alongside readOnlyRootFilesystem).

func TestHelm_ConfigMapIsMountedAtEtcSignals(t *testing.T) {
	out := renderHelm(t)

	if !strings.Contains(out, "kind: ConfigMap") {
		t.Fatalf("ConfigMap resource missing from render:\n%s", out)
	}

	// The mount is named "config" by template convention; the
	// path /etc/signals is the daemon's default config-search root
	// (see internal/config/config.go Load()).
	if !strings.Contains(out, "name: config") {
		t.Errorf("`config` volume / mount entry not rendered:\n%s", out)
	}
	if !strings.Contains(out, "mountPath: /etc/signals") {
		t.Errorf("ConfigMap mountPath /etc/signals missing; the daemon would not pick up the rendered signals.yaml:\n%s", out)
	}
	if !strings.Contains(out, "configMap:") {
		t.Errorf("ConfigMap-backed volume missing from spec.volumes:\n%s", out)
	}
}

func TestHelm_ConfigMapMountIsReadOnly(t *testing.T) {
	out := renderHelm(t)

	// Locate the /etc/signals mount block and confirm readOnly: true
	// is in it. A non-readOnly ConfigMap mount would still
	// functionally work but contradicts the rest of the chart's
	// readOnlyRootFilesystem posture.
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "mountPath: /etc/signals") {
			continue
		}
		// Inspect a small window around the mountPath line.
		start := i - 2
		if start < 0 {
			start = 0
		}
		end := i + 3
		if end > len(lines) {
			end = len(lines)
		}
		window := strings.Join(lines[start:end], "\n")
		if !strings.Contains(window, "readOnly: true") {
			t.Errorf("/etc/signals mount is not readOnly; window:\n%s", window)
		}
		return
	}
	t.Fatalf("mountPath: /etc/signals not found at all in render:\n%s", out)
}
