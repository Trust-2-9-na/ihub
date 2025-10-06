package controllers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"web/services/assets/models"

	"github.com/gorilla/mux"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type ProposalSummary struct {
	ProposalID  uint64   `json:"proposal_id"`
	Title       string   `json:"title"`
	Abstract    string   `json:"abstract"`
	DocumentURL string   `json:"document_url"`
	Category    string   `json:"category"`
	Status      string   `json:"status"`
	SubmittedBy string   `json:"submitted_by"`
	WindowTitle string   `json:"window_title"`
	TeamName    string   `json:"team_name,omitempty"`
	TeamMembers []string `json:"team_members,omitempty"`
	CreatedAt   string   `json:"created_at"`
	Subfield    *string  `json:"subfield,omitempty"`
}

// ─── CREATE PROPOSAL ───────────────────────────────────────────
func (c *Construct) CreateProposal(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Title    string  `json:"title"`
		Abstract string  `json:"abstract"`
		Document *string `json:"document_url"`
		Category string  `json:"category"`
		Subfield *string `json:"subfield,omitempty"`
		TeamID   *uint64 `json:"team_id"`
		WindowID uint64  `json:"window_id"`
		Submit   bool    `json:"submit"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	if payload.Title == "" || payload.Abstract == "" {
		http.Error(w, "title and abstract required", http.StatusBadRequest)
		return
	}

	// ─── Validate Document Type ────────────────────────────────
	if payload.Document != nil && *payload.Document != "" {
		allowedExts := []string{".pdf", ".docx"}
		allowedMIMEs := []string{
			"application/pdf",
			"application/msword",
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		}

		// --- Extension validation ---
		validExt := false
		for _, ext := range allowedExts {
			if strings.HasSuffix(strings.ToLower(*payload.Document), ext) {
				validExt = true
				break
			}
		}

		if !validExt {
			http.Error(w, "invalid document type: only PDF and DOCX files are allowed", http.StatusBadRequest)
			return
		}

		// --- MIME type validation (only if file bytes available) ---
		file, _, err := r.FormFile("document")
		if err == nil {
			defer file.Close()
			buf := make([]byte, 512)
			_, _ = file.Read(buf)
			mimeType := http.DetectContentType(buf)

			validMime := false
			for _, m := range allowedMIMEs {
				if mimeType == m {
					validMime = true
					break
				}
			}

			if !validMime {
				http.Error(w, "invalid file content type", http.StatusBadRequest)
				return
			}
		}
	}

	// ─── Get User from Context ────────────────────────────────
	userUUID, ok := r.Context().Value("user_uuid").(string)
	if !ok || userUUID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var user models.User
	if err := c.DB.Preload("Profile").First(&user, "user_uuid = ?", userUUID).Error; err != nil {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	// ─── Validate Window ───────────────────────────────────────
	var window models.ProposalSubmissionWindow
	if err := c.DB.First(&window, "window_id = ?", payload.WindowID).Error; err != nil {
		http.Error(w, "submission window not found", http.StatusBadRequest)
		return
	}

	// ─── Validate Team ─────────────────────────────────────────
	if payload.TeamID != nil {
		var team models.Team
		if err := c.DB.First(&team, "team_id = ?", *payload.TeamID).Error; err != nil {
			http.Error(w, "team not found", http.StatusBadRequest)
			return
		}
	}

	// ─── Determine Status ─────────────────────────────────────
	status := models.ProposalStatusDraft
	var submissionDate *time.Time
	if payload.Submit {
		status = models.ProposalStatusSubmitted
		t := time.Now()
		submissionDate = &t
	}

	// ─── Create Proposal Record ────────────────────────────────
	proposal := models.Proposal{
		Title:          payload.Title,
		Abstract:       payload.Abstract,
		DocumentURL:    payload.Document,
		Category:       payload.Category,
		Subfield:       payload.Subfield,
		SubmittedByID:  user.UserID,
		TeamID:         payload.TeamID,
		WindowID:       payload.WindowID,
		Status:         status,
		SubmissionDate: submissionDate,
	}

	if err := c.DB.Create(&proposal).Error; err != nil {
		http.Error(w, "failed to create proposal", http.StatusInternalServerError)
		return
	}

	// ─── Track History ─────────────────────────────────────────
	comment := ""
	if payload.Submit {
		comment = fmt.Sprintf("Submitted proposal: %s", proposal.Title)
	}

	c.NotifyAndTrack(
		user.UserID,
		"Proposal Created",
		comment,
		"CreateProposal",
		"Proposal",
		&proposal.ProposalID,
		status,
	)

	// ─── Notify Supervisors if Submitted ───────────────────────
	if payload.Submit {
		var supervisors []models.User
		if err := c.DB.Joins("Role").Where("roles.name = ?", "Supervisor").Find(&supervisors).Error; err == nil {
			for _, sup := range supervisors {
				c.NotifyAndTrack(
					sup.UserID,
					"New Proposal Submitted",
					fmt.Sprintf("Student %s submitted a proposal: %s", user.Username, proposal.Title),
					"Notification",
					"Proposal",
					&proposal.ProposalID,
					status,
				)
			}
		}
	}

	// ─── Response ─────────────────────────────────────────────
	resp := map[string]interface{}{
		"message":     "Proposal created successfully",
		"proposal_id": proposal.ProposalID,
		"status":      proposal.Status,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// ─── UPDATE PROPOSAL ───────────────────────────────────────────
func (c *Construct) UpdateProposal(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	proposalIDStr := vars["proposal_id"]
	proposalID, err := strconv.ParseUint(proposalIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid proposal id", http.StatusBadRequest)
		return
	}

	var payload struct {
		Title    *string `json:"title"`
		Abstract *string `json:"abstract"`
		Document *string `json:"document_url"` // updated file URL
		Category *string `json:"category"`
		Subfield *string `json:"subfield"`
		WindowID *uint64 `json:"window_id"`
		TeamID   *uint64 `json:"team_id"`
		Submit   *bool   `json:"submit"` // optional submit flag
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	// ─── Validate Document Type (PDF or DOCX only) ─────────────────────────────
	if payload.Document != nil && *payload.Document != "" {
		allowedExts := []string{".pdf", ".docx"}
		allowedMIMEs := []string{
			"application/pdf",
			"application/msword",
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		}

		// --- Extension validation ---
		validExt := false
		for _, ext := range allowedExts {
			if strings.HasSuffix(strings.ToLower(*payload.Document), ext) {
				validExt = true
				break
			}
		}

		if !validExt {
			http.Error(w, "invalid document type: only PDF and DOCX files are allowed", http.StatusBadRequest)
			return
		}

		// --- MIME type validation (only if file bytes available) ---
		file, _, err := r.FormFile("document")
		if err == nil {
			defer file.Close()
			buf := make([]byte, 512)
			_, _ = file.Read(buf)
			mimeType := http.DetectContentType(buf)

			validMime := false
			for _, m := range allowedMIMEs {
				if mimeType == m {
					validMime = true
					break
				}
			}

			if !validMime {
				http.Error(w, "invalid file content type", http.StatusBadRequest)
				return
			}
		}
	}

	// ─── Get Current User ──────────────────────────────────────────────────────
	userUUID, ok := r.Context().Value("user_uuid").(string)
	if !ok || userUUID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var user models.User
	if err := c.DB.Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	// ─── Fetch Proposal ───────────────────────────────────────────────────────
	var proposal models.Proposal
	if err := c.DB.First(&proposal, "proposal_id = ?", proposalID).Error; err != nil {
		http.Error(w, "proposal not found", http.StatusNotFound)
		return
	}

	// ─── Authorization Check ───────────────────────────────────────────────────
	if proposal.SubmittedByID != user.UserID {
		http.Error(w, "not allowed to update this proposal", http.StatusForbidden)
		return
	}

	// ─── Update Fields ─────────────────────────────────────────────────────────
	if payload.Title != nil {
		proposal.Title = *payload.Title
	}
	if payload.Abstract != nil {
		proposal.Abstract = *payload.Abstract
	}
	if payload.Document != nil {
		proposal.DocumentURL = payload.Document
	}
	if payload.Category != nil {
		proposal.Category = *payload.Category
	}
	if payload.Subfield != nil {
		proposal.Subfield = payload.Subfield
	}
	if payload.WindowID != nil {
		proposal.WindowID = *payload.WindowID
	}
	if payload.TeamID != nil {
		proposal.TeamID = payload.TeamID
	}

	// ─── Handle Submission ─────────────────────────────────────────────────────
	if payload.Submit != nil && *payload.Submit {
		proposal.Status = models.ProposalStatusSubmitted
		t := time.Now()
		proposal.SubmissionDate = &t
	}

	if err := c.DB.Save(&proposal).Error; err != nil {
		http.Error(w, "failed to update proposal", http.StatusInternalServerError)
		return
	}

	// ─── Notify Supervisors if Submitted ───────────────────────────────────────
	if payload.Submit != nil && *payload.Submit {
		var supervisors []models.User
		if err := c.DB.Joins("Role").Where("roles.name = ?", "Supervisor").Find(&supervisors).Error; err == nil {
			for _, sup := range supervisors {
				comment := fmt.Sprintf("Student %s resubmitted proposal: %s", user.Username, proposal.Title)
				statusStr := proposal.Status
				c.NotifyAndTrack(
					sup.UserID,
					"Proposal Resubmitted",
					comment,
					"Notification",
					"Proposal",
					&proposal.ProposalID,
					statusStr,
				)
			}
		}
	}

	// ─── SystemHistory Tracking ────────────────────────────────────────────────
	statusStr := proposal.Status
	c.NotifyAndTrack(
		user.UserID,
		"Proposal Updated",
		"Updated proposal: "+proposal.Title,
		"UpdateProposal",
		"Proposal",
		&proposal.ProposalID,
		statusStr,
	)

	// ─── Response ─────────────────────────────────────────────────────────────
	resp := map[string]interface{}{
		"message":     "Proposal updated successfully",
		"proposal_id": proposal.ProposalID,
		"status":      proposal.Status,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

//=========++++======== GET PRoposals ==============+=+++==================+=======

func (c *Construct) GetOwnProposals(w http.ResponseWriter, r *http.Request) {
	// --- Get logged-in user ---
	userUUID, ok := r.Context().Value("user_uuid").(string)
	if !ok || userUUID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var user models.User
	if err := c.DB.Preload("Role").Preload("Profile").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}

	// --- Base query ---
	query := c.DB.Preload("Window").
		Preload("Team").
		Preload("Team.Users.Profile").
		Preload("SubmittedBy.Profile").
		Preload("Cohort").
		Where("archived = ?", false).
		Order("created_at desc")

	// --- Role-based filtering ---
	switch user.Role.Name {
	case "Student":
		// Student sees their own proposals including drafts
		query = query.Where("submitted_by_id = ?", user.UserID)

	case "Supervisor":
		// Supervisors see proposals in their cohorts, excluding drafts
		query = query.Where("status != ?", "Draft")
		var cohortIDs []uint64
		c.DB.Model(&models.Cohort{}).Where("created_by = ?", user.UserID).Pluck("cohort_id", &cohortIDs)
		if len(cohortIDs) > 0 {
			query = query.Where("cohort_id IN ?", cohortIDs)
		} else {
			query = query.Where("1 = 0") // no cohorts → no access
		}

	case "Admin":
		// Admins see all non-draft proposals
		query = query.Where("status != ?", "Draft")
	}

	// --- Fetch proposals ---
	var proposals []models.Proposal
	if err := query.Find(&proposals).Error; err != nil {
		http.Error(w, "Failed to fetch proposals", http.StatusInternalServerError)
		return
	}

	// --- Build response types ---
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
		Subfield    *string      `json:"subfield,omitempty"`
		Category    string       `json:"category"`
		Status      string       `json:"status"`
		Window      windowResp   `json:"window"`
		Team        *teamResp    `json:"team,omitempty"`
		Reviews     []reviewResp `json:"reviews,omitempty"`
		CreatedAt   string       `json:"created_at"`
		Cohort      string       `json:"cohort,omitempty"`
	}

	// --- Build response data ---
	var resp []proposalResp
	for _, p := range proposals {
		// Fetch reviews
		var reviews []models.ProposalReview
		if err := c.DB.Preload("ReviewedBy.Profile").Where("proposal_id = ?", p.ProposalID).Find(&reviews).Error; err != nil {
			reviews = []models.ProposalReview{}
		}

		var revs []reviewResp
		for _, r := range reviews {
			reviewer := ""
			if r.ReviewedBy != nil {
				reviewer = strings.TrimSpace(r.ReviewedBy.Profile.FirstName + " " + r.ReviewedBy.Profile.LastName)
			}

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

		cohortName := ""
		if p.Cohort != nil {
			cohortName = p.Cohort.Name
		}

		resp = append(resp, proposalResp{
			ProposalID:  p.ProposalID,
			Title:       p.Title,
			Abstract:    p.Abstract,
			DocumentURL: p.DocumentURL,
			Subfield:    p.Subfield,
			Category:    p.Category,
			Status:      p.Status,
			Window: windowResp{
				Title:    p.Window.Title,
				Deadline: p.Window.Deadline.Format("2006-01-02"),
			},
			Team:      team,
			Reviews:   revs,
			CreatedAt: p.CreatedAt.Format("2006-01-02"),
			Cohort:    cohortName,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ─── ARCHIVE / RESTORE MULTIPLE PROPOSAL ────────────────────────────────

func (c *Construct) ArchiveRestoreProposals(w http.ResponseWriter, r *http.Request) {
	// --- Parse JSON body ---
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

	// --- Get current user ---
	userUUIDCtx := r.Context().Value("user_uuid")
	if userUUIDCtx == nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	userUUID := userUUIDCtx.(string)

	var user models.User
	if err := c.DB.Preload("Role").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Determine if user can act on all proposals ---
	canActOnAll := strings.ToLower(user.Role.Name) == "supervisor" || strings.ToLower(user.Role.Name) == "admin"

	// --- Prepare fields to update ---
	updateFields := map[string]interface{}{
		"archived": body.Action == "archive",
	}
	if body.Action == "archive" {
		now := time.Now()
		updateFields["archived_at"] = &now
		updateFields["archived_by"] = user.UserID
	} else {
		updateFields["archived_at"] = nil
		updateFields["archived_by"] = nil
	}

	// --- Build query ---
	query := c.DB.Model(&models.Proposal{}).Where("proposal_id IN ?", body.ProposalIDs)
	if !canActOnAll {
		query = query.Where("submitted_by_id = ?", user.UserID)
	}

	result := query.Updates(updateFields)
	if result.Error != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update proposals", map[string]interface{}{"error": result.Error.Error()})
		return
	}

	if result.RowsAffected == 0 {
		c.Json(w, http.StatusNotFound, "Proposals not found or not allowed to modify", nil)
		return
	}

	// --- Log audit and track system history ---
	titleCaser := cases.Title(language.Und)
	for _, pid := range body.ProposalIDs {
		pidStr := strconv.FormatUint(pid, 10)
		actionComment := fmt.Sprintf("%s proposal", body.Action)
		statusStr := "" // optional, no specific status here
		c.NotifyAndTrack(
			user.UserID,
			titleCaser.String(body.Action)+" Proposal", // use cases.Title instead of strings.Title
			actionComment,
			"ArchiveRestore",
			"Proposal",
			&pid,
			statusStr,
		)
		_ = c.LogAudit(user.UserID, body.Action+"_proposal", &pidStr, nil, nil, nil)
	}

	// --- Fetch updated proposals with preloaded relations ---
	var updatedProposals []models.Proposal
	if err := c.DB.Preload("SubmittedBy.Profile").
		Preload("ArchivedByUser.Profile").
		Preload("Team.Users.Profile").
		Preload("Cohort").
		Preload("Window").
		Where("proposal_id IN ?", body.ProposalIDs).
		Find(&updatedProposals).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch updated proposals", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Build response to include ArchivedBy full name ---
	resp := []map[string]interface{}{}
	for _, p := range updatedProposals {
		archivedByName := ""
		if p.ArchivedByUser != nil && p.ArchivedByUser.Profile.FirstName != "" {
			archivedByName = p.ArchivedByUser.Profile.FirstName + " " + p.ArchivedByUser.Profile.LastName
		}
		resp = append(resp, map[string]interface{}{
			"proposal_id":  p.ProposalID,
			"title":        p.Title,
			"abstract":     p.Abstract,
			"category":     p.Category,
			"subfield":     p.Subfield,
			"status":       p.Status,
			"archived":     p.Archived,
			"archived_at":  p.ArchivedAt,
			"archived_by":  archivedByName,
			"submitted_by": p.SubmittedBy.Profile.FirstName + " " + p.SubmittedBy.Profile.LastName,
			"cohort": func() string {
				if p.Cohort != nil {
					return p.Cohort.Name
				}
				return ""
			}(),
			"window_title": func() string {
				if p.Window != nil {
					return p.Window.Title
				}
				return ""
			}(),
			"document_url": p.DocumentURL,
			"created_at":   p.CreatedAt,
			"updated_at":   p.UpdatedAt,
		})
	}

	c.Json(w, http.StatusOK,
		fmt.Sprintf("%d proposal(s) %sd successfully", len(resp), body.Action),
		map[string]interface{}{"proposals": resp},
	)
}

// =============GET ARCHIVED PROPOSALS ===========================================

func (c *Construct) GetArchivedProposals(w http.ResponseWriter, r *http.Request) {
	userUUIDCtx := r.Context().Value("user_uuid")
	if userUUIDCtx == nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	userUUID := userUUIDCtx.(string)

	// Fetch the user
	var user models.User
	if err := c.DB.Preload("Role").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	// Build query - only SubmittedBy and ArchivedByUser
	query := c.DB.
		Preload("SubmittedBy.Profile").
		Preload("ArchivedByUser.Profile").
		Where("archived = ?", true)

	// Restrict if student
	if strings.ToLower(user.Role.Name) == "student" {
		query = query.Where("archived_by = ?", user.UserID)
	}

	var proposals []models.Proposal
	if err := query.Find(&proposals).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch archived proposals", map[string]interface{}{"error": err.Error()})
		return
	}

	resp := make([]map[string]interface{}, 0)
	for _, p := range proposals {
		log.Printf("DEBUG ProposalID=%d, ArchivedBy=%v, SubmittedByID=%d\n", p.ProposalID, p.ArchivedBy, p.SubmittedByID)

		archivedByName := ""
		if p.ArchivedByUser != nil {
			archivedByName = safeFullName(p.ArchivedByUser)
		} else {
			log.Printf("DEBUG ArchivedByUser is NULL for ProposalID=%d", p.ProposalID)
		}

		submittedByName := ""
		if p.SubmittedBy.UserID != 0 {
			submittedByName = safeFullName(p.SubmittedBy)
		} else {
			log.Printf("DEBUG SubmittedByUser is NULL for ProposalID=%d", p.ProposalID)
		}

		resp = append(resp, map[string]interface{}{
			"proposal_id":  p.ProposalID,
			"title":        p.Title,
			"abstract":     p.Abstract,
			"category":     p.Category,
			"subfield":     ifNotNil(p.Subfield),
			"status":       p.Status,
			"archived":     p.Archived,
			"archived_at":  p.ArchivedAt,
			"archived_by":  archivedByName,
			"submitted_by": submittedByName,
			"document_url": ifNotNil(p.DocumentURL),
			"created_at":   p.CreatedAt,
			"updated_at":   p.UpdatedAt,
		})
	}

	c.Json(w, http.StatusOK, fmt.Sprintf("%d archived proposal(s) fetched", len(resp)), map[string]interface{}{"proposals": resp})
}

// ----------------- Helper Functions -----------------
func safeFullName(u *models.User) string {
	if u.Profile.FirstName != "" {
		name := u.Profile.FirstName
		if u.Profile.LastName != "" {
			name += " " + u.Profile.LastName
		}
		return name
	}
	return u.Username
}

func ifNotNil(val *string) string {
	if val != nil {
		return *val
	}
	return ""
}

func ifNotNilStr[T any](val *T, f func(*T) string) string {
	if val != nil {
		return f(val)
	}
	return ""
}

// ─── GET PROPOSALS WITH PAGINATION ─────────────────────────────

func (c *Construct) GetProposals(w http.ResponseWriter, r *http.Request) {
	// --- Pagination ---
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

	// --- Get logged-in user ---
	userUUID, ok := r.Context().Value("user_uuid").(string)
	if !ok || userUUID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var user models.User
	if err := c.DB.Preload("Role").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}

	// --- Base query ---
	query := c.DB.Preload("SubmittedBy.Profile").
		Preload("Team.Users.Profile").
		Preload("Window").
		Preload("Cohort").
		Where("archived = ?", false).
		Order("created_at desc")

	// --- Role-based filtering ---
	switch user.Role.Name {
	case "Student":
		query = query.Where("submitted_by_id = ?", user.UserID)
	case "Supervisor":
		// Supervisors only see proposals in windows they created AND not draft
		query = query.Joins("JOIN proposal_submission_windows w ON proposals.window_id = w.window_id").
			Where("w.created_by_id = ? AND proposals.status != ?", user.UserID, "Draft")
	default: // Admin
		// Admin sees all non-draft proposals
		query = query.Where("status != ?", "Draft")
	}

	// --- Count total after filtering ---
	var total int64
	if err := query.Model(&models.Proposal{}).Count(&total).Error; err != nil {
		http.Error(w, "failed to count proposals", http.StatusInternalServerError)
		return
	}

	// --- Fetch proposals with pagination ---
	var proposals []models.Proposal
	if err := query.Limit(limit).Offset(offset).Find(&proposals).Error; err != nil {
		http.Error(w, "failed to fetch proposals", http.StatusInternalServerError)
		return
	}

	// --- Build response ---
	type ProposalSummary struct {
		ProposalID  uint64   `json:"proposal_id"`
		Title       string   `json:"title"`
		Abstract    string   `json:"abstract"`
		DocumentURL string   `json:"document_url,omitempty"`
		Category    string   `json:"category"`
		Subfield    string   `json:"subfield,omitempty"`
		Status      string   `json:"status"`
		SubmittedBy string   `json:"submitted_by"`
		WindowTitle string   `json:"window_title"`
		Cohort      string   `json:"cohort,omitempty"`
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

		cohortName := ""
		if p.Cohort != nil {
			cohortName = p.Cohort.Name
		}

		teamName := ""
		var teamMembers []string
		if p.Team != nil && p.Team.TeamID != 0 {
			teamName = p.Team.Name
			for _, member := range p.Team.Users {
				fullName := strings.TrimSpace(member.Profile.FirstName + " " + member.Profile.LastName)
				if fullName != "" {
					teamMembers = append(teamMembers, fullName)
				}
			}
		}

		documentURL := ""
		if p.DocumentURL != nil {
			documentURL = *p.DocumentURL
		}
		subfield := ""
		if p.Subfield != nil {
			subfield = *p.Subfield
		}

		resp = append(resp, ProposalSummary{
			ProposalID:  p.ProposalID,
			Title:       p.Title,
			Abstract:    p.Abstract,
			DocumentURL: documentURL,
			Subfield:    subfield,
			Category:    p.Category,
			Status:      p.Status,
			SubmittedBy: submittedBy,
			WindowTitle: windowTitle,
			Cohort:      cohortName,
			TeamName:    teamName,
			TeamMembers: teamMembers,
			CreatedAt:   p.CreatedAt.Format("2006-01-02 15:04"),
		})
	}

	// --- Send response ---
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"page":      page,
		"limit":     limit,
		"total":     total,
		"proposals": resp,
	})
}

//=======++=============++ DELETE API ++===============++===================++==================

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

// ======== GET ========== Approved and Rejected Proposals  ============== APIS ============

// ─── GET APPROVED PROPOSALS ─────────────────────────────────────────────
func (c *Construct) GetApprovedProposals(w http.ResponseWriter, r *http.Request) {
	c.getProposalsByStatus(w, r, models.ProposalStatusApproved)
}

// ─── GET REJECTED PROPOSALS ─────────────────────────────────────────────
func (c *Construct) GetRejectedProposals(w http.ResponseWriter, r *http.Request) {
	c.getProposalsByStatus(w, r, models.ProposalStatusRejected)
}
func (c *Construct) getProposalsByStatus(w http.ResponseWriter, r *http.Request, status string) {
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	log.Printf("[DEBUG] Role: %s | UserID: %d | Target Status: %s\n", user.Role.Name, user.UserID, status)

	var proposals []models.Proposal
	query := c.DB.Preload("SubmittedBy.Profile").Preload("Cohort").Preload("Team")

	switch user.Role.Name {
	case "Admin":
		log.Println("[DEBUG] Admin detected: fetching all proposals with status", status)
		query = query.Where("status = ?", status)

	case "Supervisor":
		log.Println("[DEBUG] Supervisor detected: fetching proposals reviewed by them")
		// Join proposals with reviews table to fetch proposals reviewed by this supervisor
		query = query.Joins("JOIN proposal_reviews pr ON pr.proposal_id = proposals.proposal_id").
			Where("pr.reviewed_by_id = ? AND pr.decision IN ?", user.UserID, []string{"Approved", "Rejected"}).
			Distinct("proposals.proposal_id") // Avoid duplicates

	default: // Student
		log.Printf("[DEBUG] Student detected: fetching proposals with status %s for user_id %d\n", status, user.UserID)
		query = query.Where("status = ? AND submitted_by_id = ?", status, user.UserID)
	}

	// Execute the query
	if err := query.Find(&proposals).Error; err != nil {
		log.Println("[ERROR] Failed to fetch proposals:", err)
		http.Error(w, "failed to fetch proposals", http.StatusInternalServerError)
		return
	}

	log.Printf("[DEBUG] Found %d proposals for role: %s\n", len(proposals), user.Role.Name)

	// Build the response
	response := []map[string]interface{}{}
	for _, p := range proposals {
		// Fetch reviews for this proposal
		var proposalReviews []models.ProposalReview
		if err := c.DB.Preload("ReviewedBy.Profile").
			Where("proposal_id = ? AND decision IN ?", p.ProposalID, []string{"Approved", "Rejected"}).
			Find(&proposalReviews).Error; err != nil {
			log.Println("[ERROR] Failed to fetch reviews for proposal", p.ProposalID, ":", err)
		}

		reviews := []map[string]interface{}{}
		for _, r := range proposalReviews {
			reviewer := map[string]interface{}{
				"first_name": "",
				"last_name":  "",
				"email":      "",
			}
			if r.ReviewedBy != nil {
				reviewer = map[string]interface{}{
					"first_name": r.ReviewedBy.Profile.FirstName,
					"last_name":  r.ReviewedBy.Profile.LastName,
					"email":      r.ReviewedBy.Email,
				}
			}
			reviews = append(reviews, map[string]interface{}{
				"reviewed_by_id": r.ReviewedByID,
				"reviewed_by":    reviewer,
				"decision":       r.Decision,
				"comments":       r.Comments,
				"review_date":    r.ReviewDate,
			})
		}

		submittedBy := map[string]interface{}{
			"first_name": "",
			"last_name":  "",
			"email":      "",
		}
		if p.SubmittedBy != nil {
			submittedBy = map[string]interface{}{
				"first_name": p.SubmittedBy.Profile.FirstName,
				"last_name":  p.SubmittedBy.Profile.LastName,
				"email":      p.SubmittedBy.Email,
			}
		}

		response = append(response, map[string]interface{}{
			"proposal_id":     p.ProposalID,
			"title":           p.Title,
			"abstract":        p.Abstract,
			"document_url":    p.DocumentURL,
			"category":        p.Category,
			"subfield":        p.Subfield,
			"status":          p.Status,
			"submission_date": p.SubmissionDate,
			"cohort": func() string {
				if p.Cohort != nil {
					return p.Cohort.Name
				}
				return ""
			}(),
			"team": func() string {
				if p.Team != nil {
					return p.Team.Name
				}
				return ""
			}(),
			"submitted_by": submittedBy,
			"reviews":      reviews,
		})
	}

	resp := map[string]interface{}{
		"status":    status,
		"total":     len(response),
		"proposals": response,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
