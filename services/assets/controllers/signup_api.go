package controllers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	"web/services/assets/middlewares"
	"web/services/assets/models"
	"web/services/utils"

	"golang.org/x/crypto/bcrypt"
)

// -------------------- INPUT STRUCTS --------------------

// Student signup payload
type StudentSignupInput struct {
	FirstName   string  `json:"first_name"`
	LastName    string  `json:"last_name"`
	Email       string  `json:"email"`
	Password    string  `json:"password"`
	School      string  `json:"school"`
	Program     string  `json:"program"`
	YearOfStudy string  `json:"year_of_study"`
	Department  *string `json:"department,omitempty"`
}

// Mentor signup payload
type MentorSignupInput struct {
	FirstName    string  `json:"first_name"`
	LastName     string  `json:"last_name"`
	Email        string  `json:"email"`
	Password     string  `json:"password"`
	Department   *string `json:"department,omitempty"`
	Organization string  `json:"organization"`
	Expertise    string  `json:"expertise"`
	YearsExp     int     `json:"years_exp"`
}

// -------------------- HANDLERS --------------------

// SignupStudent handles student self-registration
func (c *Construct) SignupStudent(w http.ResponseWriter, r *http.Request) {
	var input StudentSignupInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	c.signupUserWithRole(
		w,
		"Student",
		input.FirstName,
		input.LastName,
		input.Email,
		input.Password,
		func(userID uint64) error {
			student := models.StudentProfile{
				UserID:      userID,
				School:      input.School,
				Program:     input.Program,
				YearOfStudy: input.YearOfStudy,
			}
			return c.DB.Create(&student).Error
		},
		false,
		"", // no avatar URL for regular signup
	)
}

// SignupMentor handles mentor self-registration
func (c *Construct) SignupMentor(w http.ResponseWriter, r *http.Request) {
	var input MentorSignupInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	c.signupUserWithRole(
		w,
		"Mentor",
		input.FirstName,
		input.LastName,
		input.Email,
		input.Password,
		func(userID uint64) error {
			mentor := models.MentorProfile{
				UserID:       userID,
				Department:   input.Department,
				Organization: input.Organization,
				Expertise:    input.Expertise,
				YearsExp:     input.YearsExp,
			}
			return c.DB.Create(&mentor).Error
		},
		false,
		"", // no avatar URL for regular signup
	)
}

// -------------------- COMMON USER CREATION LOGIC --------------------

