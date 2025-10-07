package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"web/services/assets/models"

	"gorm.io/gorm"
)

type CohortUser struct {
	CohortID uint64 `gorm:"primaryKey"`
	UserID   uint64 `gorm:"primaryKey"`
	Role     string `gorm:"size:50"` // optional, if you want to track role in cohort
}

// Assign student to cohort after proposal is approved
func AssignStudentToCohort(db *gorm.DB, studentID, cohortID uint64) error {
	cu := models.CohortUser{
		CohortID: cohortID,
		UserID:   studentID,
	}
	return db.Create(&cu).Error
}
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
		Decision   string  `json:"decision,omitempty"`    // admins only
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

	// --- Only admin can set default decision ---
	if strings.ToLower(user.Role.Name) == "admin" && payload.Decision == "" {
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

	// --- Admin handles decision and cohort assignment ---
	if strings.ToLower(user.Role.Name) == "admin" {
		switch payload.Decision {
		case models.DecisionApproved:
			proposal.Status = models.ProposalStatusApproved
			proposal.SubmissionDate = ptrTime(time.Now())

			// Assign cohort if not already assigned
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

			if err := c.DB.Save(&proposal).Error; err != nil {
				http.Error(w, "failed to update proposal", http.StatusInternalServerError)
				return
			}

			// Assign student to cohort
			cohortUser := models.CohortUser{
				CohortID: *proposal.CohortID,
				UserID:   proposal.SubmittedByID,
				Role:     "Student",
			}
			if err := c.DB.Create(&cohortUser).Error; err != nil {
				http.Error(w, "failed to assign student to cohort", http.StatusInternalServerError)
				return
			}

			// Notify student
			c.NotifyAndTrack(
				proposal.SubmittedByID,
				"Cohort Assignment",
				fmt.Sprintf("You have been assigned to cohort '%s'. You can now explore it and begin working on your project.", proposal.Cohort.Name),
				"Cohort Assignment",
				"Cohort",
				proposal.CohortID,
				"Assigned",
			)

		case models.DecisionRejected:
			proposal.Status = models.ProposalStatusRejected
			_ = c.DB.Save(&proposal)

		case models.DecisionNeedsRevision:
			proposal.Status = models.ProposalStatusNeedsRevision
			_ = c.DB.Save(&proposal)
		}

		// Notify student about decision
		c.NotifyAndTrack(
			proposal.SubmittedByID,
			fmt.Sprintf("Proposal %s", payload.Decision),
			fmt.Sprintf("Your proposal '%s' has been %s.", proposal.Title, payload.Decision),
			"Proposal Decision",
			"Proposal",
			&proposal.ProposalID,
			proposal.Status,
		)
	} else {
		// --- Supervisors / Mentors: only comment ---
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

	// --- Reload proposal for response ---
	if err := c.DB.Preload("Cohort").Preload("SubmittedBy.Profile").First(&proposal, "proposal_id = ?", proposal.ProposalID).Error; err != nil {
		http.Error(w, "failed to reload proposal", http.StatusInternalServerError)
		return
	}

	// --- Build response ---
	var reviewResponse map[string]interface{}
	if strings.ToLower(user.Role.Name) == "admin" {
		// Admin sees decision
		reviewResponse = map[string]interface{}{
			"review_id":   review.ReviewID,
			"comments":    review.Comments,
			"reviewed_by": user.Profile.FirstName + " " + user.Profile.LastName,
			"review_date": review.ReviewDate,
			"decision":    review.Decision,
		}
	} else {
		// Supervisors / Mentors: exclude decision
		reviewResponse = map[string]interface{}{
			"review_id":   review.ReviewID,
			"comments":    review.Comments,
			"reviewed_by": user.Profile.FirstName + " " + user.Profile.LastName,
			"review_date": review.ReviewDate,
		}
	}

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
		"review": reviewResponse,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

func ptrTime(time time.Time) *time.Time {
	panic("unimplemented")
}

// ─── GET REVIEWS-----------------------------------------------

func (c *Construct) GetReviews(w http.ResponseWriter, r *http.Request) {
	// --- Parse proposal_ids from query ---
	proposalIDsStr := r.URL.Query()["proposal_id"]
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

	// --- Access control ---
	filteredProposals := []models.Proposal{}
	for _, p := range proposals {
		switch user.Role.Name {
		case "Admin":
			filteredProposals = append(filteredProposals, p) // admin sees all
		default:
			// student: only their own proposals
			if user.UserID == p.SubmittedByID {
				filteredProposals = append(filteredProposals, p)
			}
		}
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
			"review_id":   rev.ReviewID,
			"comments":    rev.Comments,
			"reviewed_by": reviewerName,
			"review_date": rev.ReviewDate,
		}

		// Admins see decision
		if user.Role.Name == "Admin" {
			reviewEntry["decision"] = rev.Decision
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
	if err := c.DB.Preload("Profile").Preload("Role").First(&user, "user_uuid = ?", userUUID).Error; err != nil {
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

		// --- Review object changes based on role ---
		reviewData := map[string]interface{}{
			"review_id":   rev.ReviewID,
			"comments":    rev.Comments,
			"review_date": rev.ReviewDate,
			"reviewed_by": reviewerName,
		}

		// Supervisors only: include decision
		role := strings.ToLower(user.Role.Name)
		if role == "supervisor" {
			reviewData["decision"] = rev.Decision
		}

		cleanReviews = append(cleanReviews, map[string]interface{}{
			"review": reviewData,
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

func (c *Construct) UpdateReviewByProposal(w http.ResponseWriter, r *http.Request) {
	// --- Step 1: Get proposal ID ---
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

	// --- Step 2: Decode payload ---
	var payload struct {
		Comments   *string `json:"comments,omitempty"`
		Decision   *string `json:"decision,omitempty"`    // Admin only
		CohortName *string `json:"cohort_name,omitempty"` // Admin only
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// --- Step 3: Get authenticated user ---
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// --- Step 4: Load proposal ---
	var proposal models.Proposal
	if err := c.DB.Preload("Cohort").Preload("SubmittedBy.Profile").
		First(&proposal, "proposal_id = ?", proposalID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "proposal not found", http.StatusNotFound)
			return
		}
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// --- Step 5: Load existing review by this user or create one ---
	var review models.ProposalReview
	reviewFound := true
	if err := c.DB.Where("proposal_id = ? AND reviewed_by_id = ?", proposalID, user.UserID).First(&review).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			review = models.ProposalReview{
				ProposalID:   proposalID,
				ReviewedByID: user.UserID,
			}
			reviewFound = false
		} else {
			http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	now := time.Now()

	// --- Step 6: Role-based updates ---
	switch strings.ToLower(user.Role.Name) {
	case "admin":
		// Admin can update decision and assign cohort
		if payload.Decision != nil {
			review.Decision = *payload.Decision

			if *payload.Decision == models.DecisionApproved && proposal.CohortID == nil {
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

				// Assign student to cohort
				c.DB.Create(&models.CohortUser{
					CohortID: cohort.CohortID,
					UserID:   proposal.SubmittedByID,
					Role:     "Student",
				})
			}

			// Update proposal status
			proposal.Status = *payload.Decision
			proposal.SubmissionDate = &now
			c.DB.Save(&proposal)
		}

	default:
		// Students can only add comments
		payload.Decision = nil
		payload.CohortName = nil
	}

	// --- Step 7: Update comments if provided ---
	if payload.Comments != nil {
		review.Comments = payload.Comments
	}
	review.ReviewDate = now

	if reviewFound {
		if err := c.DB.Save(&review).Error; err != nil {
			http.Error(w, "failed to update review: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if err := c.DB.Create(&review).Error; err != nil {
			http.Error(w, "failed to create review: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// --- Step 8: Notifications ---
	c.NotifyAndTrack(
		proposal.SubmittedByID,
		"Proposal Reviewed",
		fmt.Sprintf("Your proposal '%s' has new feedback/comments.", proposal.Title),
		"Proposal Review",
		"Proposal",
		&proposal.ProposalID,
		proposal.Status,
	)

	// --- Step 9: Audit log ---
	c.DB.Create(&models.SystemHistory{
		EntityType:  "ProposalReview",
		EntityID:    &review.ReviewID,
		Action:      "Updated Review",
		Comment:     review.Comments,
		ChangedByID: user.UserID,
		CreatedAt:   now,
	})

	// --- Step 10: Build response ---
	resp := map[string]interface{}{
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
		},
		"review": map[string]interface{}{
			"review_id":   review.ReviewID,
			"comments":    review.Comments,
			"reviewed_by": user.Profile.FirstName + " " + user.Profile.LastName,
			"review_date": review.ReviewDate,
			"decision": func() string {
				if strings.ToLower(user.Role.Name) == "admin" && review.Decision != "" {
					return review.Decision
				}
				return ""
			}(),
		},
	}

	c.Json(w, http.StatusOK, "Review updated successfully", resp)
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
