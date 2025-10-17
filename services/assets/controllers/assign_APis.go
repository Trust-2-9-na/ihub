package controllers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"web/services/assets/middlewares"
	"web/services/assets/models"

	"gorm.io/gorm"
)

// ===============++++===================++============================================
//
//	Supervisor Cohort Assignments
//
// ===============++++===================++============================================
func (c *Construct) AssignSupervisorToCohort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortCohortID uint64 `json:"cohort_cohort_id"`
		UserUserID     uint64 `json:"user_user_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}
	if body.CohortCohortID == 0 || body.UserUserID == 0 {
		c.Json(w, http.StatusBadRequest, "cohort_cohort_id and user_user_id are required", nil)
		return
	}

	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, err.Error(), nil)
		return
	}
	if strings.ToLower(currentUser.Role.Name) != "opsadmin" {
		c.Json(w, http.StatusForbidden, "Only OpsAdmins can assign supervisors", nil)
		return
	}

	var cohort models.Cohort
	if err := c.DB.First(&cohort, "cohort_id = ?", body.CohortCohortID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Cohort not found", nil)
		return
	}

	// Check if supervisor already exists (soft-deleted included)
	var existing models.CohortUser
	err = c.DB.Unscoped().Where("cohort_cohort_id = ? AND role = ? AND user_user_id = ?", body.CohortCohortID, "Supervisor", body.UserUserID).First(&existing).Error
	if err == nil {
		c.Json(w, http.StatusConflict, "Supervisor already assigned to this cohort", nil)
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.Json(w, http.StatusInternalServerError, "Failed to check supervisor", map[string]interface{}{"error": err.Error()})
		return
	}

	// Fetch new supervisor details
	var newSupervisor models.User
	if err := c.DB.Preload("Profile").First(&newSupervisor, "user_id = ?", body.UserUserID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Supervisor not found", nil)
		return
	}
	newSupervisorFullName := strings.TrimSpace(newSupervisor.Profile.FirstName + " " + newSupervisor.Profile.LastName)

	// Assign supervisor (soft-delete is not needed here, just create)
	assign := models.CohortUser{
		CohortID: body.CohortCohortID,
		UserID:   body.UserUserID,
		Role:     "Supervisor",
	}
	if err := c.DB.Create(&assign).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to assign supervisor", map[string]interface{}{"error": err.Error()})
		return
	}

	// Notify & audit via NotifyAndTrack
	c.NotifyAndTrack(newSupervisor.UserID, "Cohort Assignment",
		fmt.Sprintf("You have been assigned as supervisor for cohort '%s'", cohort.Name),
		"Assignment", "Cohort", &body.CohortCohortID, "",
		true,
	)

	c.Json(w, http.StatusOK, "Supervisor assigned successfully", map[string]interface{}{
		"cohort_name": cohort.Name,
		"supervisor":  newSupervisorFullName,
	})
}

// ===============++++===================++============================================
//                          Supervisor Cohort Reassignments
// ===============++++===================++============================================

func (c *Construct) ReassignSupervisorToCohort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortCohortID uint64 `json:"cohort_cohort_id"`
		OldUserID      uint64 `json:"old_user_id"`
		NewUserID      uint64 `json:"new_user_id"`
	}

	// --- Decode request ---
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}
	if body.CohortCohortID == 0 || body.OldUserID == 0 || body.NewUserID == 0 {
		c.Json(w, http.StatusBadRequest, "cohort_cohort_id, old_user_id, and new_user_id are required", nil)
		return
	}

	// --- Authenticate user ---
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, err.Error(), nil)
		return
	}
	if strings.ToLower(currentUser.Role.Name) != "opsadmin" {
		c.Json(w, http.StatusForbidden, "Only OpsAdmins can reassign supervisors", nil)
		return
	}

	// --- Fetch cohort ---
	var cohort models.Cohort
	if err := c.DB.First(&cohort, "cohort_id = ?", body.CohortCohortID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Cohort not found", nil)
		return
	}

	// --- Soft-delete old supervisor assignment ---
	var oldSupervisorAssignment models.CohortUser
	err = c.DB.Where("cohort_cohort_id = ? AND user_user_id = ? AND role = ? AND deleted_at IS NULL",
		body.CohortCohortID, body.OldUserID, "Supervisor").First(&oldSupervisorAssignment).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch old supervisor", map[string]interface{}{"error": err.Error()})
		return
	}
	if err == nil {
		if err := c.DB.Model(&oldSupervisorAssignment).Update("deleted_at", time.Now()).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to deactivate old supervisor", map[string]interface{}{"error": err.Error()})
			return
		}
	}

	// --- Check if new supervisor is already assigned ---
	var existingNew models.CohortUser
	err = c.DB.Where("cohort_cohort_id = ? AND user_user_id = ? AND role = ? AND deleted_at IS NULL",
		body.CohortCohortID, body.NewUserID, "Supervisor").First(&existingNew).Error
	if err == nil {
		c.Json(w, http.StatusConflict, "New supervisor is already assigned to this cohort", nil)
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.Json(w, http.StatusInternalServerError, "Failed to check new supervisor", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Assign new supervisor ---
	newAssign := models.CohortUser{
		CohortID: body.CohortCohortID,
		UserID:   body.NewUserID,
		Role:     "Supervisor",
	}
	if err := c.DB.Create(&newAssign).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to assign new supervisor", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Fetch new supervisor details ---
	var newSupervisor models.User
	c.DB.Preload("Profile").First(&newSupervisor, "user_id = ?", body.NewUserID)
	newSupervisorFullName := strings.TrimSpace(newSupervisor.Profile.FirstName + " " + newSupervisor.Profile.LastName)

	// --- Fetch old supervisor details (if existed) ---
	oldSupervisorFullName := "None"
	if body.OldUserID != 0 {
		var oldSupervisor models.User
		if err := c.DB.Preload("Profile").First(&oldSupervisor, "user_id = ?", body.OldUserID).Error; err == nil {
			oldSupervisorFullName = strings.TrimSpace(oldSupervisor.Profile.FirstName + " " + oldSupervisor.Profile.LastName)
		}
	}

	// --- Notify & track ---
	c.NotifyAndTrack(newSupervisor.UserID, "Supervisor Reassignment",
		fmt.Sprintf("You have been assigned as supervisor for cohort '%s'", cohort.Name),
		"Assignment", "Cohort", &body.CohortCohortID, "",
		true,
	)

	if oldSupervisorFullName != "None" {
		c.NotifyAndTrack(body.OldUserID, "Supervisor Reassignment",
			fmt.Sprintf("You have been unassigned as supervisor from cohort '%s'", cohort.Name),
			"Unassignment", "Cohort", &body.CohortCohortID, "",
			true,
		)
	}

	// --- Audit metadata ---
	metadata := map[string]interface{}{
		"cohort_name":    cohort.Name,
		"old_supervisor": oldSupervisorFullName,
		"new_supervisor": newSupervisorFullName,
		"assigned_by":    currentUser.Username,
	}
	_ = c.LogAudit(currentUser.UserID, "reassign_supervisor", nil, &body.CohortCohortID, nil, metadata)

	// --- Return response ---
	c.Json(w, http.StatusOK, "Supervisor reassigned successfully", map[string]interface{}{
		"cohort_name":    cohort.Name,
		"old_supervisor": oldSupervisorFullName,
		"new_supervisor": newSupervisorFullName,
	})
}

// ===============++++===================++============================================
//	Supervisor Cohort Unassignments
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

	// --- Fetch current supervisor assignment (including soft-deleted) ---
	var supervisorAssignment models.CohortUser
	err = c.DB.Unscoped().Where("cohort_cohort_id = ? AND role = ?", body.CohortCohortID, "Supervisor").First(&supervisorAssignment).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.Json(w, http.StatusNotFound, "No supervisor assigned to this cohort", nil)
			return
		}
		c.Json(w, http.StatusInternalServerError, "Failed to fetch supervisor assignment", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Fetch supervisor details ---
	var supervisor models.User
	supervisorFullName := "Unknown"
	if err := c.DB.Preload("Profile").First(&supervisor, "user_id = ?", supervisorAssignment.UserID).Error; err == nil {
		supervisorFullName = strings.TrimSpace(supervisor.Profile.FirstName + " " + supervisor.Profile.LastName)
	}

	// --- Hard delete supervisor assignment ---
	if err := c.DB.Unscoped().Delete(&supervisorAssignment).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to unassign supervisor", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Notify, audit, and track system activity ---
	c.NotifyAndTrack(supervisor.UserID, "Cohort Unassignment",
		fmt.Sprintf("You have been unassigned as supervisor from cohort '%s'", cohort.Name),
		"Unassignment", "Cohort", &body.CohortCohortID, "",
		true)

	c.Json(w, http.StatusOK, "Supervisor unassigned successfully", map[string]interface{}{
		"cohort_id":   cohort.CohortID,
		"cohort_name": cohort.Name,
		"supervisor": map[string]interface{}{
			"id":   supervisor.UserID,
			"name": supervisorFullName,
		},
	})
}

// ===============++++===================++============================================
//
//	GET Supervisor Cohorts
//
// ===============++++===================++============================================
func (c *Construct) GetCohortSupervisors(w http.ResponseWriter, r *http.Request) {
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Role").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "User not found", nil)
		return
	}

	roleName := strings.ToLower(user.Role.Name)

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

	query := c.DB.Table("cohort_users as cu").
		Select(`
			c.cohort_id,
			c.name as cohort_name,
			u.user_id as supervisor_id,
			COALESCE(CONCAT(up.first_name, ' ', up.last_name), u.username) as supervisor_name,
			u.email as supervisor_email,
			c.created_by as assigned_by_id,
			COALESCE(CONCAT(cp.first_name, ' ', cp.last_name), creator.username) as assigned_by_name,
			cu.created_at as assigned_at
		`).
		Joins("JOIN cohorts c ON c.cohort_id = cu.cohort_cohort_id").
		Joins("JOIN users u ON u.user_id = cu.user_user_id").
		Joins("LEFT JOIN user_profiles up ON up.user_id = u.user_id").
		Joins("LEFT JOIN users creator ON creator.user_uuid = c.created_by").
		Joins("LEFT JOIN user_profiles cp ON cp.user_id = creator.user_id").
		Where("cu.role = ?", "Supervisor")

	switch roleName {
	case "supervisor":
		query = query.Where("cu.user_user_id = ?", user.UserID)
	case "opsadmin", "systemadmin":
		// allowed
	default:
		c.Json(w, http.StatusForbidden, "Not allowed", nil)
		return
	}

	if err := query.Scan(&assignments).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch assignments", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Helper to format names (title case) ---
	formatName := func(name string) string {
		name = strings.ReplaceAll(name, "_", " ")
		name = strings.ToLower(strings.TrimSpace(name))
		words := strings.Fields(name)
		for i, w := range words {
			words[i] = strings.Title(w)
		}
		return strings.Join(words, " ")
	}

	resp := make([]map[string]interface{}, 0, len(assignments))
	for _, a := range assignments {
		supervisorName := formatName(a.SupervisorName)
		assignedByName := formatName(a.AssignedByName)

		record := map[string]interface{}{
			"cohort_id":   a.CohortID,
			"cohort_name": a.CohortName,
			"supervisor": map[string]interface{}{
				"id":    a.SupervisorID,
				"name":  supervisorName,
				"email": a.SupervisorEmail,
			},
		}

		if roleName != "supervisor" {
			record["assigned_by"] = map[string]interface{}{
				"id":   a.AssignedByID,
				"name": assignedByName,
			}
			record["assigned_at"] = a.AssignedAt
		}

		resp = append(resp, record)
	}

	c.Json(w, http.StatusOK, "Cohort supervisors fetched successfully", map[string]interface{}{
		"assignments": resp,
	})

	if roleName == "opsadmin" || roleName == "systemadmin" {
		c.NotifyAndTrack(
			user.UserID,
			"Viewed Cohort Supervisors",
			fmt.Sprintf("%s viewed cohort-supervisor assignments", user.Username),
			"Review Access",
			"CohortSupervisor",
			nil,
			"",
			false,
		)
	}
}

// ===============++++===================++============================================
//                   	Supervisor Mentors Assignments
// ===============++++===================++============================================
