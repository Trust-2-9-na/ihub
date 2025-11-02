package models

import (
	"time"

	"gorm.io/gorm"
)

type MentorStudentAssignment struct {
	ID           uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	CohortRefID  uint64         `gorm:"not null;index" json:"cohort_ref_id"`  // refers to cohort
	MentorRefID  uint64         `gorm:"not null;index" json:"mentor_ref_id"`  // refers to mentor user
	StudentRefID uint64         `gorm:"not null;index" json:"student_ref_id"` // refers to student user
	CreatedBy    *uint64        `gorm:"index" json:"created_by"`              // supervisor
	CreatedAt    time.Time      `gorm:"autoCreateTime" json:"created_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`

	// Optional relationships
	Mentor  *User `gorm:"foreignKey:MentorRefID;references:UserID" json:"mentor,omitempty"`
	Student *User `gorm:"foreignKey:StudentRefID;references:UserID" json:"student,omitempty"`
}
