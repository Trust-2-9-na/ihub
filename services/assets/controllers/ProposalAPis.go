package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
	"web/services/assets/models"

	"github.com/gorilla/mux"
	"gorm.io/gorm"
)

type ProposalSummary struct {
	ProposalID  uint64   `json:"proposal_id"`
	Title       string   `json:"title"`
	Abstract    string   `json:"abstract"`
	DocumentURL string   `json:"document_url"`
	Status      string   `json:"status"`
	SubmittedBy string   `json:"submitted_by"`
	WindowTitle string   `json:"window_title"`
	TeamName    string   `json:"team_name,omitempty"`
	TeamMembers []string `json:"team_members,omitempty"`
	CreatedAt   string   `json:"created_at"`
}

// ─── CREATE PROPOSAL ───────────────────────────────────────────
// CreateProposal allows a student to create a new proposal
func (c *Construct) CreateProposal(w http.ResponseWriter, r *http.Request) {
	// Parse incoming JSON payload
	var payload struct {
		Title    string  `json:"title"`
		Abstract string  `json:"abstract"`
		Document *string `json:"document_url"`
		TeamID   *uint64 `json:"team_id"`
		WindowID uint64  `json:"window_id"`
		Submit   bool    `json:"submit"` // true if user wants to submit now
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	// Basic validation
	if payload.Title == "" || payload.Abstract == "" {
		http.Error(w, "Title and abstract are required", http.StatusBadRequest)
		return
	}

	// Get current user from context
	userUUID, ok := r.Context().Value("user_uuid").(string)
	if !ok || userUUID == "" {
		http.Error(w, "Unauthorized: missing user UUID", http.StatusUnauthorized)
		return
	}

	var user models.User
	if err := c.DB.Preload("Profile").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}

	// Verify submission window exists
	var window models.ProposalSubmissionWindow
	if err := c.DB.First(&window, "window_id = ?", payload.WindowID).Error; err != nil {
		http.Error(w, "Submission window not found", http.StatusBadRequest)
		return
	}

	// If team is provided, check it exists
	if payload.TeamID != nil {
		var team models.Team
		if err := c.DB.First(&team, "team_id = ?", *payload.TeamID).Error; err != nil {
			http.Error(w, "Team not found", http.StatusBadRequest)
			return
		}
	}

	// Set proposal status
	status := models.ProposalStatusDraft
	var submissionDate *time.Time
	if payload.Submit {
		status = models.ProposalStatusSubmitted
		t := time.Now()
		submissionDate = &t
	}

	// Create proposal record
	proposal := models.Proposal{
		Title:          payload.Title,
		Abstract:       payload.Abstract,
		DocumentURL:    payload.Document,
		SubmittedByID:  user.UserID,
		TeamID:         payload.TeamID,
		WindowID:       payload.WindowID,
		Status:         status,
		SubmissionDate: submissionDate,
	}

	if err := c.DB.Create(&proposal).Error; err != nil {
		http.Error(w, "Failed to create proposal", http.StatusInternalServerError)
		return
	}

	// Notify user
	notificationMsg := "Your proposal has been saved as Draft."
	if status == models.ProposalStatusSubmitted {
		notificationMsg = "Your proposal has been submitted successfully. Wait for supervisor approval."
	}
	_ = c.CreateNotification(user.UserID, "Proposal Created", notificationMsg)

	// Audit log
	_ = c.LogAudit(user.UserID, "create_proposal", nil, &proposal.ProposalID, nil, nil)

	// Respond with minimal info
	resp := map[string]interface{}{
		"message":     notificationMsg,
		"proposal_id": proposal.ProposalID,
		"title":       proposal.Title,
		"status":      proposal.Status,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// ─── UPDATE PROPOSAL ───────────────────────────────────────────
func (c *Construct) UpdateProposal(w http.ResponseWriter, r *http.Request) {
	// Extract proposal_id from path param
	vars := mux.Vars(r)
	proposalIDStr := vars["proposal_id"]

	proposalID, err := strconv.ParseUint(proposalIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid proposal id", http.StatusBadRequest)
		return
	}

	// Request payload
	var payload struct {
		Title    *string `json:"title"`
		Abstract *string `json:"abstract"`
		Document *string `json:"document_url"`
		WindowID *uint64 `json:"window_id"`
		TeamID   *uint64 `json:"team_id"`
		Submit   *bool   `json:"submit"` // optional submit flag
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Verify current user
	userUUID := r.Context().Value("user_uuid").(string)
	var user models.User
	if err := c.DB.Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	// Fetch proposal
	var proposal models.Proposal
	if err := c.DB.First(&proposal, "proposal_id = ?", proposalID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "proposal not found", http.StatusNotFound)
			return
		}
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// Check ownership
	if proposal.SubmittedByID != user.UserID {
		http.Error(w, "not allowed to update this proposal", http.StatusForbidden)
		return
	}

	// Apply updates if provided
	if payload.Title != nil {
		proposal.Title = *payload.Title
	}
	if payload.Abstract != nil {
		proposal.Abstract = *payload.Abstract
	}
	if payload.Document != nil {
		proposal.DocumentURL = payload.Document
	}
	if payload.WindowID != nil {
		var window models.ProposalSubmissionWindow
		if err := c.DB.First(&window, "window_id = ?", *payload.WindowID).Error; err != nil {
			http.Error(w, "submission window not found", http.StatusBadRequest)
			return
		}
		proposal.WindowID = *payload.WindowID
	}
	if payload.TeamID != nil {
		var team models.Team
		if err := c.DB.First(&team, "team_id = ?", *payload.TeamID).Error; err != nil {
			http.Error(w, "team not found", http.StatusBadRequest)
			return
		}
		proposal.TeamID = payload.TeamID
	}

	// Handle submission
	if payload.Submit != nil && *payload.Submit {
		proposal.Status = models.ProposalStatusSubmitted
		now := time.Now()
		proposal.SubmissionDate = &now
	} else {
		proposal.Status = models.ProposalStatusDraft
		proposal.SubmissionDate = nil
	}

	// Save updates
	if err := c.DB.Save(&proposal).Error; err != nil {
		http.Error(w, "failed to update proposal", http.StatusInternalServerError)
		return
	}

	// Notify user
	notificationMsg := "Your proposal has been updated and saved as Draft."
	if proposal.Status == models.ProposalStatusSubmitted {
		notificationMsg = "Your proposal has been updated and submitted successfully."
	}
	_ = c.CreateNotification(user.UserID, "Proposal Updated", notificationMsg)

	// Audit log
	_ = c.LogAudit(user.UserID, "update_proposal", nil, &proposal.ProposalID, nil, nil)

	// Build clean response DTO
	type ProposalResponse struct {
		ProposalID     uint64     `json:"proposal_id"`
		Title          string     `json:"title"`
		Abstract       string     `json:"abstract"`
		DocumentURL    string     `json:"document_url"`
		Status         string     `json:"status"`
		SubmissionDate *time.Time `json:"submission_date,omitempty"`
		TeamID         *uint64    `json:"team_id,omitempty"`
		Window         *struct {
			Title    string    `json:"title"`
			Deadline time.Time `json:"deadline"`
		} `json:"window,omitempty"`
	}

	resp := ProposalResponse{
		ProposalID:     proposal.ProposalID,
		Title:          proposal.Title,
		Abstract:       proposal.Abstract,
		DocumentURL:    *proposal.DocumentURL,
		Status:         string(proposal.Status),
		SubmissionDate: proposal.SubmissionDate,
		TeamID:         proposal.TeamID,
	}

	// Fetch only needed window info
	if proposal.WindowID != 0 {
		var window models.ProposalSubmissionWindow
		if err := c.DB.Select("title", "deadline").
			First(&window, "window_id = ?", proposal.WindowID).Error; err == nil {
			resp.Window = &struct {
				Title    string    `json:"title"`
				Deadline time.Time `json:"deadline"`
			}{
				Title:    window.Title,
				Deadline: window.Deadline,
			}
		}
	}

	// Respond
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":  notificationMsg,
		"proposal": resp,
	})
}

