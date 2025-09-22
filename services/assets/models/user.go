// web/services/assets/models/user.go
package models

import "time"

type Role struct {
	RoleID      uint   `json:"roleid" gorm:"primaryKey;autoIncrement"`
	Name        string `json:"name" gorm:"size:50;unique;not null"`
	Description string `json:"description" gorm:"type:text"`
	// do NOT define Users slice here for migration
}

type User struct {
	UserID       uint64  `json:"user_id" gorm:"primaryKey;autoIncrement"`
	Username     string  `json:"username" gorm:"size:50;uniqueIndex;not null"` // new field
	Email        string  `json:"email" gorm:"size:150;unique;not null"`
	PasswordHash string  `json:"-" gorm:"column:password_hash;size:255;not null"`
	RoleID       uint    `json:"role_id"`
	CohortID     *uint64 `json:"cohort_id"`
	Organization *string `json:"organization"`
	IsActive     bool    `json:"is_active" gorm:"default:true"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Profile      UserProfile `gorm:"foreignKey:UserID"` // one-to-one link
	Role         Role        `gorm:"foreignKey:RoleID"` // belongs to Role
	Proposals    []Proposal  `gorm:"foreignKey:SubmittedBy"`
}

// user profile model
type UserProfile struct {
	ProfileID uint64  `json:"profile_id" gorm:"primaryKey;autoIncrement"`
	UserID    uint64  `json:"user_id" gorm:"unique;not null"`
	FirstName string  `json:"first_name" gorm:"size:50;index;not null"`
	LastName  string  `json:"last_name" gorm:"size:50;index;not null"`
	Phone     *string `json:"phone,omitempty"`
	Address   *string `json:"address,omitempty"`
	Bio       *string `json:"bio,omitempty"`
	AvatarURL *string `json:"avatar_url,omitempty"`
}

// Custom response type (not stored in DB)
type UserProfileResponse struct {
	UserProfile
	User struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	} `json:"user"`
}
