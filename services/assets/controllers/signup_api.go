package controllers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	"web/services/assets/models"
	"web/services/utils"

	"golang.org/x/crypto/bcrypt"
)

// Updated signup input structs with FirstName & LastName
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

type SupervisorSignupInput struct {
	FirstName    string  `json:"first_name"`
	LastName     string  `json:"last_name"`
	Email        string  `json:"email"`
	Password     string  `json:"password"`
	Department   *string `json:"department,omitempty"`
	Organization string  `json:"organization"`
	Expertise    string  `json:"expertise"`
	YearsExp     int     `json:"years_exp"`
}

// -------------------- SIGNUP HANDLERS --------------------

// SignupStudent handles student registration
func (c *Construct) SignupStudent(w http.ResponseWriter, r *http.Request) {
	var input StudentSignupInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request", map[string]interface{}{"error": err.Error()})
		return
	}

	c.signupUserWithRole(w,
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
	)
}

// SignupMentor handles mentor registration
func (c *Construct) SignupMentor(w http.ResponseWriter, r *http.Request) {
	var input MentorSignupInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request", map[string]interface{}{"error": err.Error()})
		return
	}

	c.signupUserWithRole(w,
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
	)
}

// SignupSupervisor handles supervisor registration with email verification
func (c *Construct) SignupSupervisor(w http.ResponseWriter, r *http.Request) {
	var input SupervisorSignupInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request", map[string]interface{}{"error": err.Error()})
		return
	}

	c.signupUserWithRole(
		w,
		"Supervisor",
		input.FirstName,
		input.LastName,
		input.Email,
		input.Password,
		func(userID uint64) error {
			supervisor := models.SupervisorProfile{
				UserID:       userID,
				Department:   input.Department,
				Organization: input.Organization,
				Expertise:    input.Expertise,
				YearsExp:     input.YearsExp,
			}
			return c.DB.Create(&supervisor).Error
		},
	)
}

// signupUserWithRole creates a User, UserProfile, role-specific profile,

func (c *Construct) signupUserWithRole(
	w http.ResponseWriter,
	roleName, firstName, lastName, email, password string,
	createRoleProfile func(userID uint64) error,
) {
	now := time.Now()

	// 1️⃣ Hash the password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to hash password", map[string]interface{}{"error": err.Error()})
		return
	}

	// 2️⃣ Check if the email already exists
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

	// 4️⃣ Create the user
	user := models.User{
		Email:         email,
		PasswordHash:  string(hashedPassword),
		RoleID:        role.RoleID,
		IsActive:      true,
		EmailVerified: false, // not verified yet
	}
	if err := c.DB.Create(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create user", map[string]interface{}{"error": err.Error()})
		return
	}

	// 5️⃣ Create UserProfile with first and last name
	profile := models.UserProfile{
		UserID:    user.UserID,
		FirstName: firstName,
		LastName:  lastName,
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

	// 7️⃣ Generate email verification token
	token, err := utils.GenerateRandomString(32)
	if err != nil {
		log.Println("Failed to generate verification token:", err)
		c.Json(w, http.StatusInternalServerError, "Could not generate verification token", nil)
		return
	}

	verification := models.EmailVerification{
		UserID:    user.UserID,
		Token:     token,
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	}
	if err := c.DB.Create(&verification).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create email verification record", map[string]interface{}{"error": err.Error()})
		return
	}

	// 8️⃣ Send verification email asynchronously
	go func() {
		verifyURL := fmt.Sprintf("%s/verify-email?token=%s", os.Getenv("FRONTEND_URL"), token)
		body := fmt.Sprintf(
			"Hello %s %s,<br><br>"+
				"Thank you for registering. Please verify your email by clicking "+
				"<a href='%s'>here</a>.<br><br>Expires in 24 hours.",
			profile.FirstName, profile.LastName, verifyURL,
		)
		if err := c.SendEmailNotification(user.Email, "Verify Your Email", body); err != nil {
			log.Printf("[ERROR] Failed to send verification email to %s: %v", user.Email, err)
		}
	}()

	// 9️⃣ Audit & notification
	c.NotifyAndTrack(
		user.UserID,
		"Account Created",
		fmt.Sprintf("New account registered: %s %s (%s)", profile.FirstName, profile.LastName, roleName),
		"Account Creation",
		"User",
		&user.UserID,
		"Pending Verification",
		true,
	)

	// 10️⃣ Return response with full name and email
	// 10️⃣ Return response with full name, email, and dev verification link
	verifyURL := fmt.Sprintf("%s/verify-email?token=%s", os.Getenv("FRONTEND_URL"), token)

	response := map[string]interface{}{
		"user_id":     user.UserID,
		"first_name":  profile.FirstName,
		"last_name":   profile.LastName,
		"email":       user.Email,
		"profile_id":  profile.ProfileID,
		"verify_link": verifyURL, // ✅ show verification link for dev/testing
		"token":       token,     // ✅ show token directly for manual testing
	}

	c.Json(w, http.StatusCreated,
		fmt.Sprintf("%s registered successfully. Please verify your email.", roleName),
		response)

}
