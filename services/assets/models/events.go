package models

import (
	"time"

	"gorm.io/datatypes"
)

const (
	EventTypeWorkshop = "Workshop"
	EventTypeDemo     = "Demo"
	EventTypePitchDay = "Pitch Day"
	EventTypeSeminar  = "Seminar"
)

const (
	VisibilityPublic     = "Public"
	VisibilityPrivate    = "Private"
	VisibilityCohortOnly = "CohortOnly"
	VisibilityRoleBased  = "RoleBased"
)

// Event represents a hub-related event (e.g., workshops, pitch days, seminars)
type Event struct {
	ID          uint64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Title       string  `gorm:"type:varchar(150);not null;index:idx_event_title" json:"title"`
	Description *string `gorm:"type:text" json:"description,omitempty"`
	EventType   string  `gorm:"type:varchar(50);not null" json:"event_type"`
	Visibility  string  `gorm:"type:varchar(20);default:'Public'" json:"visibility"`

	StartTime time.Time `gorm:"not null" json:"start_time"`
	EndTime   time.Time `gorm:"not null" json:"end_time"`
	Location  string    `gorm:"type:varchar(100);not null" json:"location"`

	// Foreign keys
	CreatedByID uint64 `gorm:"not null;index" json:"created_by_id"`
	CreatedBy   User   `gorm:"foreignKey:CreatedByID;references:UserID" json:"created_by,omitempty"`

	CohortRefID *uint64 `gorm:"index" json:"cohort_id,omitempty"`
	CohortRef   *Cohort `gorm:"foreignKey:CohortRefID;references:CohortID" json:"cohort,omitempty"`

	// Associations
	Attendances []EventAttendance `gorm:"foreignKey:EventRefID;constraint:OnDelete:CASCADE" json:"attendances,omitempty"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`

	TargetRoles   datatypes.JSON `json:"target_roles,omitempty"`                 // JSON array of role names
	TargetCohorts datatypes.JSON `json:"target_cohorts,omitempty"`               // JSON array of cohort IDs
	IsArchived    bool           `gorm:"default:false;index" json:"is_archived"` // ← new field

}

type EventAttendance struct {
	ID uint64 `gorm:"primaryKey;autoIncrement" json:"id"`

	EventRefID    uint64 `gorm:"not null;index:idx_unique_attendance,unique" json:"event_id"`
	AttendeeRefID uint64 `gorm:"not null;index:idx_unique_attendance,unique" json:"attendee_id"`

	EventRef    Event `gorm:"foreignKey:EventRefID;references:ID;constraint:OnDelete:CASCADE" json:"event,omitempty"`
	AttendeeRef User  `gorm:"foreignKey:AttendeeRefID;references:UserID;constraint:OnDelete:CASCADE" json:"attendee,omitempty"`

	RegisteredAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"registered_at"`

	// Track actual attendance
	Attended  bool       `gorm:"default:false" json:"attended"`
	CheckInAt *time.Time `json:"check_in_at,omitempty"`
}