// ─── GET OWN PROPOSAL ───────────────────────────────────────
func (c *Construct) GetOwnProposals(w http.ResponseWriter, r *http.Request) {
	// Get logged-in user UUID from context
	userUUID, ok := r.Context().Value("user_uuid").(string)
	if !ok || userUUID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var user models.User
	if err := c.DB.Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}

	// Fetch user's proposals with window and team
	var proposals []models.Proposal
	if err := c.DB.Preload("Window").
		Preload("Team").
		Preload("Team.Users.Profile").
		Where("submitted_by_id = ? AND archived = ?", user.UserID, false).
		Order("created_at desc").
		Find(&proposals).Error; err != nil {
		http.Error(w, "Failed to fetch proposals", http.StatusInternalServerError)
		return
	}

	// Build response
	type reviewResp struct {
		ReviewID   uint64  `json:"review_id"`
		Comments   *string `json:"comments,omitempty"`
		Decision   string  `json:"decision"`
		ReviewedBy string  `json:"reviewed_by"`
		ReviewDate string  `json:"review_date"`
	}

	type windowResp struct {
		Title    string `json:"title"`
		Deadline string `json:"deadline"`
	}

	type teamResp struct {
		TeamID uint64 `json:"team_id"`
		Name   string `json:"name"`
	}

	type proposalResp struct {
		ProposalID  uint64       `json:"proposal_id"`
		Title       string       `json:"title"`
		Abstract    string       `json:"abstract"`
		DocumentURL *string      `json:"document_url,omitempty"`
		Status      string       `json:"status"`
		Window      windowResp   `json:"window"`
		Team        *teamResp    `json:"team,omitempty"`
		Reviews     []reviewResp `json:"reviews,omitempty"`
		CreatedAt   string       `json:"created_at"`
	}

	var resp []proposalResp
	for _, p := range proposals {
		// Fetch reviews for this proposal
		var reviews []models.ProposalReview
		if err := c.DB.Preload("ReviewedBy.Profile").
			Where("proposal_id = ?", p.ProposalID).Find(&reviews).Error; err != nil {
			reviews = []models.ProposalReview{}
		}

		var revs []reviewResp
		for _, r := range reviews {
			reviewer := r.ReviewedBy.Profile.FirstName + " " + r.ReviewedBy.Profile.LastName
			revs = append(revs, reviewResp{
				ReviewID:   r.ReviewID,
				Comments:   r.Comments,
				Decision:   r.Decision,
				ReviewedBy: reviewer,
				ReviewDate: r.ReviewDate.Format("2006-01-02"),
			})
		}

		// Map team if exists
		var team *teamResp
		if p.Team != nil {
			team = &teamResp{
				TeamID: p.Team.TeamID,
				Name:   p.Team.Name,
			}
		}

		resp = append(resp, proposalResp{
			ProposalID:  p.ProposalID,
			Title:       p.Title,
			Abstract:    p.Abstract,
			DocumentURL: p.DocumentURL,
			Status:      p.Status,
			Window: windowResp{
				Title:    p.Window.Title,
				Deadline: p.Window.Deadline.Format("2006-01-02"),
			},
			Team:      team,
			Reviews:   revs,
			CreatedAt: p.CreatedAt.Format("2006-01-02"),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ─── ARCHIVE / RESTORE MULTIPLE PROPOSAL ────────────────────────────────

func (c *Construct) ArchiveRestoreProposals(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	var body struct {
		ProposalIDs []uint64 `json:"proposal_ids"`
		Action      string   `json:"action"` // "archive" or "restore"
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	if len(body.ProposalIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "No proposal IDs provided", nil)
		return
	}

	if body.Action != "archive" && body.Action != "restore" {
		c.Json(w, http.StatusBadRequest, "Invalid action, must be 'archive' or 'restore'", nil)
		return
	}

	// Get user from context
	userUUIDCtx := r.Context().Value("user_uuid")
	if userUUIDCtx == nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	userUUID, ok := userUUIDCtx.(string)
	if !ok {
		c.Json(w, http.StatusInternalServerError, "Invalid user context", nil)
		return
	}

	var user models.User
	if err := c.DB.Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	// Update proposals
	updateValue := body.Action == "archive"
	result := c.DB.Model(&models.Proposal{}).
		Where("proposal_id IN ? AND submitted_by_id = ?", body.ProposalIDs, user.UserID).
		Updates(map[string]interface{}{"archived": updateValue})
	if result.Error != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update proposals", map[string]interface{}{"error": result.Error.Error()})
		return
	}

	if result.RowsAffected == 0 {
		c.Json(w, http.StatusNotFound, "Proposals not found or not owned by you", nil)
		return
	}

	// Log audit
	for _, pid := range body.ProposalIDs {
		pidStr := strconv.FormatUint(pid, 10)
		_ = c.LogAudit(user.UserID, body.Action+"_proposal", &pidStr, nil, nil, nil)
	}

	// Fetch updated proposals with preloaded relations
	var updatedProposals []models.Proposal
	if err := c.DB.Preload("SubmittedBy.Profile").
		Preload("Team.Users.Profile").
		Preload("Window").
		Where("proposal_id IN ?", body.ProposalIDs).
		Find(&updatedProposals).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch updated proposals", map[string]interface{}{"error": err.Error()})
		return
	}

	// Respond
	c.Json(w, http.StatusOK,
		fmt.Sprintf("%d proposal(s) %sd successfully", len(updatedProposals), body.Action),
		map[string]interface{}{"proposals": updatedProposals},
	)
}

// ─── GET PROPOSALS WITH PAGINATION ─────────────────────────────
func (c *Construct) GetProposals(w http.ResponseWriter, r *http.Request) {
	page, limit := 1, 10
	if p := r.URL.Query().Get("page"); p != "" {
		if pp, err := strconv.Atoi(p); err == nil && pp > 0 {
			page = pp
		}
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		if ll, err := strconv.Atoi(l); err == nil && ll > 0 {
			limit = ll
		}
	}

	offset := (page - 1) * limit
	var total int64
	if err := c.DB.Model(&models.Proposal{}).Where("archived = ?", false).Count(&total).Error; err != nil {
		http.Error(w, "failed to count proposals", http.StatusInternalServerError)
		return
	}

	var proposals []models.Proposal
	if err := c.DB.Preload("SubmittedBy.Profile").
		Preload("Team.Users.Profile").
		Preload("Window").
		Where("archived = ?", false).
		Offset(offset).Limit(limit).
		Find(&proposals).Error; err != nil {
		http.Error(w, "failed to fetch proposals", http.StatusInternalServerError)
		return
	}

	type ProposalSummary struct {
		ProposalID  uint64   `json:"proposal_id"`
		Title       string   `json:"title"`
		Abstract    string   `json:"abstract"`
		DocumentURL string   `json:"document_url"`
		Status      string   `json:"status"`
		SubmittedBy string   `json:"submitted_by"`
		WindowTitle string   `json:"window_title"`
		TeamName    string   `json:"team_name,omitempty"`
		TeamMembers []string `json:"team_members,omitempty"`
		CreatedAt   string   `json:"created_at"`
	}

	var resp []ProposalSummary
	for _, p := range proposals {
		submittedBy := ""
		if p.SubmittedBy.UserID != 0 {
			submittedBy = p.SubmittedBy.Profile.FirstName + " " + p.SubmittedBy.Profile.LastName
		}

		windowTitle := ""
		if p.Window.WindowID != 0 {
			windowTitle = p.Window.Title
		}

		teamName := ""
		var teamMembers []string
		if p.Team != nil && p.Team.TeamID != 0 {
			teamName = p.Team.Name
			for _, member := range p.Team.Users {
				if member.Profile.FirstName != "" && member.Profile.LastName != "" {
					teamMembers = append(teamMembers, member.Profile.FirstName+" "+member.Profile.LastName)
				}
			}
		}

		resp = append(resp, ProposalSummary{
			ProposalID:  p.ProposalID,
			Title:       p.Title,
			Abstract:    p.Abstract,
			DocumentURL: *p.DocumentURL,
			Status:      p.Status,
			SubmittedBy: submittedBy,
			WindowTitle: windowTitle,
			TeamName:    teamName,
			TeamMembers: teamMembers,
			CreatedAt:   p.CreatedAt.Format("2006-01-02 15:04"),
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"page":      page,
		"limit":     limit,
		"total":     total,
		"proposals": resp,
	})
}

// DeleteProposal allows a student to delete one or multiple of their own proposals via JSON
func (c *Construct) DeleteProposal(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	var body struct {
		ProposalIDs []uint64 `json:"proposal_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	if len(body.ProposalIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "No proposal IDs provided", nil)
		return
	}

	// Get user_uuid from context
	userUUIDCtx := r.Context().Value("user_uuid")
	if userUUIDCtx == nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	userUUID, ok := userUUIDCtx.(string)
	if !ok {
		c.Json(w, http.StatusInternalServerError, "Invalid user context", nil)
		return
	}

	// Fetch numeric user ID
	var user models.User
	if err := c.DB.Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	// Delete proposals belonging to the user
	result := c.DB.Where("proposal_id IN ? AND submitted_by_id = ?", body.ProposalIDs, user.UserID).Delete(&models.Proposal{})
	if result.Error != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to delete proposals", map[string]interface{}{"error": result.Error.Error()})
		return
	}

	if result.RowsAffected == 0 {
		c.Json(w, http.StatusNotFound, "Proposals not found or not owned by you", nil)
		return
	}

	// Log audit for each deleted proposal
	for _, pid := range body.ProposalIDs {
		pidStr := strconv.FormatUint(pid, 10) // convert uint64 to string
		_ = c.LogAudit(user.UserID, "delete_proposal", &pidStr, nil, nil, nil)
	}

	c.Json(w, http.StatusOK, fmt.Sprintf("%d proposal(s) deleted successfully", result.RowsAffected), nil)
}
