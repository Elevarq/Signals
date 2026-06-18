package guidedconnect

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/config"
	"github.com/elevarq/arq-signals/internal/conntest"
)

// Options carries the parsed `connect --auto` inputs.
type Options struct {
	Host   string
	Port   int
	DBName string
	User   string

	// AuthMethod, when non-empty, overrides detection. Must be one of
	// config.SupportedAuthMethods.
	AuthMethod string

	// Method-specific, non-secret fields mirroring the per-target config.
	Region                       string
	AzureClientID                string
	GCPImpersonateServiceAccount string
	SecretRef                    string
	SSLCert                      string
	SSLKey                       string
	SSLRootCert                  string

	// Name is the target identifier for the emitted block. Empty derives
	// one from the host.
	Name string

	// WritePath, when non-empty, appends the verified (secret-free) block
	// to that config file. Empty is dry-run (print only).
	WritePath string

	// Password is an operator-provided password for the password fallback.
	// Never logged or persisted.
	Password string

	// PasswordPrompt, when non-nil, is called for the password fallback if
	// no Password was supplied — the CLI sets it only when stdin is a TTY,
	// implementing the "prompt only on a TTY" decision (resolved decision
	// 1). The returned value is never logged or persisted.
	PasswordPrompt func() (string, error)

	// Getenv is injected for detection; nil uses os.Getenv.
	Getenv func(string) string
}

// Deps holds the orchestrator's seams so unit tests run without cloud,
// network, or a live database (ARQ-SIGNALS-CONNECT-INV003 reuse; NFR001).
type Deps struct {
	// Detect proposes an auth_method from the environment + host. nil uses
	// the package Detect.
	Detect func(DetectInput) Detection
	// Resolver resolves credentials for the cloud / secret_store / mtls
	// methods. nil uses collector.NewCredentialResolver(nil).
	Resolver collector.CredentialResolver
	// Diagnose runs the connection + role-safety diagnostic over res. nil
	// uses conntest.TestConnectionWithResolver.
	Diagnose Diagnoser
}

// Diagnoser runs the connection diagnostic for one target over a resolver.
type Diagnoser func(ctx context.Context, tgt config.TargetConfig, res collector.CredentialResolver) conntest.Result

// Outcome is the structured result of a guided-connect run. Behavioral
// failures (bad role, missing grant, connection refused) are reported here
// with Success=false — not as a Go error. A non-nil error from Run is a
// usage or I/O failure (exit code 2).
type Outcome struct {
	Success   bool
	Method    string
	Detection Detection
	// Category is the conntest failure category on a failed run ("" on
	// success).
	Category string
	// Message is the human-readable summary or remediation.
	Message string
	// ConfigBlock is the secret-free YAML target block on success.
	ConfigBlock string
	// Wrote reports whether --write appended the block.
	Wrote bool
}

// UsageError marks an input/usage failure the CLI maps to exit code 2.
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// staticResolver returns a fixed credential. Used only for the password
// fallback so the single Diagnose path is preserved (INV004).
type staticResolver struct{ cred collector.Credential }

func (s staticResolver) Resolve(context.Context, config.TargetConfig) (collector.Credential, error) {
	return s.cred, nil
}

