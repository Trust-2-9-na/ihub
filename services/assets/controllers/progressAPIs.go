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

	"github.com/gorilla/mux"
)

// calculating performance
func calculateItemPerformance(studentStatus, verifiedStatus string) float64 {
	switch {
	case strings.EqualFold(verifiedStatus, "Verified"):
		return 1.0
	case strings.EqualFold(studentStatus, "Completed"):
		return 0.75
	case strings.EqualFold(studentStatus, "In Progress"):
		return 0.4
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
			"cohort_id":     existing.EntityCohortID,
		})
		return
	}

	// --- Create new progress entity ---
	if body.ProgressType == "" {
		body.ProgressType = "Milestone"
	}

	entity := models.ProgressEntity{
		EntityCohortID: &body.CohortID,
		EntityName:     cohort.Name,
		EntityType:     "Cohort",
		Status:         "Pending",
		ProgressType:   body.ProgressType,
		AssignedToID:   body.AssignedToID,
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
		true,
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
			true,
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

// GET Cohort Entities
func (c *Construct) GetCohortProgressEntities(w http.ResponseWriter, r *http.Request) {
	// --- Authenticate ---
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	role := strings.ToLower(user.Role.Name)

	// --- Base query ---
	query := c.DB.Model(&models.ProgressEntity{}).
		Preload("EntityCohort").
		Order("updated_at DESC")

	// --- Role-based filtering ---
	switch role {
	case "opsadmin", "systemadmin":
		// Can view everything — no filter applied
	case "supervisor", "mentor":
		// Can only view what they are assigned to
		query = query.Where("assigned_to_id = ?", user.UserID)
	default:
		c.Json(w, http.StatusForbidden, "You do not have permission to view progress entities", nil)
		return
	}

	// --- Optional: filter by cohort_id ---
	if cohortIDStr := r.URL.Query().Get("cohort_id"); cohortIDStr != "" {
		if cohortID, err := strconv.ParseUint(cohortIDStr, 10, 64); err == nil {
			query = query.Where("cohort_id = ?", cohortID)
		}
	}

	// --- Fetch entities ---
	var entities []models.ProgressEntity
	if err := query.Find(&entities).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to retrieve progress entities", map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	// --- Build clean response ---
	response := make([]map[string]interface{}, 0, len(entities))
	for _, e := range entities {
		resp := map[string]interface{}{
			"id":            e.ID,
			"entity_name":   e.EntityName,
			"entity_type":   e.EntityType,
			"status":        e.Status,
			"performance":   e.Performance,
			"progress_type": e.ProgressType,
			"cohort_id":     e.EntityCohortID,
			"assigned_to":   e.AssignedToID,
			"is_archived":   e.IsArchived,
			"created_at":    e.CreatedAt,
			"updated_at":    e.UpdatedAt,
		}

		if e.EntityCohort != nil {
			resp["cohort_name"] = e.EntityCohort.Name
			resp["start_date"] = e.EntityCohort.StartDate
			resp["end_date"] = e.EntityCohort.EndDate
		}

		response = append(response, resp)
	}

	// --- Audit & respond ---
	entity := "ProgressEntity"
	action := fmt.Sprintf("Viewed %d cohort progress entities", len(entities))
	_ = c.LogAudit(user.UserID, action, &entity, nil, nil, map[string]interface{}{
		"role":  role,
		"count": len(entities),
	})

	c.Json(w, http.StatusOK, "Progress entities retrieved successfully", map[string]interface{}{
		"count": len(response),
		"data":  response,
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
		TeamRefID    *uint64    `json:"team_ref_id,omitempty"`
	}

	// --- Parse input ---
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}
	if body.EntityID == 0 || body.PhaseName == "" || body.Status == "" {
		c.Json(w, http.StatusBadRequest, "entity_id, phase_name, and status are required", nil)
		return
	}

	// --- Get current user ---
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// --- Verify team (if provided) ---
	var team *models.Team
	if body.TeamRefID != nil {
		var userTeam models.UserTeam
		if err := c.DB.Where("team_team_id = ? AND user_user_id = ?", *body.TeamRefID, currentUser.UserID).
			First(&userTeam).Error; err != nil {
			c.Json(w, http.StatusForbidden, "You are not a member of this team", nil)
			return
		}
		if userTeam.Role != string(models.TeamLeader) {
			c.Json(w, http.StatusForbidden, "Only the team leader can add progress items", nil)
			return
		}

		team = &models.Team{}
		if err := c.DB.Preload("UserTeams.UserRef.Profile").
			Where("team_id = ?", *body.TeamRefID).First(team).Error; err != nil {
			c.Json(w, http.StatusNotFound, "Team not found", nil)
			return
		}
	}

	// --- Fetch entity ---
	var entity models.ProgressEntity
	if err := c.DB.First(&entity, body.EntityID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Progress entity not found", nil)
		return
	}

	// --- Determine statuses ---
	studentStatus := body.Status
	verifiedStatus := "Pending Verification"

	// Non-students auto-verified
	if strings.ToLower(currentUser.Role.Name) != "student" {
		studentStatus = "Completed"
		verifiedStatus = "Verified"
	}

	// --- Calculate weight dynamically if not provided ---
	weight := 0.0
	if body.Weight != nil {
		weight = *body.Weight
	} else {
		var totalWeight float64
		c.DB.Model(&models.ProgressItem{}).
			Where("entity_id = ?", body.EntityID).
			Select("COALESCE(SUM(weight),0)").Scan(&totalWeight)

		if totalWeight < 1 {
			weight = math.Max(0, 1-totalWeight)
		} else {
			weight = 1 / (totalWeight + 1)
		}
	}

	// --- Calculate performance ---
	performance := calculateItemPerformance(studentStatus, verifiedStatus)

	// --- Create progress item ---
	item := models.ProgressItem{
		CohortRefID:         entity.EntityCohortID,
		ProgressEntityRefID: &body.EntityID,
		ParentID:            body.ParentID,
		PhaseName:           body.PhaseName,
		ProgressType:        body.ProgressType,
		StudentStatus:       studentStatus,
		VerifiedStatus:      verifiedStatus,
		Weight:              weight,
		Performance:         performance,
		DueDate:             body.DueDate,
		AssignedToID:        body.AssignedToID,
		CreatedByID:         currentUser.UserID,
	}

	if body.TeamRefID != nil {
		item.TeamRefID = body.TeamRefID
	}

	if err := c.DB.Create(&item).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create progress item", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Notify relevant users ---
	if team != nil {
		for _, ut := range team.UserTeams {
			member := ut.UserRef
			c.NotifyAndTrack(
				member.UserID,
				"New Team Progress Item",
				fmt.Sprintf("A new progress item '%s' has been added to team '%s' by %s.",
					item.PhaseName, team.Name, currentUser.Profile.FirstName),
				"TeamProgress",
				"ProgressItem",
				&item.ID,
				item.StudentStatus,
				false,
			)
		}
	} else {
		c.NotifyAndTrack(
			currentUser.UserID,
			"Progress Item Created",
			fmt.Sprintf("Progress item '%s' has been added to your progress list.", item.PhaseName),
			"StudentProgress",
			"ProgressItem",
			&item.ID,
			item.StudentStatus,
			false,
		)
	}

	// --- Always recalc entity performance ---
	if err := c.RecalculateEntityPerformance(body.EntityID); err != nil {
		log.Println("Warning: entity performance recalculation failed:", err)
	}

	// --- Build response ---
	resp := map[string]interface{}{
		"id":              item.ID,
		"phase_name":      item.PhaseName,
		"status":          item.StudentStatus,
		"verified_status": item.VerifiedStatus,
		"weight":          item.Weight,
		"performance":     item.Performance,
		"entity_id":       entity.ID,
		"entity_name":     entity.EntityName,
		"entity_type":     entity.EntityType,
	}

	c.Json(w, http.StatusCreated, "Progress item created successfully", map[string]interface{}{"data": resp})
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
		entity.EntityCohortID = body.CohortID
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
			true,
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
		"cohort_id":     entity.EntityCohortID,
		"assigned_to":   assignedTo,
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
		PhaseName      *string    `json:"phase_name,omitempty"`
		StudentStatus  *string    `json:"student_status,omitempty"`
		VerifiedStatus *string    `json:"verified_status,omitempty"` // Only non-students can update
		Weight         *float64   `json:"weight,omitempty"`
		DueDate        *time.Time `json:"due_date,omitempty"`
		CompletedAt    *time.Time `json:"completed_at,omitempty"`
		AssignedToID   *uint64    `json:"assigned_to_id,omitempty"`
		IsArchived     *bool      `json:"is_archived,omitempty"`
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
	isStudent := role == "student"

	// --- Fetch progress item ---
	var item models.ProgressItem
	if err := c.DB.Preload("ProgressEntityRef").
		Preload("AssignedTo").
		Preload("TeamRef.UserTeams.UserRef.Profile").
		First(&item, id).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Progress item not found", nil)
		return
	}

	// --- Permission checks ---
	if item.TeamRefID != nil {
		var userTeam models.UserTeam
		if err := c.DB.Where("team_team_id = ? AND user_user_id = ?", *item.TeamRefID, currentUser.UserID).First(&userTeam).Error; err != nil {
			c.Json(w, http.StatusForbidden, "You are not a member of this team", nil)
			return
		}

		if isStudent && userTeam.Role != string(models.TeamLeader) {
			if item.AssignedToID == nil || *item.AssignedToID != currentUser.UserID {
				c.Json(w, http.StatusForbidden, "Only the team leader or assigned member can update this item", nil)
				return
			}
		}
	} else {
		switch role {
		case "opsadmin", "supervisor", "mentor":
			// Full access
		case "student":
			if item.CreatedByID != currentUser.UserID && (item.AssignedToID == nil || *item.AssignedToID != currentUser.UserID) {
				c.Json(w, http.StatusForbidden, "You can only update your own assigned or created items", nil)
				return
			}
		default:
			c.Json(w, http.StatusForbidden, "Unauthorized role", nil)
			return
		}
	}

	// --- Apply updates ---
	if body.PhaseName != nil {
		item.PhaseName = *body.PhaseName
	}
	if body.StudentStatus != nil {
		item.StudentStatus = *body.StudentStatus
	}

	// Only non-students can update VerifiedStatus
	if body.VerifiedStatus != nil && !isStudent {
		prevVerified := item.VerifiedStatus
		item.VerifiedStatus = *body.VerifiedStatus

		// Performance recalculation
		if item.VerifiedStatus == "Verified" && prevVerified != "Verified" {
			item.Performance = calculateItemPerformance(item.StudentStatus, item.VerifiedStatus)
		}
		// For "Send Back", keep performance same but VerifiedStatus = Pending Verification
	}

	// Other updates
	if body.Weight != nil {
		item.Weight = *body.Weight
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

	// --- Recalculate performance for student submissions ---
	if isStudent || item.VerifiedStatus != "Verified" {
		item.Performance = calculateItemPerformance(item.StudentStatus, item.VerifiedStatus)
	}

	// --- Save updates ---
	if err := c.DB.Save(&item).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update progress item", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Recalculate entity performance if verified ---
	if item.ProgressEntityRefID != nil && item.VerifiedStatus == "Verified" {
		if err := c.RecalculateEntityPerformance(*item.ProgressEntityRefID); err != nil {
			log.Printf("⚠️ Entity performance recalculation failed for EntityID %d: %v\n", *item.ProgressEntityRefID, err)
		}
	}

	// --- Notifications ---
	if item.AssignedToID != nil {
		c.NotifyAndTrack(
			*item.AssignedToID,
			"Progress Item Updated",
			fmt.Sprintf("You have been assigned/updated to progress item '%s' under %s '%s'",
				item.PhaseName, item.ProgressEntityRef.EntityType, item.ProgressEntityRef.EntityName),
			"Assignment",
			item.ProgressEntityRef.EntityType,
			item.ProgressEntityRefID,
			item.StudentStatus,
			true,
		)
	}

	if item.TeamRefID != nil {
		var team models.Team
		if err := c.DB.Preload("UserTeams.UserRef.Profile").First(&team, *item.TeamRefID).Error; err == nil {
			for _, ut := range team.UserTeams {
				member := ut.UserRef
				c.NotifyAndTrack(
					member.UserID,
					"Team Progress Item Updated",
					fmt.Sprintf("Progress item '%s' in team '%s' was updated by %s.",
						item.PhaseName, team.Name, currentUser.Username),
					"TeamProgressUpdate",
					"ProgressItem",
					&item.ID,
					item.StudentStatus,
					true,
				)
			}
		}
	}

	// --- Audit log ---
	c.NotifyAndTrack(
		currentUser.UserID,
		"Progress Item Updated",
		fmt.Sprintf("%s updated progress item '%s' (student status: %s, verified status: %s)",
			currentUser.Username, item.PhaseName, item.StudentStatus, item.VerifiedStatus),
		"Update",
		item.ProgressEntityRef.EntityType,
		item.ProgressEntityRefID,
		item.StudentStatus,
		true,
	)

	c.Json(w, http.StatusOK, "Progress item updated successfully", map[string]interface{}{"data": item})
}

// =======++++=======================================================”””””=============
//
//	Soft Deletes for Progress Items
//
// =======++++=======================================================”””””=============
// ManageProgressItems allows Supervisors to archive, unarchive, or delete progress items in bulk
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

	// --- Authenticate user ---
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	role := strings.ToLower(currentUser.Role.Name)
	if role != "supervisor" && role != "opsadmin" {
		c.Json(w, http.StatusForbidden, "Only supervisors or opsadmins can manage progress items", nil)
		return
	}

	// --- Fetch items (including team info) ---
	var items []models.ProgressItem
	if err := c.DB.
		Preload("ProgressEntityRef").
		Preload("TeamRef.TeamMembers.User.Profile").
		Where("id IN ?", input.ItemIDs).
		Find(&items).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch progress items", map[string]interface{}{"error": err.Error()})
		return
	}

	if len(items) == 0 {
		c.Json(w, http.StatusNotFound, "No progress items found for the given IDs", nil)
		return
	}

	var archivedIDs, unarchivedIDs, deletedIDs []uint64

	for _, item := range items {
		action := strings.ToLower(input.Action)
		now := time.Now()

		switch action {
		case "archive":
			item.IsArchived = true
			item.UpdatedAt = now
			if err := c.DB.Save(&item).Error; err == nil {
				archivedIDs = append(archivedIDs, item.ID)
			}

			c.NotifyAndTrack(currentUser.UserID, "Progress Item Archived",
				fmt.Sprintf("Progress item '%s' in %s '%s' was archived", item.PhaseName, item.ProgressEntityRef.EntityType, item.ProgressEntityRef.EntityName),
				"Archive",
				item.ProgressEntityRef.EntityType,
				&item.ID,
				item.VerifiedStatus,
				false,
			)

			// Notify team members if this is a team item
			if item.TeamRefID != nil {
				c.notifyTeamMembers(*item.TeamRefID, fmt.Sprintf("Team progress item '%s' has been archived by %s.",
					item.PhaseName, currentUser.Username))
			}

		case "unarchive":
			item.IsArchived = false
			item.UpdatedAt = now
			if err := c.DB.Save(&item).Error; err == nil {
				unarchivedIDs = append(unarchivedIDs, item.ID)
			}

			c.NotifyAndTrack(currentUser.UserID, "Progress Item Unarchived",
				fmt.Sprintf("Progress item '%s' in %s '%s' was unarchived", item.PhaseName, item.ProgressEntityRef.EntityType, item.ProgressEntityRef.EntityName),
				"Unarchive",
				item.ProgressEntityRef.EntityType,
				&item.ID,
				item.VerifiedStatus,
				false,
			)

			if item.TeamRefID != nil {
				c.notifyTeamMembers(*item.TeamRefID, fmt.Sprintf("Team progress item '%s' has been restored by %s.",
					item.PhaseName, currentUser.Username))
			}

		case "delete":
			// Hard delete (supervisor-only)
			if err := c.DB.Unscoped().Delete(&item).Error; err == nil {
				deletedIDs = append(deletedIDs, item.ID)
			}

			c.NotifyAndTrack(currentUser.UserID, "Progress Item Deleted Permanently",
				fmt.Sprintf("Progress item '%s' in %s '%s' was permanently deleted", item.PhaseName, item.ProgressEntityRef.EntityType, item.ProgressEntityRef.EntityName),
				"Deletion",
				item.ProgressEntityRef.EntityType,
				&item.ID,
				item.VerifiedStatus,
				true,
			)

			if item.TeamRefID != nil {
				c.notifyTeamMembers(*item.TeamRefID, fmt.Sprintf("Team progress item '%s' was permanently deleted by %s.",
					item.PhaseName, currentUser.Username))
			}

		default:
			c.Json(w, http.StatusBadRequest, "Invalid action, must be 'archive', 'unarchive', or 'delete'", nil)
			return
		}

		// --- Recalculate entity performance (if applicable) ---
		if item.ProgressEntityRefID != nil {
			if err := c.RecalculateEntityPerformance(*item.ProgressEntityRefID); err != nil {
				log.Printf("⚠️ Entity performance recalculation failed for EntityID %d: %v\n", *item.ProgressEntityRefID, err)
			}
		}
	}

	// --- Prepare response ---
	response := map[string]interface{}{}
	if len(archivedIDs) > 0 {
		response["archived_ids"] = archivedIDs
	}
	if len(unarchivedIDs) > 0 {
		response["unarchived_ids"] = unarchivedIDs
	}
	if len(deletedIDs) > 0 {
		response["deleted_ids"] = deletedIDs
	}

	c.Json(w, http.StatusOK, fmt.Sprintf("Action '%s' performed successfully", input.Action), response)
}

// 🔹 Helper to notify all team members when a team item is managed
func (c *Construct) notifyTeamMembers(teamRefID uint64, message string) {
	var team models.Team
	if err := c.DB.Preload("Users").First(&team, teamRefID).Error; err != nil {
		return
	}
	for _, ut := range team.UserTeams {
		member := ut.UserRef
		c.NotifyAndTrack(
			member.UserID,
			"Team Progress Item Update",
			message,
			"TeamItemManagement",
			"ProgressItem",
			nil,
			"Pending",
			true,
		)
	}

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
				true,
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
				true,
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
				true,
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

// =======++++=======================================================”””””=============
//
//	Retrieving Cohort Progress API DATA
//
// =======++++=======================================================”””””=============

// GET /progress/entities/{id}?page=1&limit=20&report_status=Pending&week_start=2025-10-01&week_end=2025-10-07

// --- get assigned cohorts helper ---
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

// RecalculateEntityPerformance recalculates weighted performance and entity status
func (c *Construct) RecalculateEntityPerformance(entityID uint64) error {
	if err := c.NormalizeEntityWeights(entityID); err != nil {
		fmt.Println("Warning: normalization failed:", err)
	}

	return c.UpdateEntityWeightedPerformance(entityID)
}
