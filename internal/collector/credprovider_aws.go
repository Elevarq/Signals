package collector

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"

	"github.com/elevarq/signals/internal/config"
)

// awsTokenTTL is the validity window of an RDS / Aurora IAM auth token.
// AWS fixes this at 15 minutes from minting; the cache refreshes the
// token before this elapses (refreshSkew).
const awsTokenTTL = 15 * time.Minute

// rdsTokenMinter is the seam between the resolver and AWS. The
// production implementation calls the AWS SDK v2 RDS auth-token builder;
// unit tests inject a fake so no test makes a real AWS call (NFR003).
// endpoint is "host:port"; region and dbUser are passed verbatim.
type rdsTokenMinter interface {
	Mint(ctx context.Context, endpoint, region, dbUser string) (token string, expiresAt time.Time, err error)
}

// resolveAWS mints (or reuses a cached) RDS IAM token for the target and
// returns it as a password-kind credential. The token is the connection
// password; the target carries no stored secret (INV001).
func (r *credentialResolver) resolveAWS(ctx context.Context, tgt config.TargetConfig) (Credential, error) {
	// Region first: a failure here is target-scoped (FC-AWS-005) and must
	// never reach the minter, so an unresolved region cannot trigger an
	// AWS call.
	region, err := r.region(ctx, tgt)
	if err != nil {
		return Credential{}, fmt.Errorf("target %s: %s: region could not be resolved: %w", tgt.Name, config.AuthMethodAWSRDSIAM, err)
	}

	now := r.now()
	key := awsCacheKey(tgt)
	if cred, ok := r.cache.get(key, now); ok {
		return cred, nil
	}

	port := tgt.Port
	if port == 0 {
		port = 5432
	}
	endpoint := net.JoinHostPort(tgt.Host, strconv.Itoa(port))

	token, expiresAt, err := r.minter.Mint(ctx, endpoint, region, tgt.User)
	if err != nil {
		// FC-AWS-001: redact in case the SDK error embeds anything
		// sensitive, and attribute the failure to the method so the
		// operator knows which target/provider failed.
		return Credential{}, fmt.Errorf("target %s: %s: minting RDS IAM auth token failed: %w", tgt.Name, config.AuthMethodAWSRDSIAM, redactError(err))
	}

	cred := Credential{Kind: CredKindPassword, Password: token, ExpiresAt: expiresAt}
	r.cache.put(key, cred, expiresAt.Add(-refreshSkew(expiresAt.Sub(now))))

	// INV002/INV007: log metadata only — never the token value.
	r.logger.Info("resolved aws_rds_iam credential",
		"auth_method", config.AuthMethodAWSRDSIAM,
		"target", tgt.Name,
		"region", region,
		"db_user", tgt.User,
		"resolved_at", now,
		"expires_at", expiresAt,
	)

	return cred, nil
}

// awsCacheKey is the per-target cache key: the connection identity
// (host:port/dbname@user) plus the auth method. Distinct targets — and
// distinct hosts/users — produce distinct keys, so a token is never
// shared across targets (NFR001).
func awsCacheKey(tgt config.TargetConfig) string {
	return tgt.ConnIdentity() + "|" + config.AuthMethodAWSRDSIAM
}

// resolveAWSRegion resolves the AWS region for a target in the order
// fixed by the spec: explicit config, then AWS_REGION /
// AWS_DEFAULT_REGION, then the SDK default chain (which consults EC2/ECS
// instance metadata). If none resolve, the target fails with an
// actionable error naming the sources tried (FC-AWS-005).
func resolveAWSRegion(ctx context.Context, tgt config.TargetConfig) (string, error) {
	if tgt.Region != "" {
		return tgt.Region, nil
	}
	if v := os.Getenv("AWS_REGION"); v != "" {
		return v, nil
	}
	if v := os.Getenv("AWS_DEFAULT_REGION"); v != "" {
		return v, nil
	}
	// Last resort: the SDK default config consults instance metadata
	// (IMDS) for the region.
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err == nil && cfg.Region != "" {
		return cfg.Region, nil
	}
	return "", fmt.Errorf("no region from config, AWS_REGION, AWS_DEFAULT_REGION, or instance metadata (IMDS)")
}

// awsRDSTokenMinter is the production rdsTokenMinter. It builds a
// presigned RDS IAM auth token from the SDK default credential chain.
type awsRDSTokenMinter struct{}

func (awsRDSTokenMinter) Mint(ctx context.Context, endpoint, region, dbUser string) (string, time.Time, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("load AWS config: %w", err)
	}
	token, err := auth.BuildAuthToken(ctx, endpoint, region, dbUser, cfg.Credentials)
	if err != nil {
		return "", time.Time{}, err
	}
	// AWS fixes RDS IAM token validity at 15 minutes from minting.
	return token, time.Now().Add(awsTokenTTL), nil
}

// AWSGrantGuidance returns the operator remediation text for an
// aws_rds_iam target whose DB role lacks rds_iam or whose IAM principal
// is unmapped (AC-AWS-009). It contains the exact GRANT and the minimal
// IAM action — and no secret material.
func AWSGrantGuidance(tgt config.TargetConfig) string {
	return fmt.Sprintf(`aws_rds_iam connection for target %q was rejected.
Verify both halves of RDS IAM auth:
  1. On the database, grant the rds_iam role to the login role (run as a superuser/owner):
       GRANT rds_iam TO %q;
  2. Attach an IAM policy to the collector's principal allowing the
     rds-db:connect action for this DB user, e.g.:
       {"Effect":"Allow","Action":"rds-db:connect","Resource":"arn:aws:rds-db:<region>:<account>:dbuser:<db-resource-id>/%s"}`,
		tgt.Name, tgt.User, tgt.User)
}
