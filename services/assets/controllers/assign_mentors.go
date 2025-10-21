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

//============================"""""""""======================="""""""""======================
//                           Assigning Mentors to a Cohort
//============================"""""""""======================="""""""""======================

func (c *Construct) AssignMentorToCohort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortID uint64 `json:"cohort_id"`
		MentorID uint64 `json:"mentor_id"`
	}

	// Decode request
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}
	if body.CohortID == 0 || body.MentorID == 0 {
		c.Json(w, http.StatusBadRequest, "cohort_id and mentor_id are required", nil)
		return
	}

	// Get logged-in supervisor
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, err.Error(), nil)
		return
	}
	if strings.ToLower(currentUser.Role.Name) != "supervisor" {
		c.Json(w, http.StatusForbidden, "Only supervisors can assign mentors", nil)
		return
	}

	// Check cohort exists
	var cohort models.Cohort
	if err := c.DB.First(&cohort, "cohort_id = ?", body.CohortID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Cohort not found", nil)
		return
	}

	// Check if mentor already assigned
	var existing models.CohortUser
	err = c.DB.Where("cohort_cohort_id = ? AND user_user_id = ? AND role = ?", body.CohortID, body.MentorID, "Mentor").
		First(&existing).Error
	if err == nil {
		c.Json(w, http.StatusConflict, "Mentor already assigned to this cohort", map[string]interface{}{
			"cohort_id": body.CohortID,
			"mentor_id": body.MentorID,
			"message":   "Use reassign/unassign API to change mentor assignment",
		})
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.Json(w, http.StatusInternalServerError, "Failed to check mentor", map[string]interface{}{"error": err.Error()})
		return
	}

	// Assign mentor
	assign := models.CohortUser{
		UserCohortID: body.CohortID,
		MemberID:     body.MentorID,
		Role:         "Mentor",
		CreatedBy:    &currentUser.UserID,
	}
	if err := c.DB.Create(&assign).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to assign mentor", map[string]interface{}{"error": err.Error()})
		return
	}

	// Notify & Audit
	c.NotifyAndTrack(body.MentorID, "Mentor Assignment",
		fmt.Sprintf("You have been assigned to cohort '%s'", cohort.Name),
		"Assignment", "Cohort", &body.CohortID, "",
		true,
	)

	c.Json(w, http.StatusOK, "Mentor assigned successfully", map[string]interface{}{
		"cohort_id": body.CohortID,
		"mentor_id": body.MentorID,
	})
}

//============================"""""""""======================="""""""""======================
//                        Reassigning mentors to a Cohort
//============================"""""""""======================="""""""""======================

