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

func (c *Construct) GetUsers(w http.ResponseWriter, r *http.Request) {
	var users []models.User

	// Preload profiles and roles
	if err := c.DB.Preload("Profile").
		Preload("Role").
		Preload("StudentProfile").
		Preload("MentorProfile").
		Preload("SupervisorProfile").
		Find(&users).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch users", map[string]interface{}{"error": err.Error()})
		return
	}

	c.Json(w, http.StatusOK, "Users retrieved successfully", map[string]interface{}{
		"users": users,
	})
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
