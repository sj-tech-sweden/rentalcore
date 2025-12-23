-- Add file_added and file_removed to job_history change_type enum
-- This allows tracking who uploaded/removed files from jobs

-- For SQLite (which the app uses at runtime), ENUM is stored as TEXT with CHECK
-- For PostgreSQL, we need to modify the enum type

-- SQLite compatible version (TEXT column, no enum modification needed)
-- The application model already accepts the new values

-- For MySQL, modify the ENUM:
-- Note: This is a no-op if using SQLite since SQLite doesn't have ENUM
ALTER TABLE job_history 
MODIFY COLUMN change_type ENUM(
    'created', 
    'updated', 
    'status_changed', 
    'device_added', 
    'device_removed', 
    'deleted',
    'file_added',
    'file_removed'
) NOT NULL;
