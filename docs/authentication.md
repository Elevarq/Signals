# Authentication

Arq Signals authenticates HTTP API callers with bearer tokens. This
document describes both authentication modes — `standalone`
(default) and `arq_managed` — including how tokens map to audit
`actor` identity and how to rotate the control-plane token without
restarting the daemon. Specs: ARQ-SIGNALS-R011, R083.

## Modes at a glance

| Mode | Tokens recognised | `actor` mapping |
|---|---|---|
| `standalone` (default) | `api.token` | matched → `actor = local_operator` |
| `arq_managed` | `api.token`, `arq_control_plane_token` | `api.token` → `local_operator`; `arq_control_plane_token` → `arq_control_plane` |

`mode` is set by `signals.mode` in `signals.yaml` or via the
`ARQ_SIGNALS_MODE` environment variable. Default is `standalone` —
operators who don't run the Arq control plane don't need to change
anything.

## The local API token

Used for every operator-driven call (CLI, manual `curl`, automation
scripts the operator runs). Configured the same way it always has
been:

- `api.token` field in `signals.yaml`, **or**
- `ARQ_SIGNALS_API_TOKEN` environment variable, **or**
- `ARQ_SIGNALS_API_TOKEN_FILE` pointing at a file containing the
  token.

If none of these are set, Arq Signals auto-generates a 32-byte
random token at startup and logs the SHA-256 fingerprint (not the
value) so the operator can confirm which token is active.

## The Arq control-plane token (Mode B only)

When `signals.mode: arq_managed`, the daemon also accepts a second
bearer token, distinct from `api.token`, identified in audit events
as `actor = arq_control_plane`.

### Configuration

Pick exactly **one** of these two sources. Setting both at the same
time is a hard startup error.

```yaml
signals:
  mode: arq_managed

  # Preferred: token file. Re-read on every authentication
  # attempt so rotation does not require restart.
  arq_control_plane_token_file: /etc/arq/control-plane.token

  # Alternative: env var indirection. Treat the env var as the
  # source of the token value. Same posture as the existing
  # ARQ_SIGNALS_API_TOKEN_FILE / ARQ_SIGNALS_API_TOKEN pattern.
  # arq_control_plane_token_env: ARQ_CONTROL_PLANE_TOKEN
```

Equivalent environment-variable overrides:

| Variable | Maps to |
|---|---|
| `ARQ_SIGNALS_MODE` | `signals.mode` |
| `ARQ_SIGNALS_ARQ_CONTROL_PLANE_TOKEN_FILE` | `signals.arq_control_plane_token_file` |
| `ARQ_SIGNALS_ARQ_CONTROL_PLANE_TOKEN_ENV` | `signals.arq_control_plane_token_env` |

### Token requirements

The control-plane token must:

1. Be at least **32 characters** long. (Same floor as the auto-
   generated `api.token`.)
2. Be **distinct** from `api.token`. The startup check is constant-
   time. If the two tokens are equal, the daemon refuses to start.
3. Match the `arq_managed` mode setting. Setting only one of the
   two (mode without a token, or token without the mode) aborts
   startup.

These checks live in `config.ValidateStrict` plus
`config.ValidateModeBTokens`; failures produce an explicit error
message naming the offending field.

### File vs environment-variable source

| Property | File (`_token_file`) | Env var (`_token_env`) |
|---|---|---|
| Read pattern | `os.ReadFile` on every authentication attempt. | `os.LookupEnv` on every authentication attempt — but the daemon process inherits its env at start; updating the env outside the process does not change what `os.LookupEnv` returns. |
| Rotation without restart | Yes — operator overwrites the file; the next request reads the new value. | No — env values are captured into the process at fork; the operator must restart the daemon to pick up a new value. |
| Value at rest on disk | Yes — operator restricts file mode (recommended `0600`). | No — value lives in process memory only. |
| Best fit | Long-running deployments where rotation matters; secret managers that mount tokens as files. | Short-lived containers where the orchestrator injects the value once at start and the unit is replaced wholesale on rotation. |

The `_env` source is therefore best read as "read this env var
**once at start**". Operators who treat env vars as rotatable need
to either (a) switch to `_token_file`, or (b) recycle the daemon as
the rotation mechanism — `os.LookupEnv` does not observe external
env-var mutations during the process lifetime.

