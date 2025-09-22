package controllers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"web/services/assets/models"
	"web/services/utils"

	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
)

// User SignUp api

type SignupInput struct {
	Username     string  `json:"username"`
	Email        string  `json:"email"`
	Password     string  `json:"password"`
	Role         string  `json:"role"`         // role name instead of role_id
	Organization *string `json:"organization"` // optional
}

// Signup handles new user registration
func (c *Construct) Signup(w http.ResponseWriter, r *http.Request) {
	var input SignupInput

	// Decode JSON body
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	// Basic validation
	if strings.TrimSpace(input.Username) == "" ||
		strings.TrimSpace(input.Email) == "" ||
		strings.TrimSpace(input.Password) == "" ||
		strings.TrimSpace(input.Role) == "" {
		c.Json(w, http.StatusBadRequest, "All required fields must be provided", nil)
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to hash password", map[string]interface{}{"error": err.Error()})
		return
	}

	// Check if username or email already exists
	var existingUser models.User
	if err := c.DB.Where("username = ? OR email = ?", input.Username, input.Email).First(&existingUser).Error; err == nil {
		c.Json(w, http.StatusBadRequest, "Username or email already exists", nil)
		return
	}

	// Find role by name
	var role models.Role
	if err := c.DB.Where("name ILIKE ?", input.Role).First(&role).Error; err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid role", map[string]interface{}{"error": "Role does not exist"})
		return
	}

	// Create User record
	user := models.User{
		Username:     input.Username,
		Email:        input.Email,
		PasswordHash: string(hashedPassword),
		RoleID:       role.RoleID,
		Organization: input.Organization,
		IsActive:     true,
	}

	if err := c.DB.Create(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create user", map[string]interface{}{"error": err.Error()})
		return
	}

	// Automatically create an empty profile for the new user
	profile := models.UserProfile{
		UserID:    user.UserID,
		FirstName: "",
		LastName:  "",
	}

	if err := c.DB.Create(&profile).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create user profile", map[string]interface{}{"error": err.Error()})
		return
	}

	// Success response
	c.Json(w, http.StatusCreated, "User registered successfully", map[string]interface{}{
		"user_id":    user.UserID,
		"username":   user.Username,
		"email":      user.Email,
		"profile_id": profile.ProfileID,
	})
}

// updating user API

type UpdateUserInput struct {
	Username     *string `json:"username,omitempty"`
	Email        *string `json:"email,omitempty"`
	Role         *string `json:"role,omitempty"`
	Organization *string `json:"organization,omitempty"`
	IsActive     *bool   `json:"is_active,omitempty"`
}

func (c *Construct) UpdateUser(w http.ResponseWriter, r *http.Request) {
	// Get user_id from query param: /updateuser?id=1
	userID := r.URL.Query().Get("id")
	if userID == "" {
		c.Json(w, http.StatusBadRequest, "Missing user_id", nil)
		return
	}

	id, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid user_id", nil)
		return
	}

	var input UpdateUserInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	var user models.User
	if err := c.DB.First(&user, id).Error; err != nil {
		c.Json(w, http.StatusNotFound, "User not found", nil)
		return
	}

	// Update User fields only
	if input.Username != nil {
		user.Username = strings.TrimSpace(*input.Username)
	}
	if input.Email != nil {
		user.Email = strings.TrimSpace(*input.Email)
	}
	if input.Organization != nil {
		user.Organization = input.Organization
	}
	if input.IsActive != nil {
		user.IsActive = *input.IsActive
	}
	if input.Role != nil {
		var role models.Role
		if err := c.DB.Where("name = ?", strings.ToLower(*input.Role)).First(&role).Error; err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid role", map[string]interface{}{"error": "Role does not exist"})
			return
		}
		user.RoleID = role.RoleID
	}

	if err := c.DB.Save(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update user", map[string]interface{}{"error": err.Error()})
		return
	}

	c.Json(w, http.StatusOK, "User updated successfully", map[string]interface{}{"user": user})
}

// User Login API
type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (c *Construct) Login(w http.ResponseWriter, r *http.Request) {
	var input LoginInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Role").Where("email = ?", input.Email).First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "Invalid credentials", nil)
		return
	}

	// Compare hashed password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)); err != nil {
		c.Json(w, http.StatusUnauthorized, "Invalid credentials", nil)
		return
	}

	// Generate JWT using utils.GenerateJWT
	tokenString, err := utils.GenerateJWT(user.UserID, user.Role.Name)
	if err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not generate token", nil)
		return
	}

	// Return token and user info
	c.Json(w, http.StatusOK, "Login successful", map[string]interface{}{
		"token": tokenString,
		"user": map[string]interface{}{
			"id":       user.UserID,
			"username": user.Username,
			"email":    user.Email,
			"role":     user.Role.Name,
			"role_id":  user.RoleID,
		},
	})
}

// Get All USers API

func (c *Construct) GetUsers(w http.ResponseWriter, r *http.Request) {
	var users []models.User

	// Fetch all active users
	if err := c.DB.Find(&users).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch users", map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	c.Json(w, http.StatusOK, "Users retrieved successfully", map[string]interface{}{
		"users": users,
	})
}

//delete a User API

func (c *Construct) DeleteUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["id"] // get id from URL
	if userID == "" {
		c.Json(w, http.StatusBadRequest, "Missing user_id", nil)
		return
	}

	// Delete the user
	if err := c.DB.Delete(&models.User{}, userID).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to delete user", map[string]interface{}{"error": err.Error()})
		return
	}

	c.Json(w, http.StatusOK, "User deleted successfully", nil)
}
