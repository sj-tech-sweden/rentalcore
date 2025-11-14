-- Add job_history table for audit logging
CREATE TABLE IF NOT EXISTS job_history (
    history_id BIGINT AUTO_INCREMENT PRIMARY KEY,
    job_id INT NOT NULL,
    user_id BIGINT UNSIGNED NULL,
    changed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    change_type ENUM('created', 'updated', 'status_changed', 'device_added', 'device_removed', 'deleted') NOT NULL,
    field_name VARCHAR(100) NULL,
    old_value TEXT NULL,
    new_value TEXT NULL,
    description TEXT NULL,
    ip_address VARCHAR(45) NULL,
    user_agent VARCHAR(255) NULL,
    INDEX idx_job_id (job_id),
    INDEX idx_user_id (user_id),
    INDEX idx_changed_at (changed_at),
    FOREIGN KEY (job_id) REFERENCES jobs(jobID) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(userID) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Add created_by and updated_by fields to jobs table
ALTER TABLE jobs
    ADD COLUMN created_by BIGINT UNSIGNED NULL AFTER jobcategoryID,
    ADD COLUMN created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP AFTER created_by,
    ADD COLUMN updated_by BIGINT UNSIGNED NULL AFTER created_at,
    ADD COLUMN updated_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP AFTER updated_by;

-- Add foreign keys for created_by and updated_by
ALTER TABLE jobs
    ADD CONSTRAINT fk_jobs_created_by FOREIGN KEY (created_by) REFERENCES users(userID) ON DELETE SET NULL,
    ADD CONSTRAINT fk_jobs_updated_by FOREIGN KEY (updated_by) REFERENCES users(userID) ON DELETE SET NULL;

-- Add indexes for better query performance
ALTER TABLE jobs
    ADD INDEX idx_created_by (created_by),
    ADD INDEX idx_updated_by (updated_by),
    ADD INDEX idx_created_at (created_at),
    ADD INDEX idx_updated_at (updated_at);
