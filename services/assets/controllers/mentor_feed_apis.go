package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"web/services/assets/models"

	"github.com/gorilla/mux"
)

// GetSupervisorCohortIDs returns all cohort IDs that a supervisor manages
func (c *Construct) GetSupervisorCohortIDs(supervisorID uint64) []uint64 {
	var cohortIDs []uint64
	c.DB.Model(&models.Cohort{}).
		Where("supervisor_id = ?", supervisorID). // or whatever your schema uses
		Pluck("id", &cohortIDs)
	return cohortIDs
}

// generate a feedback by mentor

func (c *Construct) CreateMentorFeedback(w http.ResponseWriter, r *http.Request) {
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// Ensure user is a mentor
	if strings.ToLower(user.Role.Name) != "mentor" {
		c.Json(w, http.StatusForbidden, "Only mentors can create feedback", nil)
		return
	}

	var input struct {
		ReportID       *uint64  `json:"report_id,omitempty"`
		ItemID         *uint64  `json:"item_id,omitempty"`
		StudentID      uint64   `json:"student_id"`
		Comment        string   `json:"comment"`
		Rating         *float64 `json:"rating,omitempty"`
		Recommendation string   `json:"recommendation"`
		IsPublic       bool     `json:"is_public"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON payload", nil)
		return
	}

	if input.ReportID == nil && input.ItemID == nil {
		c.Json(w, http.StatusBadRequest, "Either report_id or item_id must be provided", nil)
		return
	}

	feedback := models.MentorFeedback{
		MentorID:       user.UserID,
		ReportID:       input.ReportID,
		ItemID:         input.ItemID,
		StudentID:      input.StudentID,
		Comment:        input.Comment,
		Rating:         input.Rating,
		Recommendation: input.Recommendation,
		IsPublic:       input.IsPublic,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := c.DB.Create(&feedback).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create mentor feedback", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Notifications & audit/job tracking ---

	// 1️⃣ Notify the student if feedback is public
	if feedback.IsPublic {
		c.NotifyAndTrack(
			feedback.StudentID,
			"New Mentor Feedback",
			fmt.Sprintf("You have received new feedback from mentor %s %s.", user.Profile.FirstName, user.Profile.LastName),
			"Mentor Feedback",
			"MentorFeedback",
			&feedback.ID,
			"",
			true,
		)

		// Optional: create a job/audit record for student
		// c.CreateJob(feedback.StudentID, "New feedback available", ...)
	}

	// 2️⃣ Notify the supervisor(s) associated with the student
	// Assuming you have a method to fetch supervisors for this student
	supervisorIDs := c.GetSupervisorsForStudent(feedback.StudentID)
	for _, supID := range supervisorIDs {
		c.NotifyAndTrack(
			supID,
			"New Mentor Feedback Added",
			fmt.Sprintf("Mentor %s %s added feedback for student ID %d.", user.Profile.FirstName, user.Profile.LastName, feedback.StudentID),
			"Mentor Feedback",
			"MentorFeedback",
			&feedback.ID,
			"",
			true,
		)

		// Optional: create audit/job entry for supervisor
		// c.CreateJob(supID, "Mentor feedback added", ...)
	}

	// Build response
	response := map[string]interface{}{
		"id":             feedback.ID,
		"mentor_id":      feedback.MentorID,
		"mentor_name":    user.Profile.FirstName + " " + user.Profile.LastName,
		"student_id":     feedback.StudentID,
		"report_id":      feedback.ReportID,
		"item_id":        feedback.ItemID,
		"comment":        feedback.Comment,
		"rating":         feedback.Rating,
		"recommendation": feedback.Recommendation,
		"is_public":      feedback.IsPublic,
		"created_at":     feedback.CreatedAt,
		"updated_at":     feedback.UpdatedAt,
	}

	c.Json(w, http.StatusOK, "Mentor feedback created successfully", map[string]interface{}{
		"data": response,
	})
}

// updating the feedback

func (c *Construct) UpdateMentorFeedback(w http.ResponseWriter, r *http.Request) {
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	feedbackIDStr := mux.Vars(r)["id"]
	feedbackID, _ := strconv.ParseUint(feedbackIDStr, 10, 64)

	var feedback models.MentorFeedback
	if err := c.DB.Preload("Student.Profile").First(&feedback, feedbackID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Feedback not found", nil)
		return
	}

	// Only the original mentor can update
	if feedback.MentorID != user.UserID {
		c.Json(w, http.StatusForbidden, "You can only edit your own feedback", nil)
		return
	}

	var input struct {
		Comment        *string  `json:"comment,omitempty"`
		Rating         *float64 `json:"rating,omitempty"`
		Recommendation *string  `json:"recommendation,omitempty"`
		IsPublic       *bool    `json:"is_public,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON payload", nil)
		return
	}

	updates := map[string]interface{}{}
	if input.Comment != nil {
		updates["comment"] = *input.Comment
	}
	if input.Rating != nil {
		updates["rating"] = *input.Rating
	}
	if input.Recommendation != nil {
		updates["recommendation"] = *input.Recommendation
	}
	if input.IsPublic != nil {
		updates["is_public"] = *input.IsPublic
	}
	updates["updated_at"] = time.Now()

	if err := c.DB.Model(&feedback).Updates(updates).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update feedback", map[string]interface{}{"error": err.Error()})
		return
	}

	// Notify student that feedback has been updated
	c.NotifyAndTrack(
		feedback.StudentID,
		"Mentor Feedback Updated",
		fmt.Sprintf("Mentor %s %s updated feedback for you.", user.Profile.FirstName, user.Profile.LastName),
		"Mentor Feedback",
		"MentorFeedback",
		&feedback.ID,
		"",
		true,
	)

	// Notify supervisors of the student
	supervisorIDs := c.GetSupervisorsForStudent(feedback.StudentID)
	for _, supID := range supervisorIDs {
		c.NotifyAndTrack(
			supID,
			"Mentor Feedback Updated",
			fmt.Sprintf("Mentor %s %s updated feedback for student %s %s.", user.Profile.FirstName, user.Profile.LastName, feedback.Student.Profile.FirstName, feedback.Student.Profile.LastName),
			"Mentor Feedback",
			"MentorFeedback",
			&feedback.ID,
			"",
			true,
		)
	}

	c.Json(w, http.StatusOK, "Feedback updated successfully", map[string]interface{}{"data": feedback})
}

