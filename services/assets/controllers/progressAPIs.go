package controllers

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"web/services/assets/models"

	"gorm.io/datatypes"

	"github.com/gorilla/mux"
	"gorm.io/gorm"
)

// calculating performance
func calculateItemPerformance(status string) float64 {
	switch strings.ToLower(status) {
	case "completed":
		return 1.0
	case "in progress":
		return 0.5
	case "pending":
		return 0.0
	case "cancelled":
		return 0.0
	default:
		return 0.0
	}
}

// GetUintParam implementation

func (c *Construct) GetUintParam(r *http.Request, param string) (uint64, error) {
	vars := mux.Vars(r)
	valStr, ok := vars[param]
	if !ok {
		return 0, fmt.Errorf("missing URL param: %s", param)
	}
	val, err := strconv.ParseUint(valStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid uint param: %s", param)
	}
	return val, nil
}

// =======++++=======================================================”””””=============
//
//	Cohort Progress Creating APi
//
// =======++++=======================================================”””””=============

func (c *Construct) CreateCohortProgressEntity(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortID     uint64  `json:"cohort_id"`      // FK to cohort
		ProgressType string  `json:"progress_type"`  // optional: Milestone
		AssignedToID *uint64 `json:"assigned_to_id"` // optional supervisor/mentor
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}

	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, fmt.Sprintf("%v", err), nil)
		return
	}

	role := strings.ToLower(currentUser.Role.Name)
	if role != "opsadmin" && role != "supervisor" {
		c.Json(w, http.StatusForbidden, "Only OpsAdmins or Supervisors can create progress entities", nil)
		return
	}

	// --- Validate cohort exists ---
	var cohort models.Cohort
	if err := c.DB.First(&cohort, body.CohortID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Cohort not found", nil)
		return
	}

	// --- Check if progress entity for this cohort already exists ---
	var existing models.ProgressEntity
	if err := c.DB.
		Where("cohort_id = ? AND entity_type = ?", body.CohortID, "Cohort").
		First(&existing).Error; err == nil {
		c.Json(w, http.StatusConflict, "Progress entity already exists for this cohort", map[string]interface{}{
			"id":            existing.ID,
			"entity_name":   existing.EntityName,
			"entity_type":   existing.EntityType,
			"status":        existing.Status,
			"progress_type": existing.ProgressType,
			"cohort_id":     existing.CohortID,
		})
		return
	}

	// --- Create new progress entity ---
	if body.ProgressType == "" {
		body.ProgressType = "Milestone"
	}

	entity := models.ProgressEntity{
		CohortID:     &body.CohortID,
		EntityName:   cohort.Name,
		EntityType:   "Cohort",
		Status:       "Pending",
		ProgressType: body.ProgressType,
		AssignedToID: body.AssignedToID,
	}

	if err := c.DB.Create(&entity).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create progress entity", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Notify + Audit ---
	c.NotifyAndTrack(
		currentUser.UserID,
		"Progress Entity Created",
		fmt.Sprintf("%s created a new Cohort progress '%s'", currentUser.Username, cohort.Name),
		"Create",
		entity.EntityType,
		&entity.ID,
		entity.Status,
	)

	if body.AssignedToID != nil {
		c.NotifyAndTrack(
			*body.AssignedToID,
			"Assigned to Progress Entity",
			fmt.Sprintf("You have been assigned to manage Cohort '%s'", cohort.Name),
			"Assignment",
			entity.EntityType,
			&entity.ID,
			"Pending",
		)
	}

	c.Json(w, http.StatusCreated, "Progress entity created successfully", map[string]interface{}{
		"id":            entity.ID,
		"entity_name":   entity.EntityName,
		"entity_type":   entity.EntityType,
		"status":        entity.Status,
		"assigned_to":   body.AssignedToID,
		"created_by":    currentUser.Username,
		"progress_type": entity.ProgressType,
		"cohort_id":     body.CohortID,
	})
}

