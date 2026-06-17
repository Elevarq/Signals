# Local Superuser Override Example

This example shows how to run Elevarq Signals with the `postgres`
superuser for **local testing only**. This is not recommended for
production.

## Why the superuser is blocked by default

Elevarq Signals validates the connected role before collecting. If the
role has superuser, replication, or bypassrls attributes, collection
is blocked with an error:

```
collection blocked: role "postgres" has superuser attribute (rolsuper=true)
```

This is intentional — it prevents accidental execution with elevated
privileges on production databases.

## How to override (local/dev only)

Set the `ARQ_SIGNALS_ALLOW_UNSAFE_ROLE` environment variable:

```bash
ARQ_ALLOW_INSECURE_PG_TLS=true \
ARQ_SIGNALS_ALLOW_UNSAFE_ROLE=true \
ARQ_SIGNALS_API_TOKEN=dev-local-only-replace-in-prod-32chars \
./bin/arq-signals --config examples/local-superuser-override/signals.yaml
```

## What happens with the override

1. The safety check detects superuser/replication/bypassrls attributes
2. Instead of blocking, it logs a prominent warning:
   ```
   WARN UNSAFE MODE: bypassing safety checks — not recommended for production
   ```
3. Collection proceeds
4. **The override is recorded in export metadata:**

```json
{
  "unsafe_mode": true,
  "unsafe_reasons": [
    "role \"postgres\" has superuser attribute (rolsuper=true) — collection requires a non-superuser role",
    "role \"postgres\" has replication attribute (rolreplication=true) — collection requires a role without replication privileges",
    "role \"postgres\" has bypassrls attribute (rolbypassrls=true) — collection requires a role without BYPASSRLS"
  ]
}
```

## When to use this

- Local development and testing
- Quick evaluation before creating a dedicated monitoring role
- CI/CD environments using the default postgres role

## When NOT to use this

- Production databases
- Any environment where elevated privileges are a concern
- Anywhere the export metadata needs to show `unsafe_mode: false`

## Recommended next step

Create a proper monitoring role:

```sql
CREATE ROLE arq_signals LOGIN;
GRANT pg_monitor TO arq_signals;
```

Then switch to the [local safe role example](../local-safe-role/).
