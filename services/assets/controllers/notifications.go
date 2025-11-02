package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"web/services/assets/middlewares"
	"web/services/assets/models"

	"github.com/gorilla/mux"
)

// api to fetch notification
func (c *Construct) GetNotifications(w http.ResponseWriter, r *http.Request) {
	// Get the authenticated user UUID from context
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// Fetch user_id from DB using user_uuid
	var user models.User
	if err := c.DB.Select("user_id").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "User not found", nil)
		return
	}

	var notifications []models.Notification

	// Fetch last 50 notifications for this user
	if err := c.DB.Where("user_id = ?", user.UserID).
		Order("created_at DESC").
		Limit(50).
		Find(&notifications).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch notifications", map[string]interface{}{"error": err.Error()})
		return
	}

	c.Json(w, http.StatusOK, "Notifications retrieved successfully", map[string]interface{}{
		"notifications": notifications,
	})
}

// mark as read api notification
func (c *Construct) MarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NotificationIDs []uint64 `json:"notification_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}
	if len(body.NotificationIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "No notification IDs provided", nil)
		return
	}

	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	result := c.DB.Model(&models.Notification{}).
		Where("id IN ? AND user_id = ?", body.NotificationIDs, user.UserID).
		Update("is_read", true)
	if result.Error != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to mark notifications as read", map[string]interface{}{"error": result.Error.Error()})
		return
	}

	c.Json(w, http.StatusOK, fmt.Sprintf("%d notifications marked as read", result.RowsAffected), nil)
}

// mark all as read
func (c *Construct) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	result := c.DB.Model(&models.Notification{}).
		Where("user_id = ? AND is_read = false", user.UserID).
		Update("is_read", true)

	if result.Error != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to mark all as read", map[string]interface{}{"error": result.Error.Error()})
		return
	}

	c.Json(w, http.StatusOK, fmt.Sprintf("%d notifications marked as read", result.RowsAffected), nil)
}

// DeleteNotification allows a user to delete one or multiple notifications via JSON
func (c *Construct) DeleteNotification(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	var body struct {
		NotificationIDs []uint64 `json:"notification_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	if len(body.NotificationIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "No notification IDs provided", nil)
		return
	}

	// Get user_uuid from context
	userUUIDCtx := r.Context().Value("user_uuid")
	if userUUIDCtx == nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	userUUID, ok := userUUIDCtx.(string)
	if !ok {
		c.Json(w, http.StatusInternalServerError, "Invalid user context", nil)
		return
	}

	// Fetch numeric user ID
	var user models.User
	if err := c.DB.Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	// Delete notifications belonging to the user
	result := c.DB.Where("id IN ? AND user_id = ?", body.NotificationIDs, user.UserID).Delete(&models.Notification{})
	if result.Error != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to delete notifications", map[string]interface{}{"error": result.Error.Error()})
		return
	}

	if result.RowsAffected == 0 {
		c.Json(w, http.StatusNotFound, "Notifications not found", nil)
		return
	}

	c.Json(w, http.StatusOK, fmt.Sprintf("%d notifications deleted successfully", result.RowsAffected), nil)
}

// AdminDeleteNotification allows an admin to delete any notification
func (c *Construct) AdminDeleteNotification(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	notifIDStr := vars["id"]
	notifID, err := strconv.ParseUint(notifIDStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid notification ID", nil)
		return
	}

	result := c.DB.Where("id = ?", notifID).Delete(&models.Notification{})
	if result.Error != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to delete notification", map[string]interface{}{"error": result.Error.Error()})
		return
	}

	if result.RowsAffected == 0 {
		c.Json(w, http.StatusNotFound, "Notification not found", nil)
		return
	}

	c.Json(w, http.StatusOK, "Notification deleted successfully", nil)
}

// creating a new notification
type CreateNotificationInput struct {
	UserID  uint64 `json:"user_id"` // the recipient
	Title   string `json:"title"`
	Message string `json:"message"`
}

func (c *Construct) CreateNotificationHandler(w http.ResponseWriter, r *http.Request) {
	var input CreateNotificationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request", map[string]interface{}{"error": err.Error()})
		return
	}

	notification := models.Notification{
		UserID:  input.UserID,
		Title:   input.Title,
		Message: input.Message,
		IsRead:  false,
	}

	if err := c.DB.Create(&notification).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create notification", map[string]interface{}{"error": err.Error()})
		return
	}

	c.Json(w, http.StatusCreated, "Notification created successfully", map[string]interface{}{"notification": notification})
}
