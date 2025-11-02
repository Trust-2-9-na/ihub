package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"errors"

	"web/services/assets/middlewares"
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
    var existing models.CohortUser
    err := db.Where("cohort_cohort_id = ? AND user_user_id = ?", cohortID, studentID).First(&existing).Error

    if err == nil {
        // Student already assigned to this cohort
        if existing.Role == "Student" {
            return fmt.Errorf("student (ID %d) is already assigned to cohort (ID %d)", studentID, cohortID)
        }

        // Assigned but role is different — update to Student
        existing.Role = "Student"
        return db.Save(&existing).Error
    }

    if errors.Is(err, gorm.ErrRecordNotFound) {
        // Record doesn't exist — create new
        cu := models.CohortUser{
            UserCohortID: cohortID,
            MemberID:     studentID,
            Role:         "Student",
            CreatedAt:    time.Now(),
        }
        return db.Create(&cu).Error
    }

    // Unexpected error
    return fmt.Errorf("failed to assign student to cohort: %w", err)
}


func (c *Construct) AddReview(w http.ResponseWriter, r *http.Request) {
	// --- Parse proposal_id from query ---
	proposalIDStr := r.URL.Query().Get("related_proposal_id")
	if proposalIDStr == "" {
		c.Json(w, http.StatusBadRequest, "proposal_id is required", nil)
		return
	}
	proposalID, err := strconv.ParseUint(proposalIDStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "invalid proposal_id", nil)
		return
	}

	// --- Decode payload ---
	var payload struct {
		Comments   *string `json:"comments"`
		Decision   string  `json:"decision,omitempty"`    // Admin/OpsAdmin only
		CohortName *string `json:"cohort_name,omitempty"` // Optional for assigning
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Get current user ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	var user models.User
	if err := c.DB.Preload("Role").Preload("Profile").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "User not found", nil)
		return
	}
	roleName := strings.ToLower(user.Role.Name)

	// --- Fetch proposal ---
	var proposal models.Proposal
	if err := c.DB.Preload("SubmittedBy.Profile").Preload("ProposalCohort").
		First(&proposal, "proposal_id = ?", proposalID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.Json(w, http.StatusNotFound, "Proposal not found", nil)
			return
		}
		c.Json(w, http.StatusInternalServerError, "Database error", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Default decision for OpsAdmin ---
	if roleName == "opsadmin" && payload.Decision == "" {
		payload.Decision = models.DecisionPending
	}

	// --- Create review record ---
	review := models.ProposalReview{
		RelatedProposalID: proposal.ProposalID,
		ReviewedByID:      user.UserID,
		Comments:          payload.Comments,
		Decision:          payload.Decision,
		ReviewDate:        time.Now(),
	}
	if err := c.DB.Create(&review).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to add review", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Handle OpsAdmin Decisions ---
	if roleName == "opsadmin" {
		c.handleProposalDecision(&proposal, payload.Decision, payload.CohortName)
	} else {
		// Supervisors/Mentors: only comment
		c.NotifyAndTrack(
			proposal.SubmittedByID,
			"New Review Comment",
			"Your proposal has received a new comment or feedback.",
			"Proposal Review",
			"Proposal",
			nil,
			proposal.Status,
			true,
		)
	}

	// --- Reload proposal for response ---
	if err := c.DB.Preload("ProposalCohort").Preload("SubmittedBy.Profile").First(&proposal, "proposal_id = ?", proposal.ProposalID).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to reload proposal", nil)
		return
	}

	// --- Build response ---
	reviewResponse := map[string]interface{}{
		"review_id":   review.ReviewID,
		"comments":    review.Comments,
		"reviewed_by": user.Profile.FirstName + " " + user.Profile.LastName,
		"review_date": review.ReviewDate,
	}
	if roleName == "admin" || roleName == "opsadmin" {
		reviewResponse["decision"] = review.Decision
	}

	response := map[string]interface{}{
		"proposal": map[string]interface{}{
			"id":       proposal.ProposalID,
			"title":    proposal.Title,
			"abstract": proposal.Abstract,
			"category": proposal.Category,
			"status":   proposal.Status,
			"cohort": func() string {
				if proposal.ProposalCohort != nil {
					return proposal.ProposalCohort.Name
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

	c.Json(w, http.StatusCreated, "Review added successfully", response)
}

// --- Centralized Proposal Decision Handler ---
func (c *Construct) handleProposalDecision(proposal *models.Proposal, decision string, cohortName *string) {
	switch decision {
	case models.DecisionApproved:
		proposal.Status = models.ProposalStatusApproved
		proposal.SubmissionDate = ptrTime(time.Now())

		// Assign cohort if missing
		if proposal.ProposalCohortID == nil {
			if cohortName == nil || *cohortName == "" {
				fmt.Println("❌ Cohort name required when approving proposal without cohort")
				return
			}
			var cohort models.Cohort
			if err := c.DB.Where("name = ?", *cohortName).First(&cohort).Error; err != nil {
				fmt.Println("❌ Cohort not found:", err)
				return
			}
			proposal.ProposalCohortID = &cohort.CohortID
			proposal.ProposalCohort = &cohort
		}

		_ = c.DB.Save(proposal)
		_ = AssignStudentToCohort(c.DB, proposal.SubmittedByID, *proposal.ProposalCohortID)

		// Create ProgressEntity
		progressEntity := models.ProgressEntity{
			EntityCohortID: proposal.ProposalCohortID,
			AssignedToID:   &proposal.SubmittedByID,
			EntityName:     proposal.Title,
			EntityType:     "Proposal",
			Status:         "Approved",
			ProgressType:   "Milestone",
		}
		_ = c.DB.Create(&progressEntity)

		c.NotifyAndTrack(
			proposal.SubmittedByID,
			"Cohort Assignment & Progress Tracking",
			fmt.Sprintf("You have been assigned to cohort '%s' and your proposal is now tracked.", proposal.ProposalCohort.Name),
			"Cohort Assignment",
			"Proposal",
			proposal.ProposalCohortID,
			"Assigned",
			true,
		)

	case models.DecisionRejected, models.DecisionNeedsRevision:
		status := "Rejected"
		if decision == models.DecisionNeedsRevision {
			status = "Needs Revision"
			proposal.Status = models.ProposalStatusNeedsRevision
		} else {
			proposal.Status = models.ProposalStatusRejected
		}
		_ = c.DB.Save(proposal)

		progressEntity := models.ProgressEntity{
			AssignedToID: &proposal.SubmittedByID,
			EntityName:   proposal.Title,
			EntityType:   "Proposal",
			Status:       status,
			ProgressType: "Milestone",
		}
		_ = c.DB.Create(&progressEntity)

		c.NotifyAndTrack(
			proposal.SubmittedByID,
			fmt.Sprintf("Proposal %s", decision),
			fmt.Sprintf("Your proposal '%s' has been %s and is now tracked.", proposal.Title, decision),
			"Proposal Decision",
			"Proposal",
			nil,
			proposal.Status,
			true,
		)
	}
}

// --- Helper for pointer time ---
func ptrTime(t time.Time) *time.Time {
	return &t
}

// ─── GET REVIEWS-------------------------------------------------------

func (c *Construct) GetReviews(w http.ResponseWriter, r *http.Request) {
	// --- Parse proposal_ids from query ---
	proposalIDsStr := r.URL.Query()["related_proposal_id"]
	if len(proposalIDsStr) == 0 {
		c.Json(w, http.StatusBadRequest, "at least one proposal_id is required", nil)
		return
	}

	var proposalIDs []uint64
	for _, idStr := range proposalIDsStr {
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			c.Json(w, http.StatusBadRequest, fmt.Sprintf("invalid proposal_id: %s", idStr), nil)
			return
		}
		proposalIDs = append(proposalIDs, id)
	}

	// --- Get current user ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Role").Preload("Profile").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "User not found", nil)
		return
	}

	roleName := strings.ToLower(user.Role.Name)

	// --- Fetch proposals based on role ---
	var proposals []models.Proposal
	query := c.DB.Preload("SubmittedBy.Profile").Preload("ProposalCohort").Where("proposal_id IN ?", proposalIDs)

	switch roleName {
	case "admin":
		// Admin sees only approved proposals
		query = query.Where("status = ?", models.ProposalStatusApproved)
	case "opsadmin":
		// OpsAdmin sees all (submitted + approved)
		// no extra filter
	default:
		// Students see only their own proposals
		query = query.Where("submitted_by_id = ?", user.UserID)
	}

	if err := query.Find(&proposals).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch proposals", map[string]interface{}{"error": err.Error()})
		return
	}
	if len(proposals) == 0 {
		c.Json(w, http.StatusNotFound, "No proposals found", nil)
		return
	}

	// --- Fetch all reviews for these proposals ---
	var reviews []models.ProposalReview
	if err := c.DB.Preload("ReviewedBy.Profile").
		Where("related_proposal_id IN ?", proposalIDs). // <- use related_proposal_id
		Find(&reviews).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch reviews", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Group reviews by proposal_id ---
	reviewsMap := make(map[uint64][]map[string]interface{})
	for _, rev := range reviews {
		reviewerName := ""
		if rev.ReviewedBy != nil {
			reviewerName = rev.ReviewedBy.Profile.FirstName + " " + rev.ReviewedBy.Profile.LastName
		}

		entry := map[string]interface{}{
			"review_id":   rev.ReviewID,
			"comments":    rev.Comments,
			"reviewed_by": reviewerName,
			"review_date": rev.ReviewDate,
		}

		// System admin and opsadmin can see decision
		if roleName == "admin" || roleName == "opsadmin" {
			entry["decision"] = rev.Decision
		}

		reviewsMap[rev.RelatedProposalID] = append(reviewsMap[rev.RelatedProposalID], entry)
	}

	// --- Build response ---
	response := []map[string]interface{}{}
	for _, p := range proposals {
		submittedBy := map[string]interface{}{
			"first_name": p.SubmittedBy.Profile.FirstName,
			"last_name":  p.SubmittedBy.Profile.LastName,
			"email":      p.SubmittedBy.Email,
		}
		cohortName := ""
		if p.ProposalCohort != nil {
			cohortName = p.ProposalCohort.Name
		}

		proposalResp := map[string]interface{}{
			"id":           p.ProposalID,
			"title":        p.Title,
			"abstract":     p.Abstract,
			"category":     p.Category,
			"subfield":     p.Subfield,
			"status":       p.Status,
			"cohort":       cohortName,
			"submitted_by": submittedBy,
		}

		// For admins: include who approved
		if roleName == "admin" && p.Status == models.ProposalStatusApproved {
			var approvalReview models.ProposalReview
			_ = c.DB.Preload("ReviewedBy.Profile").
				Where("related_proposal_id = ? AND decision = ?", p.ProposalID, models.DecisionApproved).
				Order("review_date ASC").
				First(&approvalReview).Error
			if approvalReview.ReviewedBy != nil {
				proposalResp["approved_by"] = approvalReview.ReviewedBy.Profile.FirstName + " " + approvalReview.ReviewedBy.Profile.LastName
			}
		}

		response = append(response, map[string]interface{}{
			"proposal": proposalResp,
			"reviews":  reviewsMap[p.ProposalID],
		})
	}

	// --- Track access ---
	for _, p := range proposals {
		c.NotifyAndTrack(
			user.UserID,
			"Viewed Proposal Reviews",
			fmt.Sprintf("User viewed reviews for proposal '%s'", p.Title),
			"Review Access",
			"Proposal",
			&p.ProposalID,
			p.Status,
			false,
		)
	}
	c.Json(w, http.StatusOK, "Proposal reviews fetched successfully", map[string]interface{}{
		"reviews": response,
	})

}

