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
)

// VerifyEmail handles the verification of a user's email using a token

func (c *Construct) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		c.Json(w, http.StatusBadRequest, "Missing verification token", nil)
		return
	}

	var verification models.EmailVerification
	if err := c.DB.Where("token = ?", token).First(&verification).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Invalid or expired verification token", nil)
		return
	}

	// Check if token has expired
	if time.Now().After(verification.ExpiresAt) {
		c.Json(w, http.StatusBadRequest, "Verification token has expired", nil)
		return
	}

	// Fetch user
	var user models.User
	if err := c.DB.Preload("Profile").Where("user_id = ?", verification.UserID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "User not found", map[string]interface{}{"error": err.Error()})
		return
	}

	// Update user's email verification status
	user.EmailVerified = true
	if err := c.DB.Save(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to verify email", map[string]interface{}{"error": err.Error()})
		return
	}

	// Optionally: delete token after successful verification
	_ = c.DB.Delete(&verification).Error

	// Log audit and notify
	fullName := fmt.Sprintf("%s %s", user.Profile.FirstName, user.Profile.LastName)
	c.NotifyAndTrack(
		user.UserID,
		"Email Verified",
		fmt.Sprintf("User %s (%s) verified their email", fullName, user.Email),
		"Email Verification",
		"User",
		&user.UserID,
		"Verified",
		true,
	)

	c.Json(w, http.StatusOK, "Email verified successfully", map[string]interface{}{
		"user_id":    user.UserID,
		"first_name": user.Profile.FirstName,
		"last_name":  user.Profile.LastName,
		"email":      user.Email,
	})
}

// resend email verification link

type ResendVerificationInput struct {
	Email string `json:"email"`
}

func (c *Construct) ResendVerificationEmail(w http.ResponseWriter, r *http.Request) {
	var input ResendVerificationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request", map[string]interface{}{"error": err.Error()})
		return
	}

	// 1️⃣ Fetch the user
	var user models.User
	if err := c.DB.Preload("Profile").Where("email = ?", input.Email).First(&user).Error; err != nil {
		c.Json(w, http.StatusNotFound, "User not found", nil)
		return
	}

	// 2️⃣ Check if already verified
	if user.EmailVerified {
		c.Json(w, http.StatusBadRequest, "Email already verified", nil)
		return
	}

	now := time.Now()

	// 3️⃣ Generate new verification token
	token, err := utils.GenerateRandomString(32)
	if err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to generate verification token", nil)
		return
	}

	verification := models.EmailVerification{
		UserID:    user.UserID,
		Token:     token,
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	}

	// 4️⃣ Save token to DB
	if err := c.DB.Create(&verification).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to save verification token", map[string]interface{}{"error": err.Error()})
		return
	}

	// 5️⃣ Send email asynchronously
	go func() {
		frontendURL := strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")
		verifyURL := fmt.Sprintf("%s/verify-email?token=%s", frontendURL, token)
		body := fmt.Sprintf(
			`<p>Hello %s %s,</p>
<p>Thank you for registering with IHub. Please verify your email address by clicking the link below:</p>
<p><a href="%s">Verify Email Address</a></p>
<p>This verification link will expire in 24 hours.</p>
<p>If you did not create an account, please ignore this email.</p>`,
			user.Profile.FirstName, user.Profile.LastName, verifyURL,
		)
		if err := c.SendEmailNotification(user.Email, "Verify Your Email Address", body); err != nil {
			log.Printf("[ERROR] Failed to send verification email to %s: %v", user.Email, err)
		}
	}()

	// 6️⃣ Audit & notify
	fullName := fmt.Sprintf("%s %s", user.Profile.FirstName, user.Profile.LastName)

	// --- 1. Notify the user ---
	c.NotifyAndTrack(
		user.UserID,
		"Email Verified",
		fmt.Sprintf("Your email address (%s) has been successfully verified.", user.Email),
		"Email Verification",
		"User",
		&user.UserID,
		"Verified",
		true,
	)

	// --- 2. Log / notify admins or for auditing ---
	c.NotifyAndTrack(
		0, // 0 or system/admin user ID if needed
		"User Email Verified",
		fmt.Sprintf("User %s (%s) has verified their email.", fullName, user.Email),
		"Email Verification",
		"System",
		&user.UserID,
		"Verified",
		true,
	)

	c.Json(w, http.StatusOK, "Verification email sent successfully", nil)
}

// CheckEmailVerifiedInput is optional if you want query param instead of path param
type CheckEmailVerifiedInput struct {
	UserID uint64 `json:"user_id"`
}

// CheckEmailVerified returns whether a user's email is verified
func (c *Construct) CheckEmailVerified(w http.ResponseWriter, r *http.Request) {
	// Get user_id from query param
	userID, err := c.GetUintParam(r, "user_id")
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Missing or invalid user_id", nil)
		return
	}

	// Fetch user
	var user models.User
	if err := c.DB.Preload("Profile").Where("user_id = ?", userID).First(&user).Error; err != nil {
		c.Json(w, http.StatusNotFound, "User not found", nil)
		return
	}

	// Build response
	resp := map[string]interface{}{
		"user_id":        user.UserID,
		"first_name":     user.Profile.FirstName,
		"last_name":      user.Profile.LastName,
		"email":          user.Email,
		"email_verified": user.EmailVerified,
	}

	c.Json(w, http.StatusOK, "Email verification status fetched successfully", resp)
}
