package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"web/services/assets/middlewares"
	"web/services/assets/models"

	"github.com/gorilla/mux"
)

//============Creating proposal submission window with dynamic filtering ============

func (c *Construct) CreateSubmissionWindow(w http.ResponseWriter, r *http.Request) {
	// ---------------------------
	// Parse JSON payload
	// ---------------------------
	var payload struct {
		Title       string  `json:"title"`
		StartDate   string  `json:"start_date"` // "2006-01-02"
		Deadline    string  `json:"deadline"`
		School      *string `json:"school,omitempty"`
		Program     *string `json:"program,omitempty"`
		YearOfStudy *string `json:"year_of_study,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	// ---------------------------
	// Validate dates
	// ---------------------------
	start, err := time.Parse("2006-01-02", payload.StartDate)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid start_date format", nil)
		return
	}

	deadline, err := time.Parse("2006-01-02", payload.Deadline)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid deadline format", nil)
		return
	}

	if payload.Title == "" || deadline.Before(start) {
		c.Json(w, http.StatusBadRequest, "Invalid title or dates", nil)
		return
	}

	// ---------------------------
	// Authenticate user (OpsAdmin only)
	// ---------------------------
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Role").Preload("Profile").
		Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	role := strings.ToLower(user.Role.Name)
	if role != "opsadmin" {
		c.Json(w, http.StatusForbidden, "Only OpsAdmin can create submission windows", nil)
		return
	}

	// ---------------------------
	// Create the window
	// ---------------------------
	window := models.ProposalSubmissionWindow{
		Title:       payload.Title,
		StartDate:   start,
		Deadline:    deadline,
		CreatedByID: user.UserID,
		School:      payload.School,
		Program:     payload.Program,
		YearOfStudy: payload.YearOfStudy,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := c.DB.Create(&window).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create submission window", map[string]interface{}{"error": err.Error()})
		return
	}

	// ---------------------------
	// Dynamic student filtering
	// ---------------------------
	query := c.DB.Preload("Profile").Joins("JOIN student_profiles sp ON sp.user_id = users.user_id")
	if payload.School != nil && *payload.School != "" {
		query = query.Where("sp.school = ?", *payload.School)
	}
	if payload.Program != nil && *payload.Program != "" {
		query = query.Where("sp.program = ?", *payload.Program)
	}
	if payload.YearOfStudy != nil && *payload.YearOfStudy != "" {
		query = query.Where("sp.year_of_study = ?", *payload.YearOfStudy)
	}
	query = query.Where("users.deleted_at IS NULL")

	var students []models.User
	if err := query.Find(&students).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch students", map[string]interface{}{"error": err.Error()})
		return
	}

	// ---------------------------
	// Notify and track for each student
	// ---------------------------
	for _, student := range students {
		message := fmt.Sprintf(
			"Dear %s, a new proposal submission window '%s' is now open.\nSubmit your proposal between %s and %s.",
			student.Profile.FirstName,
			window.Title,
			start.Format("02 Jan 2006"),
			deadline.Format("02 Jan 2006"),
		)

		c.NotifyAndTrack(
			student.UserID,
			"Proposal Submission Window Open",
			message,
			"SubmissionWindow",
			"ProposalSubmissionWindow",
			&window.WindowID,
			"Notified",
			true,
		)
	}

	// ---------------------------
	// Track audit for OpsAdmin
	// ---------------------------
	c.NotifyAndTrack(
		user.UserID,
		"Created Submission Window",
		fmt.Sprintf("Created submission window '%s' for %d students.", window.Title, len(students)),
		"SubmissionWindow",
		"ProposalSubmissionWindow",
		&window.WindowID,
		"Created",
		true,
	)

	// ---------------------------
	// Response
	// ---------------------------
	resp := map[string]interface{}{
		"window_id":     window.WindowID,
		"title":         window.Title,
		"start_date":    window.StartDate.Format("2006-01-02"),
		"deadline":      window.Deadline.Format("2006-01-02"),
		"created_by":    user.Profile.FirstName + " " + user.Profile.LastName,
		"created_at":    window.CreatedAt.Format("2006-01-02 15:04:05"),
		"updated_at":    window.UpdatedAt.Format("2006-01-02 15:04:05"),
		"school":        payload.School,
		"program":       payload.Program,
		"year_of_study": payload.YearOfStudy,
		"sent_to":       len(students),
	}

	c.Json(w, http.StatusCreated, "Submission window created successfully", resp)
}

// ==============================GETALL======================================

func (c *Construct) GetSubmissionWindows(w http.ResponseWriter, r *http.Request) {
	// 1️⃣ Get user UUID from context
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// 2️⃣ Fetch user with role and profile
	var user models.User
	if err := c.DB.Preload("Role").Preload("Profile").
		Joins("LEFT JOIN student_profiles sp ON sp.user_id = users.user_id").
		Where("user_uuid = ?", userUUID).
		First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	role := strings.ToLower(user.Role.Name)

	// 3️⃣ Get query param for archived filter
	showArchived := r.URL.Query().Get("archived") == "true"

	var windows []models.ProposalSubmissionWindow

	switch role {
	case "opsadmin":
		// OpsAdmin sees all windows, optionally filter archived
		query := c.DB.Preload("CreatedBy.Profile")
		if !showArchived {
			query = query.Where("is_archived = ?", false)
		}
		if err := query.Find(&windows).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to fetch windows", map[string]interface{}{"error": err.Error()})
			return
		}

		// 🔔 Log & track
		c.NotifyAndTrack(
			user.UserID,
			"Viewed All Submission Windows",
			fmt.Sprintf("OpsAdmin %s viewed all submission windows (archived=%v).", user.Username, showArchived),
			"SubmissionWindowView",
			"ProposalSubmissionWindow",
			nil,
			"Viewed",
			false,
		)

	case "student":
		// Students see windows relevant to them
		var studentProfile models.StudentProfile
		if err := c.DB.Where("user_id = ?", user.UserID).First(&studentProfile).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to fetch student profile", map[string]interface{}{"error": err.Error()})
			return
		}

		// Get all windows (filter archived)
		var allWindows []models.ProposalSubmissionWindow
		query := c.DB.Preload("CreatedBy.Profile")
		if !showArchived {
			query = query.Where("is_archived = ?", false)
		}
		if err := query.Find(&allWindows).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to fetch windows", map[string]interface{}{"error": err.Error()})
			return
		}

		// Filter manually by student's profile
		for _, w := range allWindows {
			match := true
			if w.School != nil && *w.School != "" && studentProfile.School != *w.School {
				match = false
			}
			if w.Program != nil && *w.Program != "" && studentProfile.Program != *w.Program {
				match = false
			}
			if w.YearOfStudy != nil && *w.YearOfStudy != "" && studentProfile.YearOfStudy != *w.YearOfStudy {
				match = false
			}
			if match {
				windows = append(windows, w)
			}
		}

		// 🔔 Log & track
		c.NotifyAndTrack(
			user.UserID,
			"Viewed Submission Windows",
			fmt.Sprintf("Student %s viewed their submission windows (archived=%v).", user.Username, showArchived),
			"SubmissionWindowView",
			"ProposalSubmissionWindow",
			nil,
			"Viewed",
			false,
		)

	default:
		c.Json(w, http.StatusForbidden, "Unauthorized role", nil)
		return
	}

	// 4️⃣ Format response
	var resp []map[string]interface{}
	for _, win := range windows {
		resp = append(resp, map[string]interface{}{
			"window_id":   win.WindowID,
			"title":       win.Title,
			"start_date":  win.StartDate.Format("2006-01-02"),
			"deadline":    win.Deadline.Format("2006-01-02"),
			"created_by":  win.CreatedBy.Profile.FirstName + " " + win.CreatedBy.Profile.LastName,
			"created_at":  win.CreatedAt.Format("2006-01-02 15:04:05"),
			"updated_at":  win.UpdatedAt.Format("2006-01-02 15:04:05"),
			"school":      win.School,
			"program":     win.Program,
			"year":        win.YearOfStudy,
			"is_archived": win.IsArchived,
		})
	}

	// 5️⃣ Return response
	c.Json(w, http.StatusOK, "Submission windows fetched successfully", map[string]interface{}{
		"windows": resp,
	})
}

// updating submission windows

func (c *Construct) UpdateSubmissionWindow(w http.ResponseWriter, r *http.Request) {
	// ---------------------------
	// Get Window ID from URL
	// ---------------------------
	vars := mux.Vars(r)
	idStr, ok := vars["id"]
	if !ok || idStr == "" {
		c.Json(w, http.StatusBadRequest, "Window ID is required", nil)
		return
	}

	windowID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid Window ID", nil)
		return
	}

	// ---------------------------
	// Authenticate OpsAdmin
	// ---------------------------
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Role").Preload("Profile").
		Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	if strings.ToLower(user.Role.Name) != "opsadmin" {
		c.Json(w, http.StatusForbidden, "Only OpsAdmin can update submission windows", nil)
		return
	}

	// ---------------------------
	// Fetch window
	// ---------------------------
	var window models.ProposalSubmissionWindow
	if err := c.DB.First(&window, windowID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Submission window not found", nil)
		return
	}

	// ---------------------------
	// Parse input
	// ---------------------------
	var payload struct {
		Title       *string `json:"title,omitempty"`
		StartDate   *string `json:"start_date,omitempty"`
		Deadline    *string `json:"deadline,omitempty"`
		School      *string `json:"school,omitempty"`
		Program     *string `json:"program,omitempty"`
		YearOfStudy *string `json:"year_of_study,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	// ---------------------------
	// Apply updates
	// ---------------------------
	if payload.Title != nil {
		window.Title = *payload.Title
	}
	if payload.StartDate != nil {
		if start, err := time.Parse("2006-01-02", *payload.StartDate); err == nil {
			window.StartDate = start
		} else {
			c.Json(w, http.StatusBadRequest, "Invalid start_date format", nil)
			return
		}
	}
	if payload.Deadline != nil {
		if deadline, err := time.Parse("2006-01-02", *payload.Deadline); err == nil {
			window.Deadline = deadline
		} else {
			c.Json(w, http.StatusBadRequest, "Invalid deadline format", nil)
			return
		}
	}
	if payload.School != nil {
		window.School = payload.School
	}
	if payload.Program != nil {
		window.Program = payload.Program
	}
	if payload.YearOfStudy != nil {
		window.YearOfStudy = payload.YearOfStudy
	}

	window.UpdatedAt = time.Now()

	if err := c.DB.Save(&window).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update submission window", map[string]interface{}{"error": err.Error()})
		return
	}

	// ---------------------------
	// Notify & Track audit
	// ---------------------------
	c.NotifyAndTrack(
		user.UserID,
		"Updated Submission Window",
		fmt.Sprintf("Updated window '%s'", window.Title),
		"SubmissionWindow",
		"ProposalSubmissionWindow",
		&window.WindowID,
		"Updated",
		true,
	)
	c.Json(w, http.StatusOK, "Submission window fetched successfully", map[string]interface{}{
		"window": window, // the struct is inside a map
	})

}

