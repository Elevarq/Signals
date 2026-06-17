package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProdComposeBindsContainerWildcard pins the #206 fix: the
// production compose example MUST instruct the daemon to bind to
// the container's wildcard interface (or supply a mounted config
// that does so). Without this override Docker port publishing
// cannot reach the daemon, so the documented host-side curl /
// signalsctl flow silently fails.
//
// We accept either of two equivalent fixes per the issue body:
//
//  1. SIGNALS_LISTEN_ADDR=0.0.0.0:8081 set in the service
//     environment (current fix).
//  2. A signals.prod.yaml mount whose api.listen_addr resolves
//     to 0.0.0.0:8081 (future operator-customised variant).
//
// The static check is the cheapest regression: a future refactor
// that drops the env var (or otherwise reverts the binding) trips
// here instead of in a customer install.
func TestProdComposeBindsContainerWildcard(t *testing.T) {
	path := filepath.Join("..", "examples", "docker-compose.prod.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(data)
	wantEnv := `SIGNALS_LISTEN_ADDR: "0.0.0.0:8081"`
	wantConfigMarker := "api.listen_addr"
	if !strings.Contains(body, wantEnv) &&
		!strings.Contains(body, wantConfigMarker) {
		t.Errorf("examples/docker-compose.prod.yml MUST publish a "+
			"container-wildcard listen address. Expected either env "+
			"override %q or an api.listen_addr override mounted via "+
			"signals.prod.yaml.\nFile contents:\n%s", wantEnv, body)
	}
}

// TestProdComposeHostBindingRemainsLoopback pins the host-facing
// safety boundary: the operator-facing port publish MUST stay
// pinned to 127.0.0.1 even though the container-side bind is now
// a wildcard. A future edit that drops the loopback prefix would
// expose the API publicly and trip this test.
func TestProdComposeHostBindingRemainsLoopback(t *testing.T) {
	path := filepath.Join("..", "examples", "docker-compose.prod.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(data)
	if !strings.Contains(body, `"127.0.0.1:8081:8081"`) {
		t.Errorf("examples/docker-compose.prod.yml MUST publish the "+
			"API only on host loopback. Expected `127.0.0.1:8081:8081` "+
			"in the ports: block.\nFile contents:\n%s", body)
	}
	if strings.Contains(body, `"0.0.0.0:8081:8081"`) ||
		strings.Contains(body, `"8081:8081"`) &&
			!strings.Contains(body, `"127.0.0.1:8081:8081"`) {
		t.Errorf("examples/docker-compose.prod.yml MUST NOT publish "+
			"the API on the host wildcard interface.\nFile contents:\n%s", body)
	}
}