func (c *Construct) ReassignMentor(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortID    uint64 `json:"cohort_id"`
		OldMentorID uint64 `json:"old_mentor_id"`
		NewMentorID uint64 `json:"new_mentor_id"`
	}

	// Decode JSON
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}

	// Get current user
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, err.Error(), nil)
		return
	}

	// Role validation
	if strings.ToLower(currentUser.Role.Name) != "supervisor" {
		c.Json(w, http.StatusForbidden, "Only supervisors can reassign mentors", nil)
		return
	}

	// Start DB transaction
	tx := c.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			c.Json(w, http.StatusInternalServerError, "Unexpected system error", map[string]interface{}{"panic": r})
		}
	}()

	// 🔹 1️⃣ Soft delete existing mentor assignment
	var oldAssignment models.CohortUser
	if err := tx.Where(
		"cohort_cohort_id = ? AND user_user_id = ? AND role = ?",
		body.CohortID, body.OldMentorID, "Mentor",
	).First(&oldAssignment).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.Json(w, http.StatusNotFound, "Old mentor not found in this cohort", nil)
		} else {
			c.Json(w, http.StatusInternalServerError, "Failed to fetch old mentor assignment", map[string]interface{}{"error": err.Error()})
		}
		return
	}

	if err := tx.Delete(&oldAssignment).Error; err != nil {
		tx.Rollback()
		c.Json(w, http.StatusInternalServerError, "Failed to soft delete old mentor", map[string]interface{}{"error": err.Error()})
		return
	}

	// 🔹 2️⃣ Assign new mentor
	assign := models.CohortUser{
		UserCohortID: body.CohortID,
		MemberID:     body.NewMentorID,
		Role:         "Mentor",
		CreatedBy:    &currentUser.UserID,
	}

	if err := tx.Create(&assign).Error; err != nil {
		tx.Rollback()
		c.Json(w, http.StatusInternalServerError, "Failed to assign new mentor", map[string]interface{}{"error": err.Error()})
		return
	}

	// 🔹 3️⃣ Commit transaction
	if err := tx.Commit().Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Transaction commit failed", map[string]interface{}{"error": err.Error()})
		return
	}

	// 🔹 4️⃣ Notifications and audit tracking
	c.NotifyAndTrack(
		body.OldMentorID,
		"Mentor Reassignment",
		fmt.Sprintf("You have been removed from cohort %d by %s", body.CohortID, currentUser.Username),
		"Reassignment", "Cohort", &body.CohortID, "Completed",
		true,
	)

	c.NotifyAndTrack(
		body.NewMentorID,
		"Mentor Reassignment",
		fmt.Sprintf("You have been assigned to cohort %d by %s", body.CohortID, currentUser.Username),
		"Reassignment", "Cohort", &body.CohortID, "Completed",
		true,
	)

	// 🔹 5️⃣ Final response
	c.Json(w, http.StatusOK, "Mentor reassigned successfully", map[string]interface{}{
		"cohort_id":     body.CohortID,
		"old_mentor_id": body.OldMentorID,
		"new_mentor_id": body.NewMentorID,
		"assigned_by":   currentUser.Username,
	})
}

//============================"""""""""======================="""""""""======================
//                        Unassigning Mentors to a Cohort
//============================"""""""""======================="""""""""======================