// manage submission windows

func (c *Construct) ManageSubmissionWindows(w http.ResponseWriter, r *http.Request) {
	// ---------------------------
	// Parse input
	// ---------------------------
	var input struct {
		WindowIDs []uint64 `json:"window_ids"`
		Action    string   `json:"action"` // "delete", "archive", "unarchive"
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	if len(input.WindowIDs) == 0 || (input.Action != "delete" && input.Action != "archive" && input.Action != "unarchive") {
		c.Json(w, http.StatusBadRequest, "window_ids and valid action are required", nil)
		return
	}

	// ---------------------------
	// Authenticate OpsAdmin
	// ---------------------------
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Role").Preload("Profile").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	if strings.ToLower(user.Role.Name) != "opsadmin" {
		c.Json(w, http.StatusForbidden, "Only OpsAdmin can manage submission windows", nil)
		return
	}

	// ---------------------------
	// Process each window
	// ---------------------------
	results := make([]map[string]interface{}, 0)

	for _, windowID := range input.WindowIDs {
		var window models.ProposalSubmissionWindow
		if err := c.DB.First(&window, windowID).Error; err != nil {
			results = append(results, map[string]interface{}{
				"window_id": windowID,
				"status":    "not_found",
			})
			continue
		}

		statusText := ""
		switch input.Action {
		case "delete":
			if err := c.DB.Delete(&window).Error; err != nil {
				results = append(results, map[string]interface{}{
					"window_id": windowID,
					"status":    "delete_failed",
				})
				continue
			}
			statusText = "Deleted"

		case "archive":
			window.IsArchived = true
			window.UpdatedAt = time.Now()
			c.DB.Save(&window)
			statusText = "Archived"

		case "unarchive":
			window.IsArchived = false
			window.UpdatedAt = time.Now()
			c.DB.Save(&window)
			statusText = "Unarchived"
		}

		// Notify & Track
		c.NotifyAndTrack(
			user.UserID,
			fmt.Sprintf("%s Submission Window", statusText),
			fmt.Sprintf("Submission window '%s' has been %s", window.Title, strings.ToLower(statusText)),
			"SubmissionWindow",
			"ProposalSubmissionWindow",
			&window.WindowID,
			statusText,
			true,
		)

		results = append(results, map[string]interface{}{
			"window_id": windowID,
			"status":    strings.ToLower(statusText),
		})
	}

	c.Json(w, http.StatusOK, fmt.Sprintf("Action '%s' executed successfully", input.Action), map[string]interface{}{
		"results": results,
	})
}