func (c *Construct) signupUserWithRole(
	w http.ResponseWriter,
	roleName, firstName, lastName, email, password string,
	createRoleProfile func(userID uint64) error,
	isGoogle bool,
	avatarURL string,
) {
	now := time.Now()

	// 1️⃣ Hash password (skip for Google login)
	var hashedPassword string
	if !isGoogle {
		h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			c.Json(w, http.StatusInternalServerError, "Password hashing failed", map[string]interface{}{"error": err.Error()})
			return
		}
		hashedPassword = string(h)
	}

	// 2️⃣ Check duplicate email
	var existing models.User
	if err := c.DB.Where("email = ?", email).First(&existing).Error; err == nil {
		c.Json(w, http.StatusBadRequest, "Email already exists", nil)
		return
	}

	// 3️⃣ Lookup role
	var role models.Role
	if err := c.DB.Where("LOWER(name) = ?", strings.ToLower(roleName)).First(&role).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Role lookup failed", map[string]interface{}{"error": err.Error()})
		return
	}

	// 4️⃣ Create user
	user := models.User{
		Email:         email,
		PasswordHash:  hashedPassword,
		RoleID:        role.RoleID,
		IsActive:      true,
		EmailVerified: isGoogle,
	}
	if err := c.DB.Create(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create user", map[string]interface{}{"error": err.Error()})
		return
	}

	// 5️⃣ Create UserProfile
	profile := models.UserProfile{
		UserID:    user.UserID,
		FirstName: firstName,
		LastName:  lastName,
	}
	// Set avatar URL if provided (e.g., from Google signup)
	if avatarURL != "" {
		profile.AvatarURL = &avatarURL
	}
	if err := c.DB.Create(&profile).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create user profile", map[string]interface{}{"error": err.Error()})
		return
	}

	// 6️⃣ Create role-specific profile
	if err := createRoleProfile(user.UserID); err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create role-specific profile", map[string]interface{}{"error": err.Error()})
		return
	}

	// 7️⃣ Email verification (non-Google users)
	var token, verifyURL string
	if !isGoogle {
		var err error
		token, err = utils.GenerateRandomString(32)
		if err != nil {
			c.Json(w, http.StatusInternalServerError, "Token generation failed", nil)
			return
		}

		verification := models.EmailVerification{
			UserID:    user.UserID,
			Token:     token,
			ExpiresAt: now.Add(24 * time.Hour),
			CreatedAt: now,
		}
		_ = c.DB.Create(&verification)

		verifyURL = fmt.Sprintf("%s/verify-email?token=%s", os.Getenv("FRONTEND_URL"), token)

		// Send email asynchronously
		go func() {
			// Build the verification URL
			frontendURL := strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")
			verifyURL := fmt.Sprintf("%s/verify-email?token=%s", frontendURL, token)

			// Compose email body
			body := fmt.Sprintf(`
		<p>Hello %s %s,</p>
		<p>Thank you for registering with Innovation Hub as a <strong>%s</strong>. Please verify your email address by clicking the link below:</p>
		<p><a href="%s">Verify Email Address</a></p>
		<p>This verification link will expire in 24 hours.</p>
		<p>If you did not create an account, please ignore this email.</p>
	`, profile.FirstName, profile.LastName, roleName, verifyURL)

			// Send the email
			if err := c.SendEmailNotification(user.Email, "Verify Your Email Address", body); err != nil {
				log.Printf("[ERROR] Failed to send verification email to %s: %v", user.Email, err)
			}
		}()

	}

	// 8️⃣ Audit & notification
	c.NotifyAndTrack(
		user.UserID,
		"Account Created",
		fmt.Sprintf("%s account registered: %s %s", roleName, firstName, lastName),
		"User Registration",
		"User",
		&user.UserID,
		func() string {
			if isGoogle {
				return "Verified"
			}
			return "Pending Verification"
		}(),
		true,
	)

	// 9️⃣ Response
	response := map[string]interface{}{
		"user_id":    user.UserID,
		"first_name": firstName,
		"last_name":  lastName,
		"email":      user.Email,
		"role":       roleName,
	}
	if verifyURL != "" {
		response["verify_url"] = verifyURL
	}

	c.Json(w, http.StatusCreated, fmt.Sprintf("%s registered successfully.", roleName), response)
}

type AdminCreateUserInput struct {
	FirstName    string  `json:"first_name"`
	LastName     string  `json:"last_name"`
	Username     string  `json:"username"`
	Email        string  `json:"email"`
	Department   *string `json:"department,omitempty"`
	Organization string  `json:"organization"`
	Expertise    string  `json:"expertise"`
	YearsExp     int     `json:"years_exp"`
	RoleName     string  `json:"role_name"` // "Supervisor" or "OpsAdmin"
}

// admin account creation

