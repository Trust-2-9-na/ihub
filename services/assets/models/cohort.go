package models

import (
	"time"

	"gorm.io/gorm"
)

type Cohort struct {
	CohortID    uint64 `gorm:"primaryKey;autoIncrement" json:"cohort_id"`
	Name        string `gorm:"size:100;not null;unique" json:"name"`
	Description string `gorm:"size:255" json:"description,omitempty"`
	StartDate   string `gorm:"not null" json:"start_date"`
	EndDate     string `gorm:"not null" json:"end_date"`

	// Track who created the cohort
	CreatedBy string `gorm:"type:uuid;not null" json:"created_by"`
	Creator   User   `gorm:"foreignKey:CreatedBy;references:UserUUID" json:"creator,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
