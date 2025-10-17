package controllers

import (
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"time"
	"web/services/assets/middlewares"
	"web/services/assets/models"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// NotifyAndTrack handles notifications, system/audit logs, and optionally sends email alerts.
func (c *Construct) NotifyAndTrack(
	targetID uint64,
	title, message, category, entityType string,
	entityID *uint64,
	status string,
	sendEmail bool, // new flag
) {
	now := time.Now()

	// 1️⃣ Create in-app notification
	notification := &models.Notification{
		UserID:    targetID,
		Title:     title,
		Message:   message,
		IsRead:    false,
		CreatedAt: now,
	}
	if err := c.DB.Create(notification).Error; err != nil {
		log.Printf("[ERROR] Failed to create notification: %v\n", err)
	}

	// 2️⃣ Record system history
	history := &models.SystemHistory{
		EntityType:  entityType,
		EntityID:    entityID,
		Action:      category,
		Status:      &status,
		Comment:     &message,
		ChangedByID: targetID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := c.DB.Create(history).Error; err != nil {
		log.Printf("[ERROR] Failed to create system history: %v\n", err)
	}

	// 3️⃣ Record audit log
	audit := &models.AuditLog{
		UserID:    targetID,
		Action:    category,
		Entity:    &entityType,
		EntityID:  entityID,
		Metadata:  datatypes.JSON([]byte(fmt.Sprintf(`{"message": "%s"}`, message))),
		CreatedAt: now,
	}
	if err := c.DB.Create(audit).Error; err != nil {
		log.Printf("[ERROR] Failed to create audit log: %v\n", err)
	}

	// 4️⃣ Record job log
	job := &models.JobLog{
		JobName:   "Send_" + category + "_Notification",
		Status:    "Queued",
		StartTime: now,
		Message:   &message,
		Metadata: datatypes.JSON([]byte(fmt.Sprintf(
			`{"target_user_id": %d, "entity": "%s"}`, targetID, entityType,
		))),
		CreatedAt: now,
	}
	if err := c.DB.Create(job).Error; err != nil {
		log.Printf("[ERROR] Failed to create job log: %v\n", err)
	}

	// 5️⃣ Send email only if sendEmail is true
	if sendEmail {
		var user models.User
		if err := c.DB.Preload("Profile").Select("email, user_id").First(&user, "user_id = ?", targetID).Error; err != nil {
			log.Printf("[ERROR] Could not fetch user email/profile for ID %d: %v\n", targetID, err)
			return
		}

		fullName := user.Profile.FirstName + " " + user.Profile.LastName

		go func(to, subject, body, fullName string) {
			emailBody := fmt.Sprintf(`
				<html>
					<body style="font-family: Arial, sans-serif; line-height: 1.6;">
						<p>Hi %s,</p>
						<p>%s</p>
						<p style="margin-top: 20px;">Best regards,<br><strong>Innovation Hub System</strong></p>
						<hr>
						<p style="font-size: 12px; color: #888;">This is an automated message. Please do not reply.</p>
					</body>
				</html>`, fullName, body)

			if err := c.SendEmailNotification(to, subject, emailBody); err != nil {
				log.Printf("[ERROR] Email notification failed to %s: %v\n", to, err)
				_ = c.DB.Model(&models.JobLog{}).Where("job_name = ?", job.JobName).
					Update("status", "Failed").Error
				return
			}

			_ = c.DB.Model(&models.JobLog{}).Where("job_name = ?", job.JobName).
				Update("status", "Completed").Error
		}(user.Email, title, message, fullName)
	}
}

// SendEmailNotification sends an HTML email using SMTP
func (c *Construct) SendEmailNotification(to, subject, body string) error {
	// Load SMTP settings from environment
	from := os.Getenv("SMTP_EMAIL")
	password := os.Getenv("SMTP_PASSWORD")
	smtpHost := os.Getenv("SMTP_HOST")
	smtpPort := os.Getenv("SMTP_PORT") // "587" for TLS

	if from == "" || password == "" || smtpHost == "" || smtpPort == "" {
		return fmt.Errorf("SMTP configuration is incomplete")
	}

	// Setup authentication
	auth := smtp.PlainAuth("", from, password, smtpHost)

	// Build email message
	msg := []byte(fmt.Sprintf("To: %s\r\n"+
		"Subject: %s\r\n"+
		"Content-Type: text/html; charset=UTF-8\r\n\r\n"+
		"%s\r\n", to, subject, body))

	// Send the email
	addr := fmt.Sprintf("%s:%s", smtpHost, smtpPort)
	if err := smtp.SendMail(addr, auth, from, []string{to}, msg); err != nil {
		log.Printf("[ERROR] Failed to send email to %s: %v", to, err)
		return err
	}

	log.Printf("[INFO] Email sent to %s successfully", to)
	return nil
}

// GetAuthenticatedUser retrieves the currently authenticated user from the request context.

func (c *Construct) GetAuthenticatedUser(r *http.Request) (*models.User, error) {
	// 1️⃣ Extract user UUID from context
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		log.Println("[ERROR] No user_uuid found in request context")
		return nil, fmt.Errorf("unauthorized: no user in context")
	}

	// 2️⃣ Fetch user along with Profile and Role
	var user models.User
	err := c.DB.Preload("Profile").Preload("Role").
		First(&user, "user_uuid = ? AND deleted_at IS NULL", userUUID).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Printf("[ERROR] User not found for UUID: %s\n", userUUID)
			return nil, fmt.Errorf("user not found")
		}
		log.Printf("[ERROR] Database error while fetching user for UUID %s: %v\n", userUUID, err)
		return nil, fmt.Errorf("database error")
	}

	// 3️⃣ Construct full name for logging
	fullName := user.Profile.FirstName + " " + user.Profile.LastName

	// 4️⃣ Debug log
	log.Printf("[DEBUG] Authenticated user loaded: %s (ID: %d) | Role: %s\n",
		fullName, user.UserID, user.Role.Name)

	return &user, nil
}
