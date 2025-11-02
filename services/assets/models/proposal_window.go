package models

import "time"

type ProposalSubmissionWindow struct {
	WindowID           uint64    `gorm:"primaryKey;autoIncrement" json:"window_id"`
	Title              string    `gorm:"size:200;not null" json:"title"`
	StartDate          time.Time `json:"start_date"`
	Deadline           time.Time `json:"deadline"`
	IsActive           bool      `gorm:"default:true" json:"is_active"`    // ✅ controls open/closed state
	IsArchived         bool      `gorm:"default:false" json:"is_archived"` // ✅ keeps old windows separate
	School             *string   `json:"school,omitempty" gorm:"size:255"`
	Program            *string   `json:"program,omitempty" gorm:"size:255"`
	YearOfStudy        *string   `json:"year_of_study,omitempty" gorm:"size:50"`
	LastNotifiedStatus string    `gorm:"size:50;default:''" json:"last_notified_status"`

	CreatedByID uint64 `json:"created_by"` // FK
	CreatedBy   User   `gorm:"foreignKey:CreatedByID;references:UserID" json:"created_by_user"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}
