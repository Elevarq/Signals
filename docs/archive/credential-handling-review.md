# Credential Handling Review

## Credential Sources
Arq Signals supports three credential sources, each read fresh per connection:
- password_file -- reads from a file path (Docker secrets compatible)
- password_env -- reads from an environment variable
- pgpass_file -- reads from a pgpass-format file

## Storage: Never Persisted
| Location | Contains Credentials? |
|----------|----------------------|
| SQLite database | NO -- only non-secret metadata (host, port, user) |
| Snapshot exports (ZIP) | NO -- exports contain query results only |
| API /status response | NO -- shows host/port/user/sslmode but not secret_type, secret_ref, or passwords |
| API /health response | NO -- no target information |
| Log output | NO -- credential errors redacted via redactError() |
| Config dumps | NO -- AllowUnsafeRole is env-only; password fields not in yaml output |

## Redaction Mechanisms
- redactError() -- wraps errors containing "password" or "secret" with generic message
- RedactDSN() -- replaces password values in DSN strings for safe logging
- BeforeConnect -- password resolved per connection, not stored on pool config

## Verified by Tests
- TestMigrationSQLNoPasswordColumn -- DB schema has no password column
- TestTargetStructNoPasswordField -- db.Target struct has no Password field
- TestExportQueryRunsNoPasswordField -- export records have no credential fields
- TestRedactDSNPassword -- DSN redaction works
- TestRedactDSNURL -- URL-format DSN redaction works