// Get Feedbacks
func (c *Construct) GetMentorFeedback(w http.ResponseWriter, r *http.Request) {
	// 1️⃣ Authenticate user
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// 2️⃣ Optional query: filter by student
	studentIDStr := r.URL.Query().Get("student_id")
	var studentID uint64
	if studentIDStr != "" {
		studentID, err = strconv.ParseUint(studentIDStr, 10, 64)
		if err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid student_id", nil)
			return
		}
	}

	role := strings.ToLower(user.Role.Name)
	var feedbacks []models.MentorFeedback

	// 3️⃣ Base query for all roles except supervisor (supervisor uses custom filter)
	query := c.DB.Model(&models.MentorFeedback{}).
		Preload("Mentor.Profile").
		Preload("Report.Cohort").
		Preload("Item")

	switch role {
	case "student":
		// Students see their own public feedback
		query = query.Where("student_id = ? AND is_public = ?", user.UserID, true)

	case "mentor":
		// Mentors see feedback they authored
		query = query.Where("mentor_id = ?", user.UserID)
		if studentID != 0 {
			query = query.Where("student_id = ?", studentID)
		}

	case "opsadmin", "systemadmin":
		// Admin roles see everything
		if studentID != 0 {
			query = query.Where("student_id = ?", studentID)
		}

	case "supervisor":
		// Supervisors require custom access filtering — handled below
		var allFeedbacks []models.MentorFeedback
		err := c.DB.Model(&models.MentorFeedback{}).
			Preload("Mentor.Profile").
			Preload("Report.Cohort").
			Preload("Item").
			Find(&allFeedbacks).Error

		if err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to fetch feedbacks", map[string]interface{}{"error": err.Error()})
			return
		}

		for _, f := range allFeedbacks {
			if f.Report != nil && f.Report.CohortID != nil {
				// dereference pointer safely
				cohortID := *f.Report.CohortID
				if c.UserHasCohortAccess(user.UserID, cohortID) {
					if studentID == 0 || f.StudentID == studentID {
						feedbacks = append(feedbacks, f)
					}
				}
			}
		}

		if len(feedbacks) == 0 {
			c.Json(w, http.StatusOK, "No mentor feedbacks found for your assigned cohorts", map[string]interface{}{"data": []interface{}{}})
			return
		}

	default:
		c.Json(w, http.StatusForbidden, "You do not have permission to view mentor feedbacks", nil)
		return
	}

	// 4️⃣ Execute query (for roles that use query builder)
	if role != "supervisor" {
		if err := query.Order("created_at DESC").Find(&feedbacks).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to fetch feedbacks", map[string]interface{}{"error": err.Error()})
			return
		}
	}

	// 5️⃣ Build structured response
	response := make([]map[string]interface{}, 0, len(feedbacks))
	for _, f := range feedbacks {
		mentorName := ""
		if f.Mentor.Profile.ProfileID != 0 {
			mentorName = f.Mentor.Profile.FirstName + " " + f.Mentor.Profile.LastName
		}

		var reportData map[string]interface{}
		if f.Report != nil && f.Report.Cohort != nil {
			reportData = map[string]interface{}{
				"id":          f.Report.ID,
				"cohort_name": f.Report.Cohort.Name,
				"week_start":  f.Report.WeekStart,
				"week_end":    f.Report.WeekEnd,
				"status":      f.Report.Status,
			}
		}

		var itemData map[string]interface{}
		if f.Item != nil {
			itemData = map[string]interface{}{
				"id":              f.Item.ID,
				"phase_name":      f.Item.PhaseName,
				"progress_type":   f.Item.ProgressType,
				"student_status":  f.Item.StudentStatus,
				"verified_status": f.Item.VerifiedStatus,
			}
		}

		response = append(response, map[string]interface{}{
			"id":             f.ID,
			"mentor_id":      f.MentorID,
			"mentor_name":    mentorName,
			"student_id":     f.StudentID,
			"comment":        f.Comment,
			"rating":         f.Rating,
			"recommendation": f.Recommendation,
			"is_public":      f.IsPublic,
			"report":         reportData,
			"item":           itemData,
			"created_at":     f.CreatedAt,
			"updated_at":     f.UpdatedAt,
			"is_archived":    f.IsArchived,
		})

		// 6️⃣ Log audit for each feedback viewed
		entity := "MentorFeedback"
		action := fmt.Sprintf("Viewed mentor feedback #%d for student #%d", f.ID, f.StudentID)
		metadata := map[string]interface{}{
			"student_id": f.StudentID,
			"mentor_id":  f.MentorID,
		}

		_ = c.LogAudit(
			user.UserID,
			action,
			&entity,
			&f.ID,
			nil,      // optional: you can pass request IP
			metadata, // structured context
		)

		// 7️⃣ Notify mentor if supervisor views their feedback
		if role == "supervisor" {
			c.NotifyAndTrack(
				f.MentorID,
				"Feedback Viewed by Supervisor",
				fmt.Sprintf("Supervisor %s %s viewed your feedback for student #%d.",
					user.Profile.FirstName, user.Profile.LastName, f.StudentID),
				"View",
				"MentorFeedback",
				&f.ID,
				"Viewed",
				false,
			)
		}
	}

	// 8️⃣ Send JSON response
	c.Json(w, http.StatusOK, "Mentor feedbacks fetched successfully", map[string]interface{}{
		"data": response,
	})
}