func (c *Construct) AdminCreateUser(w http.ResponseWriter, r *http.Request) {
	var input AdminCreateUserInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request", nil)
		return
	}

	// ✅ Extract viewer UUID from middleware context
	viewerUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || viewerUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// ✅ Fetch viewer from DB
	var viewer models.User
	if err := c.DB.Preload("Role").Where("user_uuid = ?", viewerUUID).First(&viewer).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// ✅ Check if viewer is SystemAdmin
	if viewer.Role.Name != "SystemAdmin" {
		c.Json(w, http.StatusForbidden, "Access denied: insufficient privileges", nil)
		return
	}

	// ✅ Check for existing email
	var exists models.User
	if err := c.DB.Where("email = ?", input.Email).First(&exists).Error; err == nil {
		c.Json(w, http.StatusConflict, "User already exists", nil)
		return
	}

	// ✅ Lookup role to assign
	var role models.Role
	if err := c.DB.Where("LOWER(name) = ?", strings.ToLower(input.RoleName)).First(&role).Error; err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid role", nil)
		return
	}

	now := time.Now()

	// ✅ Generate temporary password
	tempPassword, _ := utils.GenerateRandomString(10)
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(tempPassword), bcrypt.DefaultCost)

	// ✅ Create User (no username)
	user := models.User{
		Email:         input.Email,
		PasswordHash:  string(hashedPassword),
		RoleID:        role.RoleID,
		IsActive:      true,
		EmailVerified: false,
		CreatedAt:     now,
	}
	if err := c.DB.Create(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create user", map[string]interface{}{"error": err.Error()})
		return
	}

	// ✅ Create UserProfile
	profile := models.UserProfile{
		UserID:    user.UserID,
		FirstName: input.FirstName,
		LastName:  input.LastName,
	}
	_ = c.DB.Create(&profile)

	// ✅ Role-specific profile (example: Supervisor or OpsAdmin)
	switch strings.ToLower(role.Name) {
	case "supervisor":
		supervisor := models.SupervisorProfile{
			UserID:       user.UserID,
			Department:   input.Department,
			Organization: input.Organization,
			Expertise:    input.Expertise,
			YearsExp:     input.YearsExp,
		}
		_ = c.DB.Create(&supervisor)
	case "opsadmin":
		// Add OpsAdmin profile if needed
		// Currently, no extra fields
	default:
		// No role-specific profile needed
	}

	// ✅ Create Email Verification Token
	token, _ := utils.GenerateRandomString(32)
	verification := models.EmailVerification{
		UserID:    user.UserID,
		Token:     token,
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	}
	_ = c.DB.Create(&verification)

	// ✅ Send verification email asynchronously
	go func() {
		// Build the verification URL
		frontendURL := strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")
		verifyURL := fmt.Sprintf("%s/verify-email?token=%s", frontendURL, token)

		// Compose the email body
		body := fmt.Sprintf(`
		<p>Hello %s %s,</p>
		<p>Your <strong>%s</strong> account has been created successfully by the System Administrator.</p>
		<p><strong>Your temporary password:</strong> %s</p>
		<p><strong>Please change this password after your first login for security purposes.</strong></p>
		<p>To complete your account setup, please verify your email address by clicking the link below:</p>
		<p><a href="%s">Verify Email Address</a></p>
		<p>This verification link will expire in 24 hours.</p>
	`, input.FirstName, input.LastName, input.RoleName, tempPassword, verifyURL)

		// Send the email and log any errors
		if err := c.SendEmailNotification(input.Email, "Your Account Has Been Created", body); err != nil {
			log.Printf("[ERROR] Failed to send account creation email to %s: %v", input.Email, err)
		}
	}()

	// ✅ Track & notify audit
	c.NotifyAndTrack(
		user.UserID,
		"Account Created by Admin",
		fmt.Sprintf("%s account for %s %s created by System Admin", input.RoleName, input.FirstName, input.LastName),
		"Account Management",
		"User",
		&user.UserID,
		"Pending Verification",
		true,
	)

	c.Json(w, http.StatusCreated, fmt.Sprintf("%s account created successfully", input.RoleName), map[string]interface{}{
		"user_id":    user.UserID,
		"email":      user.Email,
		"role":       role.Name,
		"temp_pass":  tempPassword,
		"verify_url": fmt.Sprintf("%s/verify-email?token=%s", os.Getenv("FRONTEND_URL"), token),
	})
}

// edit or update users
type AdminUpdateUserInput struct {
	UserID       uint64  `json:"user_id"`
	FirstName    string  `json:"first_name"`
	LastName     string  `json:"last_name"`
	Email        string  `json:"email"`
	RoleName     string  `json:"role_name"`
	IsActive     bool    `json:"is_active"`
	Department   *string `json:"department,omitempty"`
	Organization string  `json:"organization,omitempty"`
	Expertise    string  `json:"expertise,omitempty"`
	YearsExp     int     `json:"years_exp,omitempty"`
}

