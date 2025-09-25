package controllers

import (
	"encoding/json"
	"net/http"
	"strconv"
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

// GetProfile handles GET /api/profile/{user_id}
func (c *Construct) GetProfile(w http.ResponseWriter, r *http.Request) {
	// Get authenticated user UUID from context (set by JWT middleware)
	userUUIDCtx := r.Context().Value("user_uuid")
	if userUUIDCtx == nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	userUUID := userUUIDCtx.(string)

	// Fetch user with related profiles
	var user models.User
	if err := c.DB.Preload("Profile").
		Preload("StudentProfile").
		Preload("MentorProfile").
		Preload("SupervisorProfile").
		Where("user_uuid = ?", userUUID).
		First(&user).Error; err != nil {
		c.Json(w, http.StatusNotFound, "User not found", map[string]interface{}{"error": err.Error()})
		return
	}

	// Build response depending on role
	var resp map[string]interface{}
	switch user.RoleID {
	case 7: // Student
		resp = map[string]interface{}{
			"user_id":    user.UserID,
			"username":   user.Username,
			"email":      user.Email,
			"first_name": user.Profile.FirstName,
			"last_name":  user.Profile.LastName,
			"phone":      user.Profile.Phone,
			"address":    user.Profile.Address,
			"bio":        user.Profile.Bio,
			"student": map[string]interface{}{
				"school":        user.StudentProfile.School,
				"program":       user.StudentProfile.Program,
				"year_of_study": user.StudentProfile.YearOfStudy,
			},
		}
	case 8: // Mentor
		resp = map[string]interface{}{
			"user_id":    user.UserID,
			"username":   user.Username,
			"email":      user.Email,
			"first_name": user.Profile.FirstName,
			"last_name":  user.Profile.LastName,
			"phone":      user.Profile.Phone,
			"address":    user.Profile.Address,
			"bio":        user.Profile.Bio,
			"mentor": map[string]interface{}{
				"department":   user.MentorProfile.Department,
				"organization": user.MentorProfile.Organization,
				"expertise":    user.MentorProfile.Expertise,
				"years_exp":    user.MentorProfile.YearsExp,
			},
		}
	case 9: // Supervisor
		resp = map[string]interface{}{
			"user_id":    user.UserID,
			"username":   user.Username,
			"email":      user.Email,
			"first_name": user.Profile.FirstName,
			"last_name":  user.Profile.LastName,
			"phone":      user.Profile.Phone,
			"address":    user.Profile.Address,
			"bio":        user.Profile.Bio,
			"supervisor": map[string]interface{}{
				"department":   user.SupervisorProfile.Department,
				"organization": user.SupervisorProfile.Organization,
				"expertise":    user.SupervisorProfile.Expertise,
				"years_exp":    user.SupervisorProfile.YearsExp,
			},
		}
	default: // Admin or other roles
		resp = map[string]interface{}{
			"user_id":    user.UserID,
			"username":   user.Username,
			"email":      user.Email,
			"first_name": user.Profile.FirstName,
			"last_name":  user.Profile.LastName,
			"phone":      user.Profile.Phone,
			"address":    user.Profile.Address,
			"bio":        user.Profile.Bio,
		}
	}

	c.Json(w, http.StatusOK, "Profile fetched successfully", map[string]interface{}{"profile": resp})
}

// UpdateProfile updates the currently logged-in user's profile
func (c *Construct) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	// Extract UUID from JWT context
	userUUID, ok := r.Context().Value("user_uuid").(string)
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
		c.CreateNotification(user.UserID, "Profile Update Failed", "There was an error updating your profile. Please try again.")
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
	c.CreateNotification(user.UserID, "Profile Updated Successfully", "Your profile has been updated successfully.")

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
