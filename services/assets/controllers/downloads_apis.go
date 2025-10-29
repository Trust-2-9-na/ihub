package controllers

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"web/services/assets/models"

	"github.com/gorilla/mux"
)

//=========================================================================================
//                     Proposal download APIS
//-----------------------------------------------------------------------------------------

func (c *Construct) DownloadProposal(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr := vars["proposal_id"]
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid proposal ID", nil)
		return
	}

	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var proposal models.Proposal
	if err := c.DB.Preload("ProposalCohort").
		Preload("SubmittedBy.Profile").
		Preload("ProposalTeam.UserTeams.UserRef.Profile").
		First(&proposal, id).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Proposal not found", nil)
		return
	}

	canDownload := false
	role := strings.ToLower(user.Role.Name)

	switch role {
	case "student":
		if proposal.SubmittedByID == user.UserID {
			canDownload = true
		}
	case "supervisor":
		if proposal.ProposalCohortID != nil {
			cohortIDs := c.getAssignedCohorts(user.UserID, "Supervisor")
			for _, cid := range cohortIDs {
				if cid == *proposal.ProposalCohortID {
					canDownload = true
					break
				}
			}
		}
	case "opsadmin", "systemadmin":
		canDownload = true
	}

	if !canDownload {
		c.Json(w, http.StatusForbidden, "You do not have access to download this proposal", nil)
		return
	}

	if proposal.DocumentURL == nil || *proposal.DocumentURL == "" {
		c.Json(w, http.StatusBadRequest, "Proposal does not have a downloadable file", nil)
		return
	}

	// --- Track audit ---
	c.LogAudit(user.UserID, "download", ptrString("proposal"), &proposal.ProposalID, nil, nil)

	// --- Serve file ---
	filePath := *proposal.DocumentURL
	filename := filepath.Base(filePath)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, filePath)
}

// GET /api/proposals/download
func (c *Construct) ListDownloadableProposals(w http.ResponseWriter, r *http.Request) {
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// Pagination
	page, limit := 1, 20
	fmt.Sscan(r.URL.Query().Get("page"), &page)
	fmt.Sscan(r.URL.Query().Get("limit"), &limit)
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	query := c.DB.Preload("SubmittedBy.Profile").
		Preload("ProposalTeam.UserTeams.UserRef.Profile").
		Preload("ProposalCohort").
		Order("created_at DESC")

	role := strings.ToLower(user.Role.Name)

	switch role {
	case "student":
		query = query.Where("submitted_by_id = ?", user.UserID)
	case "supervisor":
		cohortIDs := c.getAssignedCohorts(user.UserID, "Supervisor")
		if len(cohortIDs) == 0 {
			c.Json(w, http.StatusOK, "No assigned cohorts found", map[string]interface{}{"data": []interface{}{}})
			return
		}
		query = query.Where("proposal_cohort_id IN ?", cohortIDs)
	case "opsadmin", "systemadmin":
		// no restriction, see all
	default:
		c.Json(w, http.StatusForbidden, "Unauthorized role", nil)
		return
	}

	// Count total
	var total int64
	query.Model(&models.Proposal{}).Count(&total)

	var proposals []models.Proposal
	if err := query.Limit(limit).Offset(offset).Find(&proposals).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch proposals", map[string]interface{}{"error": err.Error()})
		return
	}

	resp := []map[string]interface{}{}
	for _, p := range proposals {
		downloadLink := ""
		if p.DocumentURL != nil && *p.DocumentURL != "" {
			if strings.HasPrefix(*p.DocumentURL, "http") {
				downloadLink = *p.DocumentURL
			} else {
				downloadLink = fmt.Sprintf("/api/proposals/%d/download", p.ProposalID)
			}
		}

		cohortName := ""
		if p.ProposalCohort != nil {
			cohortName = p.ProposalCohort.Name
		}

		resp = append(resp, map[string]interface{}{
			"proposal_id":  p.ProposalID,
			"title":        p.Title,
			"submitted_by": p.SubmittedBy.Profile.FullName(),
			"cohort_id":    p.ProposalCohortID,
			"cohort_name":  cohortName,
			"document_url": downloadLink,
			"created_at":   p.CreatedAt,
			"updated_at":   p.UpdatedAt,
		})
	}

	c.NotifyAndTrack(user.UserID, "Viewed Proposal Download List",
		fmt.Sprintf("%s viewed downloadable proposals list", user.Username),
		"Read", "Proposal", nil, "Viewed", false,
	)

	c.Json(w, http.StatusOK, "Downloadable proposals fetched successfully", map[string]interface{}{
		"page":  page,
		"limit": limit,
		"total": total,
		"data":  resp,
	})
}

