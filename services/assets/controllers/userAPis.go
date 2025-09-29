package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"web/services/assets/models"
	"web/services/utils"

	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
)

// -------------------- GET USERS --------------------
type AdminUserResponse struct {
	UserID   uint64 `json:"user_id"`
	UserUUID string `json:"user_uuid"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"` // only the role name
	IsActive bool   `json:"is_active"`
	Profile  struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	} `json:"profile"`
	RoleInfo interface{} `json:"role_info,omitempty"` // Student/Mentor/Supervisor summary
}

// ================== STUDENT RESPONSE ==================
type StudentResponse struct {
	UserID         uint64                `json:"user_id"`
	UserUUID       string                `json:"user_uuid"`
	Username       string                `json:"username"`
	Email          string                `json:"email"`
	IsActive       bool                  `json:"is_active"`
	Profile        models.UserProfile    `json:"profile"`
	StudentProfile models.StudentProfile `json:"student_profile"`
}

// ================== MENTOR RESPONSE ==================
type MentorResponse struct {
	UserID        uint64               `json:"user_id"`
	UserUUID      string               `json:"user_uuid"`
	Username      string               `json:"username"`
	Email         string               `json:"email"`
	IsActive      bool                 `json:"is_active"`
	Profile       models.UserProfile   `json:"profile"`
	MentorProfile models.MentorProfile `json:"mentor_profile"`
}

// ================== SUPERVISOR RESPONSE ==================
type SupervisorResponse struct {
	UserID            uint64                   `json:"user_id"`
	UserUUID          string                   `json:"user_uuid"`
	Username          string                   `json:"username"`
	Email             string                   `json:"email"`
	IsActive          bool                     `json:"is_active"`
	Profile           models.UserProfile       `json:"profile"`
	SupervisorProfile models.SupervisorProfile `json:"supervisor_profile"`
}

func (c *Construct) GetUsers(w http.ResponseWriter, r *http.Request) {
	var users []models.User

	if err := c.DB.Preload("Profile").
		Preload("Role").
		Preload("StudentProfile").
		Preload("MentorProfile").
		Preload("SupervisorProfile").
		Find(&users).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch users", map[string]interface{}{"error": err.Error()})
		return
	}

	// Map users to simplified response
	var response []AdminUserResponse
	for _, u := range users {
		item := AdminUserResponse{
			UserID:   u.UserID,
			UserUUID: u.UserUUID,
			Username: u.Username,
			Email:    u.Email,
			Role:     u.Role.Name,
			IsActive: u.IsActive,
			Profile: struct {
				FirstName string `json:"first_name"`
				LastName  string `json:"last_name"`
			}{
				FirstName: u.Profile.FirstName,
				LastName:  u.Profile.LastName,
			},
		}

		switch u.RoleID {
		case 7:
			if u.StudentProfile != nil {
				item.RoleInfo = map[string]interface{}{
					"school":        u.StudentProfile.School,
					"program":       u.StudentProfile.Program,
					"year_of_study": u.StudentProfile.YearOfStudy,
				}
			}
		case 8:
			if u.MentorProfile != nil {
				item.RoleInfo = map[string]interface{}{
					"department":   u.MentorProfile.Department,
					"organization": u.MentorProfile.Organization,
					"expertise":    u.MentorProfile.Expertise,
					"years_exp":    u.MentorProfile.YearsExp,
				}
			}
		case 9:
			if u.SupervisorProfile != nil {
				item.RoleInfo = map[string]interface{}{
					"department":   u.SupervisorProfile.Department,
					"organization": u.SupervisorProfile.Organization,
					"expertise":    u.SupervisorProfile.Expertise,
					"years_exp":    u.SupervisorProfile.YearsExp,
				}
			}
		}

		response = append(response, item)
	}

	c.Json(w, http.StatusOK, "Users retrieved successfully", map[string]interface{}{
		"users": response,
	})
}

// ================== STUDENTS ==================
func (c *Construct) GetStudents(w http.ResponseWriter, r *http.Request) {
	var users []models.User

	if err := c.DB.Preload("Profile").
		Preload("StudentProfile").
		Joins("JOIN roles ON roles.role_id = users.role_id").
		Where("LOWER(roles.name) = ?", "student").
		Find(&users).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch students", map[string]interface{}{"error": err.Error()})
		return
	}

	var response []StudentResponse
	for _, u := range users {
		if u.StudentProfile == nil {
			continue // skip users without student profile
		}
		response = append(response, StudentResponse{
			UserID:         u.UserID,
			UserUUID:       u.UserUUID,
			Username:       u.Username,
			Email:          u.Email,
			IsActive:       u.IsActive,
			Profile:        u.Profile,
			StudentProfile: *u.StudentProfile,
		})
	}

	c.Json(w, http.StatusOK, "Students retrieved successfully", map[string]interface{}{"students": response})
}

