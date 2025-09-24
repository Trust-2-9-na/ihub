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

// get students only
type StudentResponse struct {
	UserID         uint64                `json:"user_id"`
	UserUUID       string                `json:"user_uuid"`
	Username       string                `json:"username"`
	Email          string                `json:"email"`
	IsActive       bool                  `json:"is_active"`
	Profile        models.UserProfile    `json:"profile"`
	StudentProfile models.StudentProfile `json:"student_profile"`
}

func (c *Construct) GetStudents(w http.ResponseWriter, r *http.Request) {
	var users []models.User

	if err := c.DB.Preload("Profile").
		Preload("StudentProfile").
		Where("role_id = ?", 7). // 7 = Student
		Find(&users).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch students", map[string]interface{}{"error": err.Error()})
		return
	}

	// Map to response struct
	var response []StudentResponse
	for _, u := range users {
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

type MentorResponse struct {
	UserID        uint64               `json:"user_id"`
	UserUUID      string               `json:"user_uuid"`
	Username      string               `json:"username"`
	Email         string               `json:"email"`
	IsActive      bool                 `json:"is_active"`
	Profile       models.UserProfile   `json:"profile"`
	MentorProfile models.MentorProfile `json:"mentor_profile"`
}

type SupervisorResponse struct {
	UserID            uint64                   `json:"user_id"`
	UserUUID          string                   `json:"user_uuid"`
	Username          string                   `json:"username"`
	Email             string                   `json:"email"`
	IsActive          bool                     `json:"is_active"`
	Profile           models.UserProfile       `json:"profile"`
	SupervisorProfile models.SupervisorProfile `json:"supervisor_profile"`
}

// get mentors only
func (c *Construct) GetMentors(w http.ResponseWriter, r *http.Request) {
	var users []models.User

	if err := c.DB.Preload("Profile").
		Preload("MentorProfile").
		Where("role_id = ?", 8). // 8 = Mentor
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

// get supervisors only
func (c *Construct) GetSupervisors(w http.ResponseWriter, r *http.Request) {
	var users []models.User

	if err := c.DB.Preload("Profile").
		Preload("SupervisorProfile").
		Where("role_id = ?", 9). // 9 = Supervisor
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

// -------------------- LOGIN --------------------
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
	if err := c.DB.Model(&models.User{}).Preload("Role").Where("email = ?", input.Email).First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "Invalid credentials", nil)
		return
	}

	// Confirm role was preloaded
	if user.Role.Name == "" {
		fmt.Println("⚠️ Role not found for user:", user.Email)
		c.Json(w, http.StatusInternalServerError, "User role missing", nil)
		return
	}
	fmt.Printf("User: %s, RoleID: %d, RoleName: %s\n", user.Email, user.RoleID, user.Role.Name)
	// Compare hashed password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)); err != nil {
		c.Json(w, http.StatusUnauthorized, "Invalid credentials", nil)
		return
	}

	// Normalize role to lowercase for JWT
	role := strings.ToLower(user.Role.Name)
	fmt.Println("✅ Role assigned to token:", role)
	claims := &utils.Claims{
		UserUUID: user.UserUUID,
		Role:     user.Role.Name,
	}

	tokenString, err := utils.GenerateJWT(user.UserUUID, user.Role.Name)
	if err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not generate token", nil)
		return
	}

	fmt.Printf("Generated JWT for user %s: %+v\n", user.Email, claims)

	// Return token and user info
	c.Json(w, http.StatusOK, "Login successful", map[string]interface{}{
		"token": tokenString,
		"user": map[string]interface{}{
			"user_uuid": user.UserUUID,
			"username":  user.Username,
			"email":     user.Email,
			"role":      role,
			"role_id":   user.RoleID,
		},
	})
}

// -------------------- DELETE USER --------------------
func (c *Construct) DeleteUser(w http.ResponseWriter, r *http.Request) {
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

	// Delete linked UserProfile
	if err := c.DB.Where("user_id = ?", user.UserID).Delete(&models.UserProfile{}).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to delete user profile", map[string]interface{}{"error": err.Error()})
		return
	}

	// Delete linked StudentProfile
	if err := c.DB.Where("user_id = ?", user.UserID).Delete(&models.StudentProfile{}).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to delete student profile", map[string]interface{}{"error": err.Error()})
		return
	}

	// Delete linked MentorProfile
	if err := c.DB.Where("user_id = ?", user.UserID).Delete(&models.MentorProfile{}).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to delete mentor profile", map[string]interface{}{"error": err.Error()})
		return
	}

	// Delete linked SupervisorProfile
	if err := c.DB.Where("user_id = ?", user.UserID).Delete(&models.SupervisorProfile{}).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to delete supervisor profile", map[string]interface{}{"error": err.Error()})
		return
	}

	// Finally, delete the user
	if err := c.DB.Delete(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to delete user", map[string]interface{}{"error": err.Error()})
		return
	}

	c.Json(w, http.StatusOK, "User and related profiles deleted successfully", nil)
}