func (c *Construct) UpdateUserByAdmin(w http.ResponseWriter, r *http.Request) {
	var input AdminUpdateUserInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request", nil)
		return
	}

	// ✅ Extract viewer UUID from middleware context
	viewerUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || viewerUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// ✅ Fetch viewer from DB
	var viewer models.User
	if err := c.DB.Preload("Role").Where("user_uuid = ?", viewerUUID).First(&viewer).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// ✅ Check if viewer is SystemAdmin
	if viewer.Role.Name != "SystemAdmin" {
		c.Json(w, http.StatusForbidden, "Access denied: insufficient privileges", nil)
		return
	}

	// ✅ Fetch user to update
	var user models.User
	if err := c.DB.Preload("Profile").Where("user_id = ?", input.UserID).First(&user).Error; err != nil {
		c.Json(w, http.StatusNotFound, "User not found", nil)
		return
	}

	// ✅ Check for email uniqueness (ignore current user)
	var exists models.User
	if err := c.DB.Where("email = ? AND user_id != ?", input.Email, user.UserID).First(&exists).Error; err == nil {
		c.Json(w, http.StatusConflict, "Email already in use", nil)
		return
	}

	// ✅ Update User fields
	user.Email = input.Email
	user.IsActive = input.IsActive
	if input.RoleName != "" {
		var role models.Role
		if err := c.DB.Where("LOWER(name) = ?", strings.ToLower(input.RoleName)).First(&role).Error; err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid role", nil)
			return
		}
		user.RoleID = role.RoleID
	}
	if err := c.DB.Save(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update user", map[string]interface{}{"error": err.Error()})
		return
	}

	// ✅ Update UserProfile
	profile := models.UserProfile{
		UserID:    user.UserID,
		FirstName: input.FirstName,
		LastName:  input.LastName,
	}
	c.DB.Model(&models.UserProfile{}).Where("user_id = ?", user.UserID).Updates(profile)

	// ✅ Update role-specific profile (Supervisor example)
	if strings.ToLower(input.RoleName) == "supervisor" {
		supervisor := models.SupervisorProfile{
			UserID:       user.UserID,
			Department:   input.Department,
			Organization: input.Organization,
			Expertise:    input.Expertise,
			YearsExp:     input.YearsExp,
		}
		c.DB.Model(&models.SupervisorProfile{}).Where("user_id = ?", user.UserID).Updates(supervisor)
	}

	// ✅ If email changed, create new verification token
	if !user.EmailVerified {
		token, _ := utils.GenerateRandomString(32)
		verification := models.EmailVerification{
			UserID:    user.UserID,
			Token:     token,
			ExpiresAt: time.Now().Add(24 * time.Hour),
			CreatedAt: time.Now(),
		}
		c.DB.Create(&verification)

		// Send verification email
		go func() {
			// Build the verification URL
			frontendURL := strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")
			verifyURL := fmt.Sprintf("%s/verify-email?token=%s", frontendURL, token)

			// Compose email body
			body := fmt.Sprintf(`
		<p>Hello %s %s,</p>
		<p>Your account information has been updated by the System Administrator. To complete the update, please verify your email address by clicking the link below:</p>
		<p><a href="%s">Verify Email Address</a></p>
		<p>This verification link will expire in 24 hours.</p>
	`, profile.FirstName, profile.LastName, verifyURL)

			// Send the email
			if err := c.SendEmailNotification(user.Email, "Verify Your Updated Email Address", body); err != nil {
				log.Printf("[ERROR] Failed to send verification email to %s: %v", user.Email, err)
			}
		}()

	}

	// ✅ Audit & tracking
	c.NotifyAndTrack(
		user.UserID,
		"Account Updated by Admin",
		fmt.Sprintf("User account %s %s updated by SystemAdmin", profile.FirstName, profile.LastName),
		"Account Management",
		"User",
		&user.UserID,
		"Updated",
		true,
	)

	c.Json(w, http.StatusOK, "User account updated successfully", map[string]interface{}{
		"user_id":   user.UserID,
		"email":     user.Email,
		"role":      input.RoleName,
		"is_active": user.IsActive,
	})
}
