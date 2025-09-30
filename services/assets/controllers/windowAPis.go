package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"web/services/assets/models"
)

// ======================Create API for submssion window================
func (c *Construct) CreateSubmissionWindow(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Title     string    `json:"title"`
		StartDate time.Time `json:"start_date"`
		Deadline  time.Time `json:"deadline"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if payload.Title == "" || payload.Deadline.Before(payload.StartDate) {
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
		StartDate:   payload.StartDate,
		Deadline:    payload.Deadline,
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
			student.Profile.FirstName, // Assuming Profile relation exists
			window.Title,
			payload.StartDate.Format("02 Jan 2006"),
			payload.Deadline.Format("02 Jan 2006"),
		)
		_ = c.CreateNotification(student.UserID, "Proposal Submission Open", message)
	}

	// Audit log
	_ = c.LogAudit(supervisor.UserID, "create_submission_window", nil, nil, nil, nil)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(window)
}

//----------------------------GETALL---------------------

func (c *Construct) GetSubmissionWindows(w http.ResponseWriter, r *http.Request) {
	var windows []models.ProposalSubmissionWindow

	// Preload CreatedBy.Profile so we can get first and last name
	if err := c.DB.Preload("CreatedBy.Profile").Order("start_date asc").Find(&windows).Error; err != nil {
		http.Error(w, "failed to fetch submission windows", http.StatusInternalServerError)
		return
	}

	// Build response with full name
	var resp []map[string]interface{}
	for _, wdw := range windows {
		supervisorName := ""
		if wdw.CreatedBy.UserID != 0 {
			supervisorName = wdw.CreatedBy.Profile.FirstName + " " + wdw.CreatedBy.Profile.LastName
		}

		resp = append(resp, map[string]interface{}{
			"window_id":  wdw.WindowID,
			"title":      wdw.Title,
			"start_date": wdw.StartDate,
			"deadline":   wdw.Deadline,
			"created_by": supervisorName,
			"created_at": wdw.CreatedAt,
			"updated_at": wdw.UpdatedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// checking all available windows
func (c *Construct) CheckSubmissionWindows() error {
	var windows []models.ProposalSubmissionWindow
	if err := c.DB.Find(&windows).Error; err != nil {
		return err
	}

	now := time.Now()

	for _, window := range windows {
		// Compute days left
		daysLeft := int(window.Deadline.Sub(now).Hours() / 24)

		// Fetch proposals linked to this window
		var proposals []models.Proposal
		_ = c.DB.Where("window_id = ?", window.WindowID).Find(&proposals)

		for _, proposal := range proposals {
			switch daysLeft {
			case 3:
				_ = c.CreateNotification(proposal.SubmittedByID, "Proposal Deadline Approaching",
					"Your proposal '"+proposal.Title+"' is due in 3 days (deadline: "+window.Deadline.Format("2006-01-02")+")")
			case 1:
				_ = c.CreateNotification(proposal.SubmittedByID, "Proposal Deadline Tomorrow",
					"Your proposal '"+proposal.Title+"' is due tomorrow (deadline: "+window.Deadline.Format("2006-01-02")+")")
			case 0:
				_ = c.CreateNotification(proposal.SubmittedByID, "Proposal Deadline Today",
					"Your proposal '"+proposal.Title+"' is due today (deadline: "+window.Deadline.Format("2006-01-02")+")")
			default:
				if daysLeft < 0 && proposal.Status != "Expired" {
					proposal.Status = "Expired"
					_ = c.DB.Save(&proposal)
					_ = c.CreateNotification(proposal.SubmittedByID, "Proposal Expired",
						"Your proposal '"+proposal.Title+"' has passed its submission deadline ("+window.Deadline.Format("2006-01-02")+")")
				}
			}
		}
	}

	return nil
}
