// signup apis
package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"web/services/assets/models"

	"golang.org/x/crypto/bcrypt"
)

type StudentSignupInput struct {
	Username    string  `json:"username"`
	Email       string  `json:"email"`
	Password    string  `json:"password"`
	School      string  `json:"school"`
	Program     string  `json:"program"`
	YearOfStudy string  `json:"year_of_study"`
	Department  *string `json:"department,omitempty"`
}

type MentorSignupInput struct {
	Username     string  `json:"username"`
	Email        string  `json:"email"`
	Password     string  `json:"password"`
	Department   *string `json:"department,omitempty"`
	Organization string  `json:"organization"`
	Expertise    string  `json:"expertise"`
	YearsExp     int     `json:"years_exp"`
}

type SupervisorSignupInput struct {
	Username     string  `json:"username"`
	Email        string  `json:"email"`
	Password     string  `json:"password"`
	Department   *string `json:"department,omitempty"`
	Organization string  `json:"organization"`
	Expertise    string  `json:"expertise"`
	YearsExp     int     `json:"years_exp"`
}

// SignupStudent handles student registration
func (c *Construct) SignupStudent(w http.ResponseWriter, r *http.Request) {
	var input StudentSignupInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request", map[string]interface{}{"error": err.Error()})
		return
	}

	c.signupUserWithRole(w, "Student", input.Username, input.Email, input.Password, func(userID uint64) error {
		student := models.StudentProfile{
			UserID:      userID,
			School:      input.School,
			Program:     input.Program,
			YearOfStudy: input.YearOfStudy,
		}
		return c.DB.Create(&student).Error
	})
}

// SignupMentor handles mentor registration
func (c *Construct) SignupMentor(w http.ResponseWriter, r *http.Request) {
	var input MentorSignupInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request", map[string]interface{}{"error": err.Error()})
		return
	}

	c.signupUserWithRole(w, "Mentor", input.Username, input.Email, input.Password, func(userID uint64) error {
		mentor := models.MentorProfile{
			UserID:       userID,
			Department:   input.Department,
			Organization: input.Organization,
			Expertise:    input.Expertise,
			YearsExp:     input.YearsExp,
		}
		return c.DB.Create(&mentor).Error
	})
}

// SignupSupervisor handles supervisor registration
func (c *Construct) SignupSupervisor(w http.ResponseWriter, r *http.Request) {
	var input SupervisorSignupInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request", map[string]interface{}{"error": err.Error()})
		return
	}

	c.signupUserWithRole(w, "Supervisor", input.Username, input.Email, input.Password, func(userID uint64) error {
		supervisor := models.SupervisorProfile{
			UserID:       userID,
			Department:   input.Department,
			Organization: input.Organization,
			Expertise:    input.Expertise,
			YearsExp:     input.YearsExp,
		}
		return c.DB.Create(&supervisor).Error
	})
}

// signupUserWithRole is a helper that creates the User, UserProfile, and invokes role-specific creation
func (c *Construct) signupUserWithRole(
	w http.ResponseWriter,
	roleName, username, email, password string,
	createRoleProfile func(userID uint64) error,
) {
	// Hash password
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to hash password", map[string]interface{}{"error": err.Error()})
		return
	}

	// Check if user exists
	var existing models.User
	if err := c.DB.Where("username = ? OR email = ?", username, email).First(&existing).Error; err == nil {
		c.Json(w, http.StatusBadRequest, "Username or email already exists", nil)
		return
	}

	// Find role

	var role models.Role
	if err := c.DB.Where("LOWER(name) = ?", strings.ToLower(roleName)).First(&role).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Role lookup failed", map[string]interface{}{"error": err.Error()})
		return
	}
	fmt.Println("Resolved role:", role.Name, "ID:", role.RoleID)

	// Create user
	user := models.User{
		Username:     username,
		Email:        email,
		PasswordHash: string(hashed),
		RoleID:       role.RoleID,
		IsActive:     true,
	}

	if err := c.DB.Create(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create user", map[string]interface{}{"error": err.Error()})
		return
	}

	// Create default user profile
	profile := models.UserProfile{
		UserID:    user.UserID,
		FirstName: "",
		LastName:  "",
	}
	if err := c.DB.Create(&profile).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create user profile", map[string]interface{}{"error": err.Error()})
		return
	}

	// Create role-specific profile
	if err := createRoleProfile(user.UserID); err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create role-specific profile", map[string]interface{}{"error": err.Error()})
		return
	}

	c.Json(w, http.StatusCreated, fmt.Sprintf("%s registered successfully", roleName), map[string]interface{}{
		"user_id":    user.UserID,
		"username":   user.Username,
		"email":      user.Email,
		"profile_id": profile.ProfileID,
	})
}
