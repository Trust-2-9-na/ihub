package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"web/services/assets/models"

	"gorm.io/gorm"
)

// Assigning Students to Mentors

func (c *Construct) AssignStudentsToMentor(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortID   uint64   `json:"cohort_id"`
		MentorID   uint64   `json:"mentor_id"`
		StudentIDs []uint64 `json:"student_ids"`
	}

	// Decode request
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}
	if body.CohortID == 0 || body.MentorID == 0 || len(body.StudentIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "cohort_id, mentor_id, and student_ids are required", nil)
		return
	}

	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, err.Error(), nil)
		return
	}

	// Optional: Check supervisor access to cohort
	var sup models.CohortUser
	err = c.DB.Where("cohort_cohort_id = ? AND user_user_id = ? AND role = ?", body.CohortID, currentUser.UserID, "Supervisor").
		First(&sup).Error
	if err != nil {
		c.Json(w, http.StatusForbidden, "Only supervisors assigned to this cohort can assign students", nil)
		return
	}

	tx := c.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			c.Json(w, http.StatusInternalServerError, "Unexpected error", map[string]interface{}{"panic": r})
		}
	}()

	assigned := []uint64{}
	skipped := []uint64{}

	for _, studentID := range body.StudentIDs {
		var existing models.MentorStudentAssignment
		err := tx.Where("cohort_ref_id = ? AND mentor_ref_id = ? AND student_ref_id = ?",
			body.CohortID, body.MentorID, studentID).First(&existing).Error
		if err == nil {
			skipped = append(skipped, studentID)
			continue
		} else if err != nil && err != gorm.ErrRecordNotFound {
			tx.Rollback()
			c.Json(w, http.StatusInternalServerError, "Failed to check existing assignment", map[string]interface{}{"error": err.Error()})
			return
		}

		assign := models.MentorStudentAssignment{
			CohortRefID:  body.CohortID,
			MentorRefID:  body.MentorID,
			StudentRefID: studentID,
			CreatedBy:    &currentUser.UserID,
		}

		if err := tx.Create(&assign).Error; err != nil {
			tx.Rollback()
			c.Json(w, http.StatusInternalServerError, "Failed to assign student", map[string]interface{}{"error": err.Error()})
			return
		}

		assigned = append(assigned, studentID)

		// Notify student
		c.NotifyAndTrack(studentID, "Mentor Assignment",
			fmt.Sprintf("You have been assigned to mentor '%d' in cohort '%d'", body.MentorID, body.CohortID),
			"Assignment", "Cohort", &body.CohortID, "", true)
	}

	if err := tx.Commit().Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Transaction commit failed", map[string]interface{}{"error": err.Error()})
		return
	}

	c.Json(w, http.StatusOK, "Students assigned to mentor successfully", map[string]interface{}{
		"cohort_id": body.CohortID,
		"mentor_id": body.MentorID,
		"assigned":  assigned,
		"skipped":   skipped,
	})
}

// Assigning Supervisors to Teams

func (c *Construct) AssignSupervisorsToTeam(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortID      uint64   `json:"cohort_id"`
		TeamName      string   `json:"team_name"`
		SupervisorIDs []uint64 `json:"supervisor_ids,omitempty"`
	}

	// --- Decode request ---
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}
	if body.CohortID == 0 || body.TeamName == "" {
		c.Json(w, http.StatusBadRequest, "cohort_id and team_name are required", nil)
		return
	}

	// --- Get authenticated user ---
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// --- Check if user is a supervisor of this cohort ---
	var cohortSupervisor models.CohortUser
	if err := c.DB.Where("cohort_cohort_id = ? AND user_user_id = ? AND role = ?", body.CohortID, currentUser.UserID, "Supervisor").
		First(&cohortSupervisor).Error; err != nil {
		c.Json(w, http.StatusForbidden, "Only supervisors assigned to this cohort can create teams", nil)
		return
	}

	// --- Start transaction ---
	tx := c.DB.Begin()

	// --- Create team ---
	team := models.Team{
		Name:        body.TeamName,
		CohortRefID: &body.CohortID,
		CreatedByID: currentUser.UserID,
	}
	if err := tx.Create(&team).Error; err != nil {
		tx.Rollback()
		c.Json(w, http.StatusInternalServerError, "Failed to create team", map[string]interface{}{"error": err.Error()})
		return
	}

	assignedSupervisors := []uint64{}

	// --- Assign creator supervisor as team leader ---
	leaderAssignment := models.UserTeam{
		TeamRefID: team.TeamID,
		UserRefID: currentUser.UserID,
		Role:      "Supervisor",
		JoinedAt:  time.Now(),
	}
	if err := tx.Create(&leaderAssignment).Error; err != nil {
		tx.Rollback()
		c.Json(w, http.StatusInternalServerError, "Failed to assign creator as supervisor", map[string]interface{}{"error": err.Error()})
		return
	}
	assignedSupervisors = append(assignedSupervisors, currentUser.UserID)

	// --- Assign additional supervisors if provided ---
	for _, supID := range body.SupervisorIDs {
		if supID == currentUser.UserID {
			continue
		}

		// Check if supervisor is part of this cohort
		var supCheck models.CohortUser
		if err := tx.Where("cohort_cohort_id = ? AND user_user_id = ? AND role = ?", body.CohortID, supID, "Supervisor").
			First(&supCheck).Error; err != nil {
			continue
		}

		assignment := models.UserTeam{
			TeamRefID: team.TeamID,
			UserRefID: supID,
			Role:      "Supervisor",
			JoinedAt:  time.Now(),
		}
		if err := tx.Create(&assignment).Error; err != nil {
			tx.Rollback()
			c.Json(w, http.StatusInternalServerError, "Failed to assign supervisor to team", map[string]interface{}{"error": err.Error()})
			return
		}
		assignedSupervisors = append(assignedSupervisors, supID)
	}

	tx.Commit()

	// --- Notify assigned supervisors ---
	for _, supID := range assignedSupervisors {
		c.NotifyAndTrack(supID, "Team Assignment",
			fmt.Sprintf("You have been assigned to team '%s' in cohort %d", team.Name, body.CohortID),
			"Assignment", "Team", &team.TeamID, "", true)
	}

	c.Json(w, http.StatusCreated, "Team created successfully", map[string]interface{}{
		"team_id":              team.TeamID,
		"team_name":            team.Name,
		"cohort_id":            body.CohortID,
		"assigned_supervisors": assignedSupervisors,
	})
}
