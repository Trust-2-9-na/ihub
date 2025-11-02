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

type MentorFeedbackResponse struct {
	ID             uint64    `json:"id"`
	MentorID       uint64    `json:"mentor_id"`
	MentorName     string    `json:"mentor_name"`
	MentorEmail    string    `json:"mentor_email,omitempty"`
	ItemID         *uint64   `json:"item_id,omitempty"`
	ItemName       string    `json:"item_name,omitempty"` // Phase name
	Comment        string    `json:"comment"`
	Rating         *float64  `json:"rating,omitempty"`
	Recommendation string    `json:"recommendation"`
	IsPublic       bool      `json:"is_public"`
	StudentID      uint64    `json:"student_id"`
	StudentName    string    `json:"student_name"`
	StudentEmail   string    `json:"student_email,omitempty"`
	MentorCohortID uint64    `json:"mentor_cohort_id"`
	MentorCohort   string    `json:"mentor_cohort_name,omitempty"`
	IsArchived     bool      `json:"is_archived"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

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

	// ✅ Only mentors can create feedback
	if strings.ToLower(user.Role.Name) != "mentor" {
		c.Json(w, http.StatusForbidden, "Only mentors can create feedback", nil)
		return
	}

	var input struct {
		MentorReportID *uint64  `json:"mentor_report_id,omitempty"`
		ItemID         *uint64  `json:"item_id,omitempty"`
		StudentID      uint64   `json:"student_id"`
		MentorCohortID uint64   `json:"mentor_cohort_id"`
		Comment        string   `json:"comment"`
		Rating         *float64 `json:"rating,omitempty"`
		Recommendation string   `json:"recommendation"`
		IsPublic       bool     `json:"is_public"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON payload", nil)
		return
	}

	if input.MentorReportID == nil && input.ItemID == nil {
		c.Json(w, http.StatusBadRequest, "Either mentor_report_id or item_id must be provided", nil)
		return
	}

	// ✅ Verify mentor-student assignment
	var assignmentCount int64
	if err := c.DB.Model(&models.MentorStudentAssignment{}).
		Where("mentor_ref_id = ? AND student_ref_id = ? AND cohort_ref_id = ?", user.UserID, input.StudentID, input.MentorCohortID).
		Count(&assignmentCount).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to verify mentor-student assignment", map[string]interface{}{"error": err.Error()})
		return
	}

	if assignmentCount == 0 {
		c.Json(w, http.StatusForbidden, "You are not assigned to this student in this cohort", nil)
		return
	}

	// ✅ Create the feedback
	feedback := models.MentorFeedback{
		MentorID:       user.UserID,
		MentorReportID: input.MentorReportID,
		ItemID:         input.ItemID,
		StudentID:      input.StudentID,
		MentorCohortID: input.MentorCohortID,
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

	// --- Notifications ---
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
	}

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
	}

	// --- Normalized response ---
	type normalizedResponse struct {
		ID             uint64    `json:"id"`
		MentorID       uint64    `json:"mentor_id"`
		MentorName     string    `json:"mentor_name"`
		ItemID         *uint64   `json:"item_id,omitempty"`
		ItemName       string    `json:"item_name,omitempty"`
		Comment        string    `json:"comment"`
		Rating         *float64  `json:"rating,omitempty"`
		Recommendation string    `json:"recommendation"`
		IsPublic       bool      `json:"is_public"`
		StudentID      uint64    `json:"student_id"`
		StudentName    string    `json:"student_name"`
		MentorCohortID uint64    `json:"mentor_cohort_id"`
		MentorCohort   string    `json:"mentor_cohort_name,omitempty"`
		IsArchived     bool      `json:"is_archived"`
		CreatedAt      time.Time `json:"created_at"`
		UpdatedAt      time.Time `json:"updated_at"`
	}

	resp := normalizedResponse{
		ID:             feedback.ID,
		MentorID:       feedback.MentorID,
		MentorName:     fmt.Sprintf("%s %s", user.Profile.FirstName, user.Profile.LastName), // only once
		ItemID:         feedback.ItemID,
		Comment:        feedback.Comment,
		Rating:         feedback.Rating,
		Recommendation: feedback.Recommendation,
		IsPublic:       feedback.IsPublic,
		StudentID:      feedback.StudentID,
		StudentName:    "", // Will fetch below
		MentorCohortID: feedback.MentorCohortID,
		IsArchived:     feedback.IsArchived,
		CreatedAt:      feedback.CreatedAt,
		UpdatedAt:      feedback.UpdatedAt,
	}

	// Optional: fetch student name
	var student models.User
	if err := c.DB.Preload("Profile").First(&student, feedback.StudentID).Error; err == nil {
		resp.StudentName = fmt.Sprintf("%s %s", student.Profile.FirstName, student.Profile.LastName)
	}

	// Optional: fetch item name
	if feedback.ItemID != nil {
		var item models.ProgressItem
		if err := c.DB.First(&item, *feedback.ItemID).Error; err == nil {
			resp.ItemName = item.PhaseName
		}
	}

	// Optional: fetch cohort name
	var cohort models.Cohort
	if err := c.DB.First(&cohort, feedback.MentorCohortID).Error; err == nil {
		resp.MentorCohort = cohort.Name
	}

	c.Json(w, http.StatusOK, "Mentor feedback created successfully", map[string]interface{}{
		"data": resp,
	})
}

