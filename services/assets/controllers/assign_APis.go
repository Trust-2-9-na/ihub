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
func (c *Construct) AssignSupervisorsToCohort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortCohortID uint64   `json:"cohort_cohort_id"`
		UserUserIDs    []uint64 `json:"user_user_ids"` // multiple supervisors
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}
	if body.CohortCohortID == 0 || len(body.UserUserIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "cohort_cohort_id and user_user_ids are required", nil)
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

	assignedSupervisors := []string{}
	conflicts := []string{}

	for _, userID := range body.UserUserIDs {
		// Skip if already assigned
		var existing models.CohortUser
		err = c.DB.Unscoped().Where("cohort_cohort_id = ? AND role = ? AND user_user_id = ?", body.CohortCohortID, "Supervisor", userID).First(&existing).Error
		if err == nil {
			conflicts = append(conflicts, fmt.Sprintf("%d", userID))
			continue
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			c.Json(w, http.StatusInternalServerError, "Failed to check supervisor", map[string]interface{}{"error": err.Error()})
			return
		}

		// Fetch supervisor details
		var supervisor models.User
		if err := c.DB.Preload("Profile").First(&supervisor, "user_id = ?", userID).Error; err != nil {
			conflicts = append(conflicts, fmt.Sprintf("%d", userID))
			continue
		}
		fullName := strings.TrimSpace(supervisor.Profile.FirstName + " " + supervisor.Profile.LastName)

		// Assign supervisor
		assign := models.CohortUser{
			UserCohortID: body.CohortCohortID,
			MemberID:     userID,
			Role:         "Supervisor",
		}
		if err := c.DB.Create(&assign).Error; err != nil {
			conflicts = append(conflicts, fullName)
			continue
		}

		// Notify
		c.NotifyAndTrack(supervisor.UserID, "Cohort Assignment",
			fmt.Sprintf("You have been assigned as supervisor for cohort '%s'", cohort.Name),
			"Assignment", "Cohort", &body.CohortCohortID, "", true,
		)

		assignedSupervisors = append(assignedSupervisors, fullName)
	}

	c.Json(w, http.StatusOK, "Supervisors assignment completed", map[string]interface{}{
		"cohort_name":          cohort.Name,
		"assigned_supervisors": assignedSupervisors,
		"skipped_or_conflicts": conflicts,
	})
}

// ===============++++===================++============================================
//                          Supervisor Cohort Reassignments
// ===============++++===================++============================================

