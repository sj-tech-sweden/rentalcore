-- PostgreSQL-specific fix for job_edit_sessions table
-- Adds UNIQUE constraint on (job_id, user_id) required for ON CONFLICT

-- Add UNIQUE constraint if it doesn't exist
ALTER TABLE job_edit_sessions
  DROP CONSTRAINT IF EXISTS job_edit_sessions_job_user_unique;

ALTER TABLE job_edit_sessions
  ADD CONSTRAINT job_edit_sessions_job_user_unique
  UNIQUE (job_id, user_id);
