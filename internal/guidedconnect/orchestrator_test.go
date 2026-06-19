package guidedconnect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/collector"
	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/conntest"
)

// capture records what the orchestrator handed the diagnostic seam.
type capture struct {
	tgt config.TargetConfig
	res collector.CredentialResolver
}

// fakeDiagnoser returns a Diagnoser that records the target/resolver and
// returns a fixed conntest category + detail — no DB, no network (NFR001).
func fakeDiagnoser(cap *capture, cat conntest.Category, detail string) Diagnoser {
	return func(_ context.Context, tgt config.TargetConfig, res collector.CredentialResolver) conntest.Result {
		cap.tgt = tgt
		cap.res = res
		return conntest.Result{
			Target: tgt.Name, Host: tgt.Host, Port: tgt.Port,
			DBName: tgt.DBName, Username: tgt.User,
			Category: cat, Detail: detail,
		}
	}
}

// TestRun_HappyPath_SecretFreeBlock covers CONNECT-AC001: a detectable
// cloud identity + a passing role yields a ready config block with no
// secret, verify-full, and the detected method.
func TestRun_HappyPath_SecretFreeBlock(t *testing.T) {
	var cap capture
	out, err := Run(context.Background(), Options{
		Host:   "orders.abc123.us-east-1.rds.amazonaws.com",
		DBName: "orders",
		User:   "signals",
		Region: "us-east-1",
		Getenv: envFunc(map[string]string{"AWS_ACCESS_KEY_ID": "AKIA..."}),
	}, Deps{Diagnose: fakeDiagnoser(&cap, conntest.CategoryOK, "")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Success {
		t.Fatalf("want success, got: %+v", out)
	}
	if out.Method != config.AuthMethodAWSRDSIAM {
		t.Fatalf("method = %q, want aws_rds_iam", out.Method)
	}
	if cap.tgt.SSLMode != "verify-full" {
		t.Fatalf("diagnostic target sslmode = %q, want verify-full (INV005)", cap.tgt.SSLMode)
	}
	for _, want := range []string{
		"name: orders", "host: orders.abc123.us-east-1.rds.amazonaws.com",
		"auth_method: aws_rds_iam", "sslmode: verify-full", "region: us-east-1",
		"enabled: true",
	} {
		if !strings.Contains(out.ConfigBlock, want) {
			t.Errorf("config block missing %q:\n%s", want, out.ConfigBlock)
		}
	}
	if strings.Contains(out.ConfigBlock, "password") || strings.Contains(out.ConfigBlock, "secret:") {
		t.Errorf("config block must contain no credential:\n%s", out.ConfigBlock)
	}
}

// TestRun_NoSecretEverPrinted covers CONNECT-AC004 / INV001: the supplied
// password never appears in any output field, on success or failure.
func TestRun_NoSecretEverPrinted(t *testing.T) {
	const secret = "sup3r-s3cr3t-token-value"
	cats := []conntest.Category{conntest.CategoryOK, conntest.CategoryAuth, conntest.CategoryPasswordResolve, conntest.CategoryRole}
	for _, cat := range cats {
		var cap capture
		out, err := Run(context.Background(), Options{
			Host:       "db.internal",
			DBName:     "app",
			User:       "signals",
			AuthMethod: config.AuthMethodPassword,
			Password:   secret,
			Getenv:     envFunc(nil),
		}, Deps{Diagnose: fakeDiagnoser(&cap, cat, "SQLSTATE 28P01: password authentication failed")})
		if err != nil {
			t.Fatalf("cat %s: unexpected error: %v", cat, err)
		}
		for _, field := range []string{out.Message, out.ConfigBlock} {
			if strings.Contains(field, secret) {
				t.Errorf("cat %s: secret leaked into output: %q", cat, field)
			}
		}
	}
}

// TestRun_MissingGrantGuidance covers CONNECT-AC005 / FC004: an auth
// rejection prints the exact, copy-pasteable grant for the selected method.
func TestRun_MissingGrantGuidance(t *testing.T) {
	cases := []struct {
		method string
		opts   Options
		want   string
	}{
		{config.AuthMethodAWSRDSIAM, Options{Region: "us-east-1"}, "GRANT rds_iam"},
		{config.AuthMethodAzureEntra, Options{AzureClientID: "guid"}, "pgaadauth_create_principal"},
		{config.AuthMethodGCPCloudSQLIAM, Options{}, "gcloud sql users create"},
		{config.AuthMethodSecretStore, Options{SecretRef: "arn:aws:secretsmanager:us-east-1:1:secret:db"}, "secretsmanager:GetSecretValue"},
		{config.AuthMethodMTLS, Options{SSLCert: "/c.pem", SSLKey: "/k.pem"}, "hostssl"},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			var cap capture
			o := tc.opts
			o.Host = "db.example.com"
			o.DBName = "app"
			o.User = "signals"
			o.AuthMethod = tc.method
			o.Getenv = envFunc(nil)
			out, err := Run(context.Background(), o, Deps{Diagnose: fakeDiagnoser(&cap, conntest.CategoryAuth, "SQLSTATE 28000")})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Success {
				t.Fatal("auth failure must not be a success")
			}
			if !strings.Contains(out.Message, tc.want) {
				t.Errorf("guidance missing %q:\n%s", tc.want, out.Message)
			}
			if out.ConfigBlock != "" {
				t.Error("no config block on a failure path")
			}
		})
	}
}