// ─── GET SINGLE REVIEW ─────────────────────────────────────────-------------

// GetMyReviews returns all reviews accessible to the logged-in user
func (c *Construct) GetMyReviews(w http.ResponseWriter, r *http.Request) {
	// --- Get logged-in user ---
	userUUID, ok := r.Context().Value("user_uuid").(string)
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized: no user in context", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Profile").Preload("Role").First(&user, "user_uuid = ?", userUUID).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "User not found", nil)
		return
	}

	// --- Build base query depending on role ---
	var reviews []models.ProposalReview
	dbQuery := c.DB.Preload("ReviewedBy.Profile").
		Preload("Proposal.SubmittedBy.Profile").
		Preload("Proposal.ProposalCohort ")

	switch strings.ToLower(user.Role.Name) {
	case "opsadmin":
		// OpsAdmin: only their own reviews
		dbQuery = dbQuery.Where("reviewed_by_id = ?", user.UserID)
	case "systemadmin":
		// SystemAdmin: can view all reviews
	default:
		// Students: only reviews on their submitted proposals
		dbQuery = dbQuery.Joins("JOIN proposals p ON p.proposal_id = proposal_reviews.related_proposal_id").
			Where("p.submitted_by_id = ?", user.UserID)
	}

	if err := dbQuery.Find(&reviews).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch reviews", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Audit & notification for OpsAdmin / SystemAdmin ---
	role := strings.ToLower(user.Role.Name)
	if role == "opsadmin" || role == "systemadmin" {
		c.NotifyAndTrack(
			user.UserID,
			"Viewed Proposal Reviews",
			"User viewed accessible proposal reviews",
			"Review Access",
			"ProposalReview",
			nil,
			"",
			false,
		)
	}

	// --- Build response ---
	response := make([]map[string]interface{}, 0, len(reviews))
	for _, rev := range reviews {
		reviewerName := ""
		if rev.ReviewedBy != nil {
			reviewerName = rev.ReviewedBy.Profile.FirstName + " " + rev.ReviewedBy.Profile.LastName
		}

		proposal := rev.RelatedProposal
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
		if proposal.ProposalCohort != nil {
			cohortName = proposal.ProposalCohort.Name
		}

		reviewData := map[string]interface{}{
			"review_id":   rev.ReviewID,
			"comments":    rev.Comments,
			"review_date": rev.ReviewDate,
			"reviewed_by": reviewerName,
		}

		// Only OpsAdmin and SystemAdmin see decision
		if role == "opsadmin" || role == "systemadmin" {
			reviewData["decision"] = rev.Decision
		}

		response = append(response, map[string]interface{}{
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

	c.Json(w, http.StatusOK, "Reviews fetched successfully", map[string]interface{}{
		"reviews": response,
	})
}

// ================== UPDATE REVIEW ==========================================

func (c *Construct) UpdateReviewByProposal(w http.ResponseWriter, r *http.Request) {
	// --- Step 1: Get proposal ID ---
	proposalIDStr := r.URL.Query().Get("proposal_id")
	if proposalIDStr == "" {
		c.Json(w, http.StatusBadRequest, "proposal_id is required", nil)
		return
	}
	proposalID, err := strconv.ParseUint(proposalIDStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "invalid proposal_id", nil)
		return
	}

	// --- Step 2: Decode payload ---
	var payload struct {
		Comments   *string `json:"comments,omitempty"`
		Decision   *string `json:"decision,omitempty"`    // OpsAdmin only
		CohortName *string `json:"cohort_name,omitempty"` // OpsAdmin only
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		c.Json(w, http.StatusBadRequest, "invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Step 3: Get authenticated user ---
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "unauthorized: "+err.Error(), nil)
		return
	}
	role := strings.ToLower(user.Role.Name)

	// --- Step 4: Load proposal ---
	var proposal models.Proposal
	if err := c.DB.Preload("ProposalCohort ").Preload("SubmittedBy.Profile").
		First(&proposal, "proposal_id = ?", proposalID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.Json(w, http.StatusNotFound, "proposal not found", nil)
			return
		}
		c.Json(w, http.StatusInternalServerError, "database error: "+err.Error(), nil)
		return
	}

	// --- Step 5: Load existing review or create new ---
	var review models.ProposalReview
	reviewFound := true
	if err := c.DB.Where("related_proposal_id = ? AND reviewed_by_id = ?", proposalID, user.UserID).First(&review).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			review = models.ProposalReview{
				RelatedProposalID: proposalID,
				ReviewedByID:      user.UserID,
			}
			reviewFound = false
		} else {
			c.Json(w, http.StatusInternalServerError, "database error: "+err.Error(), nil)
			return
		}
	}

	now := time.Now()

	// --- Step 6: Role-based permissions ---
	if role == "opsadmin" {
		if payload.Comments != nil {
			review.Comments = payload.Comments
		}
		if payload.Decision != nil {
			review.Decision = *payload.Decision

			if *payload.Decision == models.DecisionApproved && proposal.ProposalCohortID == nil {
				if payload.CohortName == nil || *payload.CohortName == "" {
					c.Json(w, http.StatusBadRequest, "cohort_name is required when approving a proposal without a cohort", nil)
					return
				}
				var cohort models.Cohort
				if err := c.DB.Where("name = ?", *payload.CohortName).First(&cohort).Error; err != nil {
					c.Json(w, http.StatusBadRequest, "cohort not found", nil)
					return
				}
				proposal.ProposalCohortID = &cohort.CohortID

				// Assign student to cohort
				c.DB.Create(&models.CohortUser{
					UserCohortID: cohort.CohortID,
					MemberID:     proposal.SubmittedByID,
					Role:         "Student",
				})
			}

			// Update proposal status
			proposal.Status = *payload.Decision
			proposal.SubmissionDate = &now
			c.DB.Save(&proposal)

			// --- Track progress entity ---
			progress := models.ProgressEntity{
				EntityCohortID: proposal.ProposalCohortID,
				AssignedToID:   &proposal.SubmittedByID,
				EntityName:     proposal.Title,
				EntityType:     "Proposal",
				Status:         *payload.Decision,
				ProgressType:   "Milestone",
			}
			c.DB.Create(&progress)
		}

	} else if role == "student" || role == "systemadmin" {
		c.Json(w, http.StatusForbidden, "you do not have permission to update this review", nil)
		return
	}

	// --- Step 7: Update review timestamp ---
	review.ReviewDate = now
	if reviewFound {
		if err := c.DB.Save(&review).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "failed to update review", map[string]interface{}{"error": err.Error()})
			return
		}
	} else {
		if err := c.DB.Create(&review).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "failed to create review", map[string]interface{}{"error": err.Error()})
			return
		}
	}

	// --- Step 8: Notify student ---
	c.NotifyAndTrack(
		proposal.SubmittedByID,
		"Proposal Reviewed",
		fmt.Sprintf("Your proposal '%s' has new feedback/comments or a decision.", proposal.Title),
		"Proposal Review",
		"Proposal",
		proposal.ProposalCohortID,
		proposal.Status,
		true,
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

	// --- Step 10: Response ---
	resp := map[string]interface{}{
		"proposal": map[string]interface{}{
			"id":       proposal.ProposalID,
			"title":    proposal.Title,
			"abstract": proposal.Abstract,
			"category": proposal.Category,
			"status":   proposal.Status,
			"cohort": func() string {
				if proposal.ProposalCohortID != nil {
					return proposal.ProposalCohort.Name
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
				if role == "opsadmin" && review.Decision != "" {
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
	// --- Parse JSON body for review_ids ---
	var payload struct {
		ReviewIDs []uint64 `json:"review_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	if len(payload.ReviewIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "review_ids are required", nil)
		return
	}

	// --- Get logged-in user ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Role").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "User not found", nil)
		return
	}

	roleName := strings.ToLower(user.Role.Name)

	// --- Fetch reviews with proposal relation ---
	var reviews []models.ProposalReview
	if err := c.DB.Preload("Proposal").Where("review_id IN ?", payload.ReviewIDs).Find(&reviews).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Database error", map[string]interface{}{"error": err.Error()})
		return
	}
	if len(reviews) == 0 {
		c.Json(w, http.StatusNotFound, "No reviews found for provided IDs", nil)
		return
	}

	// --- Access control ---
	switch roleName {
	case "student":
		// Students can delete reviews for proposals they submitted
		for _, review := range reviews {
			if review.RelatedProposal == nil || review.RelatedProposal.SubmittedByID != user.UserID {
				c.Json(w, http.StatusForbidden, "You can only delete reviews for your own proposals", nil)
				return
			}
		}

	case "opsadmin":
		// OpsAdmins can delete only reviews they created
		for _, review := range reviews {
			if review.ReviewedByID != user.UserID {
				c.Json(w, http.StatusForbidden, "You can only delete reviews you created", nil)
				return
			}
		}

	case "systemadmin":
		// SystemAdmins can delete any review (no restriction)

	default:
		c.Json(w, http.StatusForbidden, "You are not allowed to delete reviews", nil)
		return
	}

	// --- Delete reviews ---
	if err := c.DB.Delete(&reviews).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to delete reviews", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Audit log ---
	for _, r := range reviews {
		_ = c.LogAudit(user.UserID, "delete_review", nil, &r.ReviewID, nil, nil)
	}

	// --- Response ---
	deletedIDs := make([]uint64, len(reviews))
	for i, r := range reviews {
		deletedIDs[i] = r.ReviewID
	}

	c.Json(w, http.StatusOK, "Reviews deleted successfully", map[string]interface{}{
		"deleted_review_ids": deletedIDs,
	})
}