// updating the feedback
func (c *Construct) UpdateMentorFeedback(w http.ResponseWriter, r *http.Request) {
	// ✅ Get authenticated user
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// ✅ Parse feedback ID from URL
	feedbackIDStr := mux.Vars(r)["id"]
	feedbackID, err := strconv.ParseUint(feedbackIDStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid feedback ID", nil)
		return
	}

	// ✅ Load feedback with student profile and cohort
	var feedback models.MentorFeedback
	if err := c.DB.Preload("Student.Profile").Preload("MentorCohort").First(&feedback, feedbackID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Feedback not found", nil)
		return
	}

	// ✅ Only original mentor can update
	if feedback.MentorID != user.UserID {
		c.Json(w, http.StatusForbidden, "You can only edit your own feedback", nil)
		return
	}

	// ✅ Verify mentor-student assignment
	var assignmentCount int64
	if err := c.DB.Model(&models.MentorStudentAssignment{}).
		Where("mentor_ref_id = ? AND student_ref_id = ? AND cohort_ref_id = ?", user.UserID, feedback.StudentID, feedback.MentorCohortID).
		Count(&assignmentCount).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to verify mentor-student assignment", map[string]interface{}{"error": err.Error()})
		return
	}
	if assignmentCount == 0 {
		c.Json(w, http.StatusForbidden, "You are not assigned to this student in this cohort", nil)
		return
	}

	// ✅ Decode input payload
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

	// ✅ Prepare updates
	updates := map[string]interface{}{"updated_at": time.Now()}
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

	// ✅ Apply updates
	if err := c.DB.Model(&feedback).Updates(updates).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update feedback", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Notifications ---
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
	for _, supID := range c.GetSupervisorsForStudent(feedback.StudentID) {
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

	// ✅ Return normalized response
	resp := map[string]interface{}{
		"id":          feedback.ID,
		"mentor_id":   feedback.MentorID,
		"mentor_name": fmt.Sprintf("%s %s", user.Profile.FirstName, user.Profile.LastName),
		"item_id":     feedback.ItemID,
		"item_name": func() string {
			if feedback.Item != nil {
				return feedback.Item.PhaseName
			}
			return ""
		}(),
		"comment":          feedback.Comment,
		"rating":           feedback.Rating,
		"recommendation":   feedback.Recommendation,
		"is_public":        feedback.IsPublic,
		"student_id":       feedback.StudentID,
		"student_name":     fmt.Sprintf("%s %s", feedback.Student.Profile.FirstName, feedback.Student.Profile.LastName),
		"mentor_cohort_id": feedback.MentorCohortID,
		"mentor_cohort_name": func() string {
			if feedback.MentorCohort != nil {
				return feedback.MentorCohort.Name
			}
			return ""
		}(),
		"is_archived": feedback.IsArchived,
		"created_at":  feedback.CreatedAt,
		"updated_at":  feedback.UpdatedAt,
	}

	c.Json(w, http.StatusOK, "Feedback updated successfully", map[string]interface{}{"data": resp})
}

// Get Feedbacks

