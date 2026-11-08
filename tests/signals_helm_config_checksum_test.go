package tests

import (
	"regexp"
	"testing"
)

// #728 (audit A2) — a config-only `helm upgrade` must actually apply: the
// daemon has no config-file watcher, so the pod template must carry a
// checksum/config annotation derived from the ConfigMap, and the pod must roll
// when (and only when) the ConfigMap content changes. Combined with the
// Recreate strategy (#718) this is a single-store-safe restart.
//
// Spec: specifications/config-apply-on-upgrade.md (CAU-R001..R002).

var checksumRe = regexp.MustCompile(`checksum/config:\s*([0-9a-f]{64})`)

func configChecksum(t *testing.T, sets ...string) string {
	t.Helper()
	out := renderHelm(t, sets...)
	m := checksumRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("rendered Deployment has no checksum/config annotation (#728):\n%s", out)
	}
	return m[1]
}

// CAU-R001 — a target-config change flips the checksum, so the pod rolls.
func TestHelm_ConfigChangeRollsPod(t *testing.T) {
	base := configChecksum(t)
	changed := configChecksum(t, "target.host=changed.example.com")
	if base == changed {
		t.Error("checksum/config did not change when a target value changed; a config-only upgrade would not apply (#728)")
	}
}

// CAU-R002 — a non-config change (image tag) does NOT flip the checksum, so an
// unrelated upgrade doesn't needlessly restart the collector.
func TestHelm_NonConfigChangeDoesNotRollPod(t *testing.T) {
	base := configChecksum(t)
	imageOnly := configChecksum(t, "image.tag=v9.9.9-test")
	if base != imageOnly {
		t.Error("checksum/config changed on an image-only edit; the annotation must track the ConfigMap, not unrelated values (#728)")
	}
}
