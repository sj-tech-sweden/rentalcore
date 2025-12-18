-- Rollback PostgreSQL-specific fix for job_edit_sessions table

ALTER TABLE job_edit_sessions
  DROP CONSTRAINT IF EXISTS job_edit_sessions_job_user_unique;