// TestRun_OverPrivilegedRole covers CONNECT-AC006 / FC005: a role that
// connects but fails the read-only safety check is reported with no
// success block.
func TestRun_OverPrivilegedRole(t *testing.T) {
	var cap capture
	const detail = "role \"admin\" has superuser attribute (rolsuper=true)"
	out, err := Run(context.Background(), Options{
		Host: "db.example.com", DBName: "app", User: "admin",
		AuthMethod: config.AuthMethodPassword, Password: "pw",
		Getenv: envFunc(nil),
	}, Deps{Diagnose: fakeDiagnoser(&cap, conntest.CategoryRole, detail)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Success {
		t.Fatal("over-privileged role must not succeed")
	}
	if out.ConfigBlock != "" {
		t.Error("no success block for an over-privileged role")
	}
	if !strings.Contains(out.Message, detail) {
		t.Errorf("message must report the failed check:\n%s", out.Message)
	}
}

// TestRun_DryRunWritesNothing covers CONNECT-AC007 / NFR002 (dry-run half).
func TestRun_DryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	const original = "env: prod\ntargets:\n  - name: existing\n    host: h\n    port: 5432\n    dbname: d\n    user: u\n    enabled: true\n"
	if err := os.WriteFile(cfg, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	var cap capture
	out, err := Run(context.Background(), Options{
		Host: "new.example.com", DBName: "app", User: "signals",
		AuthMethod: config.AuthMethodPassword, Password: "pw",
		Getenv: envFunc(nil), // WritePath empty -> dry-run
	}, Deps{Diagnose: fakeDiagnoser(&cap, conntest.CategoryOK, "")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Wrote {
		t.Error("dry-run must not write")
	}
	got, _ := os.ReadFile(cfg)
	if string(got) != original {
		t.Errorf("dry-run mutated the config file:\n%s", got)
	}
}

// TestRun_WriteAppendsBlockAndRefusesDuplicate covers CONNECT-AC007
// (--write half): the block is appended once, and a duplicate name is
// refused.
func TestRun_WriteAppendsBlockAndRefusesDuplicate(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	const original = "env: prod\ntargets:\n  - name: existing\n    host: h\n    port: 5432\n    dbname: d\n    user: u\n    enabled: true\n"
	if err := os.WriteFile(cfg, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := Options{
		Host: "new.example.com", DBName: "app", User: "signals",
		Name: "newtarget", AuthMethod: config.AuthMethodPassword, Password: "pw",
		WritePath: cfg, Getenv: envFunc(nil),
	}
	var cap capture
	deps := Deps{Diagnose: fakeDiagnoser(&cap, conntest.CategoryOK, "")}

	out, err := Run(context.Background(), opts, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Wrote {
		t.Fatal("expected --write to append")
	}
	got, _ := os.ReadFile(cfg)
	if !strings.Contains(string(got), "name: newtarget") || !strings.Contains(string(got), "name: existing") {
		t.Fatalf("file should contain both targets:\n%s", got)
	}
	if strings.Contains(string(got), "pw") {
		t.Errorf("written config must not contain the password:\n%s", got)
	}
	// Verify it still parses as a config with two targets.
	loaded, err := config.Load(cfg)
	if err != nil {
		t.Fatalf("written config no longer loads: %v", err)
	}
	if len(loaded.Targets) != 2 {
		t.Fatalf("want 2 targets after append, got %d", len(loaded.Targets))
	}

	// Re-running with the same name must be refused.
	_, err = Run(context.Background(), opts, deps)
	if err == nil {
		t.Fatal("expected duplicate-name refusal")
	}
	var ue *UsageError
	if !asUsageError(err, &ue) {
		t.Fatalf("want *UsageError, got %T: %v", err, err)
	}
}

// TestRun_WriteCreatesTargetsKey covers --write to a file with no targets:
// key — the key is created.
func TestRun_WriteCreatesTargetsKey(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte("env: prod\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var cap capture
	out, err := Run(context.Background(), Options{
		Host: "new.example.com", DBName: "app", User: "signals",
		Name: "t1", AuthMethod: config.AuthMethodPassword, Password: "pw",
		WritePath: cfg, Getenv: envFunc(nil),
	}, Deps{Diagnose: fakeDiagnoser(&cap, conntest.CategoryOK, "")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Wrote {
		t.Fatal("expected write")
	}
	loaded, err := config.Load(cfg)
	if err != nil {
		t.Fatalf("written config does not load: %v", err)
	}
	if len(loaded.Targets) != 1 || loaded.Targets[0].Name != "t1" {
		t.Fatalf("want one target t1, got %+v", loaded.Targets)
	}
}

// TestRun_CoversAllMethods covers CONNECT-AC008: every auth_method can be
// selected and emits a verified, secret-free block with the right method.
func TestRun_CoversAllMethods(t *testing.T) {
	base := func(m string) Options {
		o := Options{Host: "db.example.com", DBName: "app", User: "signals", AuthMethod: m, Getenv: envFunc(nil)}
		switch m {
		case config.AuthMethodPassword:
			o.Password = "pw"
		case config.AuthMethodSecretStore:
			o.SecretRef = "arn:aws:secretsmanager:us-east-1:1:secret:db"
		case config.AuthMethodMTLS:
			o.SSLCert, o.SSLKey = "/c.pem", "/k.pem"
		case config.AuthMethodAWSRDSIAM:
			o.Region = "us-east-1"
		}
		return o
	}
	for _, m := range config.SupportedAuthMethods {
		t.Run(m, func(t *testing.T) {
			var cap capture
			out, err := Run(context.Background(), base(m), Deps{Diagnose: fakeDiagnoser(&cap, conntest.CategoryOK, "")})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !out.Success {
				t.Fatalf("method %s: want success, got %+v", m, out)
			}
			if !strings.Contains(out.ConfigBlock, "auth_method: "+m) {
				t.Errorf("method %s: block missing auth_method:\n%s", m, out.ConfigBlock)
			}
		})
	}
}

// TestRun_AuthMethodOverridesDetection covers the AC002 override clause:
// --auth-method always beats detection.
func TestRun_AuthMethodOverridesDetection(t *testing.T) {
	var cap capture
	// Environment + host would detect aws_rds_iam; the override wins.
	_, err := Run(context.Background(), Options{
		Host: "orders.abc123.us-east-1.rds.amazonaws.com", DBName: "orders", User: "signals",
		AuthMethod: config.AuthMethodAzureEntra, AzureClientID: "guid",
		Getenv: envFunc(map[string]string{"AWS_ACCESS_KEY_ID": "AKIA..."}),
	}, Deps{Diagnose: fakeDiagnoser(&cap, conntest.CategoryOK, "")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.tgt.AuthMethod != config.AuthMethodAzureEntra {
		t.Fatalf("override ignored: diagnostic used %q, want azure_entra", cap.tgt.AuthMethod)
	}
}

// TestRun_AmbiguousReported covers CONNECT-AC003 at the orchestrator level:
// an ambiguous environment with no override is reported, not guessed.
func TestRun_AmbiguousReported(t *testing.T) {
	called := false
	out, err := Run(context.Background(), Options{
		Host: "db.internal", DBName: "app", User: "signals",
		Getenv: envFunc(map[string]string{"AWS_ACCESS_KEY_ID": "AKIA...", "AZURE_CLIENT_ID": "guid"}),
	}, Deps{Diagnose: func(_ context.Context, _ config.TargetConfig, _ collector.CredentialResolver) conntest.Result {
		called = true
		return conntest.Result{Category: conntest.CategoryOK}
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("must not attempt to connect on an ambiguous detection")
	}
	if out.Success || !strings.Contains(strings.ToLower(out.Message), "ambiguous") {
		t.Fatalf("want ambiguous report, got: %+v", out)
	}
}

// TestRun_PasswordFallbackNoSource covers CONNECT-FC006: password method
// with no credential source and no TTY prompt is reported, not dialed.
func TestRun_PasswordFallbackNoSource(t *testing.T) {
	called := false
	out, err := Run(context.Background(), Options{
		Host: "db.internal", DBName: "app", User: "signals",
		AuthMethod: config.AuthMethodPassword, Getenv: envFunc(nil),
	}, Deps{Diagnose: func(_ context.Context, _ config.TargetConfig, _ collector.CredentialResolver) conntest.Result {
		called = true
		return conntest.Result{Category: conntest.CategoryOK}
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("must not dial without a credential source")
	}
	if out.Success || !strings.Contains(out.Message, "CONNECT-FC006") {
		t.Fatalf("want FC006 report, got: %+v", out)
	}
}

// TestRun_RequiresUserAndHost covers input validation (usage errors).
func TestRun_RequiresUserAndHost(t *testing.T) {
	if _, err := Run(context.Background(), Options{Host: "h"}, Deps{}); err == nil {
		t.Error("missing --user should be a usage error")
	}
	if _, err := Run(context.Background(), Options{User: "u"}, Deps{}); err == nil {
		t.Error("missing --host should be a usage error")
	}
	if _, err := Run(context.Background(), Options{Host: "h", User: "u", AuthMethod: "bogus"}, Deps{}); err == nil {
		t.Error("unknown --auth-method should be a usage error")
	}
}

// asUsageError reports whether err is a *UsageError (errors.As wrapper kept
// local to avoid importing errors in every test).
func asUsageError(err error, target **UsageError) bool {
	for err != nil {
		if ue, ok := err.(*UsageError); ok {
			*target = ue
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
