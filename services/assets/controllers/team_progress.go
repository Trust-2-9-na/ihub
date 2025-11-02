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

func (c *Construct) AddTeamProgressItem(w http.ResponseWriter, r *http.Request) {
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

	// --- Verify team membership if team ID provided ---
	var team *models.Team
	if body.TeamRefID != nil {
		var userTeam models.UserTeam
		if err := c.DB.Where("team_team_id = ? AND user_user_id = ?", *body.TeamRefID, currentUser.UserID).
			First(&userTeam).Error; err != nil {
			c.Json(w, http.StatusForbidden, "You are not a member of this team", nil)
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
	if strings.ToLower(currentUser.Role.Name) != "student" {
		studentStatus = "Completed"
		verifiedStatus = "Verified"
	}

	// --- Calculate weight ---
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

	// --- Calculate individual item performance ---
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

	// --- Notify team members or individual ---
	if team != nil {
		for _, ut := range team.UserTeams {
			c.NotifyAndTrack(
				ut.UserRef.UserID,
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

	// --- Always recalculate entity performance (includes all items for status and performance) ---
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
		"team_id":         body.TeamRefID,
	}

	c.Json(w, http.StatusCreated, "Team progress item created successfully", map[string]interface{}{"data": resp})
}

// updating progress items

func (c *Construct) UpdateTeamProgressItem(w http.ResponseWriter, r *http.Request) {
	// --- Parse progress item ID from URL ---
	vars := mux.Vars(r)
	idStr, ok := vars["id"]
	if !ok || idStr == "" {
		c.Json(w, http.StatusBadRequest, "Progress item ID is required", nil)
		return
	}
	itemID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid progress item ID", nil)
		return
	}

	// --- Parse request body ---
	var body struct {
		PhaseName    *string    `json:"phase_name,omitempty"`
		ProgressType *string    `json:"progress_type,omitempty"`
		Status       *string    `json:"status,omitempty"`
		Weight       *float64   `json:"weight,omitempty"`
		DueDate      *time.Time `json:"due_date,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Authenticate user ---
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// --- Fetch the progress item ---
	var item models.ProgressItem
	if err := c.DB.Preload("TeamDetails").First(&item, itemID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Progress item not found", nil)
		return
	}

	// --- Authorization: only creator or non-student (supervisor/mentor) can edit ---
	if item.CreatedByID != currentUser.UserID && strings.ToLower(currentUser.Role.Name) == "student" {
		c.Json(w, http.StatusForbidden, "You can only edit your own progress items", nil)
		return
	}

	// --- Verify team membership if it's a team item ---
	if item.TeamRefID != nil {
		var userTeam models.UserTeam
		if err := c.DB.Where("team_team_id = ? AND user_user_id = ?", *item.TeamRefID, currentUser.UserID).
			First(&userTeam).Error; err != nil {
			c.Json(w, http.StatusForbidden, "You are not a member of this team", nil)
			return
		}
	}

	// --- Apply updates ---
	if body.PhaseName != nil {
		item.PhaseName = *body.PhaseName
	}
	if body.ProgressType != nil {
		item.ProgressType = *body.ProgressType
	}
	if body.Status != nil {
		item.StudentStatus = *body.Status
		// Update VerifiedStatus automatically if user is not a student
		if strings.ToLower(currentUser.Role.Name) != "student" {
			item.VerifiedStatus = "Verified"
		} else {
			item.VerifiedStatus = "Pending Verification"
		}
		item.Performance = calculateItemPerformance(item.StudentStatus, item.VerifiedStatus)
	}
	if body.Weight != nil {
		item.Weight = *body.Weight
	}
	if body.DueDate != nil {
		item.DueDate = body.DueDate
	}

	if err := c.DB.Save(&item).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update progress item", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Always recalculate entity performance (includes all items for status and performance) ---
	if item.ProgressEntityRefID != nil {
		if err := c.RecalculateEntityPerformance(*item.ProgressEntityRefID); err != nil {
			log.Println("Warning: entity performance recalculation failed:", err)
		}
	}

	// --- Build response ---
	entity := map[string]interface{}{}
	if item.ProgressEntityRef != nil {
		entity = map[string]interface{}{
			"id":          item.ProgressEntityRef.ID,
			"entity_name": item.ProgressEntityRef.EntityName,
			"entity_type": item.ProgressEntityRef.EntityType,
		}
	}

	resp := map[string]interface{}{
		"id":              item.ID,
		"phase_name":      item.PhaseName,
		"progress_type":   item.ProgressType,
		"status":          item.StudentStatus,
		"verified_status": item.VerifiedStatus,
		"weight":          item.Weight,
		"performance":     item.Performance,
		"due_date":        item.DueDate,
		"entity":          entity,
		"team_id":         item.TeamRefID,
	}

	c.Json(w, http.StatusOK, "Progress item updated successfully", map[string]interface{}{"data": resp})
}
