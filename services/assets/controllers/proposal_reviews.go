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

	var payload struct {
		Comments *string `json:"comments"`
		Decision string  `json:"decision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Current user from middleware
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

	// Enforce rules:
	// - Only supervisors can decide Approved/Rejected/NeedsRevision
	// - Mentors/Admins can only comment (decision = pending)
	// (No explicit role field → handled in middleware if needed)
	if payload.Decision == "" {
		payload.Decision = models.DecisionPending
	}

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

	// Side effects
	_ = c.CreateNotification(proposal.SubmittedByID, "New Review", "Your proposal has a new review/comment.")
	_ = c.LogAudit(user.UserID, "add_review", nil, &proposal.ProposalID, nil, nil)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(review)
}

// ─── GET REVIEWS ──────────────────────────────────────────────
func (c *Construct) GetReviews(w http.ResponseWriter, r *http.Request) {
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

	var proposal models.Proposal
	if err := c.DB.First(&proposal, "proposal_id = ?", proposalID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "proposal not found", http.StatusNotFound)
			return
		}
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	var reviews []models.ProposalReview
	if err := c.DB.Preload("ReviewedBy").Where("proposal_id = ?", proposal.ProposalID).Find(&reviews).Error; err != nil {
		http.Error(w, "failed to fetch reviews", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reviews)
}

// ─── GET SINGLE REVIEW ─────────────────────────────────────────
func (c *Construct) GetReview(w http.ResponseWriter, r *http.Request) {
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

	var review models.ProposalReview
	if err := c.DB.Preload("ReviewedBy").First(&review, "review_id = ?", reviewID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "review not found", http.StatusNotFound)
			return
		}
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(review)
}

// ─── UPDATE REVIEW ─────────────────────────────────────────────
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
		Comments *string `json:"comments,omitempty"`
		Decision *string `json:"decision,omitempty"` // supervisor only
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

	var review models.ProposalReview
	if err := c.DB.First(&review, "review_id = ?", reviewID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "review not found", http.StatusNotFound)
			return
		}
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// Only owner can update
	if review.ReviewedByID != user.UserID {
		http.Error(w, "not allowed to update this review", http.StatusForbidden)
		return
	}

	if payload.Comments != nil {
		review.Comments = payload.Comments
	}
	if payload.Decision != nil {
		review.Decision = *payload.Decision
	}

	if err := c.DB.Save(&review).Error; err != nil {
		http.Error(w, "failed to update review", http.StatusInternalServerError)
		return
	}

	_ = c.LogAudit(user.UserID, "update_review", nil, &review.ReviewID, nil, nil)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(review)
}

// ─── DELETE REVIEW ─────────────────────────────────────────────
func (c *Construct) DeleteReview(w http.ResponseWriter, r *http.Request) {
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

	userUUID := r.Context().Value("user_uuid").(string)
	var user models.User
	if err := c.DB.Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	var review models.ProposalReview
	if err := c.DB.First(&review, "review_id = ?", reviewID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "review not found", http.StatusNotFound)
			return
		}
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// Only owner can delete
	if review.ReviewedByID != user.UserID {
		http.Error(w, "not allowed to delete this review", http.StatusForbidden)
		return
	}

	if err := c.DB.Delete(&review).Error; err != nil {
		http.Error(w, "failed to delete review", http.StatusInternalServerError)
		return
	}

	_ = c.LogAudit(user.UserID, "delete_review", nil, &review.ReviewID, nil, nil)

	w.WriteHeader(http.StatusNoContent)
}
