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

// ----------------- CHANGE PASSWORD -----------------

func (c *Construct) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var input models.PasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	authUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// Verify current password
	if !utils.CheckPasswordHash(input.CurrentPassword, authUser.PasswordHash) {
		c.Json(w, http.StatusBadRequest, "Current password is incorrect", nil)
		return
	}

	// Confirm new password
	if input.NewPassword != input.ConfirmPassword {
		c.Json(w, http.StatusBadRequest, "Passwords do not match", nil)
		return
	}

	// Hash new password
	hashed, err := utils.HashPassword(input.NewPassword)
	if err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to hash password", map[string]interface{}{"error": err.Error()})
		return
	}

	// Update password
	if err := c.DB.Model(&authUser).Update("password_hash", hashed).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update password", map[string]interface{}{"error": err.Error()})
		return
	}

	// Audit
	c.NotifyAndTrack(
		authUser.UserID,
		"Password Changed",
		"Your password has been changed successfully.",
		"Update",
		"User",
		&authUser.UserID,
		"Updated",
		true,
	)

	// ✅ Return meaningful data
	c.Json(w, http.StatusOK, "Password changed successfully", map[string]interface{}{
		"user_id": authUser.UserID,
	})
}

// ----------------- FORGOT PASSWORD -----------------
func (c *Construct) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	// --- 1. Parse input ---
	var input struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 2. Fetch user by email ---
	var user models.User
	if err := c.DB.Where("email = ?", input.Email).First(&user).Error; err != nil {
		// Do not reveal whether email exists
		c.Json(w, http.StatusOK, "If the email exists, a reset link has been sent", nil)
		return
	}

	// --- 3. Generate reset token ---
	token, _ := utils.GenerateRandomString(32)
	expiry := time.Now().Add(1 * time.Hour)

	// --- 4. Save token in DB ---
	reset := models.PasswordResetToken{
		UserID: user.UserID,
		Token:  token,
		Expiry: expiry,
	}
	if err := c.DB.Create(&reset).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to generate reset token", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 5. Build email ---
	frontendURL := strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")
	resetLink := fmt.Sprintf("%s/reset-password?token=%s", frontendURL, token)
	subject := "Password Reset Request"
	body := fmt.Sprintf(`<p>Hello %s,</p>
<p>You have requested to reset your password. Please click the link below to proceed:</p>
<p><a href="%s">Reset Password</a></p>
<p>This link will expire in 1 hour for security purposes.</p>
<p>If you did not request this password reset, please ignore this email and your password will remain unchanged.</p>`, user.Username, resetLink)

	// --- 6. Send email asynchronously ---
	go func() {
		if err := c.SendEmailNotification(user.Email, subject, body); err != nil {
			log.Printf("[ERROR] Failed to send password reset email to %s: %v", user.Email, err)
		}
	}()

	// --- 7. Audit ---
	c.NotifyAndTrack(user.UserID, "Forgot Password Requested", "User requested password reset", "Access", "User", &user.UserID, "Viewed", true)

	// --- 8. Return response ---
	c.Json(w, http.StatusOK, "If the email exists, a reset link has been sent", map[string]interface{}{
		"email": user.Email,
		"token": token, // optional, useful for frontend testing
	})
}

// ----------------- RESET PASSWORD -----------------
// ----------------- RESET PASSWORD -----------------
func (c *Construct) ResetPassword(w http.ResponseWriter, r *http.Request) {
	// --- 1. Parse input ---
	var input struct {
		Token           string `json:"token"`
		NewPassword     string `json:"new_password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 2. Validate token ---
	var tokenRecord models.PasswordResetToken
	if err := c.DB.Where("token = ?", input.Token).First(&tokenRecord).Error; err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid or expired token", nil)
		return
	}

	// --- 3. Check expiry ---
	if time.Now().After(tokenRecord.Expiry) {
		c.Json(w, http.StatusBadRequest, "Token has expired", nil)
		return
	}

	// --- 4. Confirm passwords match ---
	if input.NewPassword != input.ConfirmPassword {
		c.Json(w, http.StatusBadRequest, "Passwords do not match", nil)
		return
	}

	// --- 5. Hash new password ---
	hashed, err := utils.HashPassword(input.NewPassword)
	if err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to hash password", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 6. Update user password ---
	if err := c.DB.Model(&models.User{}).Where("user_id = ?", tokenRecord.UserID).
		Update("password_hash", hashed).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to reset password", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 7. Delete token after use ---
	c.DB.Delete(&tokenRecord)

	// --- 8. Audit ---
	c.NotifyAndTrack(tokenRecord.UserID, "Password Reset", "User reset password via token", "Update", "User", &tokenRecord.UserID, "Updated", true)

	// --- 9. Response ---
	c.Json(w, http.StatusOK, "Password has been reset successfully", nil)
}
