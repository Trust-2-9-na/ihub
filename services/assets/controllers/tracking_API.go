package controllers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"web/services/assets/models"
)

func (c *Construct) GetTrackingHistory(w http.ResponseWriter, r *http.Request) {
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// --- Query params ---
	entityType := r.URL.Query().Get("entity_type")
	entityIDStr := r.URL.Query().Get("entity_id")
	action := r.URL.Query().Get("action")
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")
	pageStr := r.URL.Query().Get("page")
	limitStr := r.URL.Query().Get("limit")

	page, _ := strconv.Atoi(pageStr)
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(limitStr)
	if limit < 1 {
		limit = 50
	}
	offset := (page - 1) * limit

	// --- Base query ---
	dbQuery := c.DB.Model(&models.SystemHistory{}).Preload("ChangedBy.Profile")

	if entityType != "" {
		dbQuery = dbQuery.Where("entity_type = ?", entityType)
	}
	if entityIDStr != "" {
		entityID, _ := strconv.ParseUint(entityIDStr, 10, 64)
		dbQuery = dbQuery.Where("entity_id = ?", entityID)
	}
	if action != "" {
		dbQuery = dbQuery.Where("action = ?", action)
	}
	if startDate != "" {
		dbQuery = dbQuery.Where("created_at >= ?", startDate)
	}
	if endDate != "" {
		dbQuery = dbQuery.Where("created_at <= ?", endDate)
	}

	// --- Role based filters ---
	switch user.Role.Name {
	case "Student":
		// only proposals they submitted
		dbQuery = dbQuery.Joins("JOIN proposals ON proposals.proposal_id = system_histories.entity_id").
			Where("proposals.submitted_by_id = ?", user.UserID)
	case "Supervisor":
		// proposals in supervisor's window
		dbQuery = dbQuery.Joins("JOIN proposals ON proposals.proposal_id = system_histories.entity_id").
			Joins("JOIN windows ON proposals.window_id = windows.window_id").
			Where("windows.supervisor_id = ?", user.UserID)
	case "Admin":
		// no additional filter
	default:
		http.Error(w, "Access denied for role: "+user.Role.Name, http.StatusForbidden)
		return
	}

	// --- Fetch total count ---
	var total int64
	dbQuery.Count(&total)

	// --- Fetch results ---
	var history []models.SystemHistory
	if err := dbQuery.Order("created_at DESC").Offset(offset).Limit(limit).Find(&history).Error; err != nil {
		http.Error(w, "failed to fetch history", http.StatusInternalServerError)
		return
	}

	// --- Prepare response ---
	resp := make([]map[string]interface{}, 0, len(history))
	for _, h := range history {
		changedBy := map[string]interface{}{}
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

	json.NewEncoder(w).Encode(map[string]interface{}{
		"page":    page,
		"limit":   limit,
		"total":   total,
		"history": resp,
	})
}
