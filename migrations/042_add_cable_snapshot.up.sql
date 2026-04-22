-- Migration 042: Add cable_snapshot JSONB column to job_cables
--
-- Purpose: Denormalize cable metadata into job_cables so RentalCore can
--          serve cable data without joining to the cross-service cables table.
--          The cable_snapshot column stores a point-in-time JSON copy of the
--          cable record fetched from WarehouseCore.
--
-- Rollback: run 042_add_cable_snapshot.down.sql
--
-- Rollout steps:
--   1. Apply this migration (safe – ADD COLUMN with default null).
--   2. Run the backfill script (tools/backfill_cable_snapshots.go) to populate
--      cable_snapshot for existing rows.
--   3. Enable the CABLE_SNAPSHOT_ENABLED feature flag to switch reads to the
--      snapshot path.
--   4. Monitor logs; rollback by toggling the flag then running the down migration.
--   5. Once stable, schedule a follow-up PR to drop the cross-service FK.

ALTER TABLE job_cables
    ADD COLUMN IF NOT EXISTS cable_snapshot JSONB;

-- Index on cableID supports:
--   • the DB fallback path (batched IN-query when snapshots are missing)
--   • future backfill queries (WHERE "cableID" = ?)
-- The index will remain useful until the cross-service FK is removed in the
-- follow-up PR; at that point it can be re-evaluated.
CREATE INDEX IF NOT EXISTS idx_job_cables_cable_id ON job_cables ("cableID");
