package models

import (
	"time"

	"gorm.io/datatypes"
)

type JobLog struct {
	ID        uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	JobName   string         `gorm:"size:100;not null" json:"job_name"`
	Status    string         `gorm:"size:50;not null" json:"status"`
	StartTime time.Time      `gorm:"autoCreateTime" json:"start_time"`
	EndTime   *time.Time     `json:"end_time,omitempty"`
	Message   *string        `json:"message,omitempty"`
	Metadata  datatypes.JSON `gorm:"type:jsonb" json:"metadata,omitempty"` // raw JSON
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
}
