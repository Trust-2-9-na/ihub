package controllers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"web/services/assets/models"

	"github.com/gorilla/mux"
)

// UpdateProfileInput for updating profile
type UpdateProfileInput struct {
	FirstName *string `json:"first_name,omitempty"`
	LastName  *string `json:"last_name,omitempty"`
	Phone     *string `json:"phone,omitempty"`
	Address   *string `json:"address,omitempty"`
	Bio       *string `json:"bio,omitempty"`
}

// User Profile Get API
func (c *Construct) GetProfile(w http.ResponseWriter, r *http.Request) {
	// Extract user_id from URL
	vars := mux.Vars(r)
	userID := vars["id"]

	if userID == "" {
		c.Json(w, http.StatusBadRequest, "Missing user_id", nil)
		return
	}

	// Fetch profile
	var profile models.UserProfile
	if err := c.DB.Where("user_id = ?", userID).First(&profile).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Profile not found", map[string]interface{}{"error": err.Error()})
		return
	}

	// Fetch user
	var user models.User
	if err := c.DB.First(&user, userID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "User not found", map[string]interface{}{"error": err.Error()})
		return
	}

	// Build response
	response := models.UserProfileResponse{
		UserProfile: profile,
	}
	response.User.Username = user.Username
	response.User.Email = user.Email

	c.Json(w, http.StatusOK, "Profile fetched successfully", map[string]interface{}{
		"profile": response,
	})
}

// UpdateProfile handles PUT /api/profile/{user_id}
func (c *Construct) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.URL.Query().Get("user_id")
	if userIDStr == "" {
		c.Json(w, http.StatusBadRequest, "Missing user_id", nil)
		return
	}
	userID, _ := strconv.ParseUint(userIDStr, 10, 64)

	var input UpdateProfileInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	var profile models.UserProfile
	if err := c.DB.Where("user_id = ?", userID).First(&profile).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Profile not found", nil)
		return
	}

	if input.FirstName != nil {
		profile.FirstName = *input.FirstName
	}
	if input.LastName != nil {
		profile.LastName = *input.LastName
	}
	if input.Phone != nil {
		profile.Phone = input.Phone
	}
	if input.Address != nil {
		profile.Address = input.Address
	}
	if input.Bio != nil {
		profile.Bio = input.Bio
	}

	if err := c.DB.Save(&profile).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update profile", map[string]interface{}{"error": err.Error()})
		return
	}

	c.Json(w, http.StatusOK, "Profile updated successfully", map[string]interface{}{"profile": profile})
}

// DeleteProfile handles DELETE /api/profile/{user_id}
func (c *Construct) DeleteProfile(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.URL.Query().Get("user_id")
	if userIDStr == "" {
		c.Json(w, http.StatusBadRequest, "Missing user_id", nil)
		return
	}
	userID, _ := strconv.ParseUint(userIDStr, 10, 64)

	if err := c.DB.Where("user_id = ?", userID).Delete(&models.UserProfile{}).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to delete profile", map[string]interface{}{"error": err.Error()})
		return
	}

	c.Json(w, http.StatusOK, "Profile deleted successfully", nil)
}

// UpdateAvatar handles PATCH /api/profile/{user_id}/avatar
func (c *Construct) UpdateAvatar(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.URL.Query().Get("user_id")
	if userIDStr == "" {
		c.Json(w, http.StatusBadRequest, "Missing user_id", nil)
		return
	}
	userID, _ := strconv.ParseUint(userIDStr, 10, 64)

	var payload struct {
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	var profile models.UserProfile
	if err := c.DB.Where("user_id = ?", userID).First(&profile).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Profile not found", nil)
		return
	}

	profile.AvatarURL = &payload.AvatarURL
	if err := c.DB.Save(&profile).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update avatar", map[string]interface{}{"error": err.Error()})
		return
	}

	c.Json(w, http.StatusOK, "Avatar updated successfully", map[string]interface{}{"profile": profile})
}
