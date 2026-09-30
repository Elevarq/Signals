# Scheduled auto-export (SE)

- **Prefix:** `SE`
- **Issue:** [#350](https://github.com/Elevarq/Signals/issues/350)
- **Status:** ACTIVE

## Purpose

Signals collects and stores its data on a schedule. Scheduled auto-export
adds the ability to **write the latest snapshot export to a configured file
location on that schedule**, so the destination always holds a fresh export
with no per-cycle operator action.

This is a self-contained Signals capability: **collect → store →
export-to-a-file-location, repeated on the collection schedule.** What (if
anything) consumes the exported files is out of scope; Signals has no
knowledge of any downstream consumer.

## Configuration

| Setting | Env | YAML | Default | Meaning |
|---|---|---|---|---|
| Enable | `SIGNALS_EXPORT_ON_COLLECT` | `export_on_collect` | `false` | Write an export after each collection cycle. |
| Destination | `SIGNALS_EXPORT_DEST` | `export_dest` | `""` | Directory to write exports into. |

Scheduled export runs only when enabled **and** a non-empty destination is
set; otherwise it is a no-op (existing behaviour unchanged).

## Rules

- **SE-R010** — When enabled, Signals writes the **latest-snapshot** export
  (the default export scope — the newest snapshot per target, the same scope
  a filterless on-demand export uses) to the destination **after every
  completed collection cycle**: the initial baseline cycle, each scheduled
  poll, and each on-demand collection.
- **SE-R011** — Each export is written to a **flat file** in the destination
  directory named `<instance-id>-<UTC-timestamp>.zip` (no subdirectories).
  The instance-id component disambiguates several Signals instances writing
  to one shared directory; the nanosecond timestamp makes each file unique
  so a new export never overwrites a prior one. The instance-id is sanitized
  to `[A-Za-z0-9._-]` (other runs of characters collapse to `_`); a blank
  instance-id falls back to `signals`.
- **SE-R012** — The write is **atomic**: the export is written to a temporary
  file and renamed into place, so a process watching the destination never
  observes a partially-written ZIP. A temporary file is never left behind on
  success or failure.
- **SE-R013** — A failed export (build error, unwritable destination) is
  **logged and skipped**; it MUST NOT disrupt or fail the collection cycle.

## Invariants

- **SE-INV-01** — Scheduled export never changes what is collected or stored,
  nor the export ZIP's contents; it only writes the already-defined
  latest-snapshot export to a file on the collection schedule.
- **SE-INV-02** — Files are flat in the destination (no per-target
  subdirectories), so a flat-directory consumer sees every export.

## Acceptance cases

- **SE-AC-1 (normal)** — With `SIGNALS_EXPORT_ON_COLLECT=true` and
  `SIGNALS_EXPORT_DEST=<dir>`, a fresh `<instance>-<ts>.zip` appears in
  `<dir>` after each collection cycle, unattended.
- **SE-AC-2 (boundary — shared destination)** — Two Signals instances with
  distinct instance-ids exporting to one directory never collide (distinct
  filename prefixes); distinct cycles never overwrite (distinct timestamps).
- **SE-AC-3 (failure)** — When the export build fails or the destination is
  unwritable, no partial/temp file remains and the collection cycle
  completes normally (SE-R012/SE-R013).
- **SE-AC-4 (disabled)** — With the feature off (default), no files are
  written and behaviour is byte-identical to before.

## S3 destination (#472)

`export_dest` is overloaded: in addition to a local directory it accepts an
`s3://bucket/prefix` URI, in which case each per-database export is uploaded
directly to S3 instead of written to a local file. This closes the Cloud
delivery path (Signals → S3 → analyzer inbox) with no local-file + uploader
workaround. All of SE-R010/SE-R011 (latest-per-target, one object per database,
flat `<instance>-t<targetID>-<ts>.zip` keys) apply unchanged; only the storage
backend differs.

Configuration: `export_s3_region` / `SIGNALS_EXPORT_S3_REGION` (optional; the
pod's ambient region when empty) and `export_s3_kms_key_id` /
`SIGNALS_EXPORT_S3_KMS_KEY_ID` (optional).

### Rules

- **SE-R020** — When `export_dest` begins `s3://`, the exporter uploads each
  per-target export with a single `s3:PutObject` to
  `s3://<bucket>/<prefix>/<instance>-t<targetID>-<ts>.zip`. It MUST NOT list,
  get, or delete — the delivery identity needs exactly one action.
- **SE-R021** — Credentials come only from the default AWS chain (IRSA /
  instance role). No static keys appear in config, env, logs, or state.
- **SE-R022** — Every object is written server-side encrypted: SSE-S3 (AES256)
  by default, or SSE-KMS with `export_s3_kms_key_id` when set. No ACL is set.
- **SE-R023** — A completed PutObject is atomic, so a consumer never observes a
  partial object; no temp/rename is used. A failed PutObject is returned to the
  post-cycle hook, logged, and skipped — it never disrupts collection (SE-R013).
- **SE-R024** — Retention (`export_retention_days` / `export_max_files`) is a
  no-op for the S3 backend; S3 retention is an object-lifecycle rule, because
  pruning would require list/delete (violating SE-R020).
- **SE-R025** — A malformed `s3://` URI (missing scheme or empty bucket) fails
  loud at config validation (startup), like other config errors.

### Acceptance cases (S3)

- **SE-AC-5 (normal)** — With `export_dest=s3://b/p` and two targets, a cycle
  issues two PutObjects with per-database keys under `p`, SSE-S3.
- **SE-AC-6 (KMS)** — With `export_s3_kms_key_id` set, PutObject uses SSE-KMS
  with that key.
- **SE-AC-7 (failure)** — A PutObject error is returned to the hook and logged;
  the collection cycle completes normally (no panic).
- **SE-AC-8 (invalid)** — `export_dest=s3://` (empty bucket) fails config
  validation at startup.
