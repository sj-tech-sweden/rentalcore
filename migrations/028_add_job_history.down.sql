-- Remove indexes from jobs table
ALTER TABLE jobs
    DROP INDEX IF EXISTS idx_created_by,
    DROP INDEX IF EXISTS idx_updated_by,
    DROP INDEX IF EXISTS idx_created_at,
    DROP INDEX IF EXISTS idx_updated_at;

-- Remove foreign keys from jobs table
ALTER TABLE jobs
    DROP FOREIGN KEY IF EXISTS fk_jobs_created_by,
    DROP FOREIGN KEY IF EXISTS fk_jobs_updated_by;

-- Remove created_by and updated_by fields from jobs table
ALTER TABLE jobs
    DROP COLUMN IF EXISTS created_by,
    DROP COLUMN IF EXISTS created_at,
    DROP COLUMN IF EXISTS updated_by,
    DROP COLUMN IF EXISTS updated_at;

-- Drop job_history table
DROP TABLE IF EXISTS job_history;
