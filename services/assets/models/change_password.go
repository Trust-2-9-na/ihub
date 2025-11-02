package models

import "time"

type PasswordRequest struct {
	CurrentPassword string `json:"current_password,omitempty"` // Only for change
	NewPassword     string `json:"new_password,omitempty"`     // For change & reset
	ConfirmPassword string `json:"confirm_password,omitempty"` // For change & reset
	Email           string `json:"email,omitempty"`            // For forgot password
	Token           string `json:"token,omitempty"`            // For reset password
}

type PasswordResetToken struct {
	ID     uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID uint64    `json:"user_id" gorm:"not null"`
	Token  string    `json:"token" gorm:"unique;not null"`
	Expiry time.Time `json:"expiry" gorm:"not null"`
}
