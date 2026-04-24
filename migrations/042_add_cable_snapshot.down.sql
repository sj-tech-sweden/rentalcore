-- Migration 042 rollback: remove cable_snapshot column from job_cables
--
-- NOTE: run this only after disabling the CABLE_SNAPSHOT_ENABLED feature flag
--       so that in-flight requests do not try to read a column that no longer
--       exists.  The original cross-service FK to cables("cableID") is kept
--       intact by this migration; it is only removed in a future PR.

DROP INDEX IF EXISTS idx_job_cables_snapshot_backfill;

ALTER TABLE job_cables
    DROP COLUMN IF EXISTS cable_snapshot;
