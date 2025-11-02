// web/services/assets/models/user.go
package models

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

type Role struct {
	RoleID      uint         `json:"role_id" gorm:"primaryKey;autoIncrement"`
	Name        string       `json:"name" gorm:"size:50;unique;not null"`
	Description string       `json:"description" gorm:"type:text"`
	Permissions []Permission `gorm:"many2many:role_permissions" json:"permissions"`
}

type Permission struct {
	PermissionID uint64 `gorm:"primaryKey;autoIncrement" json:"permission_id"`
	Name         string `gorm:"unique;not null" json:"name"`
	Description  string `json:"description"`
	Roles        []Role `gorm:"many2many:role_permissions" json:"roles"`
}

type User struct {
	UserID        uint64   `json:"user_id" gorm:"primaryKey;autoIncrement"`
	UserUUID      string   `json:"user_uuid" gorm:"type:uuid;default:uuid_generate_v4();uniqueIndex"`
	Username      string   `json:"username" gorm:"size:50;Index;null"`
	Email         string   `json:"email" gorm:"size:150;unique;not null"`
	PasswordHash  string   `json:"-" gorm:"column:password_hash;size:255;not null"`
	RoleID        uint     `json:"role_id"`
	Cohorts       []Cohort `gorm:"many2many:cohort_users;" json:"cohorts,omitempty"`
	IsActive      bool     `json:"is_active" gorm:"default:true"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Profile       UserProfile    `gorm:"foreignKey:UserID; references:UserID"`                                              // one-to-one link
	Role          Role           `gorm:"foreignKey:RoleID;references:RoleID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL"` // links User.RoleID -> Role.RoleID
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
	EmailVerified bool           `gorm:"default:false"`

	// belongs to Role

	StudentProfile    *StudentProfile    `gorm:"foreignKey:UserID;references:UserID"`
	MentorProfile     *MentorProfile     `gorm:"foreignKey:UserID;references:UserID"`
	SupervisorProfile *SupervisorProfile `gorm:"foreignKey:UserID;references:UserID"`
	Teams             []Team             `gorm:"many2many:user_teams;"`
}

// user profile model
type UserProfile struct {
	ProfileID uint64  `json:"profile_id" gorm:"primaryKey;autoIncrement"`
	UserID    uint64  `json:"user_id" gorm:"unique;not null"`
	FirstName string  `json:"first_name" gorm:"size:100;index;not null"`
	LastName  string  `json:"last_name" gorm:"size:100;index;not null"`
	Phone     *string `json:"phone,omitempty"`
	Address   *string `json:"address,omitempty"`
	Bio       *string `json:"bio,omitempty"`
	AvatarURL *string `json:"avatar_url,omitempty"`
	Email     *string `json:"email,omitempty"`
}

// FullName returns the full name of the user
func (p *UserProfile) FullName() string {
	return fmt.Sprintf("%s %s", p.FirstName, p.LastName)
}

// Custom response type (not stored in DB)
type UserProfileResponse struct {
	UserProfile
	User struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	} `json:"user"`
}

// role specific student attributes
type StudentProfile struct {
	StudentProfileID uint64 `gorm:"primaryKey;autoIncrement"`
	UserID           uint64 `gorm:"uniqueIndex;not null"`
	School           string `json:"school"`
	Program          string `json:"program"`
	YearOfStudy      string `json:"year_of_study"`
}

type MentorProfile struct {
	MentorProfileID uint64  `gorm:"primaryKey;autoIncrement"`
	UserID          uint64  `gorm:"uniqueIndex;not null"`
	Department      *string `json:"department"`
	Organization    string  `json:"organization"`
	Expertise       string  `json:"expertise"`
	YearsExp        int     `json:"years_exp"`
}

// supervisor-specific
type SupervisorProfile struct {
	SupervisorID uint64  `gorm:"primaryKey;autoIncrement"`
	UserID       uint64  `gorm:"uniqueIndex;not null"`
	Department   *string `json:"department"`
	Expertise    string  `json:"expertise"`
	YearsExp     int     `json:"years_exp"`
	Organization string  `json:"organization"`
}

// email verification model

type EmailVerification struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement"`
	UserID    uint64    `gorm:"not null;index"`
	Token     string    `gorm:"uniqueIndex;not null"`
	ExpiresAt time.Time `gorm:"not null"`
	CreatedAt time.Time
}