// =======++++=======================================================”””””=============
//
//	Cohort Items  Progress Creating APi
//
// =======++++=======================================================”””””=============
func (c *Construct) AddProgressItem(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EntityID     uint64     `json:"entity_id"`
		ParentID     *uint64    `json:"parent_id,omitempty"`
		PhaseName    string     `json:"phase_name"`
		ProgressType string     `json:"progress_type,omitempty"`
		Status       string     `json:"status"`
		Weight       *float64   `json:"weight,omitempty"`
		DueDate      *time.Time `json:"due_date,omitempty"`
		AssignedToID *uint64    `json:"assigned_to_id,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}

	if body.EntityID == 0 || body.PhaseName == "" || body.Status == "" {
		c.Json(w, http.StatusBadRequest, "entity_id, phase_name, and status are required", nil)
		return
	}

	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, fmt.Sprintf("%v", err), nil)
		return
	}

	role := strings.ToLower(currentUser.Role.Name)
	isStudent := role == "student"

	// --- Verify entity exists ---
	var entity models.ProgressEntity
	if err := c.DB.First(&entity, body.EntityID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Progress entity not found", nil)
		return
	}

	// --- Authorization ---
	if !isStudent && role != "opsadmin" && role != "supervisor" && role != "mentor" {
		c.Json(w, http.StatusForbidden, "You are not authorized to create progress items", nil)
		return
	}

	if isStudent {
		body.AssignedToID = &currentUser.UserID
	}

	if body.ProgressType == "" {
		body.ProgressType = "Task"
	}

	// --- Calculate initial performance ---
	performance := calculateItemPerformance(body.Status)

	// --- Weight calculation logic ---
	var weight float64
	if body.Weight != nil {
		weight = *body.Weight
	} else {
		var totalWeight float64
		var count int64

		c.DB.Model(&models.ProgressItem{}).
			Where("entity_id = ?", body.EntityID).
			Count(&count).
			Select("COALESCE(SUM(weight), 0)").Scan(&totalWeight)

		if count == 0 {
			weight = 1
		} else {
			remaining := math.Max(0, 1-totalWeight)
			weight = remaining
			if remaining == 0 {
				weight = 1 / float64(count+1)
				var items []models.ProgressItem
				if err := c.DB.Where("entity_id = ?", body.EntityID).Find(&items).Error; err == nil {
					for _, it := range items {
						it.Weight = 1 / float64(count+1)
						c.DB.Save(&it)
					}
				}
			}
		}
	}

	// --- Create item ---
	item := models.ProgressItem{
		EntityID:     body.EntityID,
		ParentID:     body.ParentID,
		PhaseName:    body.PhaseName,
		ProgressType: body.ProgressType,
		Status:       body.Status,
		Weight:       weight,
		DueDate:      body.DueDate,
		AssignedToID: body.AssignedToID,
		Performance:  performance,
		CreatedByID:  currentUser.UserID,
	}

	if err := c.DB.Create(&item).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create progress item", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- FULL entity recalculation ---
	if err := c.RecalculateEntityPerformance(body.EntityID); err != nil {
		fmt.Println("Warning: entity performance recalculation failed:", err)
	}

	// --- Notifications and audit ---
	c.NotifyAndTrack(
		currentUser.UserID,
		"Progress Item Created",
		fmt.Sprintf("%s added '%s' under %s '%s'", currentUser.Username, item.PhaseName, entity.EntityType, entity.EntityName),
		"Create",
		entity.EntityType,
		&item.ID,
		item.Status,
	)

	if body.AssignedToID != nil && *body.AssignedToID != currentUser.UserID {
		c.NotifyAndTrack(
			*body.AssignedToID,
			"Assigned to Progress Item",
			fmt.Sprintf("You have been assigned to '%s' under %s '%s'", item.PhaseName, entity.EntityType, entity.EntityName),
			"Assignment",
			entity.EntityType,
			&item.ID,
			item.Status,
		)
	}

	// --- Response ---
	c.Json(w, http.StatusCreated, "Progress item created successfully", map[string]interface{}{
		"data": map[string]interface{}{
			"id":            item.ID,
			"phase_name":    item.PhaseName,
			"status":        item.Status,
			"weight":        item.Weight,
			"entity_id":     entity.ID,
			"entity_name":   entity.EntityName,
			"entity_type":   entity.EntityType,
			"assigned_to":   body.AssignedToID,
			"created_by":    currentUser.Username,
			"progress_type": item.ProgressType,
			"performance":   item.Performance,
		},
	})
}

// =======++++=======================================================”””””=============
//
//	Cohort Progress Updating APi
//
// =======++++=======================================================”””””=============

func (c *Construct) UpdateProgressEntity(w http.ResponseWriter, r *http.Request) {
	id, err := c.GetUintParam(r, "id")
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid entity ID", nil)
		return
	}

	var body struct {
		EntityName   *string  `json:"entity_name,omitempty"`
		Status       *string  `json:"status,omitempty"`
		Performance  *float64 `json:"performance,omitempty"`
		ProgressType *string  `json:"progress_type,omitempty"`
		IsArchived   *bool    `json:"is_archived,omitempty"`
		CohortID     *uint64  `json:"cohort_id,omitempty"`
		AssignedToID *uint64  `json:"assigned_to_id,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}

	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, fmt.Sprintf("%v", err), nil)
		return
	}

	role := strings.ToLower(currentUser.Role.Name)
	if role != "opsadmin" && role != "supervisor" {
		c.Json(w, http.StatusForbidden, "Only OpsAdmins or Supervisors can update progress entities", nil)
		return
	}

	var entity models.ProgressEntity
	if err := c.DB.Preload("AssignedTo.Profile").First(&entity, id).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Progress entity not found", nil)
		return
	}

	if entity.IsArchived {
		c.Json(w, http.StatusConflict, "Cannot update archived progress entity", nil)
		return
	}

	if body.CohortID != nil {
		var existing models.ProgressEntity
		if err := c.DB.
			Where("cohort_id = ? AND entity_type = ? AND id <> ?", *body.CohortID, entity.EntityType, id).
			First(&existing).Error; err == nil {
			c.Json(w, http.StatusConflict, "Another progress entity already exists for this cohort", map[string]interface{}{
				"existing_id": existing.ID,
				"entity_name": existing.EntityName,
			})
			return
		}
	}

	// Apply updates
	if body.EntityName != nil {
		entity.EntityName = *body.EntityName
	}
	if body.Status != nil {
		entity.Status = *body.Status
	}
	if body.Performance != nil {
		entity.Performance = *body.Performance
	}
	if body.ProgressType != nil {
		entity.ProgressType = *body.ProgressType
	}
	if body.IsArchived != nil {
		entity.IsArchived = *body.IsArchived
	}
	if body.CohortID != nil {
		entity.CohortID = body.CohortID
	}
	if body.AssignedToID != nil {
		entity.AssignedToID = body.AssignedToID
	}

	if err := c.DB.Save(&entity).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update progress entity", map[string]interface{}{"error": err.Error()})
		return
	}

	// Reload with assignment details
	if err := c.DB.Preload("AssignedTo.Profile").First(&entity, id).Error; err != nil {
		log.Println("[WARN] Failed to reload entity with assignment:", err)
	}

	// Notify assigned user
	if body.AssignedToID != nil {
		c.NotifyAndTrack(
			*body.AssignedToID,
			"Assigned to Progress Entity",
			fmt.Sprintf("You have been assigned to manage progress entity '%s'", entity.EntityName),
			"Assignment",
			entity.EntityType,
			&entity.ID,
			entity.Status,
		)
	}

	// Build assigned user response
	assignedTo := map[string]interface{}{}
	if entity.AssignedTo != nil && entity.AssignedTo.UserID != 0 {
		assignedTo = map[string]interface{}{
			"id":         entity.AssignedTo.UserID,
			"first_name": entity.AssignedTo.Profile.FirstName,
			"last_name":  entity.AssignedTo.Profile.LastName,
			"email":      entity.AssignedTo.Email,
		}
	}

	c.Json(w, http.StatusOK, "Progress entity updated successfully", map[string]interface{}{
		"id":            entity.ID,
		"entity_name":   entity.EntityName,
		"entity_type":   entity.EntityType,
		"status":        entity.Status,
		"performance":   entity.Performance,
		"progress_type": entity.ProgressType,
		"is_archived":   entity.IsArchived,
		"cohort_id":     entity.CohortID,
		"assigned_to":   assignedTo,
	})
}

