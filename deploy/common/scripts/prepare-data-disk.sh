#!/usr/bin/env bash
#
# prepare-data-disk.sh — resolve, wait for, verify, and (only if provably blank)
# format the intended durable data volume, then mount it. Shared by the AWS /
# Azure / GCP VM bootstraps (#713 / audit F07).
#
# The previous per-provider logic was unsafe: when the intended device was not
# yet attached it fell back to SCANNING block devices and picking an arbitrary
# non-root disk, and it treated ANY `blkid` failure — including a probe/I-O
# error — as "no filesystem, format it". That could format an unrelated disk,
# use nondurable storage, or destroy data on a transient probe error.
#
# This script fails closed instead:
#   * Resolve ONLY by the intended provider device identity (a glob of stable
#     by-id / LUN paths). Never scan-and-pick an arbitrary disk.
#   * Wait a bounded time for the device to attach; fail if it never appears or
#     if the identity is ambiguous (matches more than one device).
#   * Format ONLY when a low-level probe proves the device is blank (blkid -p
#     exit 2). On a probe error (any other non-zero) FAIL CLOSED — never format.
#   * Verify the mount source is the intended device after mounting.
#
# Inputs — positional args take precedence; otherwise read from the environment
# (the VM bootstraps embed this script inline and export the values):
#   $1 / DATA_DISK_IDENTITY  stable provider path, may be a glob matching
#                            exactly one device, e.g.
#                            /dev/disk/by-id/google-signals-data or
#                            /dev/disk/azure/scsi1/lun0.
#   $2 / DATA_MOUNT          mount point (default /data).
#   $3 / DATA_OWNER          uid:gid to chown the mount to (default 10001:10001).
#   $4 / DATA_WAIT_SECONDS   attachment deadline in seconds (default 120).
#
set -euo pipefail

die() { echo "prepare-data-disk: $*" >&2; exit 1; }

IDENTITY="${1:-${DATA_DISK_IDENTITY:-}}"
MOUNT="${2:-${DATA_MOUNT:-/data}}"
OWNER="${3:-${DATA_OWNER:-10001:10001}}"
DEADLINE="${4:-${DATA_WAIT_SECONDS:-120}}"

[ -n "$IDENTITY" ] || die "no device identity given (arg 1 or DATA_DISK_IDENTITY)"

# Resolve the intended device by identity, waiting up to the deadline. Fail
# closed on absence or ambiguity — never fall back to scanning for "some" disk.
DATA_DEV=""
elapsed=0
while :; do
  # shellcheck disable=SC2086
  matches=$(for p in $IDENTITY; do [ -e "$p" ] && readlink -f "$p"; done | sort -u)
  count=$(printf '%s\n' "$matches" | grep -c . || true)
  if [ "$count" -eq 1 ]; then
    DATA_DEV="$matches"
    break
  elif [ "$count" -gt 1 ]; then
    die "device identity '$IDENTITY' is ambiguous (matched $count devices); refusing to guess"
  fi
  [ "$elapsed" -ge "$DEADLINE" ] && die "intended data device '$IDENTITY' did not attach within ${DEADLINE}s (fail closed)"
  sleep 2
  elapsed=$((elapsed + 2))
done

# PREPARE_DISK_SKIP_BLOCK_CHECK is a TEST-ONLY seam: creating a real block
# device needs root (mknod), so the behavioral test sets this to exercise the
# format-decision and fail-closed paths against regular-file fixtures. It is
# never set in the production bootstraps.
if [ "${PREPARE_DISK_SKIP_BLOCK_CHECK:-0}" != "1" ]; then
  [ -b "$DATA_DEV" ] || die "resolved path '$DATA_DEV' is not a block device (fail closed)"
fi

mkdir -p "$MOUNT"

# Decide whether to format using a LOW-LEVEL probe with explicit exit handling.
# blkid -p exit codes: 0 = a filesystem/signature was found (do NOT format);
# 2 = nothing found, the device is blank (safe to format); anything else = a
# probe/I-O error (fail closed — never format on an error).
set +e
blkid -p "$DATA_DEV" >/dev/null 2>&1
probe_rc=$?
set -e
case "$probe_rc" in
  0)
    : # existing filesystem/signature — preserve it, never reformat.
    ;;
  2)
    mkfs.ext4 -F "$DATA_DEV"
    ;;
  *)
    die "blkid probe of '$DATA_DEV' failed (rc=$probe_rc); refusing to format on a probe error"
    ;;
esac

# Persist by stable UUID (survives device renaming) and mount.
# PREPARE_DISK_FSTAB is a TEST-ONLY override (default /etc/fstab, which is
# root-only) so the behavioral test can assert the persisted mount entry.
FSTAB="${PREPARE_DISK_FSTAB:-/etc/fstab}"
DATA_UUID="$(blkid -s UUID -o value "$DATA_DEV")"
[ -n "$DATA_UUID" ] || die "could not read filesystem UUID of '$DATA_DEV' after preparation"
grep -q "$DATA_UUID" "$FSTAB" 2>/dev/null || \
  echo "UUID=$DATA_UUID $MOUNT ext4 defaults,nofail 0 2" >> "$FSTAB"
mount "$MOUNT" || mount "$DATA_DEV" "$MOUNT"

# Verify the mount source is the device we intended — fail closed otherwise.
mounted_src="$(findmnt -no SOURCE "$MOUNT" 2>/dev/null || true)"
mounted_real="$(readlink -f "$mounted_src" 2>/dev/null || true)"
if [ -n "$mounted_real" ] && [ "$mounted_real" != "$DATA_DEV" ]; then
  die "mounted source '$mounted_real' is not the intended device '$DATA_DEV'"
fi

chown "$OWNER" "$MOUNT"
echo "prepare-data-disk: $MOUNT ready on $DATA_DEV (uuid $DATA_UUID)"
