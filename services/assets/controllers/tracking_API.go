package controllers

import (
	"net/http"
	"strconv"
	"time"
	"web/services/assets/models"
)

//=====---->> Track Proposals <<-------===================================-------->>>

func (c *Construct) ProposalTrackingHistory(w http.ResponseWriter, r *http.Request) {
	// ✅ Step 1: Get authenticated user
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// ✅ Step 2: Parse query params safely
	query := r.URL.Query()
	entityType := query.Get("entity_type")
	entityIDStr := query.Get("entity_id")
	action := query.Get("action")
	startDateStr := query.Get("start_date")
	endDateStr := query.Get("end_date")

	page, _ := strconv.Atoi(query.Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(query.Get("limit"))
	if limit < 1 {
		limit = 50
	}
	offset := (page - 1) * limit

	// ✅ Step 3: Build base query
	dbQuery := c.DB.Model(&models.SystemHistory{}).
		Preload("ChangedBy.Profile")

	if entityType != "" {
		dbQuery = dbQuery.Where("entity_type = ?", entityType)
	}
	if entityIDStr != "" {
		if entityID, err := strconv.ParseUint(entityIDStr, 10, 64); err == nil {
			dbQuery = dbQuery.Where("entity_id = ?", entityID)
		}
	}
	if action != "" {
		dbQuery = dbQuery.Where("action ILIKE ?", "%"+action+"%")
	}

	// ✅ Step 4: Handle date range filters
	if startDateStr != "" {
		if start, err := time.Parse("2006-01-02", startDateStr); err == nil {
			dbQuery = dbQuery.Where("created_at >= ?", start)
		}
	}
	if endDateStr != "" {
		if end, err := time.Parse("2006-01-02", endDateStr); err == nil {
			dbQuery = dbQuery.Where("created_at <= ?", end.Add(24*time.Hour))
		}
	}

	// ✅ Step 5: Role-based access control
	switch user.Role.Name {
	case "Student":
		// Students: only see proposals they submitted
		dbQuery = dbQuery.Joins("JOIN proposals ON proposals.proposal_id = system_histories.entity_id").
			Where("system_histories.entity_type = 'proposal'").
			Where("proposals.submitted_by_id = ?", user.UserID)

	case "Mentor":
		dbQuery = dbQuery.Joins(`
		JOIN proposals p ON p.proposal_id = system_histories.entity_id
		JOIN cohort_users cu ON cu.cohort_cohort_id = p.cohort_id
	`).Where("system_histories.entity_type = ? AND cu.user_user_id = ? AND cu.role = ?", "proposal", user.UserID, "Mentor")

	case "Supervisor":
		// Supervisors: proposals in their window
		dbQuery = dbQuery.Joins("JOIN proposals ON proposals.proposal_id = system_histories.entity_id").
			Joins("JOIN windows ON proposals.window_id = windows.window_id").
			Where("system_histories.entity_type = 'proposal'").
			Where("windows.supervisor_id = ?", user.UserID)

	case "Admin":
		// Admins see everything
		break

	default:
		c.Json(w, http.StatusForbidden, "Access denied for role: "+user.Role.Name, nil)
		return
	}

	// ✅ Step 6: Count and fetch paginated results
	var total int64
	if err := dbQuery.Count(&total).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to count records", map[string]interface{}{"error": err.Error()})
		return
	}

	var history []models.SystemHistory
	if err := dbQuery.Order("created_at DESC").Offset(offset).Limit(limit).Find(&history).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch history", map[string]interface{}{"error": err.Error()})
		return
	}

	// ✅ Step 7: Structure response
	resp := make([]map[string]interface{}, 0, len(history))
	for _, h := range history {
		var changedBy map[string]interface{}
		if h.ChangedBy != nil && h.ChangedBy.Profile != (models.UserProfile{}) {
			changedBy = map[string]interface{}{
				"user_id":    h.ChangedBy.UserID,
				"first_name": h.ChangedBy.Profile.FirstName,
				"last_name":  h.ChangedBy.Profile.LastName,
				"email":      h.ChangedBy.Email,
			}
		}

		resp = append(resp, map[string]interface{}{
			"entity_type": h.EntityType,
			"entity_id":   h.EntityID,
			"action":      h.Action,
			"status":      h.Status,
			"comment":     h.Comment,
			"changed_by":  changedBy,
			"metadata":    h.Metadata,
			"created_at":  h.CreatedAt,
		})
	}

	// ✅ Step 8: Final JSON response
	c.Json(w, http.StatusOK, "Tracking history fetched successfully", map[string]interface{}{
		"page":    page,
		"limit":   limit,
		"total":   total,
		"history": resp,
	})
}

