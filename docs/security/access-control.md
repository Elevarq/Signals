# Access control — API bearer token

Tracking: [#135](https://github.com/Elevarq/Arq-Signals/issues/135),
[#136](https://github.com/Elevarq/Arq-Signals/issues/136),
[#137](https://github.com/Elevarq/Arq-Signals/issues/137).

This page documents the control-plane access guarantees Elevarq Signals
implements today. Suitable to cite for SOC 2 / ISO 27001 evidence
collection on **access control** and **secret management**.

## Overview

Every non-health endpoint in `internal/api/server.go` requires a
bearer token. The token authorises the control plane:
`/collect/now`, `/pause`, `/resume`, `/reload`, `/status`,
`/export`, `/metrics`. Without a valid token the server returns
401 with no diagnostic information that could fingerprint
deployments.

## Token provisioning

| Source | When to use | Strength enforcement |
|---|---|---|
| Auto-generated at startup | Local development; demo containers | Always strong (32 random bytes from `crypto/rand`); 12-char SHA-256 fingerprint logged so operators can confirm rotation |
| `ARQ_SIGNALS_API_TOKEN` env var | Operator-driven shell scripts | Validated against `config.WeakAPITokenReason`; weak tokens **hard-fail startup** in `env=prod`, **warn** in `env=dev`/`lab` |
| `ARQ_SIGNALS_API_TOKEN_FILE` env var | Docker secrets, file-backed secret stores | Same validation as the env var |
| Kubernetes `Secret` via Helm | Production / managed deployments | Same validation; declarative rotation; the Helm chart references the Secret by name only — token never lands in a ConfigMap or rendered manifest |

## Strength requirements

Operator-supplied tokens MUST be:

1. **At least 32 characters** long.
2. Carry **at least 8 distinct characters**.

Tokens that fail either rule are rejected with a closed
human-readable reason: `"token too short (N chars; minimum 32)"`
or `"token entropy too low (N distinct chars; minimum 8)"`.
**The rejection message never includes the token value** —
closed-output discipline for log + audit pipelines.

Recommended generation:

```sh
openssl rand -base64 32
```

Outputs 43 base64url characters from 32 random bytes. Always
satisfies the strength rules.

## Closed control surface

The token is the SOLE authorisation gate for the control-plane
endpoints. There is no per-endpoint allow-list bypass, no IP
allow-list short-circuit, no header alternative. A single source
of truth: the bearer token compared against `cfg.API.APIToken`
via `subtle.ConstantTimeCompare` (timing-attack-resistant).

## What gets logged

- **Token fingerprint (12 chars of SHA-256)** on auto-generation
  so operators can verify rotation across restarts.
  Implementation: `cmd/signals/main.go::generated-token logging`.
- **Validation rejections** in `env=prod` cause a hard startup
  failure with the closed reason string and no token bytes.
- **Validation warnings** in `env=dev`/`lab` log the same closed
  reason without the token value.

What never gets logged:

- The raw token bytes — neither on auto-generation, env loading,
  file loading, nor validation rejection.
- The token's encoding form (base64url vs hex vs raw) — the
  validator works on the string post-decode and doesn't echo
  upstream form to logs.
- The token via `Authorization` header echoes — the server's
  middleware strips the header before request logging.

## Helm deployment

The chart at `deploy/helm/signals/` consumes the token from
a Kubernetes `Secret`:

```yaml
api:
  tokenSecretName: signals-api      # required for production
  tokenSecretKey: token                  # defaults to "token"
```

Generation + rotation:

```sh
# First-time provision:
kubectl create secret generic signals-api \
  --from-literal=token="$(openssl rand -base64 32)"

# Rotate (kubectl creates a new generation; pods reload via
# regular deployment restart):
kubectl create secret generic signals-api \
  --from-literal=token="$(openssl rand -base64 32)" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl rollout restart deployment/signals
```

The deployment template injects the token as
`ARQ_SIGNALS_API_TOKEN` via `valueFrom.secretKeyRef`. The token
value NEVER appears in the rendered manifest beyond the Secret
reference name.

## Audit + evidence

The test surface pins the behavior described above:

| Control | Test file |
|---|---|
| Strength validation: short / low-entropy / strong matrix | `tests/signals_api_token_strength_test.go` |
| Prod hard-error vs dev warning | `tests/signals_api_token_strength_test.go` |
| Token never appears in error / warning messages | `tests/signals_api_token_strength_test.go` |
| Env / file / both-set token loading | `tests/signals_config_secret_env_test.go` |
| Helm chart wires `secretKeyRef`; no literal `value:` for the token | `tests/signals_helm_api_token_test.go` |

## Threat model

In scope:

- Operator typo (`my-token`, `test-token`) — caught at startup.
- Token leakage via log scraping — closed-output discipline.
- ConfigMap / manifest leakage — `secretKeyRef` reference only.
- Rotation visibility — fingerprint-on-restart logging.

Out of scope (separate controls):

- Network-level interception (mTLS, network policies) — operator
  responsibility, documented in deployment guides.
- Insider-threat token exfiltration from a Pod — Kubernetes RBAC
  on the Secret + audit logging on Secret reads is the layer that
  addresses this.
- Brute-force on the bearer token — the 32-char + 8-distinct
  strength rule combined with timing-attack-resistant comparison
  makes online attack infeasible, but rate-limiting is a separate
  hardening layer (out of scope for this issue).
