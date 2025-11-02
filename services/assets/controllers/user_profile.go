package controllers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"web/services/assets/middlewares"
	"web/services/assets/models"
	"web/services/utils"
)

type UpdateProfileInput struct {
	FirstName    *string `json:"first_name,omitempty"`
	LastName     *string `json:"last_name,omitempty"`
	Phone        *string `json:"phone,omitempty"`
	Address      *string `json:"address,omitempty"`
	Bio          *string `json:"bio,omitempty"`
	AvatarURL    *string `json:"avatar_url,omitempty"` // <-- new
	School       *string `json:"school,omitempty"`
	Program      *string `json:"program,omitempty"`
	YearOfStudy  *string `json:"year_of_study,omitempty"`
	Department   *string `json:"department,omitempty"`
	Organization *string `json:"organization,omitempty"`
	Expertise    *string `json:"expertise,omitempty"`
	YearsExp     *int    `json:"years_exp,omitempty"`
	Email        *string `json:"email,omitempty"`
}

//===== GetProfile handles GET /api/profile/{user_id}=====================

func (c *Construct) GetProfile(w http.ResponseWriter, r *http.Request) {
	// --- Get authenticated user UUID from context (JWT middleware) ---
	viewerUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || viewerUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// --- Fetch the viewer (current user) ---
	var viewer models.User
	if err := c.DB.Preload("Role").First(&viewer, "user_uuid = ?", viewerUUID).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "Viewer not found", nil)
		return
	}
	roleName := strings.ToLower(viewer.Role.Name)

	// --- Get query parameters ---
	userIDsStr := r.URL.Query().Get("user_id")
	roleFilter := r.URL.Query().Get("role")          // e.g., "student"
	fullNameFilter := r.URL.Query().Get("full_name") // e.g., "John Doe"

	// --- Parse user_ids (comma-separated) ---
	userIDs := []uint64{}
	if userIDsStr != "" {
		for _, idStr := range strings.Split(userIDsStr, ",") {
			idStr = strings.TrimSpace(idStr)
			if idStr == "" {
				continue
			}
			id, err := strconv.ParseUint(idStr, 10, 64)
			if err != nil {
				c.Json(w, http.StatusBadRequest, "Invalid user_id: "+idStr, nil)
				return
			}
			userIDs = append(userIDs, id)
		}
	}

	// --- Build base DB query ---
	dbQuery := c.DB.Model(&models.User{}).
		Preload("Profile").
		Preload("StudentProfile").
		Preload("MentorProfile").
		Preload("SupervisorProfile")

	// --- Apply filters ---
	if len(userIDs) > 0 {
		dbQuery = dbQuery.Where("user_id IN ?", userIDs)
	} else if roleName != "systemadmin" && roleName != "opsadmin" {
		// Non-admins can only fetch themselves
		dbQuery = dbQuery.Where("user_id = ?", viewer.UserID)
	}

	if roleFilter != "" {
		roleFilter = strings.ToLower(roleFilter)
		dbQuery = dbQuery.Joins("JOIN roles ON roles.role_id = users.role_id").
			Where("LOWER(roles.name) = ?", roleFilter)
	}

	if fullNameFilter != "" {
		fullNameFilter = strings.ToLower(fullNameFilter)
		dbQuery = dbQuery.Joins("JOIN user_profiles ON user_profiles.user_id = users.user_id").
			Where("LOWER(CONCAT(user_profiles.first_name, ' ', user_profiles.last_name)) LIKE ?", "%"+fullNameFilter+"%")
	}

	// --- Execute query ---
	var users []models.User
	if err := dbQuery.Find(&users).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Users not found", map[string]interface{}{"error": err.Error()})
		return
	}

	if len(users) == 0 {
		c.Json(w, http.StatusNotFound, "No matching users found", nil)
		return
	}

	// --- Build response ---
	profiles := []map[string]interface{}{}
	for _, user := range users {
		// Normalize avatar URL: convert backslashes to forward slashes for web URLs
		var avatarURL interface{} = user.Profile.AvatarURL
		if user.Profile.AvatarURL != nil && *user.Profile.AvatarURL != "" {
			normalizedURL := filepath.ToSlash(*user.Profile.AvatarURL)
			// Construct full URL using the request's scheme and host
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			host := r.Host
			// Ensure the URL starts with a forward slash
			if !strings.HasPrefix(normalizedURL, "/") {
				normalizedURL = "/" + normalizedURL
			}
			fullURL := fmt.Sprintf("%s://%s%s", scheme, host, normalizedURL)
			avatarURL = fullURL
		}
		
		resp := map[string]interface{}{
			"user_id":    user.UserID,
			"email":      user.Email,
			"first_name": user.Profile.FirstName,
			"last_name":  user.Profile.LastName,
			"phone":      user.Profile.Phone,
			"address":    user.Profile.Address,
			"bio":        user.Profile.Bio,
			"avatar_url": avatarURL,
		}

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

		profiles = append(profiles, resp)

		// --- Track view if viewer is not the same as the profile owner ---
		if viewer.UserID != user.UserID {
			c.NotifyAndTrack(
				viewer.UserID,
				"Profile Viewed",
				fmt.Sprintf("You viewed %s's profile.", user.Profile.FirstName),
				"View",
				"UserProfile",
				&user.UserID,
				"Viewed",
				false,
			)
		}
	}

	c.Json(w, http.StatusOK, "Profile(s) fetched successfully", map[string]interface{}{"profiles": profiles})
}

