package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"web/services/assets/middlewares"
	"web/services/assets/models"

	"gorm.io/gorm"
)

// ===============++++===================++============================================
//                          Supervisor Cohort Assignments
// ===============++++===================++============================================

func (c *Construct) AssignSupervisorToCohort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortCohortID uint64 `json:"cohort_cohort_id"`
		UserUserID     uint64 `json:"user_user_id"`
	}

	// --- Decode request body ---
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}
	if body.CohortCohortID == 0 || body.UserUserID == 0 {
		c.Json(w, http.StatusBadRequest, "cohort_cohort_id and user_user_id are required", nil)
		return
	}

	// --- Get logged-in OpsAdmin ---
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, err.Error(), nil)
		return
	}
	if strings.ToLower(currentUser.Role.Name) != "opsadmin" {
		c.Json(w, http.StatusForbidden, "Only OpsAdmins can assign supervisors", nil)
		return
	}

	// --- Validate cohort exists ---
	var cohort models.Cohort
	if err := c.DB.First(&cohort, "cohort_id = ?", body.CohortCohortID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Cohort not found", nil)
		return
	}

	// --- Fetch new supervisor details ---
	var newSupervisor models.User
	if err := c.DB.Preload("Profile").First(&newSupervisor, "user_id = ?", body.UserUserID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Supervisor not found", nil)
		return
	}
	newSupervisorFullName := strings.TrimSpace(newSupervisor.Profile.FirstName + " " + newSupervisor.Profile.LastName)

	// --- Check if cohort already has a supervisor ---
	var existing models.CohortUser
	err = c.DB.Where("cohort_cohort_id = ? AND role = ?", body.CohortCohortID, "Supervisor").First(&existing).Error

	oldSupervisorID := uint64(0)
	oldSupervisorFullName := "None"

	if err == nil {
		// --- Reassign ---
		oldSupervisorID = existing.UserID
		if oldSupervisorID != 0 {
			var oldSupervisor models.User
			if err := c.DB.Preload("Profile").First(&oldSupervisor, "user_id = ?", oldSupervisorID).Error; err == nil {
				oldSupervisorFullName = strings.TrimSpace(oldSupervisor.Profile.FirstName + " " + oldSupervisor.Profile.LastName)
			}
		}

		existing.UserID = body.UserUserID
		if err := c.DB.Save(&existing).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to reassign supervisor", map[string]interface{}{"error": err.Error()})
			return
		}

		// --- Notify new supervisor ---
		c.NotifyAndTrack(newSupervisor.UserID, "Cohort Reassignment",
			fmt.Sprintf("You have been reassigned as supervisor for cohort '%s'", cohort.Name),
			"Assignment", "Cohort", &body.CohortCohortID, "")

		// --- Audit & system tracking ---
		metadata := map[string]interface{}{
			"cohort_name":    cohort.Name,
			"old_supervisor": oldSupervisorFullName,
			"new_supervisor": newSupervisorFullName,
			"assigned_by":    currentUser.Username,
		}
		_ = c.LogAudit(currentUser.UserID, "reassign_supervisor", nil, &body.CohortCohortID, nil, metadata)

		c.Json(w, http.StatusOK, "Supervisor reassigned successfully", map[string]interface{}{
			"cohort_name": cohort.Name,
			"supervisor":  newSupervisorFullName,
		})
		return
	}

	// --- Assign new supervisor ---
	assign := models.CohortUser{
		CohortID: body.CohortCohortID,
		UserID:   body.UserUserID,
		Role:     "Supervisor",
	}
	if err := c.DB.Create(&assign).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to assign supervisor", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Notify supervisor ---
	c.NotifyAndTrack(newSupervisor.UserID, "Cohort Assignment",
		fmt.Sprintf("You have been assigned as supervisor for cohort '%s'", cohort.Name),
		"Assignment", "Cohort", &body.CohortCohortID, "")

	// --- Audit & system tracking ---
	metadata := map[string]interface{}{
		"cohort_name":    cohort.Name,
		"new_supervisor": newSupervisorFullName,
		"assigned_by":    currentUser.Username,
	}
	_ = c.LogAudit(currentUser.UserID, "assign_supervisor", nil, &body.CohortCohortID, nil, metadata)

	c.Json(w, http.StatusOK, "Supervisor assigned successfully", map[string]interface{}{
		"cohort_name": cohort.Name,
		"supervisor":  newSupervisorFullName,
	})
}

// ===============++++===================++============================================
//                          Supervisor Cohort Unassignments
// ===============++++===================++============================================

