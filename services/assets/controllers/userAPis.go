package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"web/services/assets/models"
	"web/services/utils"

	"github.com/google/uuid"
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
		AvatarURL string `json:"avatar_url"` // add avatar
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

// -------------------- GET USERS --------------------
// -------------------- GET USERS --------------------
func (c *Construct) GetUsers(w http.ResponseWriter, r *http.Request) {
	// Get authenticated user
	authUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// Only SystemAdmin or OpsAdmin allowed
	if authUser.Role.Name != "SystemAdmin" && authUser.Role.Name != "OpsAdmin" {
		c.Json(w, http.StatusForbidden, "Access denied", nil)
		return
	}

	// Build query with preloads
	var users []models.User
	query := c.DB.Preload("Profile").
		Preload("Role").
		Preload("StudentProfile").
		Preload("MentorProfile").
		Preload("SupervisorProfile")

	// Restrict OpsAdmin from retrieving SystemAdmins
	if authUser.Role.Name == "OpsAdmin" {
		query = query.Joins("JOIN roles ON roles.role_id = users.role_id").
			Where("LOWER(roles.name) IN ?", []string{"student", "mentor", "supervisor"})
	}

	if err := query.Find(&users).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch users", map[string]interface{}{"error": err.Error()})
		return
	}

	// Prepare response
	var response []AdminUserResponse
	for _, u := range users {
		fullName := u.Profile.FirstName + " " + u.Profile.LastName
		item := AdminUserResponse{
			UserID:   u.UserID,
			UserUUID: u.UserUUID,
			Username: fullName,
			Email:    u.Email,
			Role:     u.Role.Name,
			IsActive: u.IsActive,
			Profile: struct {
				FirstName string `json:"first_name"`
				LastName  string `json:"last_name"`
				AvatarURL string `json:"avatar_url"`
			}{
				FirstName: u.Profile.FirstName,
				LastName:  u.Profile.LastName,
				AvatarURL: func() string {
					if u.Profile.AvatarURL != nil {
						return *u.Profile.AvatarURL
					}
					return ""
				}(),
			},
		}

		// Add role-specific info
		switch strings.ToLower(u.Role.Name) {
		case "student":
			if u.StudentProfile != nil {
				item.RoleInfo = map[string]interface{}{
					"school":        u.StudentProfile.School,
					"program":       u.StudentProfile.Program,
					"year_of_study": u.StudentProfile.YearOfStudy,
				}
			}
		case "mentor":
			if u.MentorProfile != nil {
				item.RoleInfo = map[string]interface{}{
					"department":   u.MentorProfile.Department,
					"organization": u.MentorProfile.Organization,
					"expertise":    u.MentorProfile.Expertise,
					"years_exp":    u.MentorProfile.YearsExp,
				}
			}
		case "supervisor":
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

	// Audit log
	c.NotifyAndTrack(
		authUser.UserID,
		"Retrieved Users",
		fmt.Sprintf("%s retrieved users list", authUser.Profile.FirstName+" "+authUser.Profile.LastName),
		"Access",
		"User",
		nil,
		"Viewed",
		false, // set true if email notification is needed
	)

	c.Json(w, http.StatusOK, "Users retrieved successfully", map[string]interface{}{
		"users": response,
	})
}

// ================== STUDENTS ==================
func (c *Construct) GetStudents(w http.ResponseWriter, r *http.Request) {
	authUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// Only SystemAdmin or OpsAdmin allowed
	if authUser.Role.Name != "SystemAdmin" && authUser.Role.Name != "OpsAdmin" {
		c.Json(w, http.StatusForbidden, "Access denied", nil)
		return
	}

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
			continue
		}
		response = append(response, StudentResponse{
			UserID:         u.UserID,
			UserUUID:       u.UserUUID,
			Username:       u.Profile.FirstName + " " + u.Profile.LastName,
			Email:          u.Email,
			IsActive:       u.IsActive,
			Profile:        u.Profile,
			StudentProfile: *u.StudentProfile,
		})
	}

	c.NotifyAndTrack(authUser.UserID,
		"Retrieved Students",
		fmt.Sprintf("%s retrieved student list", authUser.Profile.FirstName+" "+authUser.Profile.LastName),
		"Access",
		"User",
		nil,
		"Viewed",
		false,
	)

	c.Json(w, http.StatusOK, "Students retrieved successfully", map[string]interface{}{"students": response})
}

