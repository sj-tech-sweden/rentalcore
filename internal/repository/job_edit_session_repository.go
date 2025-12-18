package repository

import (
	"time"

	"go-barcode-webapp/internal/models"
)

// JobEditSessionRepository manages active job editing sessions.
type JobEditSessionRepository struct {
	db *Database
}

// NewJobEditSessionRepository creates a new repository instance.
func NewJobEditSessionRepository(db *Database) *JobEditSessionRepository {
	return &JobEditSessionRepository{db: db}
}

// UpsertSession inserts or refreshes an editing session for a user/job combination.
func (r *JobEditSessionRepository) UpsertSession(jobID, userID uint, username, displayName string) error {
	now := time.Now()
	return r.db.Exec(`
		INSERT INTO job_edit_sessions (job_id, user_id, username, display_name, started_at, updated_at, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (job_id, user_id) DO UPDATE SET
			username = EXCLUDED.username,
			display_name = EXCLUDED.display_name,
			updated_at = EXCLUDED.updated_at,
			last_seen = EXCLUDED.last_seen
	`, jobID, userID, username, displayName, now, now, now).Error
}

// RemoveSession deletes an editing session for the given job/user combination.
func (r *JobEditSessionRepository) RemoveSession(jobID, userID uint) error {
	return r.db.Exec(`DELETE FROM job_edit_sessions WHERE job_id = ? AND user_id = ?`, jobID, userID).Error
}

// CleanupExpired removes sessions whose last_seen is older than the provided cutoff.
func (r *JobEditSessionRepository) CleanupExpired(cutoff time.Time) error {
	return r.db.Exec(`DELETE FROM job_edit_sessions WHERE last_seen < ?`, cutoff).Error
}

// GetActiveEditors returns all editors for the job that are not older than the cutoff and excludes the provided user ID.
func (r *JobEditSessionRepository) GetActiveEditors(jobID, excludeUserID uint, cutoff time.Time) ([]models.JobEditSession, error) {
	if err := r.CleanupExpired(cutoff); err != nil {
		return nil, err
	}

	var sessions []models.JobEditSession
	err := r.db.Where("job_id = ? AND user_id <> ?", jobID, excludeUserID).
		Where("last_seen >= ?", cutoff).
		Order("last_seen DESC").
		Find(&sessions).Error
	return sessions, err
}
