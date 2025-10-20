package models

import (
	"time"

	"gorm.io/gorm"
)

// ─── TEAM MODEL ───────────────────────────────────────────────────────────────

type Team struct {
	TeamID      uint64         `gorm:"primaryKey;autoIncrement" json:"team_id"`
	Name        string         `gorm:"size:100;not null;uniqueIndex" json:"name"`
	Description string         `gorm:"size:255" json:"description,omitempty"`
	CohortID    *uint64        `json:"cohort_id,omitempty"` // link to Cohort
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	Users       []User         `gorm:"many2many:user_teams;"`
}
type UserTeam struct {
	UserRefID uint64    `gorm:"primaryKey;column:user_ref_id"`
	TeamRefID uint64    `gorm:"primaryKey;column:team_ref_id"`
	Role      string    `gorm:"type:varchar(20);default:'Member'"`
	JoinedAt  time.Time `gorm:"autoCreateTime"`

	// Struct references
	UserRef User `gorm:"foreignKey:UserRefID;references:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"user,omitempty"`
	TeamRef Team `gorm:"foreignKey:TeamRefID;references:TeamID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"team,omitempty"`
}