//=========================================================================================
//                    Weekly Reports download APIS
//-----------------------------------------------------------------------------------------

// GET /api/weekly-reports/download/{id}
func (c *Construct) DownloadWeeklyReport(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	reportIDStr := vars["report_id"]
	reportID, err := strconv.ParseUint(reportIDStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid report ID", nil)
		return
	}

	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var report models.WeeklyReport
	if err := c.DB.Preload("Student.Profile").
		Preload("ReviewedBy.Profile").
		Preload("ReportCohortInfo").
		First(&report, reportID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Weekly report not found", nil)
		return
	}

	// --- Access control ---
	canDownload := false
	role := strings.ToLower(user.Role.Name)

	switch role {
	case "student":
		if report.StudentID == user.UserID {
			canDownload = true
		}
	case "mentor":
		var assignedStudentIDs []uint64
		c.DB.Model(&models.MentorStudentAssignment{}).
			Where("mentor_ref_id = ? AND deleted_at IS NULL", user.UserID).
			Pluck("student_ref_id", &assignedStudentIDs)
		for _, sid := range assignedStudentIDs {
			if sid == report.StudentID {
				canDownload = true
				break
			}
		}
	case "supervisor":
		cohortIDs := c.getAssignedCohorts(user.UserID, "Supervisor")
		for _, cid := range cohortIDs {
			if report.ReportCohortID != nil && cid == *report.ReportCohortID {
				canDownload = true
				break
			}
		}

	case "opsadmin", "systemadmin":
		canDownload = true
	default:
		c.Json(w, http.StatusForbidden, "Unauthorized role", nil)
		return
	}

	if !canDownload {
		c.Json(w, http.StatusForbidden, "You do not have access to download this report", nil)
		return
	}

	if report.DocumentURL == nil || *report.DocumentURL == "" {
		c.Json(w, http.StatusBadRequest, "Report does not have a downloadable file", nil)
		return
	}

	// --- Track audit ---
	c.LogAudit(user.UserID, "download", ptrString("weekly_report"), &report.ID, nil, nil)

	// --- Serve file ---
	filename := filepath.Base(*report.DocumentURL)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, *report.DocumentURL)

}

