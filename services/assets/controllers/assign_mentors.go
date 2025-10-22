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

func (c *Construct) AssignMentorsToCohort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortID  uint64   `json:"cohort_id"`
		MentorIDs []uint64 `json:"mentor_ids"` // multiple mentors
	}

	// Decode request
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}
	if body.CohortID == 0 || len(body.MentorIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "cohort_id and mentor_ids are required", nil)
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

	assigned := []string{}
	conflicts := []string{}

	for _, mid := range body.MentorIDs {
		// Check if mentor already assigned
		var existing models.CohortUser
		err := c.DB.Where("cohort_cohort_id = ? AND user_user_id = ? AND role = ?", body.CohortID, mid, "Mentor").
			First(&existing).Error
		if err == nil {
			conflicts = append(conflicts, fmt.Sprintf("%d", mid))
			continue
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			c.Json(w, http.StatusInternalServerError, "Failed to check mentor", map[string]interface{}{"error": err.Error()})
			return
		}

		// Assign mentor
		assign := models.CohortUser{
			UserCohortID: body.CohortID,
			MemberID:     mid,
			Role:         "Mentor",
			CreatedBy:    &currentUser.UserID,
		}
		if err := c.DB.Create(&assign).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to assign mentor", map[string]interface{}{"error": err.Error()})
			return
		}

		// Notify
		c.NotifyAndTrack(mid, "Mentor Assignment",
			fmt.Sprintf("You have been assigned to cohort '%s'", cohort.Name),
			"Assignment", "Cohort", &body.CohortID, "",
			true,
		)

		assigned = append(assigned, fmt.Sprintf("%d", mid))
	}

	c.Json(w, http.StatusOK, "Mentors assignment processed", map[string]interface{}{
		"cohort_id": body.CohortID,
		"assigned":  assigned,
		"conflicts": conflicts,
	})
}

//============================"""""""""======================="""""""""======================
//                        Reassigning mentors to a Cohort
//============================"""""""""======================="""""""""======================

func (c *Construct) ReassignMentors(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortID     uint64   `json:"cohort_id"`
		OldMentorIDs []uint64 `json:"old_mentor_ids"` // mentors to remove
		NewMentorIDs []uint64 `json:"new_mentor_ids"` // mentors to assign
	}

	// Decode JSON
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}

	if body.CohortID == 0 || len(body.OldMentorIDs) == 0 || len(body.NewMentorIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "cohort_id, old_mentor_ids, and new_mentor_ids are required", nil)
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

	removed := []uint64{}
	added := []uint64{}
	conflicts := []uint64{}

	// 1️⃣ Remove old mentors
	for _, oldID := range body.OldMentorIDs {
		var oldAssignment models.CohortUser
		err := tx.Where("cohort_cohort_id = ? AND user_user_id = ? AND role = ?", body.CohortID, oldID, "Mentor").
			First(&oldAssignment).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue // skip if not found
		} else if err != nil {
			tx.Rollback()
			c.Json(w, http.StatusInternalServerError, "Failed to fetch old mentor", map[string]interface{}{"error": err.Error()})
			return
		}

		if err := tx.Delete(&oldAssignment).Error; err != nil {
			tx.Rollback()
			c.Json(w, http.StatusInternalServerError, "Failed to remove old mentor", map[string]interface{}{"error": err.Error()})
			return
		}
		removed = append(removed, oldID)

		// Notify old mentor
		c.NotifyAndTrack(oldID, "Mentor Reassignment",
			fmt.Sprintf("You have been removed from cohort %d by %s", body.CohortID, currentUser.Username),
			"Reassignment", "Cohort", &body.CohortID, "Completed",
			true,
		)
	}

	// 2️⃣ Assign new mentors
	for _, newID := range body.NewMentorIDs {
		var existing models.CohortUser
		err := tx.Where("cohort_cohort_id = ? AND user_user_id = ? AND role = ?", body.CohortID, newID, "Mentor").
			First(&existing).Error
		if err == nil {
			conflicts = append(conflicts, newID) // already assigned
			continue
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			tx.Rollback()
			c.Json(w, http.StatusInternalServerError, "Failed to check new mentor", map[string]interface{}{"error": err.Error()})
			return
		}

		assign := models.CohortUser{
			UserCohortID: body.CohortID,
			MemberID:     newID,
			Role:         "Mentor",
			CreatedBy:    &currentUser.UserID,
		}
		if err := tx.Create(&assign).Error; err != nil {
			tx.Rollback()
			c.Json(w, http.StatusInternalServerError, "Failed to assign new mentor", map[string]interface{}{"error": err.Error()})
			return
		}
		added = append(added, newID)

		// Notify new mentor
		c.NotifyAndTrack(newID, "Mentor Reassignment",
			fmt.Sprintf("You have been assigned to cohort %d by %s", body.CohortID, currentUser.Username),
			"Reassignment", "Cohort", &body.CohortID, "Completed",
			true,
		)
	}

	// 3️⃣ Commit transaction
	if err := tx.Commit().Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Transaction commit failed", map[string]interface{}{"error": err.Error()})
		return
	}

	// 4️⃣ Response
	c.Json(w, http.StatusOK, "Mentor reassignment processed", map[string]interface{}{
		"cohort_id":   body.CohortID,
		"removed":     removed,
		"added":       added,
		"conflicts":   conflicts,
		"assigned_by": currentUser.Username,
	})
}

// ============================"""""""""======================="""""""""======================
//
//	Unassigning Mentors to a Cohort
//
// ============================"""""""""======================="""""""""======================
func (c *Construct) UnassignMentors(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortID  uint64   `json:"cohort_id"`
		MentorIDs []uint64 `json:"mentor_ids"` // support multiple mentors
	}

	// Decode JSON
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}
	if body.CohortID == 0 || len(body.MentorIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "cohort_id and mentor_ids are required", nil)
		return
	}

	// Authenticate user
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, err.Error(), nil)
		return
	}

	if strings.ToLower(currentUser.Role.Name) != "supervisor" {
		c.Json(w, http.StatusForbidden, "Only supervisors can unassign mentors", nil)
		return
	}

	// Start transaction
	tx := c.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			c.Json(w, http.StatusInternalServerError, "Unexpected system error", map[string]interface{}{"panic": r})
		}
	}()

	unassigned := []uint64{}
	notFound := []uint64{}

	// Loop through mentors to unassign
	for _, mentorID := range body.MentorIDs {
		result := tx.Unscoped().Where("cohort_cohort_id = ? AND user_user_id = ? AND role = ?", body.CohortID, mentorID, "Mentor").
			Delete(&models.CohortUser{})
		if result.Error != nil {
			tx.Rollback()
			c.Json(w, http.StatusInternalServerError, "Failed to unassign mentor", map[string]interface{}{"error": result.Error.Error()})
			return
		}

		if result.RowsAffected > 0 {
			unassigned = append(unassigned, mentorID)
			// Notify each mentor
			c.NotifyAndTrack(mentorID, "Mentor Unassigned",
				fmt.Sprintf("You have been unassigned from cohort '%d' by %s", body.CohortID, currentUser.Username),
				"Unassignment", "Cohort", &body.CohortID, "",
				true,
			)
		} else {
			notFound = append(notFound, mentorID)
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Transaction commit failed", map[string]interface{}{"error": err.Error()})
		return
	}

	// Final response
	c.Json(w, http.StatusOK, "Mentor unassignment processed", map[string]interface{}{
		"cohort_id":    body.CohortID,
		"unassigned":   unassigned,
		"not_found":    notFound,
		"processed_by": currentUser.Username,
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