// ================== MENTORS ==================
func (c *Construct) GetMentors(w http.ResponseWriter, r *http.Request) {
	authUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if authUser.Role.Name != "SystemAdmin" && authUser.Role.Name != "OpsAdmin" {
		c.Json(w, http.StatusForbidden, "Access denied", nil)
		return
	}

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
			continue
		}
		response = append(response, MentorResponse{
			UserID:        u.UserID,
			UserUUID:      u.UserUUID,
			Username:      u.Profile.FirstName + " " + u.Profile.LastName,
			Email:         u.Email,
			IsActive:      u.IsActive,
			Profile:       u.Profile,
			MentorProfile: *u.MentorProfile,
		})
	}

	c.NotifyAndTrack(authUser.UserID,
		"Retrieved Mentors",
		fmt.Sprintf("%s retrieved mentor list", authUser.Profile.FirstName+" "+authUser.Profile.LastName),
		"Access",
		"User",
		nil,
		"Viewed",
		false,
	)

	c.Json(w, http.StatusOK, "Mentors retrieved successfully", map[string]interface{}{"mentors": response})
}

// ================== SUPERVISORS ==================
func (c *Construct) GetSupervisors(w http.ResponseWriter, r *http.Request) {
	authUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	role := strings.ToLower(authUser.Role.Name)
	if role != "systemadmin" && role != "opsadmin" && role != "supervisor" {
		c.Json(w, http.StatusForbidden, "Access denied", nil)
		return
	}

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
			continue
		}
		response = append(response, SupervisorResponse{
			UserID:            u.UserID,
			UserUUID:          u.UserUUID,
			Username:          u.Profile.FirstName + " " + u.Profile.LastName,
			Email:             u.Email,
			IsActive:          u.IsActive,
			Profile:           u.Profile,
			SupervisorProfile: *u.SupervisorProfile,
		})
	}

	c.NotifyAndTrack(authUser.UserID,
		"Retrieved Supervisors",
		fmt.Sprintf("%s retrieved supervisor list", authUser.Profile.FirstName+" "+authUser.Profile.LastName),
		"Access",
		"User",
		nil,
		"Viewed",
		false,
	)

	c.Json(w, http.StatusOK, "Supervisors retrieved successfully", map[string]interface{}{"supervisors": response})
}