// ================== MENTORS ==================
func (c *Construct) GetMentors(w http.ResponseWriter, r *http.Request) {
	var users []models.User

	if err := c.DB.Preload("Profile").
		Preload("MentorProfile").
		Joins("JOIN roles ON roles.role_id = users.role_id").
		Where("LOWER(roles.name) = ?", "mentor").
		Find(&users).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch mentors", map[string]interface{}{"error": err.Error()})
		return
	}

	var response []MentorResponse
	for _, u := range users {
		if u.MentorProfile == nil {
			continue // skip users without mentor profile
		}
		response = append(response, MentorResponse{
			UserID:        u.UserID,
			UserUUID:      u.UserUUID,
			Username:      u.Username,
			Email:         u.Email,
			IsActive:      u.IsActive,
			Profile:       u.Profile,
			MentorProfile: *u.MentorProfile,
		})
	}

	c.Json(w, http.StatusOK, "Mentors retrieved successfully", map[string]interface{}{"mentors": response})
}

// ================== SUPERVISORS ==================
func (c *Construct) GetSupervisors(w http.ResponseWriter, r *http.Request) {
	var users []models.User

	if err := c.DB.Preload("Profile").
		Preload("SupervisorProfile").
		Joins("JOIN roles ON roles.role_id = users.role_id").
		Where("LOWER(roles.name) = ?", "supervisor").
		Find(&users).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch supervisors", map[string]interface{}{"error": err.Error()})
		return
	}

	var response []SupervisorResponse
	for _, u := range users {
		if u.SupervisorProfile == nil {
			continue // skip users without supervisor profile
		}
		response = append(response, SupervisorResponse{
			UserID:            u.UserID,
			UserUUID:          u.UserUUID,
			Username:          u.Username,
			Email:             u.Email,
			IsActive:          u.IsActive,
			Profile:           u.Profile,
			SupervisorProfile: *u.SupervisorProfile,
		})
	}

	c.Json(w, http.StatusOK, "Supervisors retrieved successfully", map[string]interface{}{"supervisors": response})
}

// -------------------- LOGIN --------------------// -------------------- LOGIN --------------------
type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (c *Construct) Login(w http.ResponseWriter, r *http.Request) {
	var input LoginInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request", map[string]interface{}{"error": err.Error()})
		return
	}

	// Fetch user and preload role
	var user models.User
	if err := c.DB.Preload("Role").Where("email = ?", input.Email).First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "Invalid credentials", nil)
		return
	}

	// ✅ Block disabled accounts right away
	if !user.IsActive {
		c.Json(w, http.StatusForbidden, "Account disabled", nil)
		return
	}

	// Check role
	if user.Role.Name == "" {
		fmt.Println("⚠️ Role not found for user:", user.Email)
		c.Json(w, http.StatusInternalServerError, "User role missing", nil)
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)); err != nil {
		c.Json(w, http.StatusUnauthorized, "Invalid credentials", nil)
		return
	}

	// Normalize role for token
	role := strings.ToLower(user.Role.Name)

	// Generate JWT
	tokenString, err := utils.GenerateJWT(user.UserUUID, user.Role.Name)
	if err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not generate token", nil)
		return
	}

	// Track login in audit log
	ip := r.RemoteAddr
	metadata := map[string]interface{}{
		"email": user.Email,
		"role":  user.Role.Name,
	}
	if err := c.LogAudit(user.UserID, "LOGIN_SUCCESS", nil, nil, &ip, metadata); err != nil {
		fmt.Println("⚠️ Failed to log audit:", err)
	}

	// Return success response
	c.Json(w, http.StatusOK, "Login successful", map[string]interface{}{
		"token": tokenString,
		"role":  role,
	})
}

// -------------------- DELETE USER --------------------
func (c *Construct) ToggleUserStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userUUID := vars["uuid"] // get UUID from URL
	if userUUID == "" {
		c.Json(w, http.StatusBadRequest, "Missing user UUID", nil)
		return
	}

	// Find the user by UUID
	var user models.User
	if err := c.DB.Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusNotFound, "User not found", map[string]interface{}{"error": err.Error()})
		return
	}

	// Toggle IsActive
	user.IsActive = !user.IsActive
	status := "disabled"
	if user.IsActive {
		status = "enabled"
	}

	if err := c.DB.Save(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update user status", map[string]interface{}{"error": err.Error()})
		return
	}

	c.Json(w, http.StatusOK, fmt.Sprintf("User %s successfully", status), map[string]interface{}{
		"user_uuid": user.UserUUID,
		"is_active": user.IsActive,
	})
}