// Run executes the guided flow: detect -> resolve -> diagnose ->
// role-safety -> guidance -> emit. It mutates nothing unless
// opts.WritePath is set (NFR002).
func Run(ctx context.Context, opts Options, deps Deps) (Outcome, error) {
	if strings.TrimSpace(opts.User) == "" {
		return Outcome{}, &UsageError{Msg: "--user is required"}
	}
	if strings.TrimSpace(opts.Host) == "" {
		return Outcome{}, &UsageError{Msg: "--host is required"}
	}
	if opts.AuthMethod != "" && !supportedMethod(opts.AuthMethod) {
		return Outcome{}, &UsageError{Msg: fmt.Sprintf("unknown --auth-method %q (supported: %s)",
			opts.AuthMethod, strings.Join(config.SupportedAuthMethods, ", "))}
	}

	detect := deps.Detect
	if detect == nil {
		detect = Detect
	}
	diagnose := deps.Diagnose
	if diagnose == nil {
		diagnose = func(ctx context.Context, tgt config.TargetConfig, res collector.CredentialResolver) conntest.Result {
			return conntest.TestConnectionWithResolver(ctx, tgt, res, conntest.Options{})
		}
	}
	resolver := deps.Resolver
	if resolver == nil {
		resolver = collector.NewCredentialResolver(nil)
	}

	det := detect(DetectInput{
		Host:      opts.Host,
		SecretRef: opts.SecretRef,
		HasCert:   opts.SSLCert != "" || opts.SSLKey != "",
		Getenv:    opts.Getenv,
	})

	// Determine the method: explicit override always wins (detection is
	// advisory). Otherwise an ambiguous detection is reported, not guessed
	// (CONNECT-AC003 / FC001).
	method := opts.AuthMethod
	if method == "" {
		if det.Ambiguous {
			return Outcome{
				Method:    config.AuthMethodPassword,
				Detection: det,
				Category:  string(conntest.CategoryConfig),
				Message: "ambiguous environment — refusing to guess.\n" +
					strings.Join(det.Notes, "\n") +
					"\nRe-run with --auth-method <" + strings.Join(config.SupportedAuthMethods, "|") + ">.",
			}, nil
		}
		method = det.Method
	}

	name := opts.Name
	if name == "" {
		name = deriveName(opts.Host)
	}

	tgt := config.TargetConfig{
		Name:                         name,
		Host:                         opts.Host,
		Port:                         port(opts.Port),
		DBName:                       opts.DBName,
		User:                         opts.User,
		SSLMode:                      "verify-full", // INV005
		SSLRootCertFile:              opts.SSLRootCert,
		AuthMethod:                   method,
		Region:                       opts.Region,
		AzureClientID:                opts.AzureClientID,
		GCPImpersonateServiceAccount: opts.GCPImpersonateServiceAccount,
		SecretRef:                    opts.SecretRef,
		SSLCert:                      opts.SSLCert,
		SSLKey:                       opts.SSLKey,
		Enabled:                      true,
	}

	// Password fallback (resolved decision 1): non-interactive by default.
	// The CLI prompts on a TTY and passes opts.Password; without one we
	// report CONNECT-FC006 rather than dialing with no credential.
	if method == config.AuthMethodPassword {
		if opts.Password == "" && opts.PasswordPrompt != nil {
			pw, err := opts.PasswordPrompt()
			if err != nil {
				return Outcome{}, &UsageError{Msg: "reading password: " + err.Error()}
			}
			opts.Password = pw
		}
		if opts.Password == "" {
			return Outcome{
				Method:    method,
				Detection: det,
				Category:  string(conntest.CategoryConfig),
				Message: "no cloud identity and no credential source supplied (CONNECT-FC006).\n" +
					strings.Join(det.Notes, "\n") + "\n" +
					"Provide one of:\n" +
					"  - a cloud method (run on AWS/Azure/GCP with an ambient identity, or pass --auth-method)\n" +
					"  - client certificates: --sslcert <file> --sslkey <file> (mtls)\n" +
					"  - a secret reference: --secret-ref <arn|uri|resource>\n" +
					"  - run interactively to be prompted for a password",
			}, nil
		}
		resolver = staticResolver{cred: collector.Credential{Kind: collector.CredKindPassword, Password: opts.Password}}
	}

	result := diagnose(ctx, tgt, resolver)

	switch result.Category {
	case conntest.CategoryOK:
		block := renderTargetBlock(tgt)
		out := Outcome{
			Success:     true,
			Method:      method,
			Detection:   det,
			Message:     fmt.Sprintf("connected to %s as %s with %s (verify-full) and the role passed the read-only safety check.", tgt.ConnIdentity(), tgt.User, method),
			ConfigBlock: block,
		}
		if opts.WritePath != "" {
			if err := appendTarget(opts.WritePath, name, block); err != nil {
				return out, err
			}
			out.Wrote = true
		}
		return out, nil

	case conntest.CategoryRole:
		// CONNECT-FC005 / AC006: connected but over-privileged. No success
		// block is emitted.
		return Outcome{
			Method:    method,
			Detection: det,
			Category:  string(result.Category),
			Message:   "the role connected but failed the read-only safety check (CONNECT-FC005):\n" + result.Detail,
		}, nil

	case conntest.CategoryAuth:
		// CONNECT-FC004 / AC005: the login mapping is missing — print the
		// exact grant for the selected method.
		return Outcome{
			Method:    method,
			Detection: det,
			Category:  string(result.Category),
			Message:   GuidanceFor(method, tgt) + "\n\nDiagnostic: " + result.Detail,
		}, nil

	case conntest.CategoryPasswordResolve:
		// CONNECT-FC002: resolve (mint/fetch/load) failed. The detail is
		// already redacted by conntest.
		return Outcome{
			Method:    method,
			Detection: det,
			Category:  string(result.Category),
			Message:   "credential resolution failed (CONNECT-FC002): " + result.Detail + "\n\n" + GuidanceFor(method, tgt),
		}, nil

	default:
		// CONNECT-FC003: connection failure (dns/tcp/tls/startup/config).
		return Outcome{
			Method:    method,
			Detection: det,
			Category:  string(result.Category),
			Message:   fmt.Sprintf("connection failed (%s, CONNECT-FC003): %s", result.Category, result.Detail),
		}, nil
	}
}