func (c *Construct) GetMentorFeedback(w http.ResponseWriter, r *http.Request) {
	// 1️⃣ Authenticate user
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// 2️⃣ Optional student filter
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

	// 3️⃣ Build query depending on role
	query := c.DB.Model(&models.MentorFeedback{}).
		Preload("Mentor.Profile").
		Preload("Student.Profile").
		Preload("MentorCohort").
		Preload("Item").
		Order("created_at DESC")

	switch role {
	case "student":
		// Students see only public feedback from their assigned mentors
		var assignedMentorIDs []uint64
		c.DB.Model(&models.MentorStudentAssignment{}).
			Where("student_ref_id = ?", user.UserID).
			Pluck("mentor_ref_id", &assignedMentorIDs)

		if len(assignedMentorIDs) == 0 {
			c.Json(w, http.StatusOK, "No mentor feedback found", map[string]interface{}{"data": []interface{}{}})
			return
		}

		query = query.Where("student_id = ? AND mentor_id IN ? AND is_public = ?", user.UserID, assignedMentorIDs, true)

	case "mentor":
		// Mentors see feedback they authored
		query = query.Where("mentor_id = ?", user.UserID)
		if studentID != 0 {
			query = query.Where("student_id = ?", studentID)
		}

	case "supervisor":
		// Supervisors see feedback for all students in their cohorts
		var cohortIDs []uint64
		c.DB.Model(&models.CohortUser{}).
			Where("user_user_id = ? AND role = ?", user.UserID, "Supervisor").
			Pluck("cohort_cohort_id", &cohortIDs)

		if len(cohortIDs) == 0 {
			c.Json(w, http.StatusOK, "No mentor feedbacks found for your assigned cohorts", map[string]interface{}{"data": []interface{}{}})
			return
		}

		query = query.Where("mentor_cohort_id IN ?", cohortIDs)
		if studentID != 0 {
			query = query.Where("student_id = ?", studentID)
		}

	case "opsadmin", "systemadmin":
		// Admins see all feedback
		if studentID != 0 {
			query = query.Where("student_id = ?", studentID)
		}

	default:
		c.Json(w, http.StatusForbidden, "You do not have permission to view mentor feedbacks", nil)
		return
	}

	// 4️⃣ Fetch feedbacks
	if err := query.Find(&feedbacks).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch feedbacks", map[string]interface{}{"error": err.Error()})
		return
	}

	// 5️⃣ Normalized response struct
	type normalizedResponse struct {
		ID             uint64    `json:"id"`
		MentorID       uint64    `json:"mentor_id"`
		MentorName     string    `json:"mentor_name"`
		ItemID         *uint64   `json:"item_id,omitempty"`
		ItemName       string    `json:"item_name,omitempty"`
		Comment        string    `json:"comment"`
		Rating         *float64  `json:"rating,omitempty"`
		Recommendation string    `json:"recommendation"`
		IsPublic       bool      `json:"is_public"`
		StudentID      uint64    `json:"student_id"`
		StudentName    string    `json:"student_name"`
		MentorCohortID uint64    `json:"mentor_cohort_id"`
		MentorCohort   string    `json:"mentor_cohort_name,omitempty"`
		IsArchived     bool      `json:"is_archived"`
		CreatedAt      time.Time `json:"created_at"`
		UpdatedAt      time.Time `json:"updated_at"`
	}

	getFullName := func(u models.User) string {
		if u.Profile.FirstName != "" || u.Profile.LastName != "" {
			return strings.TrimSpace(fmt.Sprintf("%s %s", u.Profile.FirstName, u.Profile.LastName))
		}
		if u.Username != "" {
			return u.Username
		}
		return ""
	}

	response := make([]normalizedResponse, 0, len(feedbacks))
	for _, f := range feedbacks {
		resp := normalizedResponse{
			ID:             f.ID,
			MentorID:       f.MentorID,
			MentorName:     getFullName(f.Mentor),
			ItemID:         f.ItemID,
			ItemName:       "",
			Comment:        f.Comment,
			Rating:         f.Rating,
			Recommendation: f.Recommendation,
			IsPublic:       f.IsPublic,
			StudentID:      f.StudentID,
			StudentName:    getFullName(f.Student),
			MentorCohortID: f.MentorCohortID,
			MentorCohort:   "",
			IsArchived:     f.IsArchived,
			CreatedAt:      f.CreatedAt,
			UpdatedAt:      f.UpdatedAt,
		}

		if f.Item != nil {
			resp.ItemName = f.Item.PhaseName
		}
		if f.MentorCohort != nil {
			resp.MentorCohort = f.MentorCohort.Name
		}

		response = append(response, resp)
	}

	c.Json(w, http.StatusOK, "Mentor feedbacks fetched successfully", map[string]interface{}{"data": response})
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