func (c *Construct) UnassignMentor(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortID uint64 `json:"cohort_id"`
		MentorID uint64 `json:"mentor_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}

	_, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, err.Error(), nil)
		return
	}

	tx := c.DB.Begin()
	if err := tx.Unscoped().Where("cohort_cohort_id = ? AND user_user_id = ? AND role = ?", body.CohortID, body.MentorID, "Mentor").
		Delete(&models.CohortUser{}).Error; err != nil {
		tx.Rollback()
		c.Json(w, http.StatusInternalServerError, "Failed to unassign mentor", map[string]interface{}{"error": err.Error()})
		return
	}
	tx.Commit()

	c.NotifyAndTrack(body.MentorID, "Mentor Unassigned",
		fmt.Sprintf("You have been unassigned from cohort '%d'", body.CohortID),
		"Unassignment", "Cohort", &body.CohortID, "",
		true,
	)

	c.Json(w, http.StatusOK, "Mentor unassigned successfully", map[string]interface{}{
		"cohort_id": body.CohortID,
		"mentor_id": body.MentorID,
	})
}

//============================"""""""""======================="""""""""======================
//                      Retrieving Mentors Cohorts Unified API
//============================"""""""""======================="""""""""======================

func (c *Construct) GetCohortMentors(w http.ResponseWriter, r *http.Request) {
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

	// --- Query structure ---
	var assignments []struct {
		CohortID       uint64
		CohortName     string
		MentorID       uint64
		MentorName     string
		MentorEmail    string
		SupervisorID   uint64
		SupervisorName string
		AssignedByID   uint64
		AssignedByName string
		AssignedAt     time.Time
	}

	query := c.DB.Table("cohort_users as cu").
		Select(`
			c.cohort_id,
			c.name as cohort_name,
			u.user_id as mentor_id,
			COALESCE(CONCAT(up.first_name, ' ', up.last_name), u.username) as mentor_name,
			u.email as mentor_email,
			sup.user_id as supervisor_id,
			COALESCE(CONCAT(sp.first_name, ' ', sp.last_name), sup.username) as supervisor_name,
			assigner.user_id as assigned_by_id,
			COALESCE(CONCAT(ap.first_name, ' ', ap.last_name), assigner.username) as assigned_by_name,
			cu.created_at as assigned_at
		`).
		Joins("JOIN cohorts c ON c.cohort_id = cu.cohort_cohort_id").
		Joins("JOIN users u ON u.user_id = cu.user_user_id").
		Joins("LEFT JOIN user_profiles up ON up.user_id = u.user_id").
		Joins("LEFT JOIN cohort_users su ON su.cohort_cohort_id = c.cohort_id AND su.role = 'Supervisor'").
		Joins("LEFT JOIN users sup ON sup.user_id = su.user_user_id").
		Joins("LEFT JOIN user_profiles sp ON sp.user_id = sup.user_id").
		Joins("LEFT JOIN users assigner ON assigner.user_id = cu.created_by").
		Joins("LEFT JOIN user_profiles ap ON ap.user_id = assigner.user_id").
		Where("cu.role = ?", "Mentor")

	// --- Role-based visibility ---
	switch roleName {
	case "mentor":
		// Mentor sees cohorts they belong to
		query = query.Where("cu.user_user_id = ?", user.UserID)

	case "supervisor":
		// Supervisor sees mentors in cohorts they supervise
		query = query.Where("c.cohort_id IN (?)",
			c.DB.Table("cohort_users").Select("cohort_cohort_id").
				Where("user_user_id = ? AND role = ?", user.UserID, "Supervisor"),
		)

	case "opsadmin", "systemadmin":
		// Full visibility
	default:
		c.Json(w, http.StatusForbidden, "Access denied", nil)
		return
	}

	// --- Execute query ---
	if err := query.Scan(&assignments).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch mentor assignments", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Helper for formatting names ---
	formatName := func(name string) string {
		name = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(name, "_", " ")))
		words := strings.Fields(name)
		for i, w := range words {
			words[i] = strings.Title(w)
		}
		return strings.Join(words, " ")
	}

	// --- Build response ---
	resp := make([]map[string]interface{}, 0, len(assignments))
	for _, a := range assignments {
		mentorName := formatName(a.MentorName)
		supervisorName := formatName(a.SupervisorName)
		assignedByName := formatName(a.AssignedByName)

		record := map[string]interface{}{
			"cohort_id":   a.CohortID,
			"cohort_name": a.CohortName,
			"mentor": map[string]interface{}{
				"id":    a.MentorID,
				"name":  mentorName,
				"email": a.MentorEmail,
			},
			"supervisor": map[string]interface{}{
				"id":   a.SupervisorID,
				"name": supervisorName,
			},
		}

		// Only admins/supervisors can see assigner info
		if roleName != "mentor" {
			record["assigned_by"] = map[string]interface{}{
				"id":   a.AssignedByID,
				"name": assignedByName,
			}
			record["assigned_at"] = a.AssignedAt
		}

		resp = append(resp, record)
	}

	c.Json(w, http.StatusOK, "Mentor assignments fetched successfully", map[string]interface{}{
		"assignments": resp,
	})

	// --- Log & Notify ---
	if roleName == "opsadmin" || roleName == "systemadmin" {
		c.NotifyAndTrack(
			user.UserID,
			"Viewed Mentor Assignments",
			fmt.Sprintf("%s viewed all mentor–cohort assignments", user.Username),
			"Review Access",
			"CohortMentor",
			nil,
			"",
			false,
		)
	} else if roleName == "supervisor" {
		c.NotifyAndTrack(
			user.UserID,
			"Viewed Mentor Assignments",
			fmt.Sprintf("%s viewed mentor assignments for their cohorts", user.Username),
			"Review Access",
			"CohortMentor",
			nil,
			"",
			false,
		)
	}
}
