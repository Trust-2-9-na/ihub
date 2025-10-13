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

	"reflect"

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

	// --- Fetch full student profiles from cohort ---
	var students []models.User
	err = c.DB.Preload("Profile").
		Joins("JOIN cohort_users ON cohort_users.user_user_id = users.users_id").
		Where("cohort_users.cohort_cohort_id = ? AND cohort_users.role = ?", body.CohortID, "Student").
		Find(&students).Error
	if err != nil {
		log.Printf("[ERROR] Failed to fetch student profiles for cohort %d: %v\n", body.CohortID, err)
		students = []models.User{}
	}

	// --- Build student metadata ---
	studentMeta := make([]map[string]interface{}, 0)
	for _, s := range students {
		studentMeta = append(studentMeta, map[string]interface{}{
			"id":         s.UserID,
			"first_name": s.Profile.FirstName,
			"last_name":  s.Profile.LastName,
			"email":      s.Email,
		})
	}
	metadata := map[string]interface{}{
		"students": studentMeta,
	}
	metadataJSON, _ := json.Marshal(metadata)

	// --- Create new progress entity ---
	if body.ProgressType == "" {
		body.ProgressType = "Milestone"
	}

	str := string(metadataJSON)

	entity := models.ProgressEntity{
		CohortID:     &body.CohortID,
		EntityName:   cohort.Name,
		EntityType:   "Cohort",
		Status:       "Pending",
		ProgressType: body.ProgressType,
		AssignedToID: body.AssignedToID,
		Metadata:     &str, // ✅ assign as *string
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
		"students":      studentMeta,
	})
}

