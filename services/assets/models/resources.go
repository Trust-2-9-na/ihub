package models

import (
	"time"

	"gorm.io/gorm"
)

type ResourceType string

const (
	Document ResourceType = "Document"
	Link     ResourceType = "Link"
	Video    ResourceType = "Video"
	Template ResourceType = "Template"
	Other    ResourceType = "Other"
)

type ResourceVisibility string

const (
	Public  ResourceVisibility = "Public"
	Private ResourceVisibility = "Private"
)

type Resource struct {
	ID          uint64 `gorm:"primaryKey;autoIncrement"`
	Title       string `gorm:"type:varchar(200);not null"`
	Description string `gorm:"type:text"`

	ResourceType string `gorm:"type:varchar(50);not null;default:'Document';index"`

	URL      string `gorm:"type:varchar(500);default:null"` // For external resources
	FilePath string `gorm:"type:varchar(500);default:null"` // For uploaded files

	// ✅ Uploader relationship
	UploaderID uint64 `gorm:"index;not null" json:"uploader_id"` // FK → Users.UserID
	Uploader   User   `gorm:"foreignKey:UploaderID;references:UserID" json:"uploader"`

	// ✅ Cohort relationship (renamed to avoid collision)
	ResourceCohortID *uint64 `gorm:"index" json:"resource_cohort_id,omitempty"` // FK → Cohorts.ID
	ResourceCohort   *Cohort `gorm:"foreignKey:ResourceCohortID;references:CohortID" json:"resource_cohort,omitempty"`

	// ✅ Optional direct sharing with a user
	SharedWithUserID *uint64 `gorm:"index" json:"shared_with_user_id,omitempty"` // FK → Users.UserID
	SharedWithUser   *User   `gorm:"foreignKey:SharedWithUserID;references:UserID" json:"shared_with_user,omitempty"`

	ResourceTeamID *uint64 `gorm:"index" json:"resource_team_id,omitempty"` // FK → Teams.TeamID
	ResourceTeam   *Team   `gorm:"foreignKey:ResourceTeamID;references:TeamID" json:"resource_team,omitempty"`

	Visibility ResourceVisibility `gorm:"size:20;default:'Public'" json:"visibility"`

	IsArchived bool           `gorm:"default:false" json:"is_archived"`
	CreatedAt  time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}
