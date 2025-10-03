package models

import (
	"time"

	"gorm.io/gorm"
)

//
// ─── PROPOSAL STATUS CONSTANTS ────────────────────────────────────────────────
//

const (
	ProposalStatusDraft         = "Draft"
	ProposalStatusSubmitted     = "Submitted"
	ProposalStatusUnderReview   = "Under Review"
	ProposalStatusApproved      = "Approved"
	ProposalStatusRejected      = "Rejected"
	ProposalStatusNeedsRevision = "Needs Revision"
	ProposalStatusOngoing       = "Ongoing"
	ProposalStatusExpired       = "Expired"
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
	ProposalID     uint64     `gorm:"primaryKey;autoIncrement" json:"proposal_id"`
	Title          string     `gorm:"size:200;not null;index" json:"title"`
	Abstract       string     `gorm:"type:text;not null" json:"abstract"`
	DocumentURL    *string    `gorm:"size:255" json:"document_url,omitempty"`
	SubmittedByID  uint64     `gorm:"not null" json:"submitted_by"`
	SubmittedBy    *User      `gorm:"foreignKey:SubmittedByID;references:UserID" json:"submitted_by_user"`
	TeamID         *uint64    `json:"team_id,omitempty"`
	Team           *Team      `gorm:"foreignKey:TeamID;references:TeamID" json:"team,omitempty"`
	Status         string     `gorm:"type:varchar(20);default:'Draft';index" json:"status"`
	SubmissionDate *time.Time `json:"submission_date,omitempty"` // set when submitted
	Archived       bool       `gorm:"default:false" json:"archived"`
	ArchivedAt     *time.Time `json:"archived_at,omitempty"`
	ArchivedBy     *uint64    `json:"archived_by,omitempty"` // FK -> users
	ArchivedByUser *User      `gorm:"foreignKey:ArchivedBy;references:UserID" json:"archived_by_user"`

	CreatedAt time.Time                 `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time                 `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt            `gorm:"index" json:"-"`
	WindowID  uint64                    `json:"window_id"` // Link to ProposalSubmissionWindow
	Window    *ProposalSubmissionWindow `gorm:"foreignKey:WindowID;references:WindowID" json:"window,omitempty"`
	CohortID  *uint64                   `json:"cohort_id,omitempty"` // Nullable at submission
	Cohort    *Cohort                   `gorm:"foreignKey:CohortID;references:CohortID" json:"cohort,omitempty"`
	Category  string                    `gorm:"size:100;index" json:"category"`
	Subfield  *string                   `gorm:"size:100" json:"subfield,omitempty"`
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
type SystemHistory struct {
	HistoryID   uint64         `gorm:"primaryKey;autoIncrement" json:"history_id"`
	EntityType  string         `gorm:"size:100;not null;index" json:"entity_type"` // e.g., "Proposal", "Team", "Window"
	EntityID    *uint64        `gorm:"not null;index" json:"entity_id"`            // ID of the entity
	Action      string         `gorm:"size:100;not null" json:"action"`            // e.g., "Created", "Updated", "Submitted", "Reviewed"
	Status      *string        `gorm:"size:50" json:"status,omitempty"`            // Optional, e.g., "Pending", "Approved"
	Comment     *string        `gorm:"type:text" json:"comment,omitempty"`         // Optional extra info
	ChangedByID uint64         `gorm:"not null;index" json:"changed_by"`           // User who performed action
	ChangedBy   *User          `gorm:"foreignKey:ChangedByID;references:UserID" json:"changed_by_user,omitempty"`
	Metadata    *string        `gorm:"type:jsonb" json:"metadata,omitempty"` // Optional JSON for custom info
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}