// manage feedbacks
func (c *Construct) ManageMentorFeedbacks(w http.ResponseWriter, r *http.Request) {
	// 1️⃣ Parse input
	var input struct {
		FeedbackIDs []uint64 `json:"feedback_ids"`
		Action      string   `json:"action"` // "archive", "unarchive", "delete"
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	if len(input.FeedbackIDs) == 0 || input.Action == "" {
		c.Json(w, http.StatusBadRequest, "feedback_ids and action are required", nil)
		return
	}

	// 2️⃣ Authenticate user
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// 3️⃣ Fetch feedbacks
	var feedbacks []models.MentorFeedback
	if err := c.DB.Where("id IN ?", input.FeedbackIDs).Find(&feedbacks).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch feedbacks", map[string]interface{}{"error": err.Error()})
		return
	}
	if len(feedbacks) == 0 {
		c.Json(w, http.StatusNotFound, "No feedbacks found for the given IDs", nil)
		return
	}

	// 4️⃣ Process each feedback
	updatedFeedbacks := make([]map[string]interface{}, 0, len(feedbacks))
	for _, fb := range feedbacks {
		actionLower := strings.ToLower(input.Action)
		allowed := false

		switch user.Role.Name {
		case "Student":
			// Students can only act on their own feedbacks
			allowed = fb.StudentID == user.UserID
		case "Mentor":
			// Mentors can act on their own feedbacks
			allowed = fb.MentorID == user.UserID
		case "Supervisor", "OpsAdmin", "SystemAdmin":
			// Supervisors and admins can archive or delete any feedback
			allowed = true
		}

		if !allowed {
			c.Json(w, http.StatusForbidden, fmt.Sprintf("You do not have permission to %s feedback #%d", actionLower, fb.ID), nil)
			return
		}

		switch actionLower {
		case "archive":
			fb.IsArchived = true
			if err := c.DB.Model(&fb).Update("is_archived", true).Error; err != nil {
				c.Json(w, http.StatusInternalServerError, "Failed to archive feedback", map[string]interface{}{"error": err.Error()})
				return
			}
			c.NotifyAndTrack(user.UserID, "Mentor Feedback Archived",
				fmt.Sprintf("Mentor feedback #%d was archived", fb.ID),
				"Mentor Feedback Archive", "MentorFeedback", &fb.ID, "Archived",
				false,
			)

		case "unarchive":
			fb.IsArchived = false
			if err := c.DB.Model(&fb).Update("is_archived", false).Error; err != nil {
				c.Json(w, http.StatusInternalServerError, "Failed to unarchive feedback", map[string]interface{}{"error": err.Error()})
				return
			}
			c.NotifyAndTrack(user.UserID, "Mentor Feedback Unarchived",
				fmt.Sprintf("Mentor feedback #%d was unarchived", fb.ID),
				"Mentor Feedback Unarchive", "MentorFeedback", &fb.ID, "Unarchived",
				false,
			)

		case "delete":
			if err := c.DB.Unscoped().Delete(&fb).Error; err != nil {
				c.Json(w, http.StatusInternalServerError, "Failed to delete feedback", map[string]interface{}{"error": err.Error()})
				return
			}
			c.NotifyAndTrack(user.UserID, "Mentor Feedback Deleted Permanently",
				fmt.Sprintf("Mentor feedback #%d was permanently deleted", fb.ID),
				"Mentor Feedback Deletion", "MentorFeedback", &fb.ID, "Deleted",
				true,
			)

		default:
			c.Json(w, http.StatusBadRequest, "Invalid action, must be 'archive', 'unarchive', or 'delete'", nil)
			return
		}

		// Notify student about change
		c.NotifyAndTrack(fb.StudentID, "Mentor Feedback Updated",
			fmt.Sprintf("Your feedback #%d has been %sed by %s.", fb.ID, actionLower, user.Profile.FirstName),
			"Mentor Feedback", "MentorFeedback", &fb.ID, strings.Title(actionLower),
			false,
		)

		// Append updated info to response
		updatedFeedbacks = append(updatedFeedbacks, map[string]interface{}{
			"id":          fb.ID,
			"mentor_id":   fb.MentorID,
			"student_id":  fb.StudentID,
			"is_archived": fb.IsArchived,
			"comment":     fb.Comment,
			"updated_at":  fb.UpdatedAt,
		})
	}

	// 5️⃣ Return updated feedbacks
	c.Json(w, http.StatusOK, fmt.Sprintf("Action '%s' performed on selected feedbacks successfully", input.Action),
		map[string]interface{}{"data": updatedFeedbacks},
	)
}

// GetSupervisorsForStudent returns the IDs of supervisors assigned to the cohorts the student belongs to

func (c *Construct) GetSupervisorsForStudent(studentID uint64) []uint64 {
	var supervisorIDs []uint64

	// Assumes students are linked to cohorts via WeeklyReports or another table
	// Here we get all cohorts for which the student has reports
	var cohortIDs []uint64
	c.DB.Model(&models.WeeklyReport{}).
		Where("student_id = ?", studentID).
		Pluck("cohort_id", &cohortIDs)

	if len(cohortIDs) == 0 {
		return supervisorIDs
	}

	// Fetch supervisors for these cohorts
	c.DB.Model(&models.Cohort{}).
		Where("id IN ?", cohortIDs).
		Pluck("supervisor_id", &supervisorIDs)

	return supervisorIDs
}
