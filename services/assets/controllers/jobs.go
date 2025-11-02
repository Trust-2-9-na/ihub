package controllers

import (
	"encoding/json"
	"fmt"
	"time"
	"web/services/assets/models"

	"gorm.io/datatypes"
)

// helper functionsfor audit logs
func (c *Construct) LogAudit(userID uint64, action string, entity *string, entityID *uint64, ip *string, metadata map[string]interface{}) error {
	var metadataJSON datatypes.JSON
	if metadata != nil {
		b, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		metadataJSON = datatypes.JSON(b)
	}

	logEntry := models.AuditLog{
		UserID:    userID,
		Action:    action,
		Entity:    entity,
		EntityID:  entityID,
		IPAddress: ip,
		Metadata:  metadataJSON,
	}
	return c.DB.Create(&logEntry).Error
}

// helper function for jobs log
func (c *Construct) StartJob(jobName string, metadata map[string]interface{}) (*models.JobLog, error) {
	var metadataJSON datatypes.JSON
	if metadata != nil {
		b, err := json.Marshal(metadata)
		if err != nil {
			return nil, err
		}
		metadataJSON = datatypes.JSON(b)
	}

	job := models.JobLog{
		JobName:  jobName,
		Status:   "Started",
		Metadata: metadataJSON,
	}

	if err := c.DB.Create(&job).Error; err != nil {
		return nil, err
	}

	return &job, nil
}

func (c *Construct) EndJob(job *models.JobLog, status string, message *string) error {
	now := time.Now()
	job.Status = status
	job.EndTime = &now
	job.Message = message
	return c.DB.Save(job).Error
}

// CreateNotification sends an in-app notification and optional email notification
func (c *Construct) CreateNotification(userID uint64, title, message string, sendEmail bool) error {
	// 1️⃣ Save in-app notification
	notification := models.Notification{
		UserID:  userID,
		Title:   title,
		Message: message,
	}

	if err := c.DB.Create(&notification).Error; err != nil {
		return err
	}

	// 2️⃣ Optionally send email
	if sendEmail {
		// Fetch user's email
		var user models.User
		if err := c.DB.Preload("Profile").First(&user, "user_id = ?", userID).Error; err != nil {
			return fmt.Errorf("notification saved, but failed to fetch user email: %w", err)
		}

		// Use your email sender utility
		err := c.SendEmail(user.Email, title, message)
		if err != nil {
			return fmt.Errorf("notification saved, but failed to send email: %w", err)
		}
	}

	return nil
}
