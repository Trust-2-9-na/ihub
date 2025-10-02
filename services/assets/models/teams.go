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
    CohortID    *uint64        `json:"cohort_id,omitempty"`                   // link to Cohort
    CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
    UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
    DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
    Users       []User         `gorm:"many2many:user_teams;"`
}


type UserTeam struct {
	UserID   uint64    `gorm:"primaryKey"` // part of composite PK
	TeamID   uint64    `gorm:"primaryKey"` // part of composite PK
	Role     string    `gorm:"type:varchar(20);default:'Member'"`
	JoinedAt time.Time `gorm:"autoCreateTime"`

	User User `gorm:"foreignKey:UserID;references:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	Team Team `gorm:"foreignKey:TeamID;references:TeamID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}
