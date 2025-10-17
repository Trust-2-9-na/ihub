package controllers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
	"web/services/assets/models"
	"web/services/utils"
)

// Ve// VerifyEmail handles the verification of a user's email using a token

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
		verifyURL := fmt.Sprintf("%s/verify-email?token=%s", os.Getenv("FRONTEND_URL"), token)
		body := fmt.Sprintf(
			"Hello %s %s,<br><br>Please verify your email by clicking <a href='%s'>here</a>.<br><br>Expires in 24 hours.",
			user.Profile.FirstName, user.Profile.LastName, verifyURL,
		)
		if err := c.SendEmailNotification(user.Email, "Verify Your Email", body); err != nil {
			log.Printf("[ERROR] Failed to send verification email to %s: %v", user.Email, err)
		}
	}()

	// 6️⃣ Audit & notify
	c.NotifyAndTrack(
		user.UserID,
		"Resent Email Verification",
		fmt.Sprintf("Verification email resent to %s %s", user.Profile.FirstName, user.Profile.LastName),
		"Email Verification",
		"User",
		&user.UserID,
		"Pending Verification",
		true,
	)

	c.Json(w, http.StatusOK, "Verification email sent successfully", nil)
}