// GET /api/weekly-reports/download
func (c *Construct) ListDownloadableWeeklyReports(w http.ResponseWriter, r *http.Request) {
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// Pagination
	page, limit := 1, 20
	fmt.Sscan(r.URL.Query().Get("page"), &page)
	fmt.Sscan(r.URL.Query().Get("limit"), &limit)
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	// Filters
	reportStatus := r.URL.Query().Get("status")
	var weekStart, weekEnd time.Time
	if ws := r.URL.Query().Get("week_start"); ws != "" {
		weekStart, _ = time.Parse("2006-01-02", ws)
	}
	if we := r.URL.Query().Get("week_end"); we != "" {
		weekEnd, _ = time.Parse("2006-01-02", we)
	}

	query := c.DB.Preload("Student.Profile").
		Preload("ReportCohortInfo").
		Order("created_at DESC")

	role := strings.ToLower(user.Role.Name)
	switch role {
	case "student":
		query = query.Where("student_id = ?", user.UserID)
	case "mentor":
		var assignedStudentIDs []uint64
		c.DB.Model(&models.MentorStudentAssignment{}).
			Where("mentor_ref_id = ? AND deleted_at IS NULL", user.UserID).
			Pluck("student_ref_id", &assignedStudentIDs)
		if len(assignedStudentIDs) == 0 {
			c.Json(w, http.StatusOK, "No assigned students found", map[string]interface{}{"data": []interface{}{}})
			return
		}
		query = query.Where("student_id IN ?", assignedStudentIDs)
	case "supervisor":
		cohortIDs := c.getAssignedCohorts(user.UserID, "Supervisor")
		if len(cohortIDs) == 0 {
			c.Json(w, http.StatusOK, "No assigned cohorts found", map[string]interface{}{"data": []interface{}{}})
			return
		}
		query = query.Where("report_cohort_id IN ?", cohortIDs)
	case "opsadmin", "systemadmin":
		// No filter, can see all reports
	default:
		c.Json(w, http.StatusForbidden, "Unauthorized role", nil)
		return
	}

	// Apply optional filters
	if reportStatus != "" {
		query = query.Where("status = ?", reportStatus)
	}
	if !weekStart.IsZero() {
		query = query.Where("week_start >= ?", weekStart)
	}
	if !weekEnd.IsZero() {
		query = query.Where("week_end <= ?", weekEnd)
	}

	// Count total
	var total int64
	query.Model(&models.WeeklyReport{}).Count(&total)

	var reports []models.WeeklyReport
	if err := query.Limit(limit).Offset(offset).Find(&reports).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch reports", map[string]interface{}{"error": err.Error()})
		return
	}
	resp := []map[string]interface{}{}
	for _, report := range reports {
		downloadLink := ""
		if report.DocumentURL != nil && *report.DocumentURL != "" {
			if strings.HasPrefix(*report.DocumentURL, "http") {
				downloadLink = *report.DocumentURL
			} else {
				// Local file path download endpoint
				downloadLink = fmt.Sprintf("/api/weekly-reports/%d/download", report.ID)
			}
		}

		resp = append(resp, map[string]interface{}{
			"id":           report.ID,
			"student_id":   report.StudentID,
			"student_name": report.Student.Profile.FullName(),
			"cohort_id":    report.ReportCohortID,
			"cohort_name":  report.ReportCohortInfo.Name,
			"week_start":   report.WeekStart,
			"week_end":     report.WeekEnd,
			"status":       report.Status,
			"document_url": downloadLink,
			"created_at":   report.CreatedAt,
			"updated_at":   report.UpdatedAt,
		})
	}

	c.NotifyAndTrack(user.UserID, "Viewed Weekly Reports Download List",
		fmt.Sprintf("%s viewed downloadable weekly reports list", user.Username),
		"Read", "WeeklyReport", nil, "Viewed", false,
	)

	c.Json(w, http.StatusOK, "Downloadable weekly reports fetched successfully", map[string]interface{}{
		"page":  page,
		"limit": limit,
		"total": total,
		"data":  resp,
	})
}

//=========================================================================================
//                    System Resources download APIS
//-----------------------------------------------------------------------------------------

