package models

import (
	"database/sql"
	"time"
)

// JobHistory represents an audit log entry for job changes
type JobHistory struct {
	HistoryID   uint64         `json:"history_id" gorm:"primaryKey;column:history_id;autoIncrement"`
	JobID       uint           `json:"job_id" gorm:"column:job_id;not null;index"`
	UserID      sql.NullInt64  `json:"user_id" gorm:"column:user_id;index"`
	ChangedAt   time.Time      `json:"changed_at" gorm:"column:changed_at;default:CURRENT_TIMESTAMP;index"`
	ChangeType  string         `json:"change_type" gorm:"column:change_type;type:enum('created','updated','status_changed','device_added','device_removed','deleted');not null"`
	FieldName   sql.NullString `json:"field_name" gorm:"column:field_name;size:100"`
	OldValue    sql.NullString `json:"old_value" gorm:"column:old_value;type:text"`
	NewValue    sql.NullString `json:"new_value" gorm:"column:new_value;type:text"`
	Description sql.NullString `json:"description" gorm:"column:description;type:text"`
	IPAddress   sql.NullString `json:"ip_address" gorm:"column:ip_address;size:45"`
	UserAgent   sql.NullString `json:"user_agent" gorm:"column:user_agent;size:255"`

	// Relations
	Job  *Job  `json:"job,omitempty" gorm:"foreignKey:JobID"`
	User *User `json:"user,omitempty" gorm:"foreignKey:UserID"`
}

func (JobHistory) TableName() string {
	return "job_history"
}

// JobHistoryEntry is a formatted version for API responses
type JobHistoryEntry struct {
	HistoryID   uint64    `json:"history_id"`
	JobID       uint      `json:"job_id"`
	UserID      *uint     `json:"user_id"`
	UserName    string    `json:"user_name"`
	ChangedAt   time.Time `json:"changed_at"`
	ChangeType  string    `json:"change_type"`
	FieldName   *string   `json:"field_name,omitempty"`
	OldValue    *string   `json:"old_value,omitempty"`
	NewValue    *string   `json:"new_value,omitempty"`
	Description string    `json:"description"`
	IPAddress   *string   `json:"ip_address,omitempty"`
}

// FormatFieldName returns a human-readable field name
func FormatFieldName(field string) string {
	fieldNames := map[string]string{
		"customerID":     "Customer",
		"statusID":       "Status",
		"jobcategoryID":  "Job Category",
		"description":    "Description",
		"discount":       "Discount",
		"discount_type":  "Discount Type",
		"revenue":        "Revenue",
		"final_revenue":  "Final Revenue",
		"startDate":      "Start Date",
		"endDate":        "End Date",
		"priority":       "Priority",
		"internal_notes": "Internal Notes",
		"customer_notes": "Customer Notes",
	}

	if name, ok := fieldNames[field]; ok {
		return name
	}
	return field
}