// =======++++=======================================================”””””=============
//	Cohort Items Progress Updating APi
// =======++++=======================================================”””””=============

// UpdateProgressItem handles updates to progress items by authorized users (OpsAdmin, Supervisor, Mentor, or Student)
func (c *Construct) UpdateProgressItem(w http.ResponseWriter, r *http.Request) {
	id, err := c.GetUintParam(r, "id")
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid item ID", nil)
		return
	}

	var body struct {
		PhaseName    *string    `json:"phase_name,omitempty"`
		Status       *string    `json:"status,omitempty"`
		Weight       *float64   `json:"weight,omitempty"`
		Performance  *float64   `json:"performance,omitempty"`
		DueDate      *time.Time `json:"due_date,omitempty"`
		CompletedAt  *time.Time `json:"completed_at,omitempty"`
		AssignedToID *uint64    `json:"assigned_to_id,omitempty"`
		IsArchived   *bool      `json:"is_archived,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Auth & Role Check ---
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, fmt.Sprintf("%v", err), nil)
		return
	}
	role := strings.ToLower(currentUser.Role.Name)

	// --- Fetch Item with Entity ---
	var item models.ProgressItem
	if err := c.DB.Preload("Entity").Preload("AssignedTo").First(&item, id).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Progress item not found", nil)
		return
	}

	// --- Role-Based Permission Rules ---
	switch role {
	case "opsadmin", "supervisor", "mentor":
		// Full permission
	case "student":
		if item.CreatedByID != currentUser.UserID && (item.AssignedToID == nil || *item.AssignedToID != currentUser.UserID) {
			c.Json(w, http.StatusForbidden, "You can only update your own assigned or created items", nil)
			return
		}
	default:
		c.Json(w, http.StatusForbidden, "Unauthorized role", nil)
		return
	}

	// --- Apply Updates ---
	if body.PhaseName != nil {
		item.PhaseName = *body.PhaseName
	}
	if body.Status != nil {
		item.Status = *body.Status
	}
	if body.Weight != nil {
		item.Weight = *body.Weight
	}
	if body.Performance != nil {
		item.Performance = *body.Performance
	}
	if body.DueDate != nil {
		item.DueDate = body.DueDate
	}
	if body.CompletedAt != nil {
		item.CompletedAt = body.CompletedAt
	}
	if body.AssignedToID != nil {
		item.AssignedToID = body.AssignedToID
	}
	if body.IsArchived != nil {
		item.IsArchived = *body.IsArchived
	}

	// --- Auto performance recalculation ---
	if body.Status != nil && body.Performance == nil {
		item.Performance = calculateItemPerformance(item.Status)
	}

	// --- Save item ---
	if err := c.DB.Save(&item).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update progress item", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Full entity recalculation (weights + performance + status) ---
	if err := c.RecalculateEntityPerformance(item.EntityID); err != nil {
		fmt.Println("Warning: entity performance recalculation failed:", err)
	}

	// --- Notify & Audit ---
	if body.AssignedToID != nil {
		c.NotifyAndTrack(
			*body.AssignedToID,
			"Progress Item Assignment",
			fmt.Sprintf("You have been assigned to progress item '%s' under %s '%s'",
				item.PhaseName, item.Entity.EntityType, item.Entity.EntityName),
			"Assignment",
			item.Entity.EntityType,
			&item.EntityID,
			item.Status,
		)
	}

	c.NotifyAndTrack(
		currentUser.UserID,
		"Progress Item Updated",
		fmt.Sprintf("%s updated progress item '%s' (status: %s)",
			currentUser.Username, item.PhaseName, item.Status),
		"Update",
		item.Entity.EntityType,
		&item.EntityID,
		item.Status,
	)

	c.Json(w, http.StatusOK, "Progress item updated successfully", map[string]interface{}{
		"data": item,
	})
}

// --- Recalculate the entity's performance and status ---
func (c *Construct) RecalculateEntityPerformance(entityID uint64) error {
	if err := c.NormalizeEntityWeights(entityID); err != nil {
		fmt.Println("Warning: normalization failed:", err)
	}
	return c.UpdateEntityWeightedPerformance(entityID)
}

//=======++++=======================================================''''''''''=============
//                      Soft Deletes for Progress Items
//=======++++=======================================================''''''''''=============

// ManageProgressItems allows OpsAdmins and Supervisors to archive, unarchive, or delete progress items in bulk
func (c *Construct) ManageProgressItems(w http.ResponseWriter, r *http.Request) {
	// --- Parse input ---
	var input struct {
		ItemIDs []uint64 `json:"item_ids"`
		Action  string   `json:"action"` // "archive", "unarchive", "delete"
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if len(input.ItemIDs) == 0 || input.Action == "" {
		c.Json(w, http.StatusBadRequest, "item_ids and action are required", nil)
		return
	}

	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	role := strings.ToLower(currentUser.Role.Name)

	// --- Fetch items ---
	var items []models.ProgressItem
	if err := c.DB.Preload("Entity").Where("id IN ?", input.ItemIDs).Find(&items).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch progress items", map[string]interface{}{"error": err.Error()})
		return
	}

	if len(items) == 0 {
		c.Json(w, http.StatusNotFound, "No progress items found for the given IDs", nil)
		return
	}

	for _, item := range items {
		switch input.Action {
		case "archive":
			if role != "opsadmin" && role != "supervisor" {
				c.Json(w, http.StatusForbidden, "Only supervisors or admins can archive progress items", nil)
				return
			}
			item.IsArchived = true
			item.UpdatedAt = time.Now()
			c.DB.Save(&item)
			c.NotifyAndTrack(currentUser.UserID, "Progress Item Archived",
				fmt.Sprintf("Progress item '%s' in %s '%s' was archived", item.PhaseName, item.Entity.EntityType, item.Entity.EntityName),
				"Archive",
				item.Entity.EntityType,
				&item.ID,
				item.Status,
			)

		case "unarchive":
			if role != "opsadmin" && role != "supervisor" {
				c.Json(w, http.StatusForbidden, "Only supervisors or admins can unarchive progress items", nil)
				return
			}
			item.IsArchived = false
			item.UpdatedAt = time.Now()
			c.DB.Save(&item)
			c.NotifyAndTrack(currentUser.UserID, "Progress Item Unarchived",
				fmt.Sprintf("Progress item '%s' in %s '%s' was unarchived", item.PhaseName, item.Entity.EntityType, item.Entity.EntityName),
				"Unarchive",
				item.Entity.EntityType,
				&item.ID,
				item.Status,
			)

		case "delete":
			if role == "student" {
				// Students can only "soft delete" their own items
				if item.CreatedByID != currentUser.UserID && (item.AssignedToID == nil || *item.AssignedToID != currentUser.UserID) {
					c.Json(w, http.StatusForbidden, "You can only delete your own assigned or created items", nil)
					return
				}
				item.IsArchived = true
				item.UpdatedAt = time.Now()
				c.DB.Save(&item)
				c.NotifyAndTrack(currentUser.UserID, "Progress Item Archived",
					fmt.Sprintf("You archived your progress item '%s'", item.PhaseName),
					"Archive",
					item.Entity.EntityType,
					&item.ID,
					item.Status,
				)
			} else if role == "opsadmin" || role == "supervisor" {
				c.DB.Unscoped().Delete(&item)
				c.NotifyAndTrack(currentUser.UserID, "Progress Item Deleted Permanently",
					fmt.Sprintf("Progress item '%s' in %s '%s' was permanently deleted", item.PhaseName, item.Entity.EntityType, item.Entity.EntityName),
					"Deletion",
					item.Entity.EntityType,
					&item.ID,
					item.Status,
				)
			} else {
				c.Json(w, http.StatusForbidden, "You do not have permission to delete this progress item", nil)
				return
			}

		default:
			c.Json(w, http.StatusBadRequest, "Invalid action, must be 'archive', 'unarchive', or 'delete'", nil)
			return
		}

		// --- Recalculate entity performance/status after each change ---
		if err := c.RecalculateEntityPerformance(item.EntityID); err != nil {
			fmt.Println("Warning: entity recalculation failed:", err)
		}
	}

	c.Json(w, http.StatusOK, fmt.Sprintf("Action '%s' performed on selected progress items successfully", input.Action), nil)
}

//=======++++=======================================================''''''''''=============
//                      Soft Deletes for Enties Admins Only
//=======++++=======================================================''''''''''=============

// ManageProgressEntities allows OpsAdmins to archive, unarchive, or delete entire progress entities
func (c *Construct) ManageProgressEntities(w http.ResponseWriter, r *http.Request) {
	// --- Parse input ---
	var input struct {
		EntityIDs []uint64 `json:"entity_ids"`
		Action    string   `json:"action"` // "archive", "unarchive", "delete"
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if len(input.EntityIDs) == 0 || input.Action == "" {
		c.Json(w, http.StatusBadRequest, "entity_ids and action are required", nil)
		return
	}

	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	role := strings.ToLower(currentUser.Role.Name)
	if role != "opsadmin" {
		c.Json(w, http.StatusForbidden, "Only OpsAdmins can manage progress entities", nil)
		return
	}

	// --- Fetch entities ---
	var entities []models.ProgressEntity
	if err := c.DB.Preload("Items").Where("id IN ?", input.EntityIDs).Find(&entities).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch progress entities", map[string]interface{}{"error": err.Error()})
		return
	}

	if len(entities) == 0 {
		c.Json(w, http.StatusNotFound, "No progress entities found for the given IDs", nil)
		return
	}

	for _, entity := range entities {
		switch input.Action {
		case "archive":
			entity.IsArchived = true
			entity.UpdatedAt = time.Now()
			c.DB.Save(&entity)
			c.NotifyAndTrack(currentUser.UserID, "Progress Entity Archived",
				fmt.Sprintf("Progress entity '%s' was archived", entity.EntityName),
				"Archive",
				entity.EntityType,
				&entity.ID,
				entity.Status,
			)

			// Cascade: archive all items under this entity
			for _, item := range entity.Items {
				item.IsArchived = true
				c.DB.Save(&item)
			}

		case "unarchive":
			entity.IsArchived = false
			entity.UpdatedAt = time.Now()
			c.DB.Save(&entity)
			c.NotifyAndTrack(currentUser.UserID, "Progress Entity Unarchived",
				fmt.Sprintf("Progress entity '%s' was unarchived", entity.EntityName),
				"Unarchive",
				entity.EntityType,
				&entity.ID,
				entity.Status,
			)

			// Cascade: unarchive all items
			for _, item := range entity.Items {
				item.IsArchived = false
				c.DB.Save(&item)
			}

		case "delete":
			// Permanent delete
			c.DB.Unscoped().Delete(&entity)
			c.NotifyAndTrack(currentUser.UserID, "Progress Entity Deleted",
				fmt.Sprintf("Progress entity '%s' was permanently deleted", entity.EntityName),
				"Deletion",
				entity.EntityType,
				&entity.ID,
				entity.Status,
			)
			// Cascade: delete all items
			for _, item := range entity.Items {
				c.DB.Unscoped().Delete(&item)
			}

		default:
			c.Json(w, http.StatusBadRequest, "Invalid action, must be 'archive', 'unarchive', or 'delete'", nil)
			return
		}
	}

	c.Json(w, http.StatusOK, fmt.Sprintf("Action '%s' performed on selected progress entities successfully", input.Action), nil)
}

//=======++++=======================================================''''''''''=============
//                       Retrieving Cohort Progress API DATA
//=======++++=======================================================''''''''''=============

// GET /progress/entities/{id}?page=1&limit=20&report_status=Pending&week_start=2025-10-01&week_end=2025-10-07
func (c *Construct) GetProgressEntities(w http.ResponseWriter, r *http.Request) {
	// --- Auth ---
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, fmt.Sprintf("%v", err), nil)
		return
	}
	role := strings.ToLower(currentUser.Role.Name)

	// --- Query Parameters ---
	entityID, _ := c.GetUintParam(r, "id")
	var cohortID uint64
	if cohortStr := r.URL.Query().Get("cohort_id"); cohortStr != "" {
		fmt.Sscan(cohortStr, &cohortID)
	}

	page, limit := 1, 20
	fmt.Sscan(r.URL.Query().Get("page"), &page)
	fmt.Sscan(r.URL.Query().Get("limit"), &limit)
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	reportStatus := r.URL.Query().Get("report_status")
	var weekStart, weekEnd time.Time
	if ws := r.URL.Query().Get("week_start"); ws != "" {
		weekStart, _ = time.Parse("2006-01-02", ws)
	}
	if we := r.URL.Query().Get("week_end"); we != "" {
		weekEnd, _ = time.Parse("2006-01-02", we)
	}

	// --- Base Query ---
	query := c.DB.Model(&models.ProgressEntity{}).
		Preload("Cohort").
		Preload("Items.AssignedTo.Profile").
		Preload("Items.CreatedBy.Profile").
		Preload("Items.WeeklyReports", func(db *gorm.DB) *gorm.DB {
			if reportStatus != "" {
				db = db.Where("status = ?", reportStatus)
			}
			if !weekStart.IsZero() {
				db = db.Where("week_start >= ?", weekStart)
			}
			if !weekEnd.IsZero() {
				db = db.Where("week_end <= ?", weekEnd)
			}
			return db.Preload("ProgressItems") // ✅ preload linked tasks
		})
	// --- Role-based Filtering ---
	switch role {
	case "opsadmin", "systemadmin":
		if entityID > 0 {
			query = query.Where("id = ?", entityID)
		} else if cohortID > 0 {
			query = query.Where("cohort_id = ?", cohortID)
		}

	case "supervisor", "mentor":
		cohortIDs := c.getAssignedCohorts(currentUser.UserID, strings.Title(role))
		if len(cohortIDs) == 0 {
			c.Json(w, http.StatusOK, "No cohorts found", map[string]interface{}{"entities": []interface{}{}})
			return
		}
		query = query.Where("cohort_id IN ?", cohortIDs)

	case "student":
		cohortIDs := c.getAssignedCohorts(currentUser.UserID, "Student")
		if len(cohortIDs) == 0 {
			c.Json(w, http.StatusOK, "No cohorts found", map[string]interface{}{"entities": []interface{}{}})
			return
		}
		query = query.Where("cohort_id IN ?", cohortIDs).
			Preload("Items", "created_by_id = ?", currentUser.UserID)

	default:
		c.Json(w, http.StatusForbidden, "Unauthorized role", nil)
		return
	}

	// --- Fetch Entities ---
	var entities []models.ProgressEntity
	if err := query.Limit(limit).Offset(offset).Find(&entities).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch entities", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Build Response ---
	resp := make([]map[string]interface{}, 0, len(entities))
	for _, e := range entities {
		entityMap := map[string]interface{}{
			"id":            e.ID,
			"entity_name":   e.EntityName,
			"entity_type":   e.EntityType,
			"status":        e.Status,
			"performance":   e.Performance,
			"progress_type": e.ProgressType,
			"is_archived":   e.IsArchived,
			"cohort_id":     e.CohortID,
		}

		// --- Items ---
		itemsResp := make([]map[string]interface{}, 0, len(e.Items))
		for _, item := range e.Items {
			assignedTo := map[string]interface{}{}
			if item.AssignedTo != nil {
				assignedTo = map[string]interface{}{
					"id":         item.AssignedTo.UserID,
					"first_name": item.AssignedTo.Profile.FirstName,
					"last_name":  item.AssignedTo.Profile.LastName,
					"email":      item.AssignedTo.Email,
				}
			}

			createdBy := map[string]interface{}{}
			if item.CreatedBy != nil {
				createdBy = map[string]interface{}{
					"id":         item.CreatedBy.UserID,
					"first_name": item.CreatedBy.Profile.FirstName,
					"last_name":  item.CreatedBy.Profile.LastName,
					"email":      item.CreatedBy.Email,
				}
			}
			reportsResp := make([]map[string]interface{}, 0)
			for _, r := range item.WeeklyReports {
				// collect linked progress item IDs
				linkedItemIDs := make([]uint64, 0)
				for _, p := range r.ProgressItems {
					linkedItemIDs = append(linkedItemIDs, p.ID)
				}

				reportsResp = append(reportsResp, map[string]interface{}{
					"id":           r.ID,
					"student_id":   r.StudentID,
					"status":       r.Status,
					"week_start":   r.WeekStart,
					"week_end":     r.WeekEnd,
					"linked_items": linkedItemIDs, // changed to a slice of IDs
				})
			}

			itemsResp = append(itemsResp, map[string]interface{}{
				"id":             item.ID,
				"phase_name":     item.PhaseName,
				"status":         item.Status,
				"weight":         item.Weight,
				"performance":    item.Performance,
				"progress_type":  item.ProgressType,
				"is_archived":    item.IsArchived,
				"assigned_to":    assignedTo,
				"created_by":     createdBy,
				"weekly_reports": reportsResp,
			})
		}

		entityMap["items"] = itemsResp
		resp = append(resp, entityMap)
	}

	// --- AUDIT + LOG ACCESS ---
	go func() {
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.RemoteAddr
		}
		ua := r.UserAgent()

		message := fmt.Sprintf(
			"%s (%s) viewed progress entities (page: %d, limit: %d, cohort_id: %d, entity_id: %d)",
			currentUser.Username, role, page, limit, cohortID, entityID,
		)

		meta := fmt.Sprintf(`{"ip":"%s","user_agent":"%s"}`, ip, ua)
		c.NotifyAndTrack(
			currentUser.UserID,
			"Progress Entities Viewed",
			message,
			"view",
			"ProgressEntity",
			nil,
			"success",
		)

		// Optional deeper audit with metadata
		entityName := "ProgressEntity"
		audit := &models.AuditLog{
			UserID:    currentUser.UserID,
			Action:    "view",
			Entity:    &entityName,
			EntityID:  nil,
			Metadata:  datatypes.JSON([]byte(meta)),
			CreatedAt: time.Now(),
		}
		_ = c.DB.Create(audit).Error

	}()

	// --- Response ---
	c.Json(w, http.StatusOK, "Fetched progress entities successfully", map[string]interface{}{
		"page":     page,
		"limit":    limit,
		"count":    len(resp),
		"entities": resp,
	})
}
func (c *Construct) getAssignedCohorts(userID uint64, role string) []uint64 {
	var cohortIDs []uint64

	err := c.DB.Model(&models.CohortUser{}).
		Where("user_user_id = ? AND role = ?", userID, role).
		Pluck("cohort_cohort_id", &cohortIDs).Error
	if err != nil {
		log.Printf("[ERROR] Failed to fetch cohorts for %s %d: %v\n", role, userID, err)
		return []uint64{}
	}

	return cohortIDs
}
