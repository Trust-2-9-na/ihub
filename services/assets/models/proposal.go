package models

import (
	"time"

	"gorm.io/gorm"
)

//
// ─── PROPOSAL STATUS CONSTANTS ────────────────────────────────────────────────
//

const (
	ProposalStatusSubmitted   = "Submitted"
	ProposalStatusUnderReview = "Under Review"
	ProposalStatusApproved    = "Approved"
	ProposalStatusRejected    = "Rejected"
)

//
// ─── REVIEW DECISION CONSTANTS ────────────────────────────────────────────────
//

const (
	DecisionPending       = "Pending"
	DecisionApproved      = "Approved"
	DecisionRejected      = "Rejected"
	DecisionNeedsRevision = "Needs Revision"
)

//
// ─── PROPOSAL MODEL ───────────────────────────────────────────────────────────
//

type Proposal struct {
	ProposalID     uint64           `gorm:"primaryKey;autoIncrement" json:"proposal_id"`
	Title          string           `gorm:"size:200;not null;index" json:"title"`
	Abstract       string           `gorm:"type:text;not null" json:"abstract"`
	DocumentURL    *string          `gorm:"size:255" json:"document_url,omitempty"`
	SubmittedByID  uint64           `gorm:"not null" json:"submitted_by"`
	SubmittedBy    User             `gorm:"foreignKey:SubmittedByID;references:UserID" json:"submitted_by_user"`
	TeamID         *uint64          `json:"team_id,omitempty"`
	Team           *Team            `gorm:"foreignKey:TeamID;references:TeamID" json:"team,omitempty"`
	Status         string           `gorm:"type:varchar(20);default:'Submitted'" json:"status"`
	SubmissionDate time.Time        `gorm:"autoCreateTime" json:"submission_date"`
	Deadline       *time.Time       `json:"deadline,omitempty"`
	Archived       bool             `gorm:"default:false" json:"archived"`
	Reviews        []ProposalReview `gorm:"foreignKey:ProposalID" json:"reviews"`
	CreatedAt      time.Time        `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time        `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt      gorm.DeletedAt   `gorm:"index" json:"-"`
}

//
// ─── PROPOSAL REVIEW MODEL ────────────────────────────────────────────────────
//

type ProposalReview struct {
	ReviewID     uint64         `gorm:"primaryKey;autoIncrement" json:"review_id"`
	ProposalID   uint64         `gorm:"not null;index" json:"proposal_id"`
	Proposal     Proposal       `gorm:"foreignKey:ProposalID;references:ProposalID" json:"proposal,omitempty"`
	ReviewedByID uint64         `gorm:"not null;index" json:"reviewed_by"`
	ReviewedBy   User           `gorm:"foreignKey:ReviewedByID;references:UserID" json:"reviewed_by_user"`
	Comments     *string        `gorm:"type:text" json:"comments,omitempty"`
	Decision     string         `gorm:"type:varchar(20);default:'Pending';index" json:"decision"`
	ReviewDate   time.Time      `gorm:"autoCreateTime" json:"review_date"`
	CreatedAt    time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

//
// ─── TEAM MODEL ───────────────────────────────────────────────────────────────
//

type Team struct {
	TeamID      uint64         `gorm:"primaryKey;autoIncrement" json:"team_id"`
	Name        string         `gorm:"size:100;not null;uniqueIndex" json:"name"`
	Description string         `gorm:"size:255" json:"description,omitempty"`
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

//
// ─── USER TEAM (JUNCTION TABLE) ───────────────────────────────────────────────
//

type UserTeam struct {
	ID       uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	TeamID   uint64    `gorm:"not null;index;uniqueIndex:idx_user_team" json:"team_id"`
	UserID   uint64    `gorm:"not null;index;uniqueIndex:idx_user_team" json:"user_id"`
	Role     string    `gorm:"type:varchar(20);default:'Member'" json:"role_in_team"`
	JoinedAt time.Time `gorm:"autoCreateTime" json:"joined_at"`
	User     User      `gorm:"foreignKey:UserID;references:UserID" json:"user,omitempty"`
	Team     Team      `gorm:"foreignKey:TeamID;references:TeamID" json:"team,omitempty"`
}