// =====---->> Track Cohorts <<-------===================================-------->>>
func (c *Construct) CohortTrackingHistory(w http.ResponseWriter, r *http.Request) {
	// --- Step 1: Auth ---
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// --- Step 2: Query params ---
	query := r.URL.Query()
	entityType := query.Get("entity_type") // "proposal" or "cohort"
	entityIDStr := query.Get("entity_id")
	action := query.Get("action")
	startDateStr := query.Get("start_date")
	endDateStr := query.Get("end_date")

	page, _ := strconv.Atoi(query.Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(query.Get("limit"))
	if limit < 1 {
		limit = 50
	}
	offset := (page - 1) * limit

	// --- Step 3: Base query ---
	dbQuery := c.DB.Model(&models.SystemHistory{}).Preload("ChangedBy.Profile")

	if entityType != "" {
		dbQuery = dbQuery.Where("entity_type = ?", entityType)
	}
	if entityIDStr != "" {
		if entityID, err := strconv.ParseUint(entityIDStr, 10, 64); err == nil {
			dbQuery = dbQuery.Where("entity_id = ?", entityID)
		}
	}
	if action != "" {
		dbQuery = dbQuery.Where("action ILIKE ?", "%"+action+"%")
	}
	if startDateStr != "" {
		if start, err := time.Parse("2006-01-02", startDateStr); err == nil {
			dbQuery = dbQuery.Where("created_at >= ?", start)
		}
	}
	if endDateStr != "" {
		if end, err := time.Parse("2006-01-02", endDateStr); err == nil {
			dbQuery = dbQuery.Where("created_at <= ?", end.Add(24*time.Hour))
		}
	}

	// --- Step 4: Role-based visibility ---
	switch user.Role.Name {
	case "Student":
		dbQuery = dbQuery.Where(`
			(entity_type = 'proposal' AND entity_id IN (
				SELECT proposal_id FROM proposals WHERE submitted_by_id = ?
			))
			OR
			(entity_type = 'cohort' AND entity_id IN (
				SELECT cohort_cohort_id FROM cohort_users WHERE user_user_id = ?
			))
		`, user.UserID, user.UserID)

	case "Mentor":
		dbQuery = dbQuery.Where(`
			entity_type = 'cohort' AND entity_id IN (
				SELECT cohort_cohort_id FROM cohort_users WHERE user_user_id = ? AND role = 'Mentor'
			)
		`, user.UserID)

	case "Supervisor":
		dbQuery = dbQuery.Where(`
		(
			entity_type = 'proposal' AND entity_id IN (
				SELECT p.proposal_id 
				FROM proposals p
				WHERE p.submitted_by_id IN (
					SELECT cu.user_user_id
					FROM cohort_users cu
					WHERE cu.cohort_cohort_id IN (
						SELECT c.cohort_id 
						FROM cohorts c
						LEFT JOIN cohort_users cu2 ON c.cohort_id = cu2.cohort_cohort_id
						WHERE c.created_by = ? OR (cu2.user_user_id = ? AND cu2.role = 'Supervisor')
					)
				)
			)
		)
		OR
		(
			entity_type = 'cohort' AND entity_id IN (
				SELECT c.cohort_id
				FROM cohorts c
				LEFT JOIN cohort_users cu ON c.cohort_id = cu.cohort_cohort_id
				WHERE c.created_by = ? OR (cu.user_user_id = ? AND cu.role = 'Supervisor')
			)
		)
	`, user.UserUUID, user.UserID, user.UserUUID, user.UserID)

	case "Admin":
		// Admin sees all histories
		break

	default:
		c.Json(w, http.StatusForbidden, "Access denied for role: "+user.Role.Name, nil)
		return
	}

	// --- Step 5: Count & fetch ---
	var total int64
	if err := dbQuery.Count(&total).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to count history", map[string]interface{}{"error": err.Error()})
		return
	}

	var history []models.SystemHistory
	if err := dbQuery.Order("created_at DESC").Offset(offset).Limit(limit).Find(&history).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch history", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Step 6: Prepare response ---
	resp := make([]map[string]interface{}, 0, len(history))
	for _, h := range history {
		var changedBy map[string]interface{}
		if h.ChangedBy != nil && h.ChangedBy.Profile != (models.UserProfile{}) {
			changedBy = map[string]interface{}{
				"user_id":   h.ChangedBy.UserID,
				"full_name": h.ChangedBy.Profile.FirstName + " " + h.ChangedBy.Profile.LastName,
				"email":     h.ChangedBy.Email,
			}
		}

		resp = append(resp, map[string]interface{}{
			"entity_type": h.EntityType,
			"entity_id":   h.EntityID,
			"action":      h.Action,
			"status":      h.Status,
			"comment":     h.Comment,
			"changed_by":  changedBy,
			"metadata":    h.Metadata,
			"created_at":  h.CreatedAt,
		})
	}

	// --- Step 7: Return JSON ---
	c.Json(w, http.StatusOK, "Tracking history fetched successfully", map[string]interface{}{
		"page":    page,
		"limit":   limit,
		"total":   total,
		"history": resp,
	})
}

//======================================================================
// history retrieval api
//======================================================================

func (c *Construct) GetSystemHistory(w http.ResponseWriter, r *http.Request) {
	entityType := r.URL.Query().Get("entity_type")
	entityIDStr := r.URL.Query().Get("entity_id")
	action := r.URL.Query().Get("action")
	status := r.URL.Query().Get("status")

	var entityID *uint64
	if entityIDStr != "" {
		id, err := strconv.ParseUint(entityIDStr, 10, 64)
		if err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid entity_id value", map[string]interface{}{"error": err.Error()})
			return
		}
		entityID = &id
	}

	// Build base query
	query := c.DB.Preload("ChangedBy").Model(&models.SystemHistory{})

	if entityType != "" {
		query = query.Where("entity_type = ?", entityType)
	}
	if entityID != nil {
		query = query.Where("entity_id = ?", *entityID)
	}
	if action != "" {
		query = query.Where("action ILIKE ?", "%"+action+"%")
	}
	if status != "" {
		query = query.Where("status ILIKE ?", "%"+status+"%")
	}

	// Execute query
	var histories []models.SystemHistory
	if err := query.Order("created_at ASC").Find(&histories).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch history", map[string]interface{}{"error": err.Error()})
		return
	}

	if len(histories) == 0 {
		c.Json(w, http.StatusNotFound, "No history records found for given filters", nil)
		return
	}

	c.Json(w, http.StatusOK, "System history fetched successfully", map[string]interface{}{
		"count": len(histories),
		"data":  histories,
	})
}