## Rotation

The file-based source is re-read on every authentication attempt.
Rotation is therefore a single file write:

```bash
echo "new-token-value-…" > /etc/arq/control-plane.token
chmod 600 /etc/arq/control-plane.token
```

The next request authenticates against the new token. Any client
still presenting the previous token receives `401 Unauthorized`.

If the file is rotated to an empty value, the daemon emits a
`slog.Warn` (`arq_control_plane_token resolved to empty value`) and
control-plane authentication degrades to "no token" — the
control-plane caller's requests start receiving 401s. This is
visible in the daemon log so the rotation breakage is observable.

There is **no token-rotation HTTP endpoint** — rotation is a file
operation handled by the operator's secret store / orchestrator.

## Authentication failure handling

| Symptom | Daemon response | Audit |
|---|---|---|
| Missing `Authorization` header | 401 | (no audit event) |
| Bearer token matches neither configured token | 401 + R024 rate limiter records the failure | (no audit event in R083) |
| Bearer matches `api.token` | 200/202/4xx as the handler decides | event carries `actor=local_operator` |
| Bearer matches `arq_control_plane_token` (Mode B) | 200/202/4xx as the handler decides | event carries `actor=arq_control_plane` |

The R024 per-IP rate limiter blocks an IP after a configured number
of failed attempts in a window. That behaviour is unchanged from
Phase 2.

R083 deliberately does **not** emit per-failure audit events. Auth
attempts can be noisy under bot scanning; explicit `auth_failed`
records are deferred to a future audit-completeness pass with their
own rate limiting.

## Security posture

- **Both tokens compared in constant time** (`crypto/subtle`).
- **Token values never logged.** The R078 audit-attribute denylist
  blocks any audit attribute key containing `password`, `secret`,
  `token`, `dsn`, `connection_string`, `payload`, or `query_result`.
  A small allow-list permits the boolean
  `arq_control_plane_token_configured` (no value content) on the
  `mode_configured` startup event.
- **No token in error messages.** Resolution errors carry the
  configured *path* or *env var name*, never the file contents.
- **No `arq_control_plane` actor without the mode.** In
  `mode=standalone`, the `arq_control_plane_token` config (if
  present) is ignored at auth time. A request that would have
  matched the control-plane token simply gets a 401, identical to
  any other unknown token. There is no way for a request to acquire
  the privileged actor identity except by holding the
  control-plane token AND running in `arq_managed` mode.

## Example: enabling Mode B

`signals.yaml`:

```yaml
signals:
  mode: arq_managed
  arq_control_plane_token_file: /etc/arq/control-plane.token

api:
  listen_addr: 127.0.0.1:8081
```

Token file (mode 0600, owned by the daemon user):

```
$ cat /etc/arq/control-plane.token
01HXY9QZK5T8M3FN6JBPRWADCV7E2GH4
```

Startup audit event (token VALUE never logged):

```
audit_event=mode_configured mode=arq_managed arq_control_plane_token_configured=true
```

Subsequent requests:

```bash
# As the local operator (api.token):
curl -H "Authorization: Bearer ${ARQ_SIGNALS_API_TOKEN}" http://127.0.0.1:8081/status
# audit: actor=local_operator

# As the Arq control plane (arq_control_plane_token):
curl -H "Authorization: Bearer 01HXY9QZK5T8M3FN6JBPRWADCV7E2GH4" \
  -X POST http://127.0.0.1:8081/collect/now \
  -H 'Content-Type: application/json' \
  -d '{"targets":["prod-main"], "request_id":"01J5K…", "reason":"automated"}'
# audit: actor=arq_control_plane
```

## What's intentionally not here

- **No mTLS / signed JWTs / OIDC.** Higher-strength auth is
  Phase 4+ work.
- **No `arq_managed_only` mode** that refuses the local API token
  in Mode B. The local token remains valid in both modes so
  operators are never locked out by an Arq-side outage.
- **No license enforcement in Arq Signals.** Per R082, the
  collector is open source; commercial value is in the analysis
  layer, not in obscured collector behaviour.