// UpdateProfile updates the currently logged-in user's profile
func (c *Construct) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	viewerUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || viewerUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var input UpdateProfileInput
	contentType := r.Header.Get("Content-Type")

	// --- Parse input ---
	if strings.HasPrefix(contentType, "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid JSON payload", map[string]interface{}{"error": err.Error()})
			return
		}
	} else if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			c.Json(w, http.StatusBadRequest, "Failed to parse form data", map[string]interface{}{"error": err.Error()})
			return
		}

		input.FirstName = ptr(r.FormValue("first_name"))
		input.LastName = ptr(r.FormValue("last_name"))
		input.Phone = ptr(r.FormValue("phone"))
		input.Address = ptr(r.FormValue("address"))
		input.Bio = ptr(r.FormValue("bio"))
		input.AvatarURL = ptr(r.FormValue("avatar_url"))
		input.School = ptr(r.FormValue("school"))
		input.Program = ptr(r.FormValue("program"))
		input.YearOfStudy = ptr(r.FormValue("year_of_study"))
		input.Department = ptr(r.FormValue("department"))
		input.Organization = ptr(r.FormValue("organization"))
		input.Expertise = ptr(r.FormValue("expertise"))
		input.Email = ptr(r.FormValue("email"))
	}

	// --- Load viewer ---
	var viewer models.User
	if err := c.DB.Preload("Profile").Where("user_uuid = ?", viewerUUID).First(&viewer).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Viewer not found", nil)
		return
	}

	// --- Load target user profile ---
	var user models.User
	if err := c.DB.
		Preload("Profile").
		Preload("Role").
		Preload("StudentProfile").
		Preload("MentorProfile").
		Preload("SupervisorProfile").
		Where("user_uuid = ?", viewerUUID). // user is updating their own profile
		First(&user).Error; err != nil {
		c.Json(w, http.StatusNotFound, "User not found", nil)
		return
	}

	profile := &user.Profile

	// --- Update shared profile fields ---
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

	// --- Update avatar if provided ---
	if file, header, err := r.FormFile("avatar"); err == nil {
		defer file.Close()

		ext := strings.ToLower(filepath.Ext(header.Filename))
		allowedExts := map[string]bool{
			".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
		}
		if !allowedExts[ext] {
			c.Json(w, http.StatusBadRequest, "Invalid file type for avatar", nil)
			return
		}

		// Save to uploads/avatars/
		uploadDir := "uploads/avatars"
		os.MkdirAll(uploadDir, 0755)

		newFileName := fmt.Sprintf("%d_%s", time.Now().UnixNano(), header.Filename)
		savePath := filepath.Join(uploadDir, newFileName)

		dst, err := os.Create(savePath)
		if err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to save avatar", nil)
			return
		}
		defer dst.Close()

		if _, err := io.Copy(dst, file); err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to write avatar to disk", nil)
			return
		}

		// Normalize path to use forward slashes for web URLs
		normalizedPath := filepath.ToSlash(savePath)
		profile.AvatarURL = &normalizedPath
	} else if input.AvatarURL != nil && *input.AvatarURL != "" {
		// fallback to existing string URL if client passes it
		profile.AvatarURL = input.AvatarURL
	}

	// --- Update email with verification ---
	var verifyURL string
	if input.Email != nil && *input.Email != user.Email {
		// Check for duplicate
		var existing models.User
		if err := c.DB.Where("email = ?", *input.Email).First(&existing).Error; err == nil {
			c.Json(w, http.StatusConflict, "Email already in use", nil)
			return
		}

		token, _ := utils.GenerateRandomString(32)
		verifyURL = fmt.Sprintf("%s/verify-email?token=%s", os.Getenv("FRONTEND_URL"), token)

		go func() {
			body := fmt.Sprintf(`
		<p>Hello %s,</p>
		<p>You have requested to update your email address to <strong>%s</strong>.</p>
		<p>Please verify this new email address by clicking the link below:</p>
		<p><a href="%s">Verify New Email Address</a></p>
		<p>If you did not request this change, please ignore this email and your current email address will remain unchanged.</p>
	`, profile.FirstName, *input.Email, verifyURL)
			_ = c.SendEmailNotification(*input.Email, "Verify Your New Email Address", body)
		}()

	}

	// --- Save base profile ---
	if err := c.DB.Save(profile).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update profile", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Role-specific updates ---
	role := strings.ToLower(user.Role.Name)
	switch role {
	case "student":
		if user.StudentProfile == nil {
			user.StudentProfile = &models.StudentProfile{UserID: user.UserID}
			c.DB.Create(user.StudentProfile)
		}
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

	case "mentor":
		if user.MentorProfile == nil {
			user.MentorProfile = &models.MentorProfile{UserID: user.UserID}
			c.DB.Create(user.MentorProfile)
		}
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

	case "supervisor":
		if user.SupervisorProfile == nil {
			user.SupervisorProfile = &models.SupervisorProfile{UserID: user.UserID}
			c.DB.Create(user.SupervisorProfile)
		}
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

	// --- Track profile view ---
	if viewer.UserID != user.UserID {
		c.NotifyAndTrack(
			viewer.UserID,
			"Profile Viewed",
			fmt.Sprintf("You viewed %s's profile.", user.Profile.FirstName),
			"View",
			"UserProfile",
			&user.UserID,
			"Viewed",
			false,
		)
	}

	// --- Response ---
	// --- Response ---
	resp := map[string]interface{}{
		"profile": profile,
	}

	if verifyURL != "" {
		resp["verify_url"] = verifyURL
	}

	c.Json(w, http.StatusOK, "Profile updated successfully", resp)

}

