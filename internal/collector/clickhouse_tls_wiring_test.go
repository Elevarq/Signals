package collector

import (
	"testing"
	"time"

	"github.com/elevarq/signals/internal/config"
)

// TC-CCH-07 (#558, CCH-R005): the ClickHouse session passes the target's sslmode
// + root CA through to the native connector config, so a verify-ca/verify-full
// target is actually verified instead of silently connecting in plaintext. This
// guards the exact F1 regression (TLS config dropped on the way to the driver).
func TestClickHouseConnConfigCarriesTLS(t *testing.T) {
	tgt := config.TargetConfig{
		Name:            "ch",
		Engine:          config.EngineClickHouse,
		Host:            "ch.example",
		Port:            9440,
		User:            "signals_ro",
		DBName:          "default",
		SSLMode:         "verify-full",
		SSLRootCertFile: "/etc/signals/ca.pem",
	}
	cc := clickHouseConnConfig(tgt, "pw", 7*time.Second)

	if cc.TLSMode != "verify-full" {
		t.Errorf("TLSMode = %q, want verify-full (sslmode must reach the driver)", cc.TLSMode)
	}
	if cc.TLSRootCAFile != "/etc/signals/ca.pem" {
		t.Errorf("TLSRootCAFile = %q, want the target's SSLRootCertFile", cc.TLSRootCAFile)
	}
	// Non-TLS fields still wired correctly.
	if cc.Host != "ch.example" || cc.Port != 9440 || cc.User != "signals_ro" || cc.DBName != "default" || cc.Password != "pw" || cc.QueryTimeout != 7*time.Second {
		t.Errorf("connection fields mis-wired: %+v", cc)
	}
}
