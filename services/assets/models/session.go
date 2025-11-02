package models

import "time"

type Session struct {
	ID uint64 `gorm:"primaryKey;autoIncrement" json:"id"`

	// Public unique session identifier (safe for cookies)
	SessionUUID string `gorm:"type:uuid;default:uuid_generate_v4();uniqueIndex" json:"session_uuid"`

	// Linked user (prefix avoids naming conflicts)
	SessionUserID   uint64 `gorm:"not null;index" json:"session_user_id"`
	SessionUserUUID string `gorm:"type:uuid;not null" json:"session_user_uuid"`

	// Device & client info (optional, but good for audit/security)
	IPAddress    string    `gorm:"type:varchar(100)" json:"ip_address,omitempty"`
	UserAgent    string    `gorm:"type:text" json:"user_agent,omitempty"`
	LastActiveAt time.Time `json:"last_active_at"`
	// Session status & expiration
	IsActive  bool      `gorm:"default:true" json:"is_active"`
	ExpiresAt time.Time `json:"expires_at"`

	// Standard audit timestamps
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}
