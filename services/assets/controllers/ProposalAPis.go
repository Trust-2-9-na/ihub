package controllers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"web/services/assets/models"

	"gorm.io/gorm"
)

// ─── CREATE PROPOSAL ───────────────────────────────────────────
func (c *Construct) CreateProposal(w http.ResponseWriter, r *http.Request) {
	// Payload from client
	var payload struct {
		Title    string  `json:"title"`
		Abstract string  `json:"abstract"`
		Document *string `json:"document_url"`
		TeamID   *uint64 `json:"team_id"`
		WindowID uint64  `json:"window_id"` // must link to a submission window
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if payload.Title == "" || payload.Abstract == "" {
		http.Error(w, "title and abstract are required", http.StatusBadRequest)
		return
	}

	// Get current user from middleware
	userUUID := r.Context().Value("user_uuid").(string)
	var user models.User
	if err := c.DB.Preload("Profile").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	// Verify submission window exists
	var window models.ProposalSubmissionWindow
	if err := c.DB.Preload("CreatedBy.Profile").First(&window, "window_id = ?", payload.WindowID).Error; err != nil {
		http.Error(w, "submission window not found", http.StatusBadRequest)
		return
	}

	// Create proposal
	proposal := models.Proposal{
		Title:         payload.Title,
		Abstract:      payload.Abstract,
		DocumentURL:   payload.Document,
		SubmittedByID: user.UserID,
		TeamID:        payload.TeamID,
		WindowID:      payload.WindowID,
		Status:        models.ProposalStatusDraft,
	}

	if err := c.DB.Create(&proposal).Error; err != nil {
		http.Error(w, "failed to create proposal", http.StatusInternalServerError)
		return
	}

	// Reload proposal with all relations for response
	if err := c.DB.Preload("SubmittedBy.Profile").
		Preload("Team.Users.Profile").
		Preload("Window.CreatedBy.Profile").
		First(&proposal, "proposal_id = ?", proposal.ProposalID).Error; err != nil {
		http.Error(w, "failed to fetch proposal details", http.StatusInternalServerError)
		return
	}

	// Send notification to the student
	_ = c.CreateNotification(user.UserID, "Proposal Created", "Your proposal has been created successfully.")

	// Log audit
	_ = c.LogAudit(user.UserID, "create_proposal", nil, &proposal.ProposalID, nil, nil)

	// Build structured response
	resp := map[string]interface{}{
		"message":  "proposal created successfully",
		"proposal": proposal,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// ─── UPDATE PROPOSAL ───────────────────────────────────────────
func (c *Construct) UpdateProposal(w http.ResponseWriter, r *http.Request) {
	proposalIDStr := r.URL.Query().Get("proposal_id")
	proposalID, err := strconv.ParseUint(proposalIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid proposal id", http.StatusBadRequest)
		return
	}

	var payload struct {
		Title    *string `json:"title"`
		Abstract *string `json:"abstract"`
		Document *string `json:"document_url"`
		WindowID *uint64 `json:"window_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	userUUID := r.Context().Value("user_uuid").(string)
	var user models.User
	if err := c.DB.Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	var proposal models.Proposal
	if err := c.DB.First(&proposal, "proposal_id = ?", proposalID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "proposal not found", http.StatusNotFound)
			return
		}
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	if proposal.SubmittedByID != user.UserID {
		http.Error(w, "not allowed to update this proposal", http.StatusForbidden)
		return
	}

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

	if err := c.DB.Save(&proposal).Error; err != nil {
		http.Error(w, "failed to update proposal", http.StatusInternalServerError)
		return
	}

	_ = c.LogAudit(user.UserID, "update_proposal", nil, &proposal.ProposalID, nil, nil)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":  "proposal updated successfully",
		"proposal": proposal,
	})
}

// ─── GET SINGLE PROPOSAL ───────────────────────────────────────
func (c *Construct) GetProposal(w http.ResponseWriter, r *http.Request) {
	proposalIDStr := r.URL.Query().Get("proposal_id")
	proposalID, err := strconv.ParseUint(proposalIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid proposal id", http.StatusBadRequest)
		return
	}

	var proposal models.Proposal
	if err := c.DB.Preload("SubmittedBy.Profile").
		Preload("Team.Users.Profile").
		Preload("Window").
		First(&proposal, "proposal_id = ?", proposalID).Error; err != nil {
		http.Error(w, "proposal not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(proposal)
}

// ─── ARCHIVE / RESTORE PROPOSAL ────────────────────────────────
func (c *Construct) ArchiveProposal(w http.ResponseWriter, r *http.Request) {
	proposalIDStr := r.URL.Query().Get("proposal_id")
	proposalID, err := strconv.ParseUint(proposalIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid proposal id", http.StatusBadRequest)
		return
	}

	action := r.URL.Query().Get("action")

	var proposal models.Proposal
	if err := c.DB.First(&proposal, "proposal_id = ?", proposalID).Error; err != nil {
		http.Error(w, "proposal not found", http.StatusNotFound)
		return
	}

	switch action {
	case "archive":
		proposal.Archived = true
	case "restore":
		proposal.Archived = false
	default:
		http.Error(w, "invalid action", http.StatusBadRequest)
		return
	}

	if err := c.DB.Save(&proposal).Error; err != nil {
		http.Error(w, "failed to update archive status", http.StatusInternalServerError)
		return
	}

	userUUID := r.Context().Value("user_uuid").(string)
	var user models.User
	_ = c.DB.Where("user_uuid = ?", userUUID).First(&user)
	_ = c.LogAudit(user.UserID, action+"_proposal", nil, &proposal.ProposalID, nil, nil)

	if err := c.DB.Preload("SubmittedBy.Profile").Preload("Team.Users.Profile").Preload("Window").
		First(&proposal, "proposal_id = ?", proposal.ProposalID).Error; err != nil {
		http.Error(w, "failed to fetch proposal details", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(proposal)
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

	json.NewEncoder(w).Encode(map[string]interface{}{
		"page":      page,
		"limit":     limit,
		"total":     total,
		"proposals": proposals,
	})
}

// DeleteProposal allows a user to delete their own proposal
func (c *Construct) DeleteProposal(w http.ResponseWriter, r *http.Request) {
	proposalIDStr := r.URL.Query().Get("proposal_id")
	if proposalIDStr == "" {
		http.Error(w, "proposal_id is required", http.StatusBadRequest)
		return
	}

	proposalID, err := strconv.ParseUint(proposalIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid proposal_id", http.StatusBadRequest)
		return
	}

	// Get current user from middleware/context
	userUUID := r.Context().Value("user_uuid").(string)
	var user models.User
	if err := c.DB.Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	var proposal models.Proposal
	if err := c.DB.First(&proposal, "proposal_id = ?", proposalID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "proposal not found", http.StatusNotFound)
			return
		}
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// Only allow the submitter to delete
	if proposal.SubmittedByID != user.UserID {
		http.Error(w, "not allowed to delete this proposal", http.StatusForbidden)
		return
	}

	if err := c.DB.Delete(&proposal).Error; err != nil {
		http.Error(w, "failed to delete proposal", http.StatusInternalServerError)
		return
	}

	// Audit log
	_ = c.LogAudit(user.UserID, "delete_proposal", nil, &proposal.ProposalID, nil, nil)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":     "proposal deleted successfully",
		"proposal_id": proposalID,
	})
}
