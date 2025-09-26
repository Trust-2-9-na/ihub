package models

import "time"

type JobLog struct {
	ID        uint64                 `gorm:"primaryKey;autoIncrement" json:"id"`
	JobName   string                 `gorm:"size:100;not null" json:"job_name"`
	Status    string                 `gorm:"size:50;not null" json:"status"` // Started, Success, Failed
	StartTime time.Time              `gorm:"autoCreateTime" json:"start_time"`
	EndTime   *time.Time             `json:"end_time,omitempty"`
	Message   *string                `json:"message,omitempty"`
	Metadata  map[string]interface{} `gorm:"type:jsonb" json:"metadata,omitempty"` // optional extra info
	CreatedAt time.Time              `gorm:"autoCreateTime" json:"created_at"`
}
