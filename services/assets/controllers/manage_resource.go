package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"web/services/assets/middlewares"
	"web/services/assets/models"
)

func (c *Construct) ManageResources(w http.ResponseWriter, r *http.Request) {
	// --- 1. Parse input ---
	var input struct {
		ResourceIDs []uint64 `json:"resource_ids"`
		Action      string   `json:"action"` // "archive", "unarchive", "delete"
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if len(input.ResourceIDs) == 0 || input.Action == "" {
		c.Json(w, http.StatusBadRequest, "resource_ids and action are required", nil)
		return
	}

	// --- 2. Authenticate user ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Role").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 3. Fetch resources ---
	var resources []models.Resource
	if err := c.DB.Where("id IN ?", input.ResourceIDs).Find(&resources).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch resources", map[string]interface{}{"error": err.Error()})
		return
	}

	if len(resources) == 0 {
		c.Json(w, http.StatusNotFound, "No resources found for the given IDs", nil)
		return
	}

	// --- 4. Perform actions ---
	var affected []map[string]interface{}
	for _, res := range resources {
		switch strings.ToLower(input.Action) {
		case "archive":
			if user.Role.Name != "SystemAdmin" && user.Role.Name != "OpsAdmin" {
				c.Json(w, http.StatusForbidden, "Only SystemAdmin and OpsAdmin can archive resources", nil)
				return
			}
			res.IsArchived = true
			res.UpdatedAt = time.Now()
			c.DB.Save(&res)
			c.NotifyAndTrack(user.UserID, "Resource Archived",
				fmt.Sprintf("Resource '%s' was archived", res.Title),
				"Resource Archive", "Resource", &res.ID, "Archived", false)

			affected = append(affected, map[string]interface{}{
				"id":    res.ID,
				"title": res.Title,
				"state": "Archived",
			})

		case "unarchive":
			if user.Role.Name != "SystemAdmin" && user.Role.Name != "OpsAdmin" {
				c.Json(w, http.StatusForbidden, "Only SystemAdmin and OpsAdmin can unarchive resources", nil)
				return
			}
			res.IsArchived = false
			res.UpdatedAt = time.Now()
			c.DB.Save(&res)
			c.NotifyAndTrack(user.UserID, "Resource Unarchived",
				fmt.Sprintf("Resource '%s' was unarchived", res.Title),
				"Resource Unarchive", "Resource", &res.ID, "Unarchived", false)

			affected = append(affected, map[string]interface{}{
				"id":    res.ID,
				"title": res.Title,
				"state": "Unarchived",
			})

		case "delete":
			if user.Role.Name != "SystemAdmin" && user.Role.Name != "OpsAdmin" {
				c.Json(w, http.StatusForbidden, "Only SystemAdmin and OpsAdmin can delete resources", nil)
				return
			}
			c.DB.Unscoped().Delete(&res)
			c.NotifyAndTrack(user.UserID, "Resource Deleted Permanently",
				fmt.Sprintf("Resource '%s' was permanently deleted", res.Title),
				"Resource Deletion", "Resource", &res.ID, "Deleted", true)

			affected = append(affected, map[string]interface{}{
				"id":    res.ID,
				"title": res.Title,
				"state": "Deleted",
			})

		default:
			c.Json(w, http.StatusBadRequest, "Invalid action, must be 'archive', 'unarchive', or 'delete'", nil)
			return
		}
	}

	// --- 5. Return structured response ---
	resp := map[string]interface{}{
		"action":   input.Action,
		"count":    len(affected),
		"affected": affected,
	}

	c.Json(w, http.StatusOK, fmt.Sprintf("Action '%s' performed successfully", input.Action), resp)
}
