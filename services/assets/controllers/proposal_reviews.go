package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"web/services/assets/models"

	"gorm.io/gorm"
)

// ─── ADD REVIEW ───────────────────────────────────────────────
func (c *Construct) AddReview(w http.ResponseWriter, r *http.Request) {
	// --- Parse proposal_id from query ---
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

	// --- Decode payload ---
	var payload struct {
		Comments   *string `json:"comments"`
		Decision   string  `json:"decision,omitempty"`    // supervisors only
		CohortName *string `json:"cohort_name,omitempty"` // optional, for assigning
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// --- Get current user ---
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// --- Fetch proposal ---
	var proposal models.Proposal
	if err := c.DB.Preload("SubmittedBy.Profile").
		Preload("Cohort").
		First(&proposal, "proposal_id = ?", proposalID).Error; err != nil {

		if err == gorm.ErrRecordNotFound {
			http.Error(w, "proposal not found", http.StatusNotFound)
			return
		}
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// --- Default decision ---
	if payload.Decision == "" {
		payload.Decision = models.DecisionPending
	}

	// --- Create review record ---
	review := models.ProposalReview{
		ProposalID:   proposal.ProposalID,
		ReviewedByID: user.UserID,
		Comments:     payload.Comments,
		Decision:     payload.Decision,
		ReviewDate:   time.Now(),
	}
	if err := c.DB.Create(&review).Error; err != nil {
		http.Error(w, "failed to add review", http.StatusInternalServerError)
		return
	}

	// --- Supervisor actions ---
	if user.SupervisorProfile != nil {
		// --- Move Submitted → Under Review if needed ---
		if proposal.Status == models.ProposalStatusSubmitted {
			proposal.Status = models.ProposalStatusUnderReview
			c.DB.Save(&proposal)
			c.NotifyAndTrack(
				proposal.SubmittedByID,
				"Proposal Under Review",
				fmt.Sprintf("Proposal '%s' is now under review.", proposal.Title),
				"Status Change",
				"Proposal",
				&proposal.ProposalID,
				proposal.Status,
			)
		}

		// --- Handle decision ---
		switch payload.Decision {
		case models.DecisionApproved:
			// Ensure cohort assignment
			if proposal.CohortID == nil {
				if payload.CohortName == nil || *payload.CohortName == "" {
					http.Error(w, "cohort_name is required when approving without cohort", http.StatusBadRequest)
					return
				}
				var cohort models.Cohort
				if err := c.DB.Where("name = ?", *payload.CohortName).First(&cohort).Error; err != nil {
					http.Error(w, "cohort not found", http.StatusBadRequest)
					return
				}
				proposal.CohortID = &cohort.CohortID
			}

			proposal.Status = models.ProposalStatusApproved
			proposal.SubmissionDate = ptrTime(time.Now())
			c.DB.Save(&proposal)

			// Assign student to cohort
			proposal.SubmittedBy.CohortID = proposal.CohortID
			c.DB.Save(&proposal.SubmittedBy)

			c.NotifyAndTrack(
				proposal.SubmittedByID,
				"Proposal Approved",
				"Your proposal has been approved and assigned to cohort "+proposal.Cohort.Name,
				"Proposal Decision",
				"Proposal",
				&proposal.ProposalID,
				proposal.Status,
			)

		case models.DecisionRejected:
			proposal.Status = models.ProposalStatusRejected
			c.DB.Save(&proposal)
			c.NotifyAndTrack(
				proposal.SubmittedByID,
				"Proposal Rejected",
				"Your proposal has been reviewed and rejected.",
				"Proposal Decision",
				"Proposal",
				&proposal.ProposalID,
				proposal.Status,
			)

		case models.DecisionNeedsRevision:
			proposal.Status = models.ProposalStatusNeedsRevision
			c.DB.Save(&proposal)
			c.NotifyAndTrack(
				proposal.SubmittedByID,
				"Proposal Needs Revision",
				"Your proposal requires revision. Review the feedback and resubmit.",
				"Proposal Decision",
				"Proposal",
				&proposal.ProposalID,
				proposal.Status,
			)
		}
	} else {
		// --- Mentors / other roles just comment ---
		c.NotifyAndTrack(
			proposal.SubmittedByID,
			"New Review Comment",
			"Your proposal has received a new comment or feedback.",
			"Proposal Review",
			"Proposal",
			&proposal.ProposalID,
			proposal.Status,
		)
	}

	// --- Response ---
	response := map[string]interface{}{
		"proposal": map[string]interface{}{
			"id":       proposal.ProposalID,
			"title":    proposal.Title,
			"abstract": proposal.Abstract,
			"category": proposal.Category,
			"status":   proposal.Status,
			"cohort": func() string {
				if proposal.Cohort != nil {
					return proposal.Cohort.Name
				}
				return ""
			}(),
			"submitted_by": map[string]interface{}{
				"first_name": proposal.SubmittedBy.Profile.FirstName,
				"last_name":  proposal.SubmittedBy.Profile.LastName,
				"email":      proposal.SubmittedBy.Email,
			},
		},
		"review": map[string]interface{}{
			"decision":    review.Decision,
			"comments":    review.Comments,
			"reviewed_by": user.Profile.FirstName + " " + user.Profile.LastName,
			"review_date": review.ReviewDate,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// helper
func ptrTime(t time.Time) *time.Time {
	return &t
}

// ─── GET REVIEWS ──────────────────────────────────────────────
// GetReviews returns reviews for multiple proposals (or a single proposal)
func (c *Construct) GetReviews(w http.ResponseWriter, r *http.Request) {
	// --- Parse proposal_ids from query ---
	proposalIDsStr := r.URL.Query()["proposal_id"] // can be ?proposal_id=1&proposal_id=2
	if len(proposalIDsStr) == 0 {
		http.Error(w, "at least one proposal_id is required", http.StatusBadRequest)
		return
	}

	var proposalIDs []uint64
	for _, idStr := range proposalIDsStr {
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid proposal_id: "+idStr, http.StatusBadRequest)
			return
		}
		proposalIDs = append(proposalIDs, id)
	}

	// --- Get current user ---
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		http.Error(w, "user not found or unauthorized", http.StatusUnauthorized)
		return
	}

	// --- Fetch proposals ---
	var proposals []models.Proposal
	if err := c.DB.Preload("SubmittedBy.Profile").
		Preload("Cohort").
		Where("proposal_id IN ?", proposalIDs).
		Find(&proposals).Error; err != nil {
		http.Error(w, "failed to fetch proposals", http.StatusInternalServerError)
		return
	}

	if len(proposals) == 0 {
		http.Error(w, "no proposals found", http.StatusNotFound)
		return
	}

	// --- Access control: students can only see their own proposals ---
	filteredProposals := []models.Proposal{}
	for _, p := range proposals {
		if user.SupervisorProfile != nil || user.UserID == p.SubmittedByID {
			filteredProposals = append(filteredProposals, p)
		}
	}
	if len(filteredProposals) == 0 {
		http.Error(w, "forbidden: no access to these proposals", http.StatusForbidden)
		return
	}

	// --- Fetch all reviews for these proposals ---
	var reviews []models.ProposalReview
	if err := c.DB.Preload("ReviewedBy.Profile").
		Where("proposal_id IN ?", proposalIDs).
		Find(&reviews).Error; err != nil {
		http.Error(w, "failed to fetch reviews", http.StatusInternalServerError)
		return
	}

	// --- Group reviews by proposal_id ---
	reviewsMap := make(map[uint64][]map[string]interface{})
	for _, rev := range reviews {
		reviewerName := ""
		if rev.ReviewedBy != nil {
			reviewerName = rev.ReviewedBy.Profile.FirstName + " " + rev.ReviewedBy.Profile.LastName
		}
		reviewEntry := map[string]interface{}{
			"decision":    rev.Decision,
			"comments":    rev.Comments,
			"reviewed_by": reviewerName,
			"review_date": rev.ReviewDate,
		}
		reviewsMap[rev.ProposalID] = append(reviewsMap[rev.ProposalID], reviewEntry)
	}

	// --- Build response ---
	response := []map[string]interface{}{}
	for _, p := range filteredProposals {
		submittedBy := map[string]interface{}{
			"first_name": "",
			"last_name":  "",
			"email":      "",
		}
		if p.SubmittedBy.Profile.FirstName != "" || p.SubmittedBy.Profile.LastName != "" {
			submittedBy = map[string]interface{}{
				"first_name": p.SubmittedBy.Profile.FirstName,
				"last_name":  p.SubmittedBy.Profile.LastName,
				"email":      p.SubmittedBy.Email,
			}
		}

		cohortName := ""
		if p.Cohort != nil {
			cohortName = p.Cohort.Name
		}

		response = append(response, map[string]interface{}{
			"proposal": map[string]interface{}{
				"id":           p.ProposalID,
				"title":        p.Title,
				"abstract":     p.Abstract,
				"category":     p.Category,
				"subfield":     p.Subfield,
				"status":       p.Status,
				"cohort":       cohortName,
				"submitted_by": submittedBy,
			},
			"reviews": reviewsMap[p.ProposalID],
		})
	}

	// --- Track the event ---
	for _, p := range filteredProposals {
		c.NotifyAndTrack(
			user.UserID,
			"Viewed Proposal Reviews",
			fmt.Sprintf("User viewed reviews for proposal %s", p.Title),
			"Review Access",
			"Proposal",
			&p.ProposalID,
			p.Status,
		)
	}

	// --- Send JSON response ---
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ─── GET SINGLE REVIEW ─────────────────────────────────────────

// GetMyReviews returns all reviews authored by the logged-in user
func (c *Construct) GetMyReviews(w http.ResponseWriter, r *http.Request) {
	// --- Get current user from context ---
	userUUID, ok := r.Context().Value("user_uuid").(string)
	if !ok || userUUID == "" {
		http.Error(w, "unauthorized: no user in context", http.StatusUnauthorized)
		return
	}

	var user models.User
	if err := c.DB.Preload("Profile").First(&user, "user_uuid = ?", userUUID).Error; err != nil {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	// --- Fetch all reviews authored by this user ---
	var reviews []models.ProposalReview
	err := c.DB.Preload("ReviewedBy.Profile").
		Preload("Proposal.SubmittedBy.Profile").
		Preload("Proposal.Cohort").
		Where("reviewed_by_id = ?", user.UserID).
		Find(&reviews).Error
	if err != nil {
		http.Error(w, "failed to fetch reviews", http.StatusInternalServerError)
		return
	}

	// --- Track the event ---
	c.NotifyAndTrack(
		user.UserID,
		"Viewed Own Reviews",
		"User viewed their reviews for proposals.",
		"Review Access",
		"ProposalReview",
		nil,
		"",
	)

	// --- Build response ---
	cleanReviews := make([]map[string]interface{}, 0, len(reviews))
	for _, rev := range reviews {
		// Reviewer name
		reviewerName := ""
		if rev.ReviewedBy != nil {
			reviewerName = rev.ReviewedBy.Profile.FirstName + " " + rev.ReviewedBy.Profile.LastName
		}

		proposal := rev.Proposal
		submittedBy := map[string]interface{}{
			"first_name": "",
			"last_name":  "",
			"email":      "",
		}

		if proposal.SubmittedBy.Profile.FirstName != "" || proposal.SubmittedBy.Profile.LastName != "" {
			submittedBy = map[string]interface{}{
				"first_name": proposal.SubmittedBy.Profile.FirstName,
				"last_name":  proposal.SubmittedBy.Profile.LastName,
				"email":      proposal.SubmittedBy.Email,
			}
		}
		cohortName := ""
		if proposal.Cohort != nil {
			cohortName = proposal.Cohort.Name
		}

		cleanReviews = append(cleanReviews, map[string]interface{}{
			"review": map[string]interface{}{
				"comments":    rev.Comments,
				"decision":    rev.Decision,
				"review_date": rev.ReviewDate,
				"reviewed_by": reviewerName,
			},
			"proposal": map[string]interface{}{
				"id":           proposal.ProposalID,
				"title":        proposal.Title,
				"abstract":     proposal.Abstract,
				"category":     proposal.Category,
				"subfield":     proposal.Subfield,
				"status":       proposal.Status,
				"cohort":       cohortName,
				"submitted_by": submittedBy,
			},
		})
	}

	// --- Send response ---
	w.Header().Set("Content-Type", "application/json")
	if len(cleanReviews) == 0 {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "No reviews found for this user",
			"reviews": []map[string]interface{}{},
		})
		return
	}

	json.NewEncoder(w).Encode(cleanReviews)
}

// ================== UPDATE REVIEW ==========================================

func (c *Construct) UpdateReview(w http.ResponseWriter, r *http.Request) {
	// --- Parse review_id ---
	reviewIDStr := r.URL.Query().Get("review_id")
	if reviewIDStr == "" {
		http.Error(w, "review_id is required", http.StatusBadRequest)
		return
	}
	reviewID, err := strconv.ParseUint(reviewIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid review_id", http.StatusBadRequest)
		return
	}

	// --- Decode payload ---
	var payload struct {
		Comments   *string `json:"comments,omitempty"`
		Decision   *string `json:"decision,omitempty"`    // supervisors only
		CohortName *string `json:"cohort_name,omitempty"` // used if approving without cohort
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// --- Get current user ---
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// --- Fetch review with proposal details ---
	var review models.ProposalReview
	if err := c.DB.Preload("Proposal.SubmittedBy.Profile").
		Preload("Proposal.Cohort").
		First(&review, "review_id = ?", reviewID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "review not found", http.StatusNotFound)
			return
		}
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// --- Only reviewer can update their own review ---
	if review.ReviewedByID != user.UserID {
		http.Error(w, "not allowed to update this review", http.StatusForbidden)
		return
	}

	// --- Ignore decisions if not a supervisor ---
	if payload.Decision != nil && user.SupervisorProfile == nil {
		payload.Decision = nil
	}

	// --- Update comments if provided ---
	if payload.Comments != nil {
		review.Comments = payload.Comments
	}

	// --- Handle decision update (supervisors only) ---
	if payload.Decision != nil {
		review.Decision = *payload.Decision

		proposal := &review.Proposal
		if *payload.Decision == models.DecisionApproved {
			// Assign cohort if not assigned
			if proposal.CohortID == nil {
				if payload.CohortName == nil || *payload.CohortName == "" {
					http.Error(w, "cohort_name is required when approving a proposal without a cohort", http.StatusBadRequest)
					return
				}
				var cohort models.Cohort
				if err := c.DB.Where("name = ?", *payload.CohortName).First(&cohort).Error; err != nil {
					http.Error(w, "cohort not found", http.StatusBadRequest)
					return
				}
				proposal.CohortID = &cohort.CohortID
			}

			proposal.SubmittedBy.CohortID = proposal.CohortID
			c.DB.Save(&proposal.SubmittedBy)

			proposal.Status = models.ProposalStatusApproved
			now := time.Now()
			proposal.SubmissionDate = &now
			c.DB.Save(proposal)

			// --- Notify and track ---
			c.NotifyAndTrack(
				proposal.SubmittedByID,
				"Proposal Approved",
				"Your proposal has been approved. You have been assigned to cohort "+proposal.Cohort.Name,
				"Proposal Decision",
				"Proposal",
				&proposal.ProposalID,
				proposal.Status,
			)

		} else if *payload.Decision == models.DecisionRejected {
			proposal.Status = models.ProposalStatusRejected
			c.DB.Save(proposal)

			c.NotifyAndTrack(
				proposal.SubmittedByID,
				"Proposal Rejected",
				"Your proposal has been reviewed and rejected.",
				"Proposal Decision",
				"Proposal",
				&proposal.ProposalID,
				proposal.Status,
			)
		} else if *payload.Decision == models.DecisionNeedsRevision {
			proposal.Status = models.ProposalStatusNeedsRevision
			c.DB.Save(proposal)

			c.NotifyAndTrack(
				proposal.SubmittedByID,
				"Proposal Needs Revision",
				"Your proposal needs revision. Please review the comments and resubmit.",
				"Proposal Decision",
				"Proposal",
				&proposal.ProposalID,
				proposal.Status,
			)
		}
	} else {
		// --- If only comments updated, ensure proposal is at least Under Review ---
		if review.Proposal.Status == models.ProposalStatusSubmitted || review.Proposal.Status == models.ProposalStatusDraft {
			review.Proposal.Status = models.ProposalStatusUnderReview
			c.DB.Save(&review.Proposal)

			c.NotifyAndTrack(
				review.Proposal.SubmittedByID,
				"Proposal Under Review",
				"Your proposal is now under review.",
				"Proposal Status",
				"Proposal",
				&review.Proposal.ProposalID,
				review.Proposal.Status,
			)
		} else {
			// Track comment-only update
			c.NotifyAndTrack(
				review.Proposal.SubmittedByID,
				"Proposal Reviewed",
				"A new comment/review has been added to your proposal.",
				"Proposal Comment",
				"Proposal",
				&review.Proposal.ProposalID,
				review.Proposal.Status,
			)
		}
	}

	// --- Save review ---
	if err := c.DB.Save(&review).Error; err != nil {
		http.Error(w, "failed to update review", http.StatusInternalServerError)
		return
	}

	// --- Response ---
	response := map[string]interface{}{
		"proposal": map[string]interface{}{
			"id":       review.Proposal.ProposalID,
			"title":    review.Proposal.Title,
			"abstract": review.Proposal.Abstract,
			"category": review.Proposal.Category,
			"subfield": review.Proposal.Subfield,
			"status":   review.Proposal.Status,
			"cohort": func() string {
				if review.Proposal.Cohort != nil {
					return review.Proposal.Cohort.Name
				}
				return ""
			}(),
			"submitted_by": map[string]interface{}{
				"first_name": review.Proposal.SubmittedBy.Profile.FirstName,
				"last_name":  review.Proposal.SubmittedBy.Profile.LastName,
				"email":      review.Proposal.SubmittedBy.Email,
			},
		},
		"review": map[string]interface{}{
			"decision":    review.Decision,
			"comments":    review.Comments,
			"reviewed_by": user.Profile.FirstName + " " + user.Profile.LastName,
			"review_date": review.ReviewDate,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// ─── DELETE REVIEW ─────────────────────────────────────────────
func (c *Construct) DeleteReview(w http.ResponseWriter, r *http.Request) {
	// --- Parse review_id ---
	reviewIDStr := r.URL.Query().Get("review_id")
	if reviewIDStr == "" {
		http.Error(w, "review_id is required", http.StatusBadRequest)
		return
	}
	reviewID, err := strconv.ParseUint(reviewIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid review_id", http.StatusBadRequest)
		return
	}

	// --- Get logged-in user ---
	userUUID := r.Context().Value("user_uuid").(string)
	var user models.User
	if err := c.DB.Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	// --- Fetch review ---
	var review models.ProposalReview
	if err := c.DB.First(&review, "review_id = ?", reviewID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "review not found", http.StatusNotFound)
			return
		}
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// --- Access control: only the reviewer can delete ---
	if review.ReviewedByID != user.UserID {
		http.Error(w, "not allowed to delete this review", http.StatusForbidden)
		return
	}

	// --- Delete (soft delete if gorm.Model is used) ---
	if err := c.DB.Delete(&review).Error; err != nil {
		http.Error(w, "failed to delete review", http.StatusInternalServerError)
		return
	}

	// --- Audit log ---
	_ = c.LogAudit(user.UserID, "delete_review", nil, &review.ReviewID, nil, nil)

	// --- Response ---
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": "review deleted successfully",
		"data": map[string]interface{}{
			"review_id":   review.ReviewID,
			"proposal_id": review.ProposalID,
		},
	})
}
