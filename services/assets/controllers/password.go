package controllers

import (
	"encoding/json"
	"net/http"
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
		"You changed Your password",
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
	var input models.PasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	var user models.User
	if err := c.DB.Where("email = ?", input.Email).First(&user).Error; err != nil {
		// Do not reveal if email exists
		c.Json(w, http.StatusOK, "If the email exists, a reset link has been sent", nil)
		return
	}

	// Generate token
	token, _ := utils.GenerateRandomString(32)
	expiry := time.Now().Add(1 * time.Hour)

	// Save token
	reset := models.PasswordResetToken{
		UserID: user.UserID,
		Token:  token,
		Expiry: expiry,
	}
	c.DB.Create(&reset)

	// Audit
	c.NotifyAndTrack(user.UserID, "Forgot Password Requested", "User requested password reset", "Access", "User", &user.UserID, "Viewed", true)

	// Return token for testing
	c.Json(w, http.StatusOK, "Password reset token generated successfully", map[string]interface{}{
		"email": user.Email,
		"token": token,
	})
}

// ----------------- RESET PASSWORD -----------------
func (c *Construct) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var input models.PasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	var tokenRecord models.PasswordResetToken
	if err := c.DB.Where("token = ?", input.Token).First(&tokenRecord).Error; err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid or expired token", nil)
		return
	}

	// Check expiry
	if time.Now().After(tokenRecord.Expiry) {
		c.Json(w, http.StatusBadRequest, "Token has expired", nil)
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

	// Update user password
	if err := c.DB.Model(&models.User{}).Where("user_id = ?", tokenRecord.UserID).Update("password_hash", hashed).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to reset password", map[string]interface{}{"error": err.Error()})
		return
	}

	// Delete token after use
	c.DB.Delete(&tokenRecord)

	// Audit
	c.NotifyAndTrack(tokenRecord.UserID, "Password Reset", "User reset password via token", "Update", "User", &tokenRecord.UserID, "Updated", true)

	c.Json(w, http.StatusOK, "Password has been reset successfully", nil)
}