//=======++++=======================================================''''''''''=============
//                        Cohort Items  Progress Creating APi
//=======++++=======================================================''''''''''=============

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
	if role != "opsadmin" && role != "supervisor" && role != "mentor" {
		c.Json(w, http.StatusForbidden, "Only OpsAdmins, Supervisors, or Mentors can add progress items", nil)
		return
	}

	// Ensure entity exists
	var entity models.ProgressEntity
	if err := c.DB.First(&entity, body.EntityID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Progress entity not found", nil)
		return
	}

	// Default progress type
	if body.ProgressType == "" {
		body.ProgressType = "Task"
	}

	// Calculate initial performance from status
	performance := calculateItemPerformance(body.Status)

	// Determine weight logic
	var weight float64
	if body.Weight != nil {
		weight = *body.Weight
	} else {
		// Auto weight distribution
		var totalWeight float64
		var count int64
		c.DB.Model(&models.ProgressItem{}).
			Where("entity_id = ?", body.EntityID).
			Count(&count).
			Select("COALESCE(SUM(weight), 0)").Scan(&totalWeight)

		// If no existing items, default to 1
		if count == 0 {
			weight = 1
		} else {
			// New item shares the remaining weight proportionally
			remaining := math.Max(0, 1-totalWeight)
			weight = remaining
			if remaining == 0 {
				// Rebalance all items equally
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
	}

	if err := c.DB.Create(&item).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create progress item", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Update the entity's weighted performance ---
	if err := c.UpdateEntityWeightedPerformance(body.EntityID); err != nil {
		fmt.Println("Warning: entity performance update failed:", err)
	}

	// --- Notify and Audit ---
	c.NotifyAndTrack(
		currentUser.UserID,
		"Progress Item Created",
		fmt.Sprintf("%s added '%s' under %s '%s'", currentUser.Username, item.PhaseName, entity.EntityType, entity.EntityName),
		"Create",
		entity.EntityType,
		&item.ID,
		item.Status,
	)

	if body.AssignedToID != nil {
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
		EntityName   *string                 `json:"entity_name,omitempty"`
		Status       *string                 `json:"status,omitempty"`
		Performance  *float64                `json:"performance,omitempty"`
		ProgressType *string                 `json:"progress_type,omitempty"`
		IsArchived   *bool                   `json:"is_archived,omitempty"`
		CohortID     *uint64                 `json:"cohort_id,omitempty"`
		AssignedToID *uint64                 `json:"assigned_to_id,omitempty"`
		Metadata     *map[string]interface{} `json:"metadata,omitempty"` // 👈 New field
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
	if body.Metadata != nil {
		raw, err := json.Marshal(body.Metadata)
		if err == nil {
			str := string(raw)
			entity.Metadata = &str
		}
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
		"metadata":      body.Metadata, // 👈 Echo back updated metadata
	})
}

// =======++++=======================================================”””””=============
//	Cohort Items Progress Updating APi
// =======++++=======================================================”””””=============

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
	if role != "opsadmin" && role != "supervisor" && role != "mentor" {
		c.Json(w, http.StatusForbidden, "Only OpsAdmins, Supervisors, or Mentors can update progress items", nil)
		return
	}

	// --- Fetch Item with Entity ---
	var item models.ProgressItem
	if err := c.DB.Preload("Entity").First(&item, id).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Progress item not found", nil)
		return
	}

	// --- Update Fields ---
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

	// --- Save Item ---
	if err := c.DB.Save(&item).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update progress item", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Normalize weights if changed ---
	if body.Weight != nil {
		if err := c.NormalizeEntityWeights(item.EntityID); err != nil {
			fmt.Println("Warning: weight normalization failed:", err)
		}
	}

	// --- Update Entity Weighted Performance ---
	if err := c.UpdateEntityWeightedPerformance(item.EntityID); err != nil {
		fmt.Println("Warning: entity performance update failed:", err)
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

//=======++++=======================================================''''''''''=============
//                        Cohort Progress Archiving APi
//=======++++=======================================================''''''''''=============

func (c *Construct) ToggleArchiveProgressEntity(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EntityID   uint64 `json:"entity_id"`
		IsArchived bool   `json:"is_archived"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": fmt.Sprintf("%v", err)})
		return
	}

	if body.EntityID == 0 {
		c.Json(w, http.StatusBadRequest, "entity_id is required", nil)
		return
	}

	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, fmt.Sprintf("%v", err), nil)
		return
	}

	var entity models.ProgressEntity
	if err := c.DB.First(&entity, body.EntityID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Progress entity not found", nil)
		return
	}

	entity.IsArchived = body.IsArchived
	if err := c.DB.Save(&entity).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update archive status", map[string]interface{}{"error": err.Error()})
		return
	}

	action := "Archived"
	if !body.IsArchived {
		action = "Unarchived"
	}

	// 🔹 Log system history and notifications
	message := fmt.Sprintf("%s %s progress entity '%s'", currentUser.Username, action, entity.EntityName)
	c.NotifyAndTrack(currentUser.UserID, action+" Progress Entity", message, action, entity.EntityType, &entity.ID, "Completed")

	c.Json(w, http.StatusOK, fmt.Sprintf("Progress entity %s successfully", action), map[string]interface{}{
		"entity_id":   entity.ID,
		"entity_name": entity.EntityName,
		"is_archived": entity.IsArchived,
	})
}

//=======++++=======================================================''''''''''=============
//                        Cohort Progress Unarchiving APi
//=======++++=======================================================''''''''''=============

func (c *Construct) ToggleArchiveProgressItem(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ItemID     uint64 `json:"item_id"`
		IsArchived bool   `json:"is_archived"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": fmt.Sprintf("%v", err)})
		return
	}

	if body.ItemID == 0 {
		c.Json(w, http.StatusBadRequest, "item_id is required", nil)
		return
	}

	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, fmt.Sprintf("%v", err), nil)
		return
	}

	var item models.ProgressItem
	if err := c.DB.First(&item, body.ItemID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Progress item not found", nil)
		return
	}

	item.IsArchived = body.IsArchived
	if err := c.DB.Save(&item).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update archive status", map[string]interface{}{"error": err.Error()})
		return
	}

	action := "Archived"
	if !body.IsArchived {
		action = "Unarchived"
	}

	// 🔹 Log system history and notifications
	message := fmt.Sprintf("%s %s progress item '%s'", currentUser.Username, action, item.PhaseName)
	c.NotifyAndTrack(currentUser.UserID, action+" Progress Item", message, action, "ProgressItem", &item.ID, "Completed")

	c.Json(w, http.StatusOK, fmt.Sprintf("Progress item %s successfully", action), map[string]interface{}{
		"item_id":     item.ID,
		"phase_name":  item.PhaseName,
		"is_archived": item.IsArchived,
	})
}

//=======++++=======================================================''''''''''=============
//                       Retrieving Cohort Progress API DATA
//=======++++=======================================================''''''''''=============