func (c *Construct) DownloadResource(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr := vars["resource_id"]
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid resource ID", nil)
		return
	}

	// --- 1️⃣ Authenticated user ---
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// --- 2️⃣ Fetch resource with relations ---
	var res models.Resource
	if err := c.DB.Preload("ResourceCohort.Users").
		Preload("ResourceTeam.UserTeams.UserRef").
		Preload("SharedWithUser").
		Preload("Uploader.Profile").
		First(&res, id).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Resource not found", nil)
		return
	}

	// --- 3️⃣ Access control ---
	canDownload := false
	role := strings.ToLower(user.Role.Name)

	if role == "opsadmin" || role == "systemadmin" {
		canDownload = true
	} else {
		// Public resource
		if res.ResourceCohortID == nil && res.ResourceTeamID == nil && res.SharedWithUserID == nil {
			canDownload = true
		}

		// Cohort
		if !canDownload && res.ResourceCohortID != nil {
			for _, u := range res.ResourceCohort.Users {
				if u.UserID == user.UserID {
					canDownload = true
					break
				}
			}
		}

		// Team
		if !canDownload && res.ResourceTeamID != nil {
			for _, ut := range res.ResourceTeam.UserTeams {
				if ut.UserRefID == user.UserID {
					canDownload = true
					break
				}
			}
		}

		// Shared user
		if !canDownload && res.SharedWithUserID != nil && *res.SharedWithUserID == user.UserID {
			canDownload = true
		}
	}

	if !canDownload {
		c.Json(w, http.StatusForbidden, "You do not have access to download this resource", nil)
		return
	}

	// --- 4️⃣ Check for downloadable file ---
	if res.FilePath == "" && res.URL == "" {
		c.Json(w, http.StatusBadRequest, "Resource does not have a downloadable file or URL", nil)
		return
	}

	// --- 5️⃣ Track audit ---
	c.LogAudit(user.UserID, "download", ptrString("resource"), &res.ID, nil, nil)

	// --- 6️⃣ Serve resource ---
	if res.FilePath != "" {
		filename := filepath.Base(res.FilePath)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeFile(w, r, res.FilePath)
		return
	}

	// If it’s an external URL, redirect
	http.Redirect(w, r, res.URL, http.StatusFound)
}

// download list
func (c *Construct) ListDownloadableResources(w http.ResponseWriter, r *http.Request) {
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	page, limit := 1, 20
	fmt.Sscan(r.URL.Query().Get("page"), &page)
	fmt.Sscan(r.URL.Query().Get("limit"), &limit)
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	var resources []models.Resource
	if err := c.DB.Preload("ResourceCohort.Users").
		Preload("ResourceTeam.UserTeams.UserRef").
		Preload("SharedWithUser").
		Preload("Uploader.Profile").
		Order("created_at DESC").
		Limit(limit).Offset(offset).Find(&resources).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch resources", map[string]interface{}{"error": err.Error()})
		return
	}

	resp := []map[string]interface{}{}
	for _, res := range resources {
		canDownload := false

		if role := strings.ToLower(user.Role.Name); role == "opsadmin" || role == "systemadmin" {
			canDownload = true
		} else {
			// Public: no team, cohort, or shared user
			if res.ResourceCohortID == nil && res.ResourceTeamID == nil && res.SharedWithUserID == nil {
				canDownload = true
			}
			// Private: check if user is allowed
			if res.ResourceCohortID != nil {
				for _, u := range res.ResourceCohort.Users {
					if u.UserID == user.UserID {
						canDownload = true
						break
					}
				}
			}
			if !canDownload && res.ResourceTeamID != nil {
				for _, ut := range res.ResourceTeam.UserTeams {
					if ut.UserRefID == user.UserID {
						canDownload = true
						break
					}
				}
			}
			if !canDownload && res.SharedWithUserID != nil && *res.SharedWithUserID == user.UserID {
				canDownload = true
			}
		}

		if !canDownload {
			continue
		}

		downloadLink := ""
		if res.FilePath != "" {
			downloadLink = fmt.Sprintf("/api/resources/%d/download", res.ID)
		} else if res.URL != "" {
			downloadLink = res.URL
		}

		resp = append(resp, map[string]interface{}{
			"id":            res.ID,
			"title":         res.Title,
			"description":   res.Description,
			"resource_type": res.ResourceType,
			"document_url":  downloadLink,
			"created_at":    res.CreatedAt,
			"updated_at":    res.UpdatedAt,
		})
	}

	c.NotifyAndTrack(user.UserID, "Viewed downloadable resources list",
		fmt.Sprintf("%s viewed downloadable resources list", user.Username),
		"Read", "Resource", nil, "Viewed", false,
	)

	c.Json(w, http.StatusOK, "Downloadable resources fetched successfully", map[string]interface{}{
		"page":  page,
		"limit": limit,
		"total": len(resp),
		"data":  resp,
	})
}
