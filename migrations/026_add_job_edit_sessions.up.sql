CREATE TABLE IF NOT EXISTS job_edit_sessions (
    session_id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    job_id INT NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    username VARCHAR(255) NOT NULL,
    display_name VARCHAR(255) NOT NULL,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    last_seen DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_job_edit_sessions_job FOREIGN KEY (job_id) REFERENCES jobs(jobID) ON DELETE CASCADE,
    CONSTRAINT fk_job_edit_sessions_user FOREIGN KEY (user_id) REFERENCES users(userID) ON DELETE CASCADE,
    UNIQUE KEY uk_job_edit_sessions_job_user (job_id, user_id),
    INDEX idx_job_edit_sessions_job (job_id),
    INDEX idx_job_edit_sessions_user (user_id),
    INDEX idx_job_edit_sessions_last_seen (last_seen)
);