func (c *Construct) ReassignSupervisorsToCohort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortCohortID uint64   `json:"cohort_cohort_id"`
		OldUserIDs     []uint64 `json:"old_user_ids"`
		NewUserIDs     []uint64 `json:"new_user_ids"`
	}

	// Decode request
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}

	if body.CohortCohortID == 0 || len(body.OldUserIDs) == 0 || len(body.NewUserIDs) == 0 || len(body.OldUserIDs) != len(body.NewUserIDs) {
		c.Json(w, http.StatusBadRequest, "cohort_cohort_id, old_user_ids, and new_user_ids are required and must have the same length", nil)
		return
	}

	// Authenticate
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, err.Error(), nil)
		return
	}
	if strings.ToLower(currentUser.Role.Name) != "opsadmin" {
		c.Json(w, http.StatusForbidden, "Only OpsAdmins can reassign supervisors", nil)
		return
	}

	var cohort models.Cohort
	if err := c.DB.First(&cohort, "cohort_id = ?", body.CohortCohortID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Cohort not found", nil)
		return
	}

	assigned := []string{}
	unassigned := []string{}
	conflicts := []string{}

	for i := range body.OldUserIDs {
		oldID := body.OldUserIDs[i]
		newID := body.NewUserIDs[i]

		// Soft-delete old supervisor assignment
		var oldAssign models.CohortUser
		err := c.DB.Where("cohort_cohort_id = ? AND user_user_id = ? AND role = ? AND deleted_at IS NULL",
			body.CohortCohortID, oldID, "Supervisor").First(&oldAssign).Error
		if err == nil {
			c.DB.Model(&oldAssign).Update("deleted_at", time.Now())
			// Notify old supervisor
			c.NotifyAndTrack(oldID, "Supervisor Reassignment",
				fmt.Sprintf("You have been unassigned as supervisor from cohort '%s'", cohort.Name),
				"Unassignment", "Cohort", &body.CohortCohortID, "", true,
			)
			var oldUser models.User
			if err := c.DB.Preload("Profile").First(&oldUser, "user_id = ?", oldID).Error; err == nil {
				unassigned = append(unassigned, strings.TrimSpace(oldUser.Profile.FirstName+" "+oldUser.Profile.LastName))
			}
		}

		// Check if new supervisor already exists
		var existingNew models.CohortUser
		err = c.DB.Where("cohort_cohort_id = ? AND user_user_id = ? AND role = ? AND deleted_at IS NULL",
			body.CohortCohortID, newID, "Supervisor").First(&existingNew).Error
		if err == nil {
			conflicts = append(conflicts, fmt.Sprintf("%d", newID))
			continue
		}

		// Assign new supervisor
		newAssign := models.CohortUser{
			UserCohortID: body.CohortCohortID,
			MemberID:     newID,
			Role:         "Supervisor",
		}
		if err := c.DB.Create(&newAssign).Error; err != nil {
			conflicts = append(conflicts, fmt.Sprintf("%d", newID))
			continue
		}

		// Notify new supervisor
		var newUser models.User
		c.DB.Preload("Profile").First(&newUser, "user_id = ?", newID)
		c.NotifyAndTrack(newID, "Supervisor Reassignment",
			fmt.Sprintf("You have been assigned as supervisor for cohort '%s'", cohort.Name),
			"Assignment", "Cohort", &body.CohortCohortID, "", true,
		)
		assigned = append(assigned, strings.TrimSpace(newUser.Profile.FirstName+" "+newUser.Profile.LastName))
	}

	c.Json(w, http.StatusOK, "Supervisors reassignment completed", map[string]interface{}{
		"cohort_name": cohort.Name,
		"assigned":    assigned,
		"unassigned":  unassigned,
		"conflicts":   conflicts,
	})
}

// ===============++++===================++============================================
//	Supervisor Cohort Unassignments
// ===============++++===================++============================================

func (c *Construct) UnassignSupervisorsFromCohort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortCohortID uint64   `json:"cohort_cohort_id"`
		UserIDs        []uint64 `json:"user_ids"` // IDs of supervisors to unassign
	}

	// --- Decode request body ---
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}
	if body.CohortCohortID == 0 || len(body.UserIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "cohort_cohort_id and user_ids are required", nil)
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

	unassigned := []string{}
	notFound := []string{}

	for _, uid := range body.UserIDs {
		var supervisorAssignment models.CohortUser
		err := c.DB.Unscoped().Where("cohort_cohort_id = ? AND user_user_id = ? AND role = ?", body.CohortCohortID, uid, "Supervisor").First(&supervisorAssignment).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				notFound = append(notFound, fmt.Sprintf("%d", uid))
				continue
			}
			c.Json(w, http.StatusInternalServerError, "Failed to fetch supervisor assignment", map[string]interface{}{"error": err.Error()})
			return
		}

		// Fetch supervisor details
		var supervisor models.User
		supervisorFullName := "Unknown"
		if err := c.DB.Preload("Profile").First(&supervisor, "user_id = ?", supervisorAssignment.MemberID).Error; err == nil {
			supervisorFullName = strings.TrimSpace(supervisor.Profile.FirstName + " " + supervisor.Profile.LastName)
		}

		// Hard delete
		if err := c.DB.Unscoped().Delete(&supervisorAssignment).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to unassign supervisor", map[string]interface{}{"error": err.Error()})
			return
		}

		// Notify
		c.NotifyAndTrack(supervisor.UserID, "Cohort Unassignment",
			fmt.Sprintf("You have been unassigned as supervisor from cohort '%s'", cohort.Name),
			"Unassignment", "Cohort", &body.CohortCohortID, "",
			true,
		)

		unassigned = append(unassigned, supervisorFullName)
	}

	c.Json(w, http.StatusOK, "Supervisors unassigned successfully", map[string]interface{}{
		"cohort_id":   cohort.CohortID,
		"cohort_name": cohort.Name,
		"unassigned":  unassigned,
		"not_found":   notFound,
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
