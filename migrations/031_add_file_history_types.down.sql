-- Rollback file_added and file_removed from job_history change_type enum
-- This reverts the enum to its original values

-- First, update any existing file_added/file_removed entries to 'updated'
UPDATE job_history SET change_type = 'updated' WHERE change_type IN ('file_added', 'file_removed');

-- For MySQL, revert the ENUM:
ALTER TABLE job_history 
MODIFY COLUMN change_type ENUM(
    'created', 
    'updated', 
    'status_changed', 
    'device_added', 
    'device_removed', 
    'deleted'
) NOT NULL;
