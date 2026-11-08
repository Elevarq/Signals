package config

import "testing"

// #722 / F16: RequiresCredentialResolver decides which auth methods need the
// runtime credential resolver (cloud-identity mint/fetch + mtls cert) vs the
// static password-by-reference DSN path.
func TestRequiresCredentialResolver(t *testing.T) {
	cases := []struct {
		method string
		want   bool
	}{
		{"", false},                  // default == password
		{AuthMethodPassword, false},  // static DSN (actionable errors)
		{AuthMethodAWSRDSIAM, true},  // mints an IAM token
		{AuthMethodAzureEntra, true}, // mints an Entra token
		{AuthMethodGCPCloudSQLIAM, true},
		{AuthMethodSecretStore, true}, // fetches from a vault
		{AuthMethodMTLS, true},        // loads a client certificate
	}
	for _, c := range cases {
		got := TargetConfig{AuthMethod: c.method}.RequiresCredentialResolver()
		if got != c.want {
			t.Errorf("RequiresCredentialResolver(%q) = %v, want %v", c.method, got, c.want)
		}
	}
}
