package models

import "time"

type ProposalSubmissionWindow struct {
	WindowID    uint64    `gorm:"primaryKey;autoIncrement" json:"window_id"`
	Title       string    `gorm:"size:200;not null" json:"title"`
	StartDate   time.Time `json:"start_date"`
	Deadline    time.Time `json:"deadline"`
	CreatedByID uint64    `json:"created_by"` // FK
	CreatedBy   User      `gorm:"foreignKey:CreatedByID" json:"created_by_user"`
	CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}
