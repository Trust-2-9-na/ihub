package controllers

import (
	"encoding/json"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"web/services/assets/middlewares"
	"web/services/assets/models"
)

type UpdateProfileInput struct {
	FirstName    *string `json:"first_name,omitempty"`
	LastName     *string `json:"last_name,omitempty"`
	Phone        *string `json:"phone,omitempty"`
	Address      *string `json:"address,omitempty"`
	Bio          *string `json:"bio,omitempty"`
	School       *string `json:"school,omitempty"`        // For students
	Program      *string `json:"program,omitempty"`       // For students
	YearOfStudy  *string `json:"year_of_study,omitempty"` // For students
	Department   *string `json:"department,omitempty"`    // For mentors/supervisors
	Organization *string `json:"organization,omitempty"`  // For mentors/supervisors
	Expertise    *string `json:"expertise,omitempty"`     // For mentors/supervisors
	YearsExp     *int    `json:"years_exp,omitempty"`     // For mentors/supervisors
}

//===== GetProfile handles GET /api/profile/{user_id}=====================

func (c *Construct) GetProfile(w http.ResponseWriter, r *http.Request) {
	// Get authenticated user UUID from context (set by JWT middleware)
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// Fetch user with related profiles
	var user models.User
	err := c.DB.
		Preload("Profile").
		Preload("StudentProfile").
		Preload("MentorProfile").
		Preload("SupervisorProfile").
		First(&user, "user_uuid = ?", userUUID).Error
	if err != nil {
		c.Json(w, http.StatusNotFound, "User not found", map[string]interface{}{"error": err.Error()})
		return
	}

	// Base response (common to all users)
	resp := map[string]interface{}{
		"user_id":    user.UserID,
		"email":      user.Email,
		"first_name": user.Profile.FirstName,
		"last_name":  user.Profile.LastName,
		"phone":      user.Profile.Phone,
		"address":    user.Profile.Address,
		"bio":        user.Profile.Bio,
		"avatar_url": user.Profile.AvatarURL,
	}

	// Add role-specific profile if available
	if user.StudentProfile != nil {
		resp["student"] = map[string]interface{}{
			"school":        user.StudentProfile.School,
			"program":       user.StudentProfile.Program,
			"year_of_study": user.StudentProfile.YearOfStudy,
		}
	}

	if user.MentorProfile != nil {
		resp["mentor"] = map[string]interface{}{
			"department":   user.MentorProfile.Department,
			"organization": user.MentorProfile.Organization,
			"expertise":    user.MentorProfile.Expertise,
			"years_exp":    user.MentorProfile.YearsExp,
		}
	}

	if user.SupervisorProfile != nil {
		resp["supervisor"] = map[string]interface{}{
			"department":   user.SupervisorProfile.Department,
			"organization": user.SupervisorProfile.Organization,
			"expertise":    user.SupervisorProfile.Expertise,
			"years_exp":    user.SupervisorProfile.YearsExp,
		}
	}

	// Return JSON response
	c.Json(w, http.StatusOK, "Profile fetched successfully", map[string]interface{}{"profile": resp})
}

// UpdateProfile updates the currently logged-in user's profile
func (c *Construct) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	// Extract UUID from JWT context
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// Parse request body
	var input UpdateProfileInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	// Fetch user by UUID
	var user models.User
	if err := c.DB.Preload("Profile").
		Preload("StudentProfile").
		Preload("MentorProfile").
		Preload("SupervisorProfile").
		Where("user_uuid = ?", userUUID).
		First(&user).Error; err != nil {
		c.Json(w, http.StatusNotFound, "User not found", nil)
		return
	}

	// Update main profile
	profile := &user.Profile
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

	if err := c.DB.Save(profile).Error; err != nil {
		// Notification for failure
		c.NotifyAndTrack(
			user.UserID,
			"Profile Update Failed", // Notification/Email title
			"There was an error updating your profile. Please try again.", // Message
			"Update",      // Category / Action type
			"UserProfile", // Entity type
			&user.UserID,  // Entity ID
			"Failed",      // Status
			true,          // Send email (set false if you don't want an email)
		)

		c.Json(w, http.StatusInternalServerError, "Failed to update profile", map[string]interface{}{"error": err.Error()})
		return
	}

	// Role-specific updates
	switch user.RoleID {
	case 7: // Student
		if user.StudentProfile != nil {
			if input.School != nil {
				user.StudentProfile.School = *input.School
			}
			if input.Program != nil {
				user.StudentProfile.Program = *input.Program
			}
			if input.YearOfStudy != nil {
				user.StudentProfile.YearOfStudy = *input.YearOfStudy
			}
			c.DB.Save(user.StudentProfile)
		}
	case 8: // Mentor
		if user.MentorProfile != nil {
			if input.Department != nil {
				user.MentorProfile.Department = input.Department
			}
			if input.Organization != nil {
				user.MentorProfile.Organization = *input.Organization
			}
			if input.Expertise != nil {
				user.MentorProfile.Expertise = *input.Expertise
			}
			if input.YearsExp != nil {
				user.MentorProfile.YearsExp = *input.YearsExp
			}
			c.DB.Save(user.MentorProfile)
		}
	case 9: // Supervisor
		if user.SupervisorProfile != nil {
			if input.Department != nil {
				user.SupervisorProfile.Department = input.Department
			}
			if input.Organization != nil {
				user.SupervisorProfile.Organization = *input.Organization
			}
			if input.Expertise != nil {
				user.SupervisorProfile.Expertise = *input.Expertise
			}
			if input.YearsExp != nil {
				user.SupervisorProfile.YearsExp = *input.YearsExp
			}
			c.DB.Save(user.SupervisorProfile)
		}
	}

	// ✅ Notify user
	c.NotifyAndTrack(
		user.UserID,
		"Profile Updated Successfully",                // Notification/Email title
		"Your profile has been updated successfully.", // Message
		"Update",      // Category / Action type
		"UserProfile", // Entity type
		&user.UserID,  // Entity ID
		"Updated",     // Status
		true,          // Send email (set false if you don't want an email)
	)

	// ✅ Audit log
	ip := r.RemoteAddr
	action := "update_profile"
	entity := "user_profile"
	metadata := map[string]interface{}{
		"user_id":   user.UserID,
		"user_uuid": user.UserUUID,
		"username":  user.Username,
	}
	_ = c.LogAudit(user.UserID, action, &entity, &user.UserID, &ip, metadata)

	c.Json(w, http.StatusOK, "Profile updated successfully", map[string]interface{}{
		"profile": profile,
	})
}

// UpdateAvatar updates a user's avatar with file validation
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

	// --- Validate file extension ---
	allowedExts := map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".webp": true,
	}

	ext := strings.ToLower(filepath.Ext(payload.AvatarURL))
	if !allowedExts[ext] {
		c.Json(w, http.StatusBadRequest, "Invalid file type. Allowed: .jpg, .jpeg, .png, .gif, .webp", nil)
		return
	}

	// --- Validate MIME type ---
	mimeType := mime.TypeByExtension(ext)
	allowedMIMEs := map[string]bool{
		"image/jpeg": true,
		"image/png":  true,
		"image/gif":  true,
		"image/webp": true,
	}

	if !allowedMIMEs[mimeType] {
		c.Json(w, http.StatusBadRequest, "Invalid MIME type for image", nil)
		return
	}

	// --- Find and update profile ---
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
