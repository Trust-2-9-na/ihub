package controllers

import (
	"fmt"
	"log"
	"net/http"
	"time"
	"web/services/assets/models"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// NotifyAndTrack handles notifications, audit tracking, and background jobs in one call.
func (c *Construct) NotifyAndTrack(
	targetID uint64,
	title, message, category, entityType string,
	entityID *uint64,
	status string,
) {
	// 1️⃣ Notification
	notification := &models.Notification{
		UserID:    targetID,
		Title:     title,
		Message:   message,
		IsRead:    false,
		CreatedAt: time.Now(),
	}
	_ = c.DB.Create(notification).Error

	// 2️⃣ System History (optional if you still use it)
	history := &models.SystemHistory{
		EntityType:  entityType,
		EntityID:    entityID,
		Action:      category,
		Status:      &status,
		Comment:     &message, // optional readable text
		ChangedByID: targetID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	_ = c.DB.Create(history).Error

	// 3️⃣ Audit Log
	audit := &models.AuditLog{
		UserID:    targetID,
		Action:    category,
		Entity:    &entityType,
		EntityID:  entityID,
		Metadata:  datatypes.JSON([]byte(`{"message": "` + message + `"}`)),
		CreatedAt: time.Now(),
	}
	_ = c.DB.Create(audit).Error

	// 4️⃣ Job Log (for background jobs like sending email notifications)
	job := &models.JobLog{
		JobName:   "Send_" + category + "_Notification",
		Status:    "Queued",
		StartTime: time.Now(),
		Message:   &message,
		Metadata: datatypes.JSON([]byte(`{"target_user_id": ` +
			fmt.Sprintf("%d", targetID) +
			`, "entity": "` + entityType + `"}`)),
		CreatedAt: time.Now(),
	}
	_ = c.DB.Create(job).Error
}

func (c *Construct) GetAuthenticatedUser(r *http.Request) (*models.User, error) {
	// Try to get user UUID from context (set by JWT/auth middleware)
	userUUID, ok := r.Context().Value("user_uuid").(string)
	if !ok || userUUID == "" {
		log.Println("[ERROR] No user_uuid found in request context")
		return nil, fmt.Errorf("unauthorized: no user in context")
	}

	var user models.User
	// Preload Profile and Role so you can check role directly
	err := c.DB.Preload("Profile").Preload("Role").
		First(&user, "user_uuid = ? AND deleted_at IS NULL", userUUID).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Printf("[ERROR] User not found for UUID: %s\n", userUUID)
			return nil, fmt.Errorf("user not found")
		}
		log.Println("[ERROR] Database error while fetching user:", err)
		return nil, fmt.Errorf("database error")
	}

	log.Printf("[DEBUG] Authenticated user loaded: %s (ID: %d) | Role: %s\n",
		user.Username, user.UserID, user.Role.Name)

	return &user, nil
}
