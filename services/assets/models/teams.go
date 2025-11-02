package models

import (
	"time"

	"gorm.io/gorm"
)

// ─── TEAM MODEL ───────────────────────────────────────────────────────────────

type TeamRole string

const (
	TeamLeader TeamRole = "TeamLeader"
	Member     TeamRole = "Member"
)

type Team struct {
	TeamID        uint64  `gorm:"primaryKey;autoIncrement" json:"team_id"`
	Name          string  `gorm:"size:100;not null;Index" json:"name"`
	Description   string  `gorm:"size:255" json:"description,omitempty"`
	CohortRefID   *uint64 `gorm:"index;constraint:OnDelete:SET NULL;" json:"cohort_ref_id,omitempty"`
	CohortDetails *Cohort `gorm:"foreignKey:CohortRefID;references:CohortID" json:"cohort_details,omitempty"`

	CreatedByID uint64 `gorm:"not null;index" json:"created_by_id"` // New field for tracking creator

	LinkedEntityID *uint64         `gorm:"column:linked_entity_id;index" json:"linked_entity_id,omitempty"`
	LinkedEntity   *ProgressEntity `gorm:"foreignKey:LinkedEntityID;references:ID" json:"linked_entity,omitempty"`

	CreatedAt  time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
	UserTeams  []UserTeam     `gorm:"foreignKey:TeamRefID" json:"user_teams,omitempty"`
	IsArchived bool           `gorm:"default:false" json:"is_archived"`
}
type UserTeam struct {
	TeamRefID uint64    `gorm:"column:team_team_id;primaryKey" json:"team_ref_id"` // renamed for clarity
	UserRefID uint64    `gorm:"column:user_user_id;primaryKey" json:"user_ref_id"`
	Role      string    `gorm:"type:varchar(20);default:'Member'"`
	JoinedAt  time.Time `gorm:"autoCreateTime"`

	// Struct references
	UserRef User `gorm:"foreignKey:UserRefID;references:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"user,omitempty"`
	TeamRef Team `gorm:"foreignKey:TeamRefID;references:TeamID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"team,omitempty"`
}
