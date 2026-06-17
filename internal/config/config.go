package config

import (
	"crypto/subtle"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for Arq Signals.
type Config struct {
	Env                string         `yaml:"env"` // "dev" (default), "lab", "prod"
	AllowInsecurePgTLS bool           `yaml:"-"`   // env-only via ARQ_ALLOW_INSECURE_PG_TLS
	AllowUnsafeRole    bool           `yaml:"-"`   // env-only via ARQ_SIGNALS_ALLOW_UNSAFE_ROLE
	Signals            SignalsConfig  `yaml:"signals"`
	Targets            []TargetConfig `yaml:"targets"`
	API                APIConfig      `yaml:"api"`
	Database           DatabaseConfig `yaml:"database"`
}

type SignalsConfig struct {
	PollInterval  time.Duration `yaml:"-"`
	PollIntervalS string        `yaml:"poll_interval"` // e.g. "5m"
	// RetentionDays is the legacy flat retention setting — same
	// cutoff for every retention class. R099 adds the structured
	// `Retention` block below; the two are mutually exclusive
	// (FC-21). When `Retention` is set, RetentionDays is ignored.
	RetentionDays                    int             `yaml:"retention_days"`
	Retention                        RetentionConfig `yaml:"retention"`
	LogLevel                         string          `yaml:"log_level"`
	LogJSON                          bool            `yaml:"log_json"`
	MaxConcurrentTargets             int             `yaml:"max_concurrent_targets"`
	TargetTimeout                    time.Duration   `yaml:"-"`
	TargetTimeoutS                   string          `yaml:"target_timeout"`
	QueryTimeout                     time.Duration   `yaml:"-"`
	QueryTimeoutS                    string          `yaml:"query_timeout"`
	HighSensitivityCollectorsEnabled bool            `yaml:"high_sensitivity_collectors_enabled"`
	// CollectArrayRangeHistograms (#128) is the per-collector
	// opt-in for pg_stats_array_range_v1. Layered ON TOP of
	// HighSensitivityCollectorsEnabled — both must be true for the
	// collector to run. Operators who want the standard MCV /
	// histogram collector but NOT the per-element array / range
	// data set this to false while leaving the daemon-wide flag
	// true. Env: ARQ_SIGNALS_COLLECT_ARRAY_RANGE_HISTOGRAMS.
	CollectArrayRangeHistograms bool   `yaml:"collect_array_range_histograms"`
	MetricsEnabled              bool   `yaml:"metrics_enabled"`
	MetricsPath                 string `yaml:"metrics_path"`
	// R091: minimum interval between completed snapshots for the
	// same logical target. Default 60s. Env override
	// ARQ_SIGNALS_MIN_SNAPSHOT_INTERVAL accepts the same time-string
	// format as `poll_interval`. FC-10 rejects zero/negative.
	MinSnapshotInterval  time.Duration `yaml:"-"`
	MinSnapshotIntervalS string        `yaml:"min_snapshot_interval"`
	// R083: Mode B opt-in. "standalone" (default) keeps Phase 2
	// behaviour byte-for-byte. "arq_managed" activates the
	// arq_control_plane_token check.
	Mode                     string `yaml:"mode"`
	ArqControlPlaneTokenFile string `yaml:"arq_control_plane_token_file"`
	ArqControlPlaneTokenEnv  string `yaml:"arq_control_plane_token_env"`

	// R097: per-target circuit-breaker thresholds.
	Circuit CircuitConfig `yaml:"circuit"`

	// R080: opt-in per-collector export view. Adds a derivative
	// `per-collector/<query_id>.json` directory to the export ZIP.
	// Default off — canonical NDJSON layout is unaffected.
	ExportPerCollectorFiles bool `yaml:"export_per_collector_files"`
}

// CircuitConfig holds the per-target circuit-breaker thresholds
// surfaced under `signals.circuit` in YAML. Zero values fall back
// to the documented defaults (3 consecutive failures, 5 min cooldown).
type CircuitConfig struct {
	FailThreshold int           `yaml:"fail_threshold"`
	OpenCooldown  time.Duration `yaml:"-"`
	OpenCooldownS string        `yaml:"open_cooldown"`
}

// RetentionConfig (R099) carries per-class retention thresholds.
// Zero values fall back to the flat `retention_days` setting, so a
// partial structured block (e.g. only LongDays) still inherits sane
// defaults for the other classes.
type RetentionConfig struct {
	ShortDays  int `yaml:"short_days"`
	MediumDays int `yaml:"medium_days"`
	LongDays   int `yaml:"long_days"`
}

// IsSet returns true when the operator supplied at least one
// retention class explicitly.
func (r RetentionConfig) IsSet() bool {
	return r.ShortDays > 0 || r.MediumDays > 0 || r.LongDays > 0
}

// DaysFor returns the configured day count for a retention class.
// `class` is the `pgqueries.RetentionClass` enum surface value
// ("short", "medium", "long"). Falls back to `defaultDays` when the
// class has no explicit value.
func (r RetentionConfig) DaysFor(class string, defaultDays int) int {
	switch class {
	case "short":
		if r.ShortDays > 0 {
			return r.ShortDays
		}
	case "medium":
		if r.MediumDays > 0 {
			return r.MediumDays
		}
	case "long":
		if r.LongDays > 0 {
			return r.LongDays
		}
	}
	return defaultDays
}

// MaxDays returns the largest configured retention so the snapshot-
// row pruner uses an envelope that doesn't drop snapshots whose
// long-class runs are still within retention.
func (r RetentionConfig) MaxDays(defaultDays int) int {
	out := defaultDays
	for _, v := range []int{r.ShortDays, r.MediumDays, r.LongDays} {
		if v > out {
			out = v
		}
	}
	return out
}

// R083 mode values.
const (
	ModeStandalone = "standalone"
	ModeArqManaged = "arq_managed"
)

// MinArqControlPlaneTokenLength is the floor for the R083 control-
// plane token. 32 chars matches the doc-stated minimum and is
// sufficient entropy for HMAC-equivalent strength.
const MinArqControlPlaneTokenLength = 32

type TargetConfig struct {
	Name            string `yaml:"name"`
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	DBName          string `yaml:"dbname"`
	User            string `yaml:"user"`
	SSLMode         string `yaml:"sslmode"`
	SSLRootCertFile string `yaml:"sslrootcert_file"`
	PasswordFile    string `yaml:"password_file"`
	PasswordEnv     string `yaml:"password_env"`
	PgpassFile      string `yaml:"pgpass_file"`
	Enabled         bool   `yaml:"enabled"`

	// AuthMethod selects the credential provider for this target
	// (credential-providers.md, #93). Empty means the default
	// password provider, preserving existing behavior. Non-empty
	// values must appear in SupportedAuthMethods or ValidateStrict
	// rejects the config (keystone FC001).
	AuthMethod string `yaml:"auth_method"`

	// Region is the AWS region of the target instance, consumed only
	// by the aws_rds_iam provider (ARQ-SIGNALS-AUTH-AWS-, #94). Optional;
	// when empty the region is resolved from the environment / instance
	// metadata at connect time. See the AWS spec's region-resolution
	// decision.
	Region string `yaml:"region"`

	// AzureClientID is the client (application) ID of a user-assigned
	// managed identity, consumed only by the azure_entra provider
	// (ARQ-SIGNALS-AUTH-AZURE-, #95). Optional; it disambiguates which
	// identity to use when a host carries more than one. When empty the
	// AZURE_CLIENT_ID environment variable is consulted, and failing that
	// the credential chain selects the system-assigned / single identity.
	// Not a secret: a client id is a public GUID.
	AzureClientID string `yaml:"azure_client_id"`

	// GCPImpersonateServiceAccount is the email of a Google service
	// account the collector should impersonate when minting Cloud SQL IAM
	// access tokens, consumed only by the gcp_cloudsql_iam provider
	// (ARQ-SIGNALS-AUTH-GCP-, #96). Optional; when empty the ambient
	// Application Default Credentials identity is used directly. Not a
	// secret: a service-account email is a public identifier.
	GCPImpersonateServiceAccount string `yaml:"gcp_impersonate_service_account"`

	// SecretRef is the cloud secret-store reference for the secret_store
	// provider (ARQ-SIGNALS-AUTH-SECRET-, #97): an AWS Secrets Manager ARN,
	// an Azure Key Vault secret URI, or a GCP Secret Manager resource name.
	// Its shape selects the backend (InferSecretBackend). Required when
	// auth_method is secret_store (FC-SECRET-007). Not a secret: the
	// reference names, but does not contain, the credential.
	SecretRef string `yaml:"secret_ref"`

	// SecretJSONKey, when set, parses the fetched secret as a JSON object
	// and uses this key's string value as the password (e.g. "password" for
	// an AWS RDS-managed secret {"username":…,"password":…}). When empty the
	// fetched value is used verbatim. Consumed only by the secret_store
	// provider (#97).
	SecretJSONKey string `yaml:"secret_json_key"`

	// MaxCacheTTL bounds how long a fetched secret may be reused between
	// reconnects when the vault supplies no TTL/lease of its own
	// (ARQ-SIGNALS-AUTH-SECRET-INV003). Optional; zero with no vault TTL
	// means re-fetch on every reconnect. MaxCacheTTLS is the YAML duration
	// string folded into MaxCacheTTL by parseDurations.
	MaxCacheTTL  time.Duration `yaml:"-"`
	MaxCacheTTLS string        `yaml:"max_cache_ttl"`

	// R098: per-target sensitivity profile. Empty / absent means
	// "inherit daemon-wide". See specifications/sensitivity-profiles.md.
	Collectors TargetCollectorConfig `yaml:"collectors"`
}

// TargetCollectorConfig carries the per-target sensitivity profile
// (R098). Empty value is equivalent to `profile: default`.
type TargetCollectorConfig struct {
	Profile string   `yaml:"profile"`
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

// R098 profile values.
const (
	ProfileDefault    = "default"
	ProfileRestricted = "restricted"
	ProfileCustom     = "custom"
)

// SupportedProfiles enumerates every valid value of
// TargetCollectorConfig.Profile (plus the empty default).
var SupportedProfiles = []string{"", ProfileDefault, ProfileRestricted, ProfileCustom}

// UnmarshalYAML decodes a TargetConfig with Enabled defaulting to true.
// Without this, an omitted `enabled:` key would deserialize to the zero
// value (false), silently disabling targets in configs that don't mention
// the field. Operators must use explicit `enabled: false` to disable a
// target.
func (t *TargetConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawTarget TargetConfig
	raw := rawTarget{Enabled: true}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*t = TargetConfig(raw)
	return nil
}

// SecretType returns the credential source type for display/storage.
func (t TargetConfig) SecretType() string {
	switch {
	case t.PasswordFile != "":
		return "FILE"
	case t.PasswordEnv != "":
		return "ENV"
	case t.PgpassFile != "":
		return "PGPASS"
	default:
		return "NONE"
	}
}

// CredentialSourceRef returns the non-secret reference for the credential
// source (the password_file / password_env / pgpass_file path). It is
// distinct from the secret_store SecretRef field, which names a cloud
// secret-store reference.
func (t TargetConfig) CredentialSourceRef() string {
	switch {
	case t.PasswordFile != "":
		return t.PasswordFile
	case t.PasswordEnv != "":
		return t.PasswordEnv
	case t.PgpassFile != "":
		return t.PgpassFile
	default:
		return ""
	}
}

// ConnIdentity returns a stable string identifying the target's connection
// (host:port/dbname@user) for hashing purposes, without any secrets.
func (t TargetConfig) ConnIdentity() string {
	port := t.Port
	if port == 0 {
		port = 5432
	}
	return fmt.Sprintf("%s:%d/%s@%s", t.Host, port, t.DBName, t.User)
}

// Auth-method values (credential-providers.md, #93). Only the methods
// this build implements appear in SupportedAuthMethods; methods named in
// the keystone but not yet built (e.g. gcp_cloudsql_iam) are rejected by
// ValidateStrict with an actionable error (keystone FC001).
const (
	AuthMethodPassword       = "password"
	AuthMethodAWSRDSIAM      = "aws_rds_iam"
	AuthMethodAzureEntra     = "azure_entra"
	AuthMethodGCPCloudSQLIAM = "gcp_cloudsql_iam"
	AuthMethodSecretStore    = "secret_store"
)

// SupportedAuthMethods enumerates every auth_method this build can serve.
// The empty value is equivalent to AuthMethodPassword (see
// EffectiveAuthMethod) and is always accepted.
var SupportedAuthMethods = []string{AuthMethodPassword, AuthMethodAWSRDSIAM, AuthMethodAzureEntra, AuthMethodGCPCloudSQLIAM, AuthMethodSecretStore}

// EffectiveAuthMethod returns the target's auth_method, defaulting an
// empty value to AuthMethodPassword so existing configs keep their
// current behavior (keystone NFR003).
func (t TargetConfig) EffectiveAuthMethod() string {
	if t.AuthMethod == "" {
		return AuthMethodPassword
	}
	return t.AuthMethod
}

type APIConfig struct {
	ListenAddr    string        `yaml:"listen_addr"`
	ReadTimeout   time.Duration `yaml:"-"`
	ReadTimeoutS  string        `yaml:"read_timeout"`
	WriteTimeout  time.Duration `yaml:"-"`
	WriteTimeoutS string        `yaml:"write_timeout"`
	// Token / TokenFile are operator-supplied YAML inputs. Both
	// optional. TokenFile, when set, references a file containing the
	// token (matches the _FILE convention). At Load time the file is
	// read and folded into APIToken; ENV overrides (ARQ_SIGNALS_API_TOKEN
	// and ARQ_SIGNALS_API_TOKEN_FILE) apply on top. The resolved value
	// lives on APIToken; downstream consumers read APIToken only.
	Token     string `yaml:"token"`
	TokenFile string `yaml:"token_file"`
	APIToken  string `yaml:"-"`

	// TLS (R113). Optional daemon-terminated TLS for the HTTP API.
	// All-or-nothing: set both to serve HTTPS, neither to serve plain
	// HTTP (the loopback default). Setting exactly one is a hard
	// config error. Env overrides: ARQ_SIGNALS_API_TLS_CERT_FILE /
	// ARQ_SIGNALS_API_TLS_KEY_FILE.
	TLSCertFile string `yaml:"tls_cert_file"`
	TLSKeyFile  string `yaml:"tls_key_file"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
	WAL  bool   `yaml:"wal"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Env: "dev",
		Signals: SignalsConfig{
			PollInterval:         5 * time.Minute,
			PollIntervalS:        "5m",
			RetentionDays:        30,
			LogLevel:             "info",
			LogJSON:              false,
			MaxConcurrentTargets: 4,
			TargetTimeout:        60 * time.Second,
			TargetTimeoutS:       "60s",
			QueryTimeout:         10 * time.Second,
			QueryTimeoutS:        "10s",
			MinSnapshotInterval:  60 * time.Second,
			MinSnapshotIntervalS: "60s",
			MetricsEnabled:       false,
			MetricsPath:          "/metrics",
			Mode:                 ModeStandalone,
			// R075 (revised 2026-05, issue #6): collect-everything default.
			// High-sensitivity collectors (application-authored SQL
			// definitions + live pg_stat_activity statement text) run by
			// default; set this to false to opt OUT for privacy.
			HighSensitivityCollectorsEnabled: true,
		},
		API: APIConfig{
			ListenAddr:    "127.0.0.1:8081",
			ReadTimeout:   30 * time.Second,
			ReadTimeoutS:  "30s",
			WriteTimeout:  180 * time.Second,
			WriteTimeoutS: "180s",
		},
		Database: DatabaseConfig{
			Path: "/data/arq-signals.db",
			WAL:  true,
		},
	}
}

// Load reads configuration from the given file path, then applies env overrides.
func Load(path string) (Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		// Try default locations.
		for _, p := range []string{"/etc/arq/signals.yaml", "./signals.yaml"} {
			if _, err := os.Stat(p); err == nil {
				path = p
				break
			}
		}
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("read config %s: %w", path, err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config %s: %w", path, err)
		}
	}

	if err := resolveAPITokenFromYAML(&cfg); err != nil {
		return cfg, err
	}

	if err := applyEnvOverrides(&cfg); err != nil {
		return cfg, err
	}

	if err := parseDurations(&cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// parseEnvInt returns the parsed integer for the given ARQ_SIGNALS_* env
// variable, or an error if the value is set but not a valid integer.
// Empty/unset returns ok=false with no error.
func parseEnvInt(name string) (int, bool, error) {
	v := os.Getenv(name)
	if v == "" {
		return 0, false, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false, fmt.Errorf("environment variable %s=%q is not a valid integer", name, v)
	}
	return n, true, nil
}

// parseEnvBool accepts "true"/"false"/"1"/"0" (case-insensitive). Any other
// non-empty value is a hard error so a typo like "yes" is not silently
// treated as false. Empty/unset returns ok=false with no error.
func parseEnvBool(name string) (bool, bool, error) {
	v := os.Getenv(name)
	if v == "" {
		return false, false, nil
	}
	switch strings.ToLower(v) {
	case "true", "1":
		return true, true, nil
	case "false", "0":
		return false, true, nil
	}
	return false, false, fmt.Errorf("environment variable %s=%q is not a valid boolean (expected true/false/1/0)", name, v)
}

// resolveAPITokenFromYAML folds the YAML `api.token_file` and
// `api.token` fields into APIConfig.APIToken. Precedence (lowest to
// highest, later overrides earlier): api.token, api.token_file. ENV
// overrides applied later in applyEnvOverrides win over both. Setting
// both api.token and api.token_file is a hard error so a deployment
// mistake does not silently pick one source over the other.
func resolveAPITokenFromYAML(cfg *Config) error {
	if cfg.API.Token != "" && cfg.API.TokenFile != "" {
		return fmt.Errorf("api.token and api.token_file are mutually exclusive — supply exactly one")
	}
	if cfg.API.TokenFile != "" {
		data, err := os.ReadFile(cfg.API.TokenFile)
		if err != nil {
			return fmt.Errorf("read api.token_file %s: %w", cfg.API.TokenFile, err)
		}
		cfg.API.APIToken = strings.TrimRight(string(data), "\n\r")
		return nil
	}
	if cfg.API.Token != "" {
		cfg.API.APIToken = cfg.API.Token
	}
	return nil
}

func applyEnvOverrides(cfg *Config) error {
	if v := os.Getenv("ARQ_ENV"); v != "" {
		cfg.Env = strings.ToLower(v)
	}
	if b, ok, err := parseEnvBool("ARQ_ALLOW_INSECURE_PG_TLS"); err != nil {
		return err
	} else if ok {
		cfg.AllowInsecurePgTLS = b
	}
	if b, ok, err := parseEnvBool("ARQ_SIGNALS_ALLOW_UNSAFE_ROLE"); err != nil {
		return err
	} else if ok {
		cfg.AllowUnsafeRole = b
	}
	if v := os.Getenv("ARQ_SIGNALS_POLL_INTERVAL"); v != "" {
		cfg.Signals.PollIntervalS = v
	}
	if n, ok, err := parseEnvInt("ARQ_SIGNALS_RETENTION_DAYS"); err != nil {
		return err
	} else if ok {
		cfg.Signals.RetentionDays = n
	}
	if v := os.Getenv("ARQ_SIGNALS_LOG_LEVEL"); v != "" {
		cfg.Signals.LogLevel = strings.ToLower(v)
	}
	if b, ok, err := parseEnvBool("ARQ_SIGNALS_LOG_JSON"); err != nil {
		return err
	} else if ok {
		cfg.Signals.LogJSON = b
	}
	if n, ok, err := parseEnvInt("ARQ_SIGNALS_MAX_CONCURRENT_TARGETS"); err != nil {
		return err
	} else if ok {
		cfg.Signals.MaxConcurrentTargets = n
	}
	if v := os.Getenv("ARQ_SIGNALS_TARGET_TIMEOUT"); v != "" {
		cfg.Signals.TargetTimeoutS = v
	}
	if v := os.Getenv("ARQ_SIGNALS_QUERY_TIMEOUT"); v != "" {
		cfg.Signals.QueryTimeoutS = v
	}
	if v := os.Getenv("ARQ_SIGNALS_MIN_SNAPSHOT_INTERVAL"); v != "" {
		// R091: env override. Same time-string format as
		// poll_interval; validation happens in the post-parse
		// validator (FC-10).
		cfg.Signals.MinSnapshotIntervalS = v
	}
	if v := os.Getenv("ARQ_SIGNALS_LISTEN_ADDR"); v != "" {
		cfg.API.ListenAddr = v
	}
	if v := os.Getenv("ARQ_SIGNALS_API_TLS_CERT_FILE"); v != "" {
		cfg.API.TLSCertFile = v
	}
	if v := os.Getenv("ARQ_SIGNALS_API_TLS_KEY_FILE"); v != "" {
		cfg.API.TLSKeyFile = v
	}
	if v := os.Getenv("ARQ_SIGNALS_DB_PATH"); v != "" {
		cfg.Database.Path = v
	}
	if v := os.Getenv("ARQ_SIGNALS_WRITE_TIMEOUT"); v != "" {
		cfg.API.WriteTimeoutS = v
	}
	if v := os.Getenv("ARQ_SIGNALS_API_TOKEN"); v != "" {
		cfg.API.APIToken = v
	}
	if b, ok, err := parseEnvBool("ARQ_SIGNALS_HIGH_SENSITIVITY_COLLECTORS_ENABLED"); err != nil {
		return err
	} else if ok {
		cfg.Signals.HighSensitivityCollectorsEnabled = b
	}
	// #128: per-collector opt-in for pg_stats_array_range_v1.
	// Layered ON TOP of HighSensitivityCollectorsEnabled — both
	// must be true for the collector to run.
	if b, ok, err := parseEnvBool("ARQ_SIGNALS_COLLECT_ARRAY_RANGE_HISTOGRAMS"); err != nil {
		return err
	} else if ok {
		cfg.Signals.CollectArrayRangeHistograms = b
	}
	if b, ok, err := parseEnvBool("ARQ_SIGNALS_METRICS_ENABLED"); err != nil {
		return err
	} else if ok {
		cfg.Signals.MetricsEnabled = b
	}
	if v := os.Getenv("ARQ_SIGNALS_METRICS_PATH"); v != "" {
		cfg.Signals.MetricsPath = v
	}
	// R083 — Mode B knobs.
	if v := os.Getenv("ARQ_SIGNALS_MODE"); v != "" {
		cfg.Signals.Mode = strings.ToLower(v)
	}
	if v := os.Getenv("ARQ_SIGNALS_ARQ_CONTROL_PLANE_TOKEN_FILE"); v != "" {
		cfg.Signals.ArqControlPlaneTokenFile = v
	}
	if v := os.Getenv("ARQ_SIGNALS_ARQ_CONTROL_PLANE_TOKEN_ENV"); v != "" {
		cfg.Signals.ArqControlPlaneTokenEnv = v
	}
	// R080 — opt-in per-collector export view.
	if b, ok, err := parseEnvBool("ARQ_SIGNALS_EXPORT_PER_COLLECTOR_FILES"); err != nil {
		return err
	} else if ok {
		cfg.Signals.ExportPerCollectorFiles = b
	}
	// File takes precedence over the raw env var when both are set —
	// matches the _FILE convention used by the official postgres image.
	// A missing or unreadable file is a hard error so a deployment
	// mistake does not silently fall through to the weaker env-based
	// value.
	if v := os.Getenv("ARQ_SIGNALS_API_TOKEN_FILE"); v != "" {
		data, err := os.ReadFile(v)
		if err != nil {
			return fmt.Errorf("read ARQ_SIGNALS_API_TOKEN_FILE %s: %w", v, err)
		}
		cfg.API.APIToken = strings.TrimRight(string(data), "\n\r")
	}

	// Allow a single target via env (common for containers).
	if host := os.Getenv("ARQ_SIGNALS_TARGET_HOST"); host != "" {
		name := os.Getenv("ARQ_SIGNALS_TARGET_NAME")
		if name == "" {
			name = "default"
		}
		port := 5432
		if n, ok, err := parseEnvInt("ARQ_SIGNALS_TARGET_PORT"); err != nil {
			return err
		} else if ok {
			port = n
		}
		dbname := os.Getenv("ARQ_SIGNALS_TARGET_DBNAME")
		if dbname == "" {
			dbname = "postgres"
		}
		tgt := TargetConfig{
			Name:            name,
			Host:            host,
			Port:            port,
			DBName:          dbname,
			User:            os.Getenv("ARQ_SIGNALS_TARGET_USER"),
			SSLMode:         os.Getenv("ARQ_SIGNALS_TARGET_SSLMODE"),
			SSLRootCertFile: os.Getenv("ARQ_SIGNALS_TARGET_SSLROOTCERT_FILE"),
			PasswordFile:    os.Getenv("ARQ_SIGNALS_TARGET_PASSWORD_FILE"),
			PasswordEnv:     os.Getenv("ARQ_SIGNALS_TARGET_PASSWORD_ENV"),
			PgpassFile:      os.Getenv("ARQ_SIGNALS_TARGET_PGPASS_FILE"),
			Enabled:         true,
		}
		cfg.Targets = append(cfg.Targets, tgt)
	}
	return nil
}

// ValidateStrict implements R076. It returns the list of non-fatal warnings
// (caller logs and continues) and a hard error that the caller must abort
// on. The hard / warn taxonomy is defined in
// `features/arq-signals/appendix-b-configuration-schema.md`.
func ValidateStrict(cfg Config) (warnings []string, err error) {
	// Hard errors first; we still gather as many as we can find before
	// returning so the operator sees the full picture in one run.
	var hard []string

	if cfg.Database.Path == "" {
		hard = append(hard, "database.path is empty")
	}
	if cfg.API.ListenAddr == "" {
		hard = append(hard, "api.listen_addr is empty")
	}
	// R113: API TLS is all-or-nothing. A half-configured listener must
	// never silently fall back to cleartext.
	if (cfg.API.TLSCertFile == "") != (cfg.API.TLSKeyFile == "") {
		hard = append(hard, "api.tls_cert_file and api.tls_key_file must both be set or both be empty")
	}
	if cfg.Signals.PollInterval <= 0 {
		hard = append(hard, "signals.poll_interval must be > 0")
	}
	if cfg.Signals.TargetTimeout <= 0 {
		hard = append(hard, "signals.target_timeout must be > 0")
	}
	if cfg.Signals.QueryTimeout <= 0 {
		hard = append(hard, "signals.query_timeout must be > 0")
	}
	// FC-10 (R091): zero or negative min_snapshot_interval is not
	// supported in v1.x. Disabling the protection would defeat the
	// rule entirely; operators who genuinely want every poll to
	// result in a collection set poll_interval >= min_snapshot_interval.
	if cfg.Signals.MinSnapshotInterval <= 0 {
		hard = append(hard, fmt.Sprintf(
			"signals.min_snapshot_interval must be > 0 (got %v); see ARQ-SIGNALS-R091/FC-10",
			cfg.Signals.MinSnapshotInterval,
		))
	}
	// Codex post-0.3.1 M-004: any non-positive RetentionDays disables
	// cleanup (cleanup() returns immediately). It's a warning, not a
	// hard error — operators sometimes legitimately want indefinite
	// retention pinned at the daemon and rely on external pruning.

	// R097 circuit thresholds: zero falls back to package defaults.
	// Negative is operator-typo territory and would silently be
	// rewritten to the default — reject explicitly (issue #90).
	if cfg.Signals.Circuit.FailThreshold < 0 {
		hard = append(hard, fmt.Sprintf(
			"signals.circuit.fail_threshold must be >= 0 (got %d); use 0 for default",
			cfg.Signals.Circuit.FailThreshold))
	}
	if cfg.Signals.Circuit.OpenCooldown < 0 {
		hard = append(hard, fmt.Sprintf(
			"signals.circuit.open_cooldown must be >= 0 (got %s); use 0 for default",
			cfg.Signals.Circuit.OpenCooldown))
	}

	// R099: structured retention block + flat retention_days are
	// mutually exclusive (FC-21). Either form alone is fine; both
	// together is a configuration smell that fails fast.
	if cfg.Signals.RetentionDays > 0 && cfg.Signals.Retention.IsSet() {
		hard = append(hard,
			"signals.retention_days and signals.retention.* are mutually exclusive — pick one")
	}
	for _, classDays := range []struct {
		name string
		v    int
	}{
		{"short_days", cfg.Signals.Retention.ShortDays},
		{"medium_days", cfg.Signals.Retention.MediumDays},
		{"long_days", cfg.Signals.Retention.LongDays},
	} {
		if classDays.v < 0 {
			hard = append(hard, fmt.Sprintf(
				"signals.retention.%s must be >= 0 (got %d)", classDays.name, classDays.v))
		}
	}

	if cfg.Signals.MetricsEnabled {
		path := cfg.Signals.MetricsPath
		switch {
		case !strings.HasPrefix(path, "/"):
			hard = append(hard, fmt.Sprintf("signals.metrics_path %q must start with /", path))
		case path == "/health":
			hard = append(hard, "signals.metrics_path must not be /health (reserved for liveness probes)")
		case path == "/status" || path == "/collect/now" || path == "/export":
			hard = append(hard, fmt.Sprintf("signals.metrics_path %q collides with an existing API path", path))
		}
	}

	// R083: mode + control-plane token configuration. Cross-token
	// equality and length checks happen later in ValidateModeBTokens
	// because they need the resolved api.token, which is generated
	// after Load returns.
	switch cfg.Signals.Mode {
	case "", ModeStandalone, ModeArqManaged:
		// allowed (empty == standalone via default)
	default:
		hard = append(hard, fmt.Sprintf("signals.mode %q must be %q or %q", cfg.Signals.Mode, ModeStandalone, ModeArqManaged))
	}
	if cfg.Signals.ArqControlPlaneTokenFile != "" && cfg.Signals.ArqControlPlaneTokenEnv != "" {
		hard = append(hard, "signals.arq_control_plane_token_file and signals.arq_control_plane_token_env are mutually exclusive — pick one")
	}
	if cfg.Signals.Mode == ModeArqManaged &&
		cfg.Signals.ArqControlPlaneTokenFile == "" &&
		cfg.Signals.ArqControlPlaneTokenEnv == "" {
		hard = append(hard, `signals.mode is "arq_managed" but no control-plane token is configured (set arq_control_plane_token_file or arq_control_plane_token_env)`)
	}

	seen := make(map[string]int, len(cfg.Targets))
	for i, t := range cfg.Targets {
		if t.Name == "" {
			hard = append(hard, fmt.Sprintf("target[%d]: name is required", i))
		} else if prev, ok := seen[t.Name]; ok {
			hard = append(hard, fmt.Sprintf("target[%d] (%s): duplicate name (also at target[%d])", i, t.Name, prev))
		} else {
			seen[t.Name] = i
		}
		if t.Host == "" {
			hard = append(hard, fmt.Sprintf("target[%d] (%s): host is required", i, t.Name))
		}
		if t.User == "" {
			hard = append(hard, fmt.Sprintf("target[%d] (%s): user is required", i, t.Name))
		}
		if t.DBName == "" {
			hard = append(hard, fmt.Sprintf("target[%d] (%s): dbname is required", i, t.Name))
		}
		secretCount := 0
		if t.PasswordFile != "" {
			secretCount++
		}
		if t.PasswordEnv != "" {
			secretCount++
		}
		if t.PgpassFile != "" {
			secretCount++
		}
		if secretCount > 1 {
			hard = append(hard, fmt.Sprintf("target[%d] (%s): specify at most one of password_file, password_env, pgpass_file", i, t.Name))
		}
		// Codex post-0.3.1 M-006: reject sslmode values outside the
		// libpq enum. Empty is allowed — libpq applies its default.
		if t.SSLMode != "" && !validSSLModes[t.SSLMode] {
			hard = append(hard, fmt.Sprintf("target[%d] (%s): sslmode %q is not a valid libpq value; use disable, allow, prefer, require, verify-ca, or verify-full", i, t.Name, t.SSLMode))
		}

		// Credential-provider validation (credential-providers.md #93;
		// aws_rds_iam #94). Only methods in SupportedAuthMethods are
		// served by this build.
		switch t.EffectiveAuthMethod() {
		case AuthMethodPassword:
			// Default password provider — existing behavior, no extra rules.
		case AuthMethodAWSRDSIAM:
			// FC-AWS-003: token auth is passwordless — reject any stored
			// password source on the target.
			if secretCount > 0 {
				hard = append(hard, fmt.Sprintf("target[%d] (%s): auth_method %q is passwordless — remove password_file, password_env, and pgpass_file", i, t.Name, AuthMethodAWSRDSIAM))
			}
			// FC-AWS-004: the IAM token must only traverse a fully
			// verified TLS channel, in every environment.
			if t.SSLMode != "verify-full" {
				hard = append(hard, fmt.Sprintf("target[%d] (%s): auth_method %q requires sslmode=verify-full (got %q)", i, t.Name, AuthMethodAWSRDSIAM, t.SSLMode))
			}
			// Region-resolution decision: a missing config+env region is
			// a startup WARNING only (fail-soft); the target is failed at
			// connect time (FC-AWS-005) if it still cannot be resolved.
			if t.Region == "" && os.Getenv("AWS_REGION") == "" && os.Getenv("AWS_DEFAULT_REGION") == "" {
				warnings = append(warnings, fmt.Sprintf("target[%d] (%s): auth_method %q has no region configured and AWS_REGION/AWS_DEFAULT_REGION are unset; region will be resolved from instance metadata at connect time", i, t.Name, AuthMethodAWSRDSIAM))
			}
		case AuthMethodAzureEntra:
			// FC-AZURE-003: token auth is passwordless — reject any stored
			// password source on the target.
			if secretCount > 0 {
				hard = append(hard, fmt.Sprintf("target[%d] (%s): auth_method %q is passwordless — remove password_file, password_env, and pgpass_file", i, t.Name, AuthMethodAzureEntra))
			}
			// FC-AZURE-004: the Entra token must only traverse a fully
			// verified TLS channel, in every environment.
			if t.SSLMode != "verify-full" {
				hard = append(hard, fmt.Sprintf("target[%d] (%s): auth_method %q requires sslmode=verify-full (got %q)", i, t.Name, AuthMethodAzureEntra, t.SSLMode))
			}
			// Identity resolution is deliberately NOT validated at startup:
			// a missing azure_client_id is the common case (single /
			// system-assigned identity), and an undiscoverable or ambiguous
			// identity is a connect-time, target-scoped failure (FC-AZURE-005),
			// not a whole-collector startup failure.
		case AuthMethodGCPCloudSQLIAM:
			// FC-GCP-003: token auth is passwordless — reject any stored
			// password source on the target.
			if secretCount > 0 {
				hard = append(hard, fmt.Sprintf("target[%d] (%s): auth_method %q is passwordless — remove password_file, password_env, and pgpass_file", i, t.Name, AuthMethodGCPCloudSQLIAM))
			}
			// FC-GCP-004: the Cloud SQL IAM access token must only traverse a
			// fully verified TLS channel (direct libpq path), in every
			// environment.
			if t.SSLMode != "verify-full" {
				hard = append(hard, fmt.Sprintf("target[%d] (%s): auth_method %q requires sslmode=verify-full (got %q)", i, t.Name, AuthMethodGCPCloudSQLIAM, t.SSLMode))
			}
			// Identity resolution is deliberately NOT validated at startup:
			// a missing gcp_impersonate_service_account is the common case
			// (ambient ADC identity), and an undiscoverable identity or a
			// denied impersonation is a connect-time, target-scoped failure
			// (FC-GCP-005), not a whole-collector startup failure.
		case AuthMethodSecretStore:
			// FC-SECRET-005: the password comes only from the vault — reject
			// any inline password source on the target.
			if secretCount > 0 {
				hard = append(hard, fmt.Sprintf("target[%d] (%s): auth_method %q does not take an inline password source — remove password_file, password_env, and pgpass_file; the password comes from the vault", i, t.Name, AuthMethodSecretStore))
			}
			// FC-SECRET-006: the fetched secret must only traverse a fully
			// verified TLS channel, in every environment.
			if t.SSLMode != "verify-full" {
				hard = append(hard, fmt.Sprintf("target[%d] (%s): auth_method %q requires sslmode=verify-full (got %q)", i, t.Name, AuthMethodSecretStore, t.SSLMode))
			}
			// FC-SECRET-007: secret_ref is required and must match one of the
			// three accepted shapes (the shape selects the backend).
			if t.SecretRef == "" {
				hard = append(hard, fmt.Sprintf("target[%d] (%s): auth_method %q requires secret_ref; %s", i, t.Name, AuthMethodSecretStore, secretRefForms))
			} else if _, err := InferSecretBackend(t.SecretRef); err != nil {
				hard = append(hard, fmt.Sprintf("target[%d] (%s): auth_method %q: %v", i, t.Name, AuthMethodSecretStore, err))
			}
			// Identity resolution is deliberately NOT validated at startup:
			// an undiscoverable workload identity for the vault is a
			// connect-time, target-scoped failure (FC-SECRET-002), not a
			// whole-collector startup failure.
		default:
			hard = append(hard, fmt.Sprintf("target[%d] (%s): auth_method %q is not supported by this build (supported: %s)", i, t.Name, t.AuthMethod, strings.Join(SupportedAuthMethods, ", ")))
		}
	}

	// Warnings.
	if cfg.Signals.PollInterval > 0 && cfg.Signals.PollInterval < 30*time.Second {
		warnings = append(warnings, fmt.Sprintf("signals.poll_interval is very short (%s); minimum recommended is 30s", cfg.Signals.PollInterval))
	}
	if cfg.Signals.RetentionDays <= 0 {
		// Codex post-0.3.1 M-004: align warning text with the
		// implementation. cleanup() returns immediately when
		// RetentionDays <= 0, i.e. snapshots and query_runs are
		// retained forever — the previous warning falsely claimed
		// the next cycle would delete them.
		warnings = append(warnings, "signals.retention_days <= 0; cleanup is disabled — snapshots and query runs will be retained until the daemon disk fills up")
	}
	if len(cfg.Targets) == 0 {
		warnings = append(warnings, "no targets configured; the collector will start but have nothing to collect")
	}
	for i, t := range cfg.Targets {
		if cfg.Env != "prod" && t.SSLMode == "prefer" {
			warnings = append(warnings, fmt.Sprintf("target[%d] (%s): sslmode=prefer does not verify server identity; consider verify-ca or verify-full", i, t.Name))
		}
		// R098: per-target sensitivity profile validation (FC-19,
		// FC-20). Empty profile is allowed (= default).
		if t.Collectors.Profile != "" {
			valid := false
			for _, p := range SupportedProfiles {
				if t.Collectors.Profile == p {
					valid = true
					break
				}
			}
			if !valid {
				hard = append(hard, fmt.Sprintf(
					"target[%d] (%s): collectors.profile %q is invalid (supported: %s)",
					i, t.Name, t.Collectors.Profile, strings.Join(SupportedProfiles[1:], ", "),
				))
			}
		}
		if len(t.Collectors.Include) > 0 || len(t.Collectors.Exclude) > 0 {
			incSet := make(map[string]bool, len(t.Collectors.Include))
			for _, id := range t.Collectors.Include {
				incSet[id] = true
			}
			for _, id := range t.Collectors.Exclude {
				if incSet[id] {
					hard = append(hard, fmt.Sprintf(
						"target[%d] (%s): collector %q appears in both include and exclude",
						i, t.Name, id,
					))
				}
			}
		}
	}

	// #135: API bearer-token strength. The auto-generated token is
	// always strong (32 random bytes from crypto/rand); only
	// operator-supplied tokens can be weak. A weak control-plane
	// token undermines pause/resume/reload/export authorisation
	// regardless of TLS posture, so we fail-fast in prod and warn
	// in dev/lab.
	if cfg.API.APIToken != "" {
		if reason := WeakAPITokenReason(cfg.API.APIToken); reason != "" {
			msg := fmt.Sprintf("api.api_token: %s — supply a strong random secret (e.g. `openssl rand -base64 32`) via api.token / api.token_file in signals.yaml, or ARQ_SIGNALS_API_TOKEN / ARQ_SIGNALS_API_TOKEN_FILE", reason)
			if cfg.Env == "prod" {
				hard = append(hard, msg)
			} else {
				warnings = append(warnings, "non-prod env: "+msg)
			}
		}
	}

	if len(hard) > 0 {
		return warnings, fmt.Errorf("configuration is invalid:\n  - %s", strings.Join(hard, "\n  - "))
	}
	return warnings, nil
}

// APITokenMinLength is the minimum length the validator accepts on
// operator-supplied tokens (#135). 32 bytes is below the raw-bytes
// minimum the AC names (32 raw / 43 base64url / 64 hex); 32 chars
// covers the raw case AND reasonable formatted variants.
const APITokenMinLength = 32

// APITokenMinUniqueChars is the minimum distinct-character count the
// validator accepts on operator-supplied tokens (#135). Catches the
// "32-char-but-all-zeros" weak pattern that length alone would
// permit. Tuned so well-formed base64url / hex / random-bytes
// tokens always pass; "dev-token-padded-to-32-chars-aaaa" fails.
const APITokenMinUniqueChars = 8

// WeakAPITokenReason returns the closed human-readable reason why a
// token is rejected, or "" when the token passes the minimum
// strength rules (#135). Returns a non-empty string for: too-short,
// low-distinct-character-count.
//
// The reason MUST NOT include the token itself — the validator
// outputs flow into config-error logs which can be surfaced in
// dashboards or audit pipelines.
func WeakAPITokenReason(token string) string {
	if len(token) < APITokenMinLength {
		return fmt.Sprintf("token too short (%d chars; minimum %d)", len(token), APITokenMinLength)
	}
	uniq := make(map[rune]struct{})
	for _, r := range token {
		uniq[r] = struct{}{}
	}
	if len(uniq) < APITokenMinUniqueChars {
		return fmt.Sprintf("token entropy too low (%d distinct chars; minimum %d)", len(uniq), APITokenMinUniqueChars)
	}
	return ""
}

// Validate checks the Config for common issues, returning human-readable
// warnings. An empty slice means the config is healthy.
func Validate(cfg Config) []string {
	var issues []string

	if cfg.Signals.PollInterval < 10*time.Second {
		issues = append(issues, fmt.Sprintf("signals.poll_interval is very short (%s); minimum recommended is 30s", cfg.Signals.PollInterval))
	}
	if cfg.Signals.RetentionDays < 1 {
		issues = append(issues, "signals.retention_days <= 0; cleanup is disabled — snapshots and query runs will be retained indefinitely")
	}
	if cfg.Database.Path == "" {
		issues = append(issues, "database.path is empty")
	}
	if cfg.API.ListenAddr == "" {
		issues = append(issues, "api.listen_addr is empty")
	}
	if len(cfg.Targets) == 0 {
		issues = append(issues, "no targets configured; the collector will have nothing to collect")
	}
	for i, t := range cfg.Targets {
		if t.Name == "" {
			issues = append(issues, fmt.Sprintf("target[%d]: name is empty", i))
		}
		if t.Host == "" {
			issues = append(issues, fmt.Sprintf("target[%d] (%s): host is empty", i, t.Name))
		}
		if t.User == "" {
			issues = append(issues, fmt.Sprintf("target[%d] (%s): user is empty", i, t.Name))
		}
		if t.DBName == "" {
			issues = append(issues, fmt.Sprintf("target[%d] (%s): dbname is empty", i, t.Name))
		}
		// Reject multiple secret sources.
		secretCount := 0
		if t.PasswordFile != "" {
			secretCount++
		}
		if t.PasswordEnv != "" {
			secretCount++
		}
		if t.PgpassFile != "" {
			secretCount++
		}
		if secretCount > 1 {
			issues = append(issues, fmt.Sprintf("target[%d] (%s): specify at most one of password_file, password_env, pgpass_file", i, t.Name))
		}
		// Warn on weak sslmode.
		if t.SSLMode == "disable" || t.SSLMode == "allow" || t.SSLMode == "prefer" {
			issues = append(issues, fmt.Sprintf("target[%d] (%s): sslmode=%s is not recommended for production; consider require, verify-ca, or verify-full", i, t.Name, t.SSLMode))
		}
	}
	return issues
}

// validSSLModes is the canonical libpq enum. Any other value is a
// hard configuration error — silently accepting unknown strings would
// pass through to libpq and trigger an opaque connect-time failure
// per target. Codex post-0.3.1 M-006.
var validSSLModes = map[string]bool{
	"disable":     true,
	"allow":       true,
	"prefer":      true,
	"require":     true,
	"verify-ca":   true,
	"verify-full": true,
}

// weakSSLModes are sslmode values that do not provide adequate TLS
// guarantees against MITM. Only verify-ca and verify-full count as
// strong; require negotiates TLS but does not verify server identity.
var weakSSLModes = map[string]bool{
	"disable": true,
	"allow":   true,
	"prefer":  true,
	"require": true, // require does not verify server identity
}

// ValidateProdTLS enforces strict Postgres TLS requirements in production.
// In prod, all targets must use verify-ca or verify-full with a CA cert.
// In non-prod, ARQ_ALLOW_INSECURE_PG_TLS=true suppresses the error.
// Returns nil if all checks pass.
func ValidateProdTLS(cfg Config) error {
	isProd := cfg.Env == "prod"

	for i, t := range cfg.Targets {
		if !t.Enabled {
			continue
		}

		mode := t.SSLMode
		if mode == "" {
			mode = "prefer" // libpq default
		}

		if !weakSSLModes[mode] {
			// verify-ca or verify-full — check that sslrootcert is set.
			if t.SSLRootCertFile == "" {
				return fmt.Errorf("target[%d] (%s): sslmode=%s requires sslrootcert_file to be set", i, t.Name, mode)
			}
			continue
		}

		// Weak mode detected.
		if isProd {
			if cfg.AllowInsecurePgTLS {
				return fmt.Errorf("target[%d] (%s): ARQ_ALLOW_INSECURE_PG_TLS is not permitted in prod; use verify-ca or verify-full", i, t.Name)
			}
			return fmt.Errorf("target[%d] (%s): sslmode=%s is not allowed in prod; set sslmode=verify-full and provide sslrootcert_file", i, t.Name, mode)
		}

		// Non-prod: allow with override, warn otherwise.
		if !cfg.AllowInsecurePgTLS {
			return fmt.Errorf("target[%d] (%s): sslmode=%s is insecure; set ARQ_ALLOW_INSECURE_PG_TLS=true to allow in %s environment", i, t.Name, mode, cfg.Env)
		}
	}

	return nil
}

// ResolveArqControlPlaneToken reads the configured Arq control-plane
// token (R083). It is called per authentication attempt by the auth
// middleware so rotating the file's contents takes effect on the
// next request — no daemon restart required. Returns empty string
// when mode != arq_managed or when no source is configured (caller
// treats that as "control-plane auth disabled" without allocation).
//
// File source is preferred over env-var source. If both are set the
// file wins; ValidateStrict already rejects the both-set case at
// startup so this only matters under a lossy env reload.
func ResolveArqControlPlaneToken(s SignalsConfig) (string, error) {
	if s.Mode != ModeArqManaged {
		return "", nil
	}
	switch {
	case s.ArqControlPlaneTokenFile != "":
		data, err := os.ReadFile(s.ArqControlPlaneTokenFile)
		if err != nil {
			return "", fmt.Errorf("read arq_control_plane_token_file: %w", err)
		}
		// Strip a single trailing newline pair — same convention as
		// the api.token file and pgpass handling.
		return strings.TrimRight(string(data), "\n\r"), nil
	case s.ArqControlPlaneTokenEnv != "":
		v, ok := os.LookupEnv(s.ArqControlPlaneTokenEnv)
		if !ok {
			return "", fmt.Errorf("env var %q referenced by arq_control_plane_token_env is not set", s.ArqControlPlaneTokenEnv)
		}
		return v, nil
	}
	return "", nil
}

// ValidateModeBTokens runs the cross-token checks that depend on the
// resolved values of both tokens (R083): control-plane token length
// floor and distinctness from the API token. Called from main.go
// once both tokens are populated; not called when mode != arq_managed.
//
// arqToken is the resolved control-plane token (i.e. the result of
// ResolveArqControlPlaneToken at startup). apiToken is the
// effective api.token after auto-generation.
func ValidateModeBTokens(cfg Config, apiToken, arqToken string) error {
	if cfg.Signals.Mode != ModeArqManaged {
		return nil
	}
	if arqToken == "" {
		return fmt.Errorf("signals.arq_control_plane_token resolved to empty string — check the configured file or env var")
	}
	if len(arqToken) < MinArqControlPlaneTokenLength {
		return fmt.Errorf("signals.arq_control_plane_token must be at least %d characters", MinArqControlPlaneTokenLength)
	}
	if subtle.ConstantTimeCompare([]byte(arqToken), []byte(apiToken)) == 1 {
		return fmt.Errorf("signals.arq_control_plane_token must differ from api.token")
	}
	return nil
}

func parseDurations(cfg *Config) error {
	if cfg.Signals.PollIntervalS != "" {
		d, err := time.ParseDuration(cfg.Signals.PollIntervalS)
		if err != nil {
			return fmt.Errorf("parse signals.poll_interval %q: %w", cfg.Signals.PollIntervalS, err)
		}
		cfg.Signals.PollInterval = d
	}
	if cfg.API.ReadTimeoutS != "" {
		d, err := time.ParseDuration(cfg.API.ReadTimeoutS)
		if err != nil {
			return fmt.Errorf("parse api.read_timeout %q: %w", cfg.API.ReadTimeoutS, err)
		}
		cfg.API.ReadTimeout = d
	}
	if cfg.API.WriteTimeoutS != "" {
		d, err := time.ParseDuration(cfg.API.WriteTimeoutS)
		if err != nil {
			return fmt.Errorf("parse api.write_timeout %q: %w", cfg.API.WriteTimeoutS, err)
		}
		cfg.API.WriteTimeout = d
	}
	if cfg.Signals.TargetTimeoutS != "" {
		d, err := time.ParseDuration(cfg.Signals.TargetTimeoutS)
		if err != nil {
			return fmt.Errorf("parse signals.target_timeout %q: %w", cfg.Signals.TargetTimeoutS, err)
		}
		cfg.Signals.TargetTimeout = d
	}
	if cfg.Signals.QueryTimeoutS != "" {
		d, err := time.ParseDuration(cfg.Signals.QueryTimeoutS)
		if err != nil {
			return fmt.Errorf("parse signals.query_timeout %q: %w", cfg.Signals.QueryTimeoutS, err)
		}
		cfg.Signals.QueryTimeout = d
	}
	if cfg.Signals.MinSnapshotIntervalS != "" {
		d, err := time.ParseDuration(cfg.Signals.MinSnapshotIntervalS)
		if err != nil {
			return fmt.Errorf("parse signals.min_snapshot_interval %q: %w", cfg.Signals.MinSnapshotIntervalS, err)
		}
		cfg.Signals.MinSnapshotInterval = d
	}
	if cfg.Signals.Circuit.OpenCooldownS != "" {
		d, err := time.ParseDuration(cfg.Signals.Circuit.OpenCooldownS)
		if err != nil {
			return fmt.Errorf("parse signals.circuit.open_cooldown %q: %w", cfg.Signals.Circuit.OpenCooldownS, err)
		}
		cfg.Signals.Circuit.OpenCooldown = d
	}
	// Per-target durations: secret_store max_cache_ttl (#97).
	for i := range cfg.Targets {
		if cfg.Targets[i].MaxCacheTTLS != "" {
			d, err := time.ParseDuration(cfg.Targets[i].MaxCacheTTLS)
			if err != nil {
				return fmt.Errorf("parse target[%d] (%s) max_cache_ttl %q: %w", i, cfg.Targets[i].Name, cfg.Targets[i].MaxCacheTTLS, err)
			}
			if d < 0 {
				return fmt.Errorf("target[%d] (%s): max_cache_ttl must be >= 0 (got %s)", i, cfg.Targets[i].Name, d)
			}
			cfg.Targets[i].MaxCacheTTL = d
		}
	}
	return nil
}
