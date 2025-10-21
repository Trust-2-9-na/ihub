package controllers

import (
	"net/http"
	"strings"
	"web/services/assets/models"
)

var performanceMap = map[string]float64{
	models.ProposalStatusSubmitted:     0,
	models.ProposalStatusUnderReview:   30,
	models.ProposalStatusOngoing:       30,
	models.ProposalStatusApproved:      100,
	models.ProposalStatusRejected:      0,
	models.ProposalStatusNeedsRevision: 0,
}

func (c *Construct) GetProgressView(w http.ResponseWriter, r *http.Request) {
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "unauthorized: "+err.Error(), nil)
		return
	}

	role := strings.ToLower(user.Role.Name)

	// ─── STUDENT VIEW ───────────────────────────────
	if role == "student" {
		var proposals []models.Proposal
		if err := c.DB.
			Preload("ProposalReviews.ReviewedBy.Profile").
			Preload("Cohort").
			Preload("SubmittedBy.Profile").
			Where("submitted_by_id = ?", user.UserID).
			Find(&proposals).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "failed to fetch proposals", map[string]interface{}{"error": err.Error()})
			return
		}

		result := make([]map[string]interface{}, 0, len(proposals))
		for _, p := range proposals {
			history := make([]map[string]interface{}, 0, len(p.ProposalReviews))
			for _, review := range p.ProposalReviews {
				history = append(history, map[string]interface{}{
					"phase":    review.Decision,
					"date":     review.ReviewDate,
					"comments": review.Comments,
					"by":       review.ReviewedBy.Profile.FirstName + " " + review.ReviewedBy.Profile.LastName,
				})
			}

			// calculate performance
			var performance float64
			switch p.Status {
			case models.ProposalStatusSubmitted:
				performance = 0
			case models.ProposalStatusUnderReview, models.ProposalStatusOngoing:
				performance = 30
			case models.ProposalStatusApproved:
				performance = 100
			case models.ProposalStatusRejected, models.ProposalStatusNeedsRevision:
				performance = 0
			}

			result = append(result, map[string]interface{}{
				"proposal_id": p.ProposalID,
				"title":       p.Title,
				"abstract":    p.Abstract,
				"category":    p.Category,
				"subfield":    p.Subfield,
				"status":      p.Status,
				"cohort": func() string {
					if p.ProposalCohort != nil {
						return p.ProposalCohort.Name
					}
					return ""
				}(),
				"performance":  performance,
				"history":      history,
				"submitted_at": p.SubmissionDate,
			})
		}

		c.Json(w, http.StatusOK, "Student progress fetched successfully", map[string]interface{}{
			"progress": result,
		})

	}

	// ─── ADMIN / OPSADMIN VIEW ───────────────────────────────
	if role == "opsadmin" || role == "systemadmin" {
		var counts struct {
			Submitted     int64
			UnderReview   int64
			Approved      int64
			Rejected      int64
			NeedsRevision int64
			Total         int64
		}

		c.DB.Model(&models.Proposal{}).Count(&counts.Total)
		c.DB.Model(&models.Proposal{}).Where("status = ?", models.ProposalStatusSubmitted).Count(&counts.Submitted)
		c.DB.Model(&models.Proposal{}).Where("status = ?", models.ProposalStatusUnderReview).Count(&counts.UnderReview)
		c.DB.Model(&models.Proposal{}).Where("status = ?", models.ProposalStatusApproved).Count(&counts.Approved)
		c.DB.Model(&models.Proposal{}).Where("status = ?", models.ProposalStatusRejected).Count(&counts.Rejected)
		c.DB.Model(&models.Proposal{}).Where("status = ?", models.ProposalStatusNeedsRevision).Count(&counts.NeedsRevision)

		// calculate overall performance
		var performance float64
		if counts.Total > 0 {
			performance = float64(counts.Approved*100+counts.UnderReview*30+counts.Submitted*0) / float64(counts.Total)
		}

		response := map[string]interface{}{
			"counts": map[string]int64{
				"submitted":      counts.Submitted,
				"under_review":   counts.UnderReview,
				"approved":       counts.Approved,
				"rejected":       counts.Rejected,
				"needs_revision": counts.NeedsRevision,
				"total":          counts.Total,
			},
			"performance": performance,
		}

		c.Json(w, http.StatusOK, "Admin progress fetched successfully", response)
		return
	}

	c.Json(w, http.StatusForbidden, "you do not have permission to view progress", nil)
}