// GuidanceFor returns the copy-pasteable operator remediation for the
// selected method, reusing the provider guidance functions
// (ARQ-SIGNALS-CONNECT-INV003).
func GuidanceFor(method string, tgt config.TargetConfig) string {
	switch method {
	case config.AuthMethodAWSRDSIAM:
		return collector.AWSGrantGuidance(tgt)
	case config.AuthMethodAzureEntra:
		return collector.AzureEntraGuidance(tgt)
	case config.AuthMethodGCPCloudSQLIAM:
		return collector.GCPCloudSQLGuidance(tgt)
	case config.AuthMethodSecretStore:
		return collector.SecretStoreGuidance(tgt)
	case config.AuthMethodMTLS:
		return collector.MTLSGuidance(tgt)
	default:
		return fmt.Sprintf(`password connection for target %q was rejected.
Verify the credential is correct and that pg_hba.conf permits this role from
the collector's host (a `+"`host ... md5/scram-sha-256`"+` line), then reload
the server (SELECT pg_reload_conf();).`, tgt.Name)
	}
}

// renderTargetBlock renders the secret-free YAML target block: a 2-space
// indented list item ready to paste under a `targets:` key. It never
// contains a credential (ARQ-SIGNALS-CONNECT-INV001) — only the method's
// non-secret fields.
func renderTargetBlock(tgt config.TargetConfig) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  - name: %s\n", tgt.Name)
	fmt.Fprintf(&b, "    host: %s\n", tgt.Host)
	fmt.Fprintf(&b, "    port: %d\n", tgt.Port)
	fmt.Fprintf(&b, "    dbname: %s\n", tgt.DBName)
	fmt.Fprintf(&b, "    user: %s\n", tgt.User)
	fmt.Fprintf(&b, "    auth_method: %s\n", tgt.AuthMethod)
	fmt.Fprintf(&b, "    sslmode: %s\n", tgt.SSLMode)
	switch tgt.AuthMethod {
	case config.AuthMethodAWSRDSIAM:
		if tgt.Region != "" {
			fmt.Fprintf(&b, "    region: %s\n", tgt.Region)
		}
	case config.AuthMethodAzureEntra:
		if tgt.AzureClientID != "" {
			fmt.Fprintf(&b, "    azure_client_id: %s\n", tgt.AzureClientID)
		}
	case config.AuthMethodGCPCloudSQLIAM:
		if tgt.GCPImpersonateServiceAccount != "" {
			fmt.Fprintf(&b, "    gcp_impersonate_service_account: %s\n", tgt.GCPImpersonateServiceAccount)
		}
	case config.AuthMethodSecretStore:
		if tgt.SecretRef != "" {
			fmt.Fprintf(&b, "    secret_ref: %s\n", tgt.SecretRef)
		}
	case config.AuthMethodMTLS:
		if tgt.SSLCert != "" {
			fmt.Fprintf(&b, "    sslcert: %s\n", tgt.SSLCert)
		}
		if tgt.SSLKey != "" {
			fmt.Fprintf(&b, "    sslkey: %s\n", tgt.SSLKey)
		}
	}
	if tgt.SSLRootCertFile != "" {
		fmt.Fprintf(&b, "    sslrootcert_file: %s\n", tgt.SSLRootCertFile)
	}
	fmt.Fprintf(&b, "    enabled: true\n")
	return b.String()
}

