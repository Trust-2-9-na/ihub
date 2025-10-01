package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"web/services/assets/models"
)

// ======================Create API for submission window================
func (c *Construct) CreateSubmissionWindow(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Title     string `json:"title"`
		StartDate string `json:"start_date"` // expect string in "2006-01-02" format
		Deadline  string `json:"deadline"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Parse dates
	start, err := time.Parse("2006-01-02", payload.StartDate)
	if err != nil {
		http.Error(w, "invalid start_date format", http.StatusBadRequest)
		return
	}
	deadline, err := time.Parse("2006-01-02", payload.Deadline)
	if err != nil {
		http.Error(w, "invalid deadline format", http.StatusBadRequest)
		return
	}

	if payload.Title == "" || deadline.Before(start) {
		http.Error(w, "invalid title or dates", http.StatusBadRequest)
		return
	}

	// Supervisor info from middleware
	userUUID := r.Context().Value("user_uuid").(string)
	var supervisor models.User
	if err := c.DB.Where("user_uuid = ?", userUUID).First(&supervisor).Error; err != nil {
		http.Error(w, "supervisor not found", http.StatusUnauthorized)
		return
	}

	window := models.ProposalSubmissionWindow{
		Title:       payload.Title,
		StartDate:   start,
		Deadline:    deadline,
		CreatedByID: supervisor.UserID,
	}

	if err := c.DB.Create(&window).Error; err != nil {
		http.Error(w, "failed to create submission window", http.StatusInternalServerError)
		return
	}

	// Fetch all students
	var students []models.User
	if err := c.DB.Where("role_id = ?", 7).Find(&students).Error; err != nil {
		http.Error(w, "failed to fetch students", http.StatusInternalServerError)
		return
	}

	// Notify all students
	for _, student := range students {
		message := fmt.Sprintf(
			"Dear %s, a new proposal submission window '%s' is now open.\nSubmit your proposal between %s and %s.",
			student.Profile.FirstName,
			window.Title,
			start.Format("02 Jan 2006"),
			deadline.Format("02 Jan 2006"),
		)
		_ = c.CreateNotification(student.UserID, "Proposal Submission Open", message)
	}

	// Audit log
	_ = c.LogAudit(supervisor.UserID, "create_submission_window", nil, nil, nil, nil)

	// Respond with string dates
	resp := map[string]interface{}{
		"window_id":  window.WindowID,
		"title":      window.Title,
		"start_date": start.Format("2006-01-02"),
		"deadline":   deadline.Format("2006-01-02"),
		"created_by": supervisor.Profile.FirstName + " " + supervisor.Profile.LastName,
		"created_at": window.CreatedAt.Format("2006-01-02 15:04:05"),
		"updated_at": window.UpdatedAt.Format("2006-01-02 15:04:05"),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

//----------------------------GETALL---------------------

func (c *Construct) GetSubmissionWindows(w http.ResponseWriter, r *http.Request) {
	var windows []models.ProposalSubmissionWindow
	if err := c.DB.Preload("CreatedBy.Profile").Order("start_date asc").Find(&windows).Error; err != nil {
		http.Error(w, "failed to fetch submission windows", http.StatusInternalServerError)
		return
	}

	resp := make([]map[string]interface{}, 0)
	for _, wdw := range windows {
		supervisorName := ""
		if wdw.CreatedBy.UserID != 0 {
			supervisorName = wdw.CreatedBy.Profile.FirstName + " " + wdw.CreatedBy.Profile.LastName
		}

		resp = append(resp, map[string]interface{}{
			"window_id":  wdw.WindowID,
			"title":      wdw.Title,
			"start_date": wdw.StartDate.Format("2006-01-02"),
			"deadline":   wdw.Deadline.Format("2006-01-02"),
			"created_by": supervisorName,
			"created_at": wdw.CreatedAt.Format("2006-01-02 15:04:05"),
			"updated_at": wdw.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