// -------------------- LOGIN --------------------// -------------------- LOGIN --------------------
type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// -------------------- LOGIN --------------------
func (c *Construct) Login(w http.ResponseWriter, r *http.Request) {
	var input LoginInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Fetch user ---
	var user models.User
	if err := c.DB.Preload("Profile").Preload("Role").
		Where("email = ?", input.Email).First(&user).Error; err != nil {
		c.LogAudit(0, "LOGIN_FAILED", nil, nil, nil, map[string]interface{}{
			"email":  input.Email,
			"reason": "user not found",
		})
		c.Json(w, http.StatusUnauthorized, "Invalid credentials", nil)
		return
	}

	// --- Account checks ---
	if !user.IsActive {
		c.LogAudit(user.UserID, "LOGIN_FAILED", nil, nil, nil, map[string]interface{}{
			"full_name": user.Profile.FirstName + " " + user.Profile.LastName,
			"role":      user.Role.Name,
			"reason":    "account disabled",
		})
		c.Json(w, http.StatusForbidden, "Account disabled", nil)
		return
	}
	if !user.EmailVerified {
		c.LogAudit(user.UserID, "LOGIN_FAILED", nil, nil, nil, map[string]interface{}{
			"full_name": user.Profile.FirstName + " " + user.Profile.LastName,
			"role":      user.Role.Name,
			"reason":    "email not verified",
		})
		c.Json(w, http.StatusForbidden, "Email not verified. Please verify your email first.", nil)
		return
	}

	// --- Verify password ---
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)); err != nil {
		c.LogAudit(user.UserID, "LOGIN_FAILED", nil, nil, nil, map[string]interface{}{
			"full_name": user.Profile.FirstName + " " + user.Profile.LastName,
			"role":      user.Role.Name,
			"reason":    "wrong password",
		})
		c.Json(w, http.StatusUnauthorized, "Invalid credentials", nil)
		return
	}

	// --- Create session ---
	sessionUUID := uuid.New().String()
	session := models.Session{
		SessionUUID:     sessionUUID,
		SessionUserID:   user.UserID,
		SessionUserUUID: user.UserUUID,
		IsActive:        true,
		ExpiresAt:       time.Now().Add(24 * time.Hour),
		UserAgent:       r.UserAgent(),
		IPAddress:       r.RemoteAddr,
		LastActiveAt:    time.Now(),
	}
	if err := c.DB.Create(&session).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not create session", nil)
		return
	}

	// --- Generate JWT including session UUID ---
	tokenString, err := utils.GenerateJWT(user.UserUUID, user.Role.Name, session.SessionUUID)
	if err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not generate token", nil)
		return
	}

	// --- Set HttpOnly cookie ---
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    session.SessionUUID,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   24 * 3600,
	})

	// --- Audit login success ---
	ip := r.RemoteAddr
	c.LogAudit(user.UserID, "LOGIN_SUCCESS", nil, nil, &ip, map[string]interface{}{
		"full_name": user.Profile.FirstName + " " + user.Profile.LastName,
		"role":      user.Role.Name,
		"email":     user.Email,
	})

	// --- Response ---
	c.Json(w, http.StatusOK, "Login successful", map[string]interface{}{
		"token":      tokenString,
		"role":       strings.ToLower(user.Role.Name),
		"full_name":  user.Profile.FirstName + " " + user.Profile.LastName,
		"session_id": session.SessionUUID, // optional
	})
}

// ===============================================================================================
//	                             Logout API
// -----------------------------------------------------------------------------------------------

func (c *Construct) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		c.Json(w, http.StatusBadRequest, "No session cookie found", nil)
		return
	}

	// Fetch session
	var session models.Session
	if err := c.DB.Where("session_uuid = ?", cookie.Value).First(&session).Error; err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid session", nil)
		return
	}

	// Fetch user associated with session
	var user models.User
	if err := c.DB.Preload("Profile").Preload("Role").
		Where("user_uuid = ?", session.SessionUserUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "User not found", nil)
		return
	}

	// Calculate session duration
	duration := time.Since(session.CreatedAt)

	// Deactivate session
	c.DB.Model(&models.Session{}).Where("session_uuid = ?", cookie.Value).Updates(map[string]interface{}{
		"is_active":      false,
		"last_active_at": time.Now(),
	})

	// Clear cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		MaxAge:   -1,
		SameSite: http.SameSiteStrictMode,
	})

	// Audit logout
	c.LogAudit(user.UserID, "LOGOUT", nil, &session.ID, nil, map[string]interface{}{
		"session_uuid": session.SessionUUID,
		"duration":     duration.String(),
		"ip_address":   session.IPAddress,
		"user_agent":   session.UserAgent,
	})

	// Notify and track
	c.NotifyAndTrack(user.UserID, "User Logged Out",
		fmt.Sprintf("User logged out from session %s after %s", session.SessionUUID, duration.String()),
		"Logout", "Session", &session.ID, "Ended", false)

	// Respond with full details
	fullName := user.Profile.FullName()
	role := strings.ToLower(user.Role.Name)

	c.Json(w, http.StatusOK, "Logout successful", map[string]interface{}{
		"session_id":    session.SessionUUID,
		"user_id":       user.UserID,
		"full_name":     fullName,
		"role":          role,
		"duration":      duration.String(),
		"logged_out_at": time.Now(),
	})
}