func (c *Construct) UnassignSupervisorFromCohort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortCohortID uint64 `json:"cohort_cohort_id"`
	}

	// --- Decode request body ---
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}
	if body.CohortCohortID == 0 {
		c.Json(w, http.StatusBadRequest, "cohort_cohort_id is required", nil)
		return
	}

	// --- Get logged-in OpsAdmin ---
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, err.Error(), nil)
		return
	}
	if strings.ToLower(currentUser.Role.Name) != "opsadmin" {
		c.Json(w, http.StatusForbidden, "Only OpsAdmins can unassign supervisors", nil)
		return
	}

	// --- Fetch cohort ---
	var cohort models.Cohort
	if err := c.DB.First(&cohort, "cohort_id = ?", body.CohortCohortID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Cohort not found", nil)
		return
	}

	// --- Fetch current supervisor ---
	var supervisorAssignment models.CohortUser
	err = c.DB.Where("cohort_cohort_id = ? AND role = ?", body.CohortCohortID, "Supervisor").First(&supervisorAssignment).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.Json(w, http.StatusNotFound, "No supervisor assigned to this cohort", nil)
			return
		}
		c.Json(w, http.StatusInternalServerError, "Failed to fetch supervisor", map[string]interface{}{"error": err.Error()})
		return
	}

	var supervisor models.User
	supervisorFullName := "Unknown"
	if err := c.DB.Preload("Profile").First(&supervisor, "user_id = ?", supervisorAssignment.UserID).Error; err == nil {
		supervisorFullName = strings.TrimSpace(supervisor.Profile.FirstName + " " + supervisor.Profile.LastName)
	}

	// --- Unassign supervisor ---
	if err := c.DB.Delete(&supervisorAssignment).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to unassign supervisor", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Notify supervisor ---
	c.NotifyAndTrack(supervisor.UserID, "Cohort Unassignment",
		fmt.Sprintf("You have been unassigned as supervisor from cohort '%s'", cohort.Name),
		"Unassignment", "Cohort", &body.CohortCohortID, "")

	// --- Audit & system tracking ---
	metadata := map[string]interface{}{
		"cohort_name":   cohort.Name,
		"supervisor":    supervisorFullName,
		"unassigned_by": currentUser.Username,
	}
	_ = c.LogAudit(currentUser.UserID, "unassign_supervisor", nil, &body.CohortCohortID, nil, metadata)

	c.Json(w, http.StatusOK, "Supervisor unassigned successfully", map[string]interface{}{
		"cohort_name": cohort.Name,
		"supervisor":  supervisorFullName,
	})
}

// ===============++++===================++============================================
//
//	GET Supervisor Cohorts
//
// ===============++++===================++============================================
func (c *Construct) GetCohortSupervisors(w http.ResponseWriter, r *http.Request) {
	// --- Get authenticated user UUID from context ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// --- Fetch full user record using UUID ---
	var user models.User
	if err := c.DB.Preload("Role").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "User not found", nil)
		return
	}

	roleName := strings.ToLower(user.Role.Name)

	// --- Prepare response structure ---
	var assignments []struct {
		CohortID        uint64
		CohortName      string
		SupervisorID    uint64
		SupervisorName  string
		SupervisorEmail string
		AssignedByID    string
		AssignedByName  string
		AssignedAt      time.Time
	}

	// --- Build base query ---
	query := c.DB.Table("cohort_users as cu").
		Select(`
            c.cohort_id,
            c.name as cohort_name,
            u.user_id as supervisor_id,
            u.username as supervisor_name,
            u.email as supervisor_email,
            c.created_by as assigned_by_id,
            creator.username as assigned_by_name,
            cu.created_at as assigned_at
        `).
		Joins("JOIN cohorts c ON c.cohort_id = cu.cohort_cohort_id").
		Joins("JOIN users u ON u.user_user_id = cu.user_user_id").
		Joins("LEFT JOIN users creator ON creator.user_user_id = c.created_by").
		Where("cu.role = ?", "Supervisor")

	// --- Role-based filtering ---
	switch roleName {
	case "supervisor":
		query = query.Where("cu.user_user_id = ?", user.UserID)
	case "opsadmin", "systemadmin":
		// No additional filter
	default:
		c.Json(w, http.StatusForbidden, "Not allowed", nil)
		return
	}

	// --- Execute query ---
	if err := query.Scan(&assignments).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch assignments", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Format response ---
	resp := make([]map[string]interface{}, 0, len(assignments))
	for _, a := range assignments {
		record := map[string]interface{}{
			"cohort_id":   a.CohortID,
			"cohort_name": a.CohortName,
			"supervisor": map[string]interface{}{
				"id":    a.SupervisorID,
				"name":  a.SupervisorName,
				"email": a.SupervisorEmail,
			},
		}

		if roleName != "supervisor" {
			record["assigned_by"] = map[string]interface{}{
				"id":   a.AssignedByID,
				"name": a.AssignedByName,
			}
			record["assigned_at"] = a.AssignedAt
		}

		resp = append(resp, record)
	}

	// --- Send response ---
	c.Json(w, http.StatusOK, "Cohort supervisors fetched successfully", map[string]interface{}{
		"assignments": resp,
	})

	// --- Optional: Track access for admins ---
	if roleName == "opsadmin" || roleName == "systemadmin" {
		c.NotifyAndTrack(
			user.UserID,
			"Viewed Cohort Supervisors",
			fmt.Sprintf("%s viewed cohort-supervisor assignments", user.Username),
			"Review Access",
			"CohortSupervisor",
			nil,
			"",
		)
	}
}
