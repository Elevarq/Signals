-- Migration: persisted per-target circuit-breaker state (#455).
--
-- Before this migration the circuit-breaker state (open after repeated
-- failures, or paused by an operator) lived only in memory. A daemon
-- restart therefore silently reset every circuit to closed: an
-- auto-tripped target immediately resumed collecting (re-hammering a
-- struggling target during the incident it was protecting), and an
-- operator-initiated pause was silently undone.
--
-- This table records the non-closed circuit state per target so it can
-- be rehydrated on startup. It is keyed by target_name (the stable
-- identity the collector already logs) and upserted on every transition
-- to open/paused, and deleted on the transition back to closed — so a
-- row exists exactly for the targets whose circuit is currently
-- open or paused.
--
-- `since` is the transition timestamp: for an open circuit it anchors
-- the open-cooldown so a cooldown that elapsed during downtime closes on
-- the first post-restart check; for a paused circuit it is informational
-- (a pause has no cooldown and persists until an explicit resume).
--
-- Metadata only — target name, state, timestamp, and the operator
-- pause actor/reason. No credential, DSN, host, or secret material
-- (INV-SIGNALS-07).

CREATE TABLE IF NOT EXISTS circuit_state (
    target_name TEXT PRIMARY KEY,
    state       TEXT NOT NULL,
    since       TEXT NOT NULL,
    reason      TEXT NOT NULL DEFAULT '',
    actor       TEXT NOT NULL DEFAULT '',
    updated_at  TEXT NOT NULL
);