// -------------------- DELETE USER ----------------------
// ToggleUserStatusInput allows enabling/disabling multiple users
type ToggleUserStatusInput struct {
	UserIDs []uint64 `json:"user_ids"`
	Enable  bool     `json:"enable"` // true = enable, false = disable
}

func (c *Construct) ToggleUserStatus(w http.ResponseWriter, r *http.Request) {
	authUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var input ToggleUserStatusInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request", map[string]interface{}{"error": err.Error()})
		return
	}

	if len(input.UserIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "No user IDs provided", nil)
		return
	}

	// Fetch all users
	var users []models.User
	if err := c.DB.Preload("Profile").Preload("Role").Where("user_id IN ?", input.UserIDs).Find(&users).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch users", map[string]interface{}{"error": err.Error()})
		return
	}

	if len(users) == 0 {
		c.Json(w, http.StatusNotFound, "No matching users found", nil)
		return
	}

	statusText := "disabled"
	if input.Enable {
		statusText = "enabled"
	}

	var updatedUsers []map[string]interface{}
	var skippedUsers []map[string]interface{}

	for _, user := range users {
		fullName := user.Profile.FirstName + " " + user.Profile.LastName
		canToggle := true
		skipReason := ""

		// Security: Only SystemAdmin can toggle another SystemAdmin
		if user.Role.Name == "SystemAdmin" && authUser.Role.Name != "SystemAdmin" {
			canToggle = false
			skipReason = "cannot toggle SystemAdmin"
		}

		// Only SystemAdmin or OpsAdmin can toggle users
		if authUser.Role.Name != "SystemAdmin" && authUser.Role.Name != "OpsAdmin" {
			canToggle = false
			if skipReason == "" {
				skipReason = "insufficient privileges"
			}
		}

		if !canToggle {
			skippedUsers = append(skippedUsers, map[string]interface{}{
				"user_id":   user.UserID,
				"full_name": fullName,
				"role":      user.Role.Name,
				"reason":    skipReason,
			})
			// Log skipped attempt in audit
			c.LogAudit(authUser.UserID, "USER_TOGGLE_SKIPPED", nil, &user.UserID, nil, map[string]interface{}{
				"full_name": fullName,
				"role":      user.Role.Name,
				"email":     user.Email,
				"reason":    skipReason,
			})
			continue
		}

		// Toggle status
		user.IsActive = input.Enable
		if err := c.DB.Save(&user).Error; err != nil {
			fmt.Println("[ERROR] Failed to update user:", user.UserID, err)
			continue
		}

		// Notify the user
		c.NotifyAndTrack(
			user.UserID,
			fmt.Sprintf("Account %s", strings.Title(statusText)),
			fmt.Sprintf("Hello %s, your account has been %s by %s.", fullName, statusText, authUser.Profile.FirstName+" "+authUser.Profile.LastName),
			"Account Status",
			"User",
			&user.UserID,
			strings.Title(statusText),
			true,
		)

		// Record audit log for successful toggle
		c.LogAudit(authUser.UserID, "USER_"+strings.ToUpper(statusText), nil, &user.UserID, nil, map[string]interface{}{
			"full_name": fullName,
			"role":      user.Role.Name,
			"email":     user.Email,
		})

		updatedUsers = append(updatedUsers, map[string]interface{}{
			"user_id":   user.UserID,
			"full_name": fullName,
			"is_active": user.IsActive,
		})
	}

	c.Json(w, http.StatusOK, fmt.Sprintf("Users successfully %s", statusText), map[string]interface{}{
		"updated_users": updatedUsers,
		"skipped_users": skippedUsers,
	})
}
