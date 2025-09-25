package models

import (
	"time"

	"gorm.io/datatypes"
)

type AuditLog struct {
	ID        uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint64         `gorm:"not null" json:"user_id"`
	Action    string         `gorm:"size:255;not null" json:"action"`
	Entity    *string        `gorm:"size:100" json:"entity,omitempty"`
	EntityID  *uint64        `json:"entity_id,omitempty"`
	IPAddress *string        `gorm:"size:50" json:"ip_address,omitempty"`
	Metadata  datatypes.JSON `json:"metadata,omitempty"` // <- change here
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
}