// appendTarget appends block to the targets: list of the config file at
// path, refusing to duplicate an existing target name (CONNECT-AC007). The
// write is atomic (temp file + rename) and the result is re-parsed to
// verify it still loads and contains the new target before it replaces the
// original.
func appendTarget(path, name, block string) error {
	existing, err := os.ReadFile(path)
	switch {
	case err == nil:
		names, perr := targetNames(existing)
		if perr != nil {
			return &UsageError{Msg: fmt.Sprintf("--write: %s is not valid YAML: %v", path, perr)}
		}
		for _, n := range names {
			if n == name {
				return &UsageError{Msg: fmt.Sprintf("--write: a target named %q already exists in %s; pass --name to choose a different identifier", name, path)}
			}
		}
	case os.IsNotExist(err):
		existing = nil
	default:
		return fmt.Errorf("--write: read %s: %w", path, err)
	}

	content := buildAppended(existing, block)

	// Verify the result parses and contains the new target before
	// replacing the original — never leave a broken config behind.
	names, perr := targetNames([]byte(content))
	if perr != nil {
		return fmt.Errorf("--write: appended config would not parse: %w", perr)
	}
	found := false
	for _, n := range names {
		if n == name {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("--write: appended target %q not found after merge; aborting", name)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".signalsctl-connect-*.yaml")
	if err != nil {
		return fmt.Errorf("--write: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return fmt.Errorf("--write: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("--write: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("--write: replace %s: %w", path, err)
	}
	return nil
}

// buildAppended returns existing with block appended under a targets: key,
// creating the key when absent.
func buildAppended(existing []byte, block string) string {
	if len(existing) == 0 {
		return "targets:\n" + block
	}
	s := string(existing)
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	if hasTopLevelKey(s, "targets") {
		return s + block
	}
	return s + "targets:\n" + block
}

// hasTopLevelKey reports whether s has a top-level YAML key (a line
// starting in column zero with `key:`).
func hasTopLevelKey(s, key string) bool {
	for _, line := range strings.Split(s, "\n") {
		if line == key+":" || strings.HasPrefix(line, key+":") && !strings.HasPrefix(line, " ") {
			return true
		}
	}
	return false
}

// targetNames parses just the target names from a config document.
func targetNames(data []byte) ([]string, error) {
	var doc struct {
		Targets []struct {
			Name string `yaml:"name"`
		} `yaml:"targets"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(doc.Targets))
	for _, t := range doc.Targets {
		names = append(names, t.Name)
	}
	return names, nil
}

func supportedMethod(m string) bool {
	for _, s := range config.SupportedAuthMethods {
		if s == m {
			return true
		}
	}
	return false
}

func port(p int) int {
	if p == 0 {
		return 5432
	}
	return p
}

// deriveName builds a target name from the host's first DNS label, falling
// back to the whole host. Cloud SQL connection names (project:region:instance)
// use the instance segment.
func deriveName(host string) string {
	h := strings.TrimSpace(host)
	if h == "" {
		return "target"
	}
	if parts := strings.Split(h, ":"); len(parts) == 3 {
		return parts[2]
	}
	if i := strings.IndexByte(h, '.'); i > 0 {
		return h[:i]
	}
	return h
}