func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// UpdateAvatar handles uploading/updating a user's avatar with tracking and notifications

func (c *Construct) UpdateAvatar(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.URL.Query().Get("user_id")
	if userIDStr == "" {
		c.Json(w, http.StatusBadRequest, "Missing user_id", nil)
		return
	}
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid user_id", nil)
		return
	}

	// Parse multipart form (max 5MB)
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		c.Json(w, http.StatusBadRequest, "Failed to parse form data", map[string]interface{}{"error": err.Error()})
		return
	}

	file, handler, err := r.FormFile("avatar")
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Failed to read file", map[string]interface{}{"error": err.Error()})
		return
	}
	defer file.Close()

	// Validate extension
	allowedExts := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true}
	ext := strings.ToLower(filepath.Ext(handler.Filename))
	if !allowedExts[ext] {
		c.Json(w, http.StatusBadRequest, "Invalid file type. Allowed: .jpg, .jpeg, .png, .gif, .webp", nil)
		return
	}

	// Validate MIME type
	buf := make([]byte, 512)
	if _, err := file.Read(buf); err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to read file for validation", nil)
		return
	}
	mimeType := http.DetectContentType(buf)
	file.Seek(0, 0) // reset read pointer
	allowedMIMEs := map[string]bool{"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true}
	if !allowedMIMEs[mimeType] {
		c.Json(w, http.StatusBadRequest, "Invalid MIME type for image", nil)
		return
	}

	// Save file locally
	avatarPath := fmt.Sprintf("uploads/avatars/user_%d%s", userID, ext)
	out, err := os.Create(avatarPath)
	if err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to save avatar", map[string]interface{}{"error": err.Error()})
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, file); err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to write file", map[string]interface{}{"error": err.Error()})
		return
	}

	// Update user profile
	var profile models.UserProfile
	if err := c.DB.Where("user_id = ?", userID).First(&profile).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Profile not found", nil)
		return
	}

	// Normalize path to use forward slashes for web URLs
	normalizedPath := filepath.ToSlash(avatarPath)
	profile.AvatarURL = &normalizedPath
	if err := c.DB.Save(&profile).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update avatar", map[string]interface{}{"error": err.Error()})
		return
	}

	// Notify & track
	c.NotifyAndTrack(
		userID,
		"Profile Updated Successfully", // Notification title
		"Your profile has been updated successfully.", // Message
		"Update",      // Action type
		"UserProfile", // Entity type
		&userID,       // Entity ID
		"Updated",     // Status
		true,          // Send email
	)

	c.Json(w, http.StatusOK, "Avatar uploaded successfully", map[string]interface{}{"profile": profile})
}
