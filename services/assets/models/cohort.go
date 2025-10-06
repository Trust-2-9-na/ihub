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
	Users     []User `gorm:"many2many:cohort_users;" json:"users,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// CohortUser represents the many-to-many relationship between users and cohorts
type CohortUser struct {
	CohortID uint64 `gorm:"column:cohort_cohort_id;primaryKey" json:"cohort_id"`
	UserID   uint64 `gorm:"column:user_user_id;primaryKey" json:"user_id"`
	Role     string `gorm:"size:50" json:"role,omitempty"` // "Student", "Mentor", "Supervisor"

	Cohort Cohort `gorm:"foreignKey:CohortID;references:CohortID" json:"cohort,omitempty"`
	User   User   `gorm:"foreignKey:UserID;references:UserID" json:"user,omitempty"`

	CreatedAt time.Time      `json:"created_at,omitempty"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