// GET /progress/entities/{id}?page=1&limit=20&fields=id,entity_name,status&report_status=Pending&week_start=2025-10-01&week_end=2025-10-07
func (c *Construct) GetProgressEntities(w http.ResponseWriter, r *http.Request) {
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, fmt.Sprintf("%v", err), nil)
		return
	}
	role := strings.ToLower(currentUser.Role.Name)

	// Parse optional entityID and cohortID
	entityID, _ := c.GetUintParam(r, "id")
	var cohortID uint64
	if cohortStr := r.URL.Query().Get("cohort_id"); cohortStr != "" {
		fmt.Sscan(cohortStr, &cohortID)
	}

	// Pagination
	page, limit := 1, 20
	if p := r.URL.Query().Get("page"); p != "" {
		fmt.Sscan(p, &page)
		if page < 1 {
			page = 1
		}
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscan(l, &limit)
		if limit < 1 {
			limit = 20
		}
	}
	offset := (page - 1) * limit

	// Selective fields
	selectFields := "*"
	if f := r.URL.Query().Get("fields"); f != "" {
		fields := strings.Split(f, ",")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		selectFields = strings.Join(fields, ",")
	}

	// Optional report filters
	reportStatus := r.URL.Query().Get("report_status")
	var weekStart, weekEnd time.Time
	if ws := r.URL.Query().Get("week_start"); ws != "" {
		weekStart, _ = time.Parse("2006-01-02", ws)
	}
	if we := r.URL.Query().Get("week_end"); we != "" {
		weekEnd, _ = time.Parse("2006-01-02", we)
	}

	// Base query
	query := c.DB.Model(&models.ProgressEntity{}).
		Select(selectFields).
		Limit(limit).Offset(offset).
		Preload("Items.AssignedTo.Profile").
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
			return db
		}).
		Preload("Cohort")

	// Role-based filtering
	switch role {
	case "opsadmin", "systemadmin":
		if entityID > 0 {
			query = query.Where("id = ?", entityID)
		} else if cohortID > 0 {
			query = query.Where("cohort_id = ?", cohortID)
		}
	case "supervisor", "mentor", "student":
		cohortIDs := c.getAssignedCohorts(currentUser.UserID, strings.Title(role))
		query = query.Where("cohort_id IN ?", cohortIDs)
		if role == "student" {
			// Optional: filter only items linked to this student
			query = query.Where("metadata->>'student_id' = ?", fmt.Sprint(currentUser.UserID))
		}
	default:
		c.Json(w, http.StatusForbidden, "Unauthorized role", nil)
		return
	}

	// Execute query
	var entities []models.ProgressEntity
	if err := query.Find(&entities).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch entities", map[string]interface{}{"error": err.Error()})
		return
	}

	// Build response
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

		itemsResp := make([]map[string]interface{}, 0, len(e.Items))
		for _, item := range e.Items {
			assignedTo := map[string]interface{}{}
			if item.AssignedTo != nil && !reflect.DeepEqual(item.AssignedTo.Profile, models.UserProfile{}) {
				assignedTo = map[string]interface{}{
					"id":         item.AssignedTo.UserID,
					"first_name": item.AssignedTo.Profile.FirstName,
					"last_name":  item.AssignedTo.Profile.LastName,
					"email":      item.AssignedTo.Email,
				}
			}

			// Include weekly reports directly
			reportsResp := make([]map[string]interface{}, 0)
			for _, r := range item.WeeklyReports {
				reportsResp = append(reportsResp, map[string]interface{}{
					"id":          r.ID,
					"student_id":  r.StudentID,
					"status":      r.Status,
					"week_start":  r.WeekStart,
					"week_end":    r.WeekEnd,
					"linked_item": r.LinkedItemID,
				})
			}

			itemMap := map[string]interface{}{
				"id":             item.ID,
				"phase_name":     item.PhaseName,
				"status":         item.Status,
				"weight":         item.Weight,
				"performance":    item.Performance,
				"progress_type":  item.ProgressType,
				"is_archived":    item.IsArchived,
				"assigned_to":    assignedTo,
				"weekly_reports": reportsResp,
			}
			itemsResp = append(itemsResp, itemMap)
		}
		entityMap["items"] = itemsResp
		resp = append(resp, entityMap)
	}

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
