package controllers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"web/services/assets/models"

	"gorm.io/gorm"
)

// ─── ADD REVIEW ───────────────────────────────────────────────
func (c *Construct) AddReview(w http.ResponseWriter, r *http.Request) {
	// --- Parse proposal_id ---
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
		CohortName *string `json:"cohort_name,omitempty"` // used if approving without cohort
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// --- Get current user ---
	userUUID := r.Context().Value("user_uuid").(string)
	var user models.User
	if err := c.DB.Preload("Profile").Preload("SupervisorProfile").First(&user, "user_uuid = ?", userUUID).Error; err != nil {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	// --- Fetch proposal ---
	var proposal models.Proposal
	if err := c.DB.Preload("SubmittedBy.Profile").Preload("Cohort").First(&proposal, "proposal_id = ?", proposalID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "proposal not found", http.StatusNotFound)
			return
		}
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// Mentors/Admins cannot set decisions
	if payload.Decision != "" && user.SupervisorProfile == nil {
		payload.Decision = "" // ignore if not supervisor
	}

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

	// --- Supervisor-only approval logic ---
	if payload.Decision == models.DecisionApproved && user.SupervisorProfile != nil {
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
		if err := c.DB.Save(&proposal.SubmittedBy).Error; err != nil {
			http.Error(w, "failed to assign user to cohort", http.StatusInternalServerError)
			return
		}
		proposal.Status = models.ProposalStatusApproved
		now := time.Now()
		proposal.SubmissionDate = &now
		if err := c.DB.Save(&proposal).Error; err != nil {
			http.Error(w, "failed to update proposal", http.StatusInternalServerError)
			return
		}

		// --- Notifications for approval ---
		_ = c.CreateNotification(proposal.SubmittedByID, "Proposal Approved", "Your proposal has been approved. You have been added to the "+proposal.Cohort.Name+" cohort. You can now start working on your project.")
	} else if payload.Decision == models.DecisionRejected && user.SupervisorProfile != nil {
		proposal.Status = models.ProposalStatusRejected
		c.DB.Save(&proposal)
		_ = c.CreateNotification(proposal.SubmittedByID, "Proposal Rejected", "Your proposal has been reviewed and rejected.")
	} else if payload.Decision == models.DecisionNeedsRevision && user.SupervisorProfile != nil {
		proposal.Status = models.ProposalStatusNeedsRevision
		c.DB.Save(&proposal)
		_ = c.CreateNotification(proposal.SubmittedByID, "Proposal Needs Revision", "Your proposal needs revision. Please review the comments and resubmit.")
	}

	// --- Notifications for any review/comment ---
	if payload.Decision == models.DecisionPending || user.SupervisorProfile == nil {
		_ = c.CreateNotification(proposal.SubmittedByID, "Proposal Reviewed", "Your proposal has a new review/comment.")
	}

	// --- Response ---
	response := map[string]interface{}{
		"proposal": map[string]interface{}{
			"id":       proposal.ProposalID,
			"title":    proposal.Title,
			"subfield": proposal.Subfield,
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

// ─── GET REVIEWS ──────────────────────────────────────────────
func (c *Construct) GetReviews(w http.ResponseWriter, r *http.Request) {
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

	// --- Get current user ---
	userUUID := r.Context().Value("user_uuid").(string)
	var user models.User
	if err := c.DB.Preload("Profile").Preload("SupervisorProfile").First(&user, "user_uuid = ?", userUUID).Error; err != nil {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	// --- Fetch proposal ---
	var proposal models.Proposal
	if err := c.DB.Preload("SubmittedBy.Profile").Preload("Cohort").First(&proposal, "proposal_id = ?", proposalID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "proposal not found", http.StatusNotFound)
			return
		}
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// --- Access control ---
	if user.SupervisorProfile == nil && user.UserID != proposal.SubmittedByID {
		http.Error(w, "forbidden: you do not have access to these reviews", http.StatusForbidden)
		return
	}

	// --- Fetch reviews ---
	var reviews []models.ProposalReview
	if err := c.DB.Preload("ReviewedBy.Profile").Where("proposal_id = ?", proposal.ProposalID).Find(&reviews).Error; err != nil {
		http.Error(w, "failed to fetch reviews", http.StatusInternalServerError)
		return
	}

	// --- Prepare clean review list ---
	cleanReviews := []map[string]interface{}{}
	for _, rev := range reviews {
		reviewerName := ""
		if rev.ReviewedBy.Profile.FirstName != "" || rev.ReviewedBy.Profile.LastName != "" {
			reviewerName = rev.ReviewedBy.Profile.FirstName + " " + rev.ReviewedBy.Profile.LastName
		}
		cleanReviews = append(cleanReviews, map[string]interface{}{
			"decision":    rev.Decision,
			"comments":    rev.Comments,
			"reviewed_by": reviewerName,
			"review_date": rev.ReviewDate,
		})
	}

	// --- Prepare response ---
	response := map[string]interface{}{
		"proposal": map[string]interface{}{
			"id":       proposal.ProposalID,
			"title":    proposal.Title,
			"abstract": proposal.Abstract,
			"category": proposal.Category,
			"subfield": proposal.Subfield,
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
		"reviews": cleanReviews,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ─── GET SINGLE REVIEW ─────────────────────────────────────────

func (c *Construct) GetMyReviews(w http.ResponseWriter, r *http.Request) {
	// --- Get current user ---
	userUUID := r.Context().Value("user_uuid").(string)
	var user models.User
	if err := c.DB.Preload("Profile").First(&user, "user_uuid = ?", userUUID).Error; err != nil {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	// --- Fetch all reviews contributed by this user ---
	var reviews []models.ProposalReview
	if err := c.DB.Preload("ReviewedBy.Profile").
		Preload("Proposal.SubmittedBy.Profile").
		Preload("Proposal.Cohort").
		Where("reviewed_by_id = ?", user.UserID).
		Find(&reviews).Error; err != nil {
		http.Error(w, "failed to fetch reviews", http.StatusInternalServerError)
		return
	}

	// --- Prepare response ---
	cleanReviews := []map[string]interface{}{}
	for _, rev := range reviews {
		reviewerName := rev.ReviewedBy.Profile.FirstName + " " + rev.ReviewedBy.Profile.LastName
		cleanReviews = append(cleanReviews, map[string]interface{}{
			"review": map[string]interface{}{
				"comments":    rev.Comments,
				"decision":    rev.Decision,
				"review_date": rev.ReviewDate,
				"reviewed_by": reviewerName,
			},
			"proposal": map[string]interface{}{
				"id":       rev.Proposal.ProposalID,
				"title":    rev.Proposal.Title,
				"abstract": rev.Proposal.Abstract,
				"category": rev.Proposal.Category,
				"subfield": rev.Proposal.Subfield,
				"status":   rev.Proposal.Status,
				"cohort": func() string {
					if rev.Proposal.Cohort != nil {
						return rev.Proposal.Cohort.Name
					}
					return ""
				}(),
				"submitted_by": map[string]interface{}{
					"first_name": rev.Proposal.SubmittedBy.Profile.FirstName,
					"last_name":  rev.Proposal.SubmittedBy.Profile.LastName,
					"email":      rev.Proposal.SubmittedBy.Email,
				},
			},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cleanReviews)
}

// ================== UPDATE REVIEW ==========================================
func (c *Construct) UpdateReview(w http.ResponseWriter, r *http.Request) {
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

	var payload struct {
		Comments   *string `json:"comments,omitempty"`
		Decision   *string `json:"decision,omitempty"`    // supervisors only
		CohortName *string `json:"cohort_name,omitempty"` // used if approving without cohort
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	userUUID := r.Context().Value("user_uuid").(string)
	var user models.User
	if err := c.DB.Preload("Profile").Preload("SupervisorProfile").First(&user, "user_uuid = ?", userUUID).Error; err != nil {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	var review models.ProposalReview
	if err := c.DB.Preload("Proposal.SubmittedBy.Profile").Preload("Proposal.Cohort").First(&review, "review_id = ?", reviewID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "review not found", http.StatusNotFound)
			return
		}
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// Only reviewer can update their own review
	if review.ReviewedByID != user.UserID {
		http.Error(w, "not allowed to update this review", http.StatusForbidden)
		return
	}

	// Mentors/Admins cannot set decisions
	if payload.Decision != nil && user.SupervisorProfile == nil {
		payload.Decision = nil // ignore
	}

	// Update comments if provided
	if payload.Comments != nil {
		review.Comments = payload.Comments
	}

	// Handle decision update (supervisors only)
	if payload.Decision != nil {
		review.Decision = *payload.Decision

		proposal := &review.Proposal
		if *payload.Decision == models.DecisionApproved {
			// Assign cohort if not already assigned
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

			// --- Notification for approval and cohort assignment ---
			_ = c.CreateNotification(proposal.SubmittedByID, "Proposal Approved", "Your proposal has been approved. You have been added to the "+proposal.Cohort.Name+" cohort. You can now start working on your project.")
		} else if *payload.Decision == models.DecisionRejected {
			review.Proposal.Status = models.ProposalStatusRejected
			c.DB.Save(&review.Proposal)
			_ = c.CreateNotification(review.Proposal.SubmittedByID, "Proposal Rejected", "Your proposal has been reviewed and rejected.")
		} else if *payload.Decision == models.DecisionNeedsRevision {
			review.Proposal.Status = models.ProposalStatusNeedsRevision
			c.DB.Save(&review.Proposal)
			_ = c.CreateNotification(review.Proposal.SubmittedByID, "Proposal Needs Revision", "Your proposal needs revision. Please review the comments and resubmit.")
		}
	}

	if err := c.DB.Save(&review).Error; err != nil {
		http.Error(w, "failed to update review", http.StatusInternalServerError)
		return
	}

	// Response: minimal clean JSON
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
