package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #719 (F13) — the Azure/GCP deployment templates use Docker bridge networking
// (`-p 127.0.0.1:8081:8081`) while the daemon bound `127.0.0.1:8081` INSIDE the
// container. Published traffic reaches the container's eth0, not its loopback,
// so host-side status/export/control could not reach a live collector. The fix:
// bind the daemon to the container interface (0.0.0.0) while keeping the host
// publication restricted to loopback.
//
// Spec: specifications/cloud-api-binding.md (CAB-R001..R002).

// cloudBridgeTemplates are the bridge-networking deployment descriptors whose
// in-container bind must be the container interface, with a loopback host
// publish. (AWS uses host networking and is intentionally excluded.)
var cloudBridgeTemplates = []string{
	"deploy/azure/terraform/main.tf",
	"deploy/gcp/terraform/main.tf",
	"deploy/azure/bicep/main.bicep",
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// CAB-R001 — the daemon binds the container interface (0.0.0.0:8081) and never
// the container's loopback, so the host publish can reach it.
func TestCloudTemplates_BindContainerInterface(t *testing.T) {
	for _, tmpl := range cloudBridgeTemplates {
		body := readRepoFile(t, tmpl)
		if !strings.Contains(body, `listen_addr: "0.0.0.0:8081"`) {
			t.Errorf("%s: daemon must bind the container interface (0.0.0.0:8081) under bridge networking (#719)", tmpl)
		}
		if strings.Contains(body, `listen_addr: "127.0.0.1:8081"`) {
			t.Errorf("%s: a 127.0.0.1-inside bind is unreachable from the host bridge publish (#719)", tmpl)
		}
	}
}

// CAB-R002 — the host publication stays restricted to loopback so the API is
// not exposed to other hosts.
func TestCloudTemplates_HostPublishStaysLoopback(t *testing.T) {
	for _, tmpl := range cloudBridgeTemplates {
		body := readRepoFile(t, tmpl)
		if !strings.Contains(body, "127.0.0.1:8081:8081") {
			t.Errorf("%s: host publish must stay loopback-restricted (-p 127.0.0.1:8081:8081) (#719)", tmpl)
		}
		// A bare/all-interfaces host publish would expose the API externally.
		if strings.Contains(body, "0.0.0.0:8081:8081") {
			t.Errorf("%s: host publish must not bind all interfaces (0.0.0.0:8081:8081) — external exposure (#719)", tmpl)
		}
	}
}
