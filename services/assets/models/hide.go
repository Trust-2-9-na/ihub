package models

import "time"

// HiddenEntity tracks entities hidden from a specific user
type HiddenEntity struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement"`
	UserID     uint64    `gorm:"index;not null"`   // who hid it
	EntityID   uint64    `gorm:"index;not null"`   // ID of the hidden entity
	EntityType string    `gorm:"size:64;not null"` // e.g., "ProposalReview", "Proposal", "Comment"
	HiddenAt   time.Time `gorm:"autoCreateTime"`   // when it was hidden
}
