package controllers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"web/services/assets/models"

	"github.com/gorilla/mux"
)

// ================================================ Team Report Submissions ====================================

func (c *Construct) CreateTeamWeeklyReport(w http.ResponseWriter, r *http.Request) {
	type inputStruct struct {
		CohortID        uint64   `json:"cohort_id"`
		TeamID          *uint64  `json:"team_id,omitempty"` // optional for team reports
		WeekStartStr    string   `json:"week_start"`        // YYYY-MM-DD
		WeekEndStr      string   `json:"week_end"`          // YYYY-MM-DD
		WorkDone        string   `json:"work_done"`
		PlannedWork     string   `json:"planned_work"`
		NextWeek        string   `json:"next_week"`
		Challenges      string   `json:"challenges"`
		ProgressItemIDs []uint64 `json:"progress_item_ids,omitempty"` // optional
		DocumentURL     *string  `json:"document_url,omitempty"`      // <-- add this
	}

	var input inputStruct
	var documentURL *string

	// --- Parse request body ---
	contentType := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(contentType, "application/json"):
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid JSON payload", map[string]interface{}{"error": err.Error()})
			return
		}
		//document upload
		documentURL = input.DocumentURL

	case strings.HasPrefix(contentType, "multipart/form-data"):
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			c.Json(w, http.StatusBadRequest, "Failed to parse form data", map[string]interface{}{"error": err.Error()})
			return
		}

		input.CohortID, _ = strconv.ParseUint(r.FormValue("cohort_id"), 10, 64)
		if val := r.FormValue("team_id"); val != "" {
			if tid, err := strconv.ParseUint(val, 10, 64); err == nil {
				input.TeamID = &tid
			}
		}
		input.WorkDone = r.FormValue("work_done")
		input.PlannedWork = r.FormValue("planned_work")
		input.NextWeek = r.FormValue("next_week")
		input.Challenges = r.FormValue("challenges")
		input.WeekStartStr = r.FormValue("week_start")
		input.WeekEndStr = r.FormValue("week_end")

		// Parse progress item IDs (comma-separated)
		if ids := r.FormValue("progress_item_ids"); ids != "" {
			for _, idStr := range strings.Split(ids, ",") {
				if id, err := strconv.ParseUint(strings.TrimSpace(idStr), 10, 64); err == nil {
					input.ProgressItemIDs = append(input.ProgressItemIDs, id)
				}
			}
		}

		// Handle optional file upload
		file, handler, err := r.FormFile("document")
		if err == nil {
			defer file.Close()

			allowedExts := map[string]bool{".pdf": true, ".csv": true}
			ext := strings.ToLower(filepath.Ext(handler.Filename))
			if !allowedExts[ext] {
				c.Json(w, http.StatusBadRequest, "Invalid file type. Only PDF or CSV allowed.", nil)
				return
			}

			buff := make([]byte, 512)
			if _, err := file.Read(buff); err != nil {
				c.Json(w, http.StatusInternalServerError, "Failed to read uploaded file", nil)
				return
			}
			file.Seek(0, io.SeekStart)

			mimeType := http.DetectContentType(buff)
			if mimeType != "application/pdf" && mimeType != "text/csv" && mimeType != "application/vnd.ms-excel" {
				c.Json(w, http.StatusBadRequest, fmt.Sprintf("Invalid MIME type: %s. Only PDF or CSV allowed.", mimeType), nil)
				return
			}

			dateDir := time.Now().Format("20060102")
			uploadDir := filepath.Join("uploads", "reports", dateDir)
			os.MkdirAll(uploadDir, os.ModePerm)

			filename := fmt.Sprintf("report_%d_%d%s", r.Context().Value("user_id"), time.Now().Unix(), ext)
			filePath := filepath.Join(uploadDir, filename)

			dest, err := os.Create(filePath)
			if err != nil {
				c.Json(w, http.StatusInternalServerError, "Failed to save uploaded file", nil)
				return
			}
			defer dest.Close()
			io.Copy(dest, file)

			url := fmt.Sprintf("/%s", filePath)
			documentURL = &url
		}

	default:
		c.Json(w, http.StatusBadRequest, "Unsupported Content-Type. Use application/json or multipart/form-data.", nil)
		return
	}

	// --- Authenticate user ---
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// --- Parse week start/end ---
	weekStart, err := time.Parse("2006-01-02", input.WeekStartStr)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid week_start format. Use YYYY-MM-DD.", nil)
		return
	}
	weekEnd, err := time.Parse("2006-01-02", input.WeekEndStr)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid week_end format. Use YYYY-MM-DD.", nil)
		return
	}

	// --- Initialize report ---
	report := models.WeeklyReport{
		ReportCohortID: &input.CohortID,
		StudentID:      user.UserID,
		WeekStart:      weekStart,
		WeekEnd:        weekEnd,
		WorkDone:       input.WorkDone,
		PlannedWork:    input.PlannedWork,
		NextWeek:       input.NextWeek,
		Challenges:     input.Challenges,
		Status:         "Pending Verification",
		DocumentURL:    documentURL,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// --- Validate team membership if team_id provided ---
	var userTeam models.UserTeam
	if input.TeamID != nil {
		if err := c.DB.Where("team_team_id = ? AND user_user_id = ?", *input.TeamID, user.UserID).First(&userTeam).Error; err != nil {
			c.Json(w, http.StatusForbidden, "You are not part of this team", nil)
			return
		}
		report.TeamInfoID = input.TeamID
	}

	// --- Validate progress items ownership ---
	if len(input.ProgressItemIDs) > 0 {
		var studentItems []models.ProgressItem
		c.DB.Where("id IN ? AND created_by_id = ?", input.ProgressItemIDs, user.UserID).Find(&studentItems)
		if len(studentItems) == 0 {
			c.Json(w, http.StatusForbidden, "You can only submit a report for items you created", nil)
			return
		}

		// Replace IDs with verified ones
		input.ProgressItemIDs = make([]uint64, len(studentItems))
		for i, item := range studentItems {
			input.ProgressItemIDs[i] = item.ID
		}
	}

	// --- Save report ---
	if err := c.DB.Create(&report).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create weekly report", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Link progress items ---
	if len(input.ProgressItemIDs) > 0 {
		var items []models.ProgressItem
		c.DB.Where("id IN ?", input.ProgressItemIDs).Find(&items)
		if len(items) > 0 {
			c.DB.Model(&report).Association("ProgressItems").Append(items)
		}
	}

	// --- Notify supervisors ---
	var supervisors []models.User
	c.DB.Joins("JOIN cohort_supervisors cs ON cs.supervisor_id = users.user_id").
		Where("cs.cohort_id = ?", input.CohortID).
		Find(&supervisors)

	for _, sup := range supervisors {
		c.NotifyAndTrack(
			sup.UserID,
			"New Weekly Report Submitted",
			fmt.Sprintf("A new weekly report from %s is available for review.", user.Username),
			"Weekly Report Review",
			"WeeklyReport",
			&report.ID,
			"Pending Verification",
			false,
		)
	}

	// --- Optional: Notify team members if leader submits ---
	if input.TeamID != nil && userTeam.Role == string(models.TeamLeader) {
		var team models.Team
		if err := c.DB.Preload("UserTeams.UserRef").Where("team_id = ?", *input.TeamID).First(&team).Error; err == nil {
			for _, ut := range team.UserTeams {
				if ut.UserRefID != user.UserID {
					c.NotifyAndTrack(
						ut.UserRefID,
						"New Team Progress Update",
						fmt.Sprintf("Leader %s submitted a weekly report. You can now submit yours.", user.Username),
						"TeamProgress",
						"ProgressItem",
						nil,
						"Pending Verification",
						false,
					)
				}
			}
		}
	}

	// --- Response ---
	c.Json(w, http.StatusCreated, "Weekly report submitted successfully", map[string]interface{}{
		"data": map[string]interface{}{
			"id":             report.ID,
			"student_id":     report.StudentID,
			"cohort_id":      report.ReportCohortID,
			"team_id":        report.TeamInfoID,
			"status":         report.Status,
			"week_start":     input.WeekStartStr,
			"week_end":       input.WeekEndStr,
			"document_url":   report.DocumentURL,
			"work_done":      report.WorkDone,
			"planned_work":   report.PlannedWork,
			"next_week":      report.NextWeek,
			"challenges":     report.Challenges,
			"progress_items": input.ProgressItemIDs,
			"created_at":     report.CreatedAt,
			"updated_at":     report.UpdatedAt,
		},
	})
}

// Update a weekly report

func (c *Construct) UpdateTeamWeeklyReport(w http.ResponseWriter, r *http.Request) {
	// --- Parse report ID ---
	vars := mux.Vars(r)
	idStr, ok := vars["id"]
	if !ok || idStr == "" {
		c.Json(w, http.StatusBadRequest, "Team report ID is required", nil)
		return
	}
	reportID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid report ID", nil)
		return
	}

	// --- Parse input ---
	var input struct {
		WorkDone        *string  `json:"work_done,omitempty"`
		PlannedWork     *string  `json:"planned_work,omitempty"`
		NextWeek        *string  `json:"next_week,omitempty"`
		Challenges      *string  `json:"challenges,omitempty"`
		ProgressItemIDs []uint64 `json:"progress_item_ids,omitempty"`
		DocumentURL     *string  `json:"document_url,omitempty"`
	}
	var documentURL *string

	contentType := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(contentType, "application/json"):
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid JSON payload", map[string]interface{}{"error": err.Error()})
			return
		}
		documentURL = input.DocumentURL

	case strings.HasPrefix(contentType, "multipart/form-data"):
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			c.Json(w, http.StatusBadRequest, "Failed to parse form data", map[string]interface{}{"error": err.Error()})
			return
		}

		if data := r.FormValue("data"); data != "" {
			if err := json.Unmarshal([]byte(data), &input); err != nil {
				c.Json(w, http.StatusBadRequest, "Invalid JSON in 'data' field", nil)
				return
			}
			documentURL = input.DocumentURL
		}

		// Handle file upload
		file, fileHeader, err := r.FormFile("document")
		if err == nil {
			defer file.Close()
			ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
			if ext != ".pdf" && ext != ".csv" {
				c.Json(w, http.StatusBadRequest, "Invalid file type: only PDF or CSV allowed", nil)
				return
			}

			buf := make([]byte, 512)
			_, _ = file.Read(buf)
			fileType := http.DetectContentType(buf)
			file.Seek(0, io.SeekStart)
			if !strings.Contains(fileType, "pdf") && !strings.Contains(fileType, "csv") && !strings.Contains(fileType, "text/plain") {
				c.Json(w, http.StatusBadRequest, "Invalid file MIME type", nil)
				return
			}

			uploadDir := filepath.Join("uploads", "reports")
			os.MkdirAll(uploadDir, os.ModePerm)
			savePath := filepath.Join(uploadDir, fmt.Sprintf("team_report_%d_%d%s", reportID, time.Now().Unix(), ext))
			dst, err := os.Create(savePath)
			if err != nil {
				c.Json(w, http.StatusInternalServerError, "Failed to save file", nil)
				return
			}
			defer dst.Close()
			io.Copy(dst, file)

			url := fmt.Sprintf("/%s", savePath)
			documentURL = &url
		}

	default:
		c.Json(w, http.StatusBadRequest, "Unsupported Content-Type. Use JSON or multipart/form-data", nil)
		return
	}

	// --- Authenticate user ---
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// --- Fetch the team report ---
	var report models.WeeklyReport
	if err := c.DB.Preload("ProgressItems").Preload("TeamInfo.UserTeams").First(&report, reportID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Team report not found", nil)
		return
	}

	// --- Determine user role ---
	userTeamRole := ""
	if report.TeamInfo != nil {
		for _, ut := range report.TeamInfo.UserTeams {
			if ut.UserRefID == user.UserID {
				userTeamRole = ut.Role
				break
			}
		}
	}
	isLeader := userTeamRole == string(models.TeamLeader)

	// --- Update report fields ---
	if input.WorkDone != nil {
		report.WorkDone = *input.WorkDone
	}
	if input.PlannedWork != nil {
		report.PlannedWork = *input.PlannedWork
	}
	if input.NextWeek != nil {
		report.NextWeek = *input.NextWeek
	}
	if input.Challenges != nil {
		report.Challenges = *input.Challenges
	}
	if documentURL != nil {
		report.DocumentURL = documentURL
	}
	report.UpdatedAt = time.Now()

	// --- Filter progress items for update ---
	var itemsToUpdate []models.ProgressItem
	if len(input.ProgressItemIDs) > 0 {
		if isLeader {
			c.DB.Where("id IN ?", input.ProgressItemIDs).Find(&itemsToUpdate)
		} else {
			c.DB.Where("id IN ? AND created_by_id = ?", input.ProgressItemIDs, user.UserID).Find(&itemsToUpdate)
		}
	}

	// --- Update individual items ---
	for _, item := range itemsToUpdate {
		item.StudentStatus = "Completed"
		item.Performance = calculateItemPerformance(item.StudentStatus, item.VerifiedStatus)
		c.DB.Save(&item)

		if item.EntityID != nil {
			c.RecalculateEntityPerformance(*item.EntityID)
		}
	}

	// --- Save report ---
	if err := c.DB.Save(&report).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update team report", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Notify supervisors ---
	var supervisors []models.User
	c.DB.Joins("JOIN cohort_supervisors cs ON cs.supervisor_id = users.user_id").
		Where("cs.cohort_id = ?", report.ReportCohortID).
		Find(&supervisors)

	for _, sup := range supervisors {
		c.NotifyAndTrack(
			sup.UserID,
			"Weekly Report Updated",
			fmt.Sprintf("Weekly report from %s has been updated.", user.Username),
			"Weekly Report Review",
			"WeeklyReport",
			&report.ID,
			report.Status,
			false,
		)
	}

	// --- Notify team members if leader ---
	if isLeader && report.TeamInfo != nil {
		for _, ut := range report.TeamInfo.UserTeams {
			if ut.UserRefID != user.UserID {
				c.NotifyAndTrack(
					ut.UserRefID,
					"Team Weekly Report Updated",
					fmt.Sprintf("Leader %s has updated the team weekly report. Submit your items if pending.", user.Username),
					"TeamWeeklyReport",
					"WeeklyReport",
					&report.ID,
					report.Status,
					false,
				)
			}
		}
	}

	// --- Build response ---
	progressItemsResp := []map[string]interface{}{}
	for _, item := range report.ProgressItems {
		studentName := ""
		studentID := uint64(0)
		if item.CreatedBy != nil {
			studentID = item.CreatedBy.UserID
			studentName = item.CreatedBy.Profile.FirstName + " " + item.CreatedBy.Profile.LastName
		}

		entity := map[string]interface{}{}
		if item.Entity != nil {
			entity = map[string]interface{}{
				"id":          item.Entity.ID,
				"entity_name": item.Entity.EntityName,
				"entity_type": item.Entity.EntityType,
			}
		}

		progressItemsResp = append(progressItemsResp, map[string]interface{}{
			"id":              item.ID,
			"phase_name":      item.PhaseName,
			"progress_type":   item.ProgressType,
			"student_id":      studentID,
			"student_name":    studentName,
			"verified_status": item.VerifiedStatus,
			"weight":          item.Weight,
			"performance":     item.Performance,
			"entity":          entity,
		})
	}

	resp := map[string]interface{}{
		"id":             report.ID,
		"team_id":        report.TeamInfoID,
		"team_name":      report.TeamInfo.Name,
		"status":         report.Status,
		"week_start":     report.WeekStart,
		"week_end":       report.WeekEnd,
		"document_url":   report.DocumentURL,
		"progress_items": progressItemsResp,
		"updated_by": map[string]interface{}{
			"id":         user.UserID,
			"username":   user.Username,
			"first_name": user.Profile.FirstName,
			"last_name":  user.Profile.LastName,
		},
		"created_at": report.CreatedAt,
		"updated_at": report.UpdatedAt,
	}

	c.Json(w, http.StatusOK, "Team weekly report updated successfully", map[string]interface{}{"data": resp})
}

//======================================== GET Reports for Teams =====================================

func (c *Construct) GetWeeklyReportsByTeam(w http.ResponseWriter, r *http.Request) {
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

	reportStatus := r.URL.Query().Get("status") // filter by status
	var weekStart, weekEnd time.Time
	if ws := r.URL.Query().Get("week_start"); ws != "" { //filter by week start
		weekStart, _ = time.Parse("2006-01-02", ws)
	}
	if we := r.URL.Query().Get("week_end"); we != "" { // filter by week end
		weekEnd, _ = time.Parse("2006-01-02", we)
	}

	var reports []models.WeeklyReport
	query := c.DB.
		Preload("Student.Profile").
		Preload("Student.Role").
		Preload("ReviewedBy.Profile").
		Preload("ReportCohortInfo").
		Preload("ProgressItems.Entity").
		Preload("Comments.EditedBy.Profile").
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

		// Students assigned to this mentor OR in teams containing assigned students
		var teamIDs []uint64
		c.DB.Model(&models.UserTeam{}).Where("user_user_id IN ?", assignedStudentIDs).Pluck("team_team_id", &teamIDs)
		var teamMemberIDs []uint64
		if len(teamIDs) > 0 {
			c.DB.Model(&models.UserTeam{}).Where("team_team_id IN ?", teamIDs).Pluck("user_user_id", &teamMemberIDs)
		}
		allStudentIDs := append(assignedStudentIDs, teamMemberIDs...)
		query = query.Where("student_id IN ?", allStudentIDs)

	case "supervisor":
		// Find teams where user is Supervisor
		var supervisedTeamIDs []uint64
		c.DB.Model(&models.UserTeam{}).Where("user_user_id = ? AND role = ?", user.UserID, "Supervisor").Pluck("team_team_id", &supervisedTeamIDs)

		if len(supervisedTeamIDs) == 0 {
			c.Json(w, http.StatusOK, "No supervised teams found", map[string]interface{}{"data": []interface{}{}})
			return
		}

		// Get all students in these teams
		var supervisedStudentIDs []uint64
		c.DB.Model(&models.UserTeam{}).Where("team_team_id IN ?", supervisedTeamIDs).Pluck("user_user_id", &supervisedStudentIDs)

		if len(supervisedStudentIDs) == 0 {
			c.Json(w, http.StatusOK, "No students found in supervised teams", map[string]interface{}{"data": []interface{}{}})
			return
		}

		query = query.Where("student_id IN ?", supervisedStudentIDs)

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

	var total int64
	query.Model(&models.WeeklyReport{}).Count(&total)
	if err := query.Limit(limit).Offset(offset).Find(&reports).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch reports", map[string]interface{}{"error": err.Error()})
		return
	}

	// Build response (reuse same logic as before)
	resp := buildWeeklyReportResponse(reports)

	// Track action
	c.NotifyAndTrack(
		user.UserID,
		"Viewed Weekly Reports",
		fmt.Sprintf("%s viewed weekly reports list", user.Username),
		"Read",
		"WeeklyReport",
		nil,
		"Viewed",
		false,
	)

	c.Json(w, http.StatusOK, "Weekly reports fetched successfully", map[string]interface{}{
		"page":  page,
		"limit": limit,
		"total": total,
		"data":  resp,
	})
}

// response builder
func buildWeeklyReportResponse(reports []models.WeeklyReport) []map[string]interface{} {
	resp := make([]map[string]interface{}, 0, len(reports))

	for _, report := range reports {
		// Build progress items response
		itemsResp := make([]map[string]interface{}, 0, len(report.ProgressItems))
		for _, item := range report.ProgressItems {
			itemsResp = append(itemsResp, map[string]interface{}{
				"id":              item.ID,
				"phase_name":      item.PhaseName,
				"student_status":  item.StudentStatus,
				"verified_status": item.VerifiedStatus,
				"weight":          item.Weight,
				"performance":     item.Performance,
				"progress_type":   item.ProgressType,
				"entity": map[string]interface{}{
					"id":          item.EntityID,
					"entity_name": item.Entity.EntityName,
					"entity_type": item.Entity.EntityType,
				},
			})
		}

		// Build comments response
		commentsResp := make([]map[string]interface{}, 0, len(report.Comments))
		for _, cmt := range report.Comments {
			commenter := map[string]interface{}{}
			if cmt.EditedBy != nil && cmt.EditedBy.UserID != 0 && cmt.EditedBy.Profile.ProfileID != 0 {
				commenter = map[string]interface{}{
					"id":         cmt.EditedBy.UserID,
					"username":   cmt.EditedBy.Username,
					"first_name": cmt.EditedBy.Profile.FirstName,
					"last_name":  cmt.EditedBy.Profile.LastName,
					"email":      cmt.EditedBy.Email,
				}
			}
			commentsResp = append(commentsResp, map[string]interface{}{
				"id":         cmt.ID,
				"comment":    cmt.Comment,
				"edited_by":  commenter,
				"created_at": cmt.CreatedAt,
				"updated_at": cmt.UpdatedAt,
			})
		}

		// Append final report map
		resp = append(resp, map[string]interface{}{
			"id":             report.ID,
			"student_id":     report.StudentID,
			"student_name":   report.Student.Profile.FullName(),
			"cohort_id":      report.ReportCohortID,
			"cohort_name":    report.ReportCohortInfo.Name,
			"week_start":     report.WeekStart,
			"week_end":       report.WeekEnd,
			"status":         report.Status,
			"document_url":   report.DocumentURL,
			"work_done":      report.WorkDone,
			"planned_work":   report.PlannedWork,
			"next_week":      report.NextWeek,
			"challenges":     report.Challenges,
			"progress_items": itemsResp,
			"comments":       commentsResp,
			"reviewed_by": func() map[string]interface{} {
				if report.ReviewedBy != nil && report.ReviewedBy.Profile.ProfileID != 0 {
					return map[string]interface{}{
						"id":         report.ReviewedBy.UserID,
						"username":   report.ReviewedBy.Username,
						"first_name": report.ReviewedBy.Profile.FirstName,
						"last_name":  report.ReviewedBy.Profile.LastName,
						"email":      report.ReviewedBy.Email,
					}
				}
				return nil
			}(),
			"created_at": report.CreatedAt,
			"updated_at": report.UpdatedAt,
		})
	}

	return resp
}

func (c *Construct) ApproveOrSendBackTeamReport(w http.ResponseWriter, r *http.Request) {
	// --- Parse team report ID ---
	vars := mux.Vars(r)
	idStr, ok := vars["id"]
	if !ok || idStr == "" {
		c.Json(w, http.StatusBadRequest, "Team report ID is required", nil)
		return
	}
	reportID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid report ID", nil)
		return
	}

	// --- Parse action ---
	var input struct {
		Action  string `json:"action"`            // "approve" or "sendback"
		Comment string `json:"comment,omitempty"` // optional
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON payload", nil)
		return
	}
	input.Action = strings.ToLower(input.Action)
	if input.Action != "approve" && input.Action != "sendback" {
		c.Json(w, http.StatusBadRequest, "Action must be 'approve' or 'sendback'", nil)
		return
	}

	// --- Authenticate supervisor ---
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	if strings.ToLower(user.Role.Name) != "supervisor" {
		c.Json(w, http.StatusForbidden, "Only supervisors can approve or send back team reports", nil)
		return
	}

	// --- Fetch team report with items and team ---
	var teamReport models.WeeklyReport
	if err := c.DB.
		Preload("ProgressItems").
		Preload("ProgressItems.CreatedBy.Profile").
		Preload("TeamInfo.UserTeams.UserRef.Profile").
		First(&teamReport, reportID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Team report not found", nil)
		return
	}

	// --- Check supervisor assignment ---
	supervisorAssigned := false
	if teamReport.TeamInfo != nil {
		for _, ut := range teamReport.TeamInfo.UserTeams {
			if ut.UserRef.UserID == user.UserID && strings.ToLower(ut.Role) == "supervisor" {
				supervisorAssigned = true
				break
			}
		}
	}
	if !supervisorAssigned {
		c.Json(w, http.StatusForbidden, "You are not the supervisor for this team", nil)
		return
	}

	now := time.Now()
	teamReport.ReviewedByID = &user.UserID
	teamReport.UpdatedAt = now

	// --- Update individual progress items ---
	for _, item := range teamReport.ProgressItems {
		if input.Action == "approve" {
			item.VerifiedStatus = "Verified"
			item.Performance = calculateItemPerformance(item.StudentStatus, item.VerifiedStatus)
			if err := c.DB.Save(&item).Error; err != nil {
				c.Json(w, http.StatusInternalServerError, "Failed to update progress item", map[string]interface{}{"error": err.Error()})
				return
			}

			if item.EntityID != nil {
				// Update entity performance considering all verified items (including team aggregate)
				if err := c.UpdateEntityWeightedPerformance(*item.EntityID); err != nil {
					log.Println("Warning: failed to update entity performance for item", item.ID, err)
				}
			}
		}
	}

	// --- Update team report status ---
	switch input.Action {
	case "approve":
		teamReport.Status = "Approved"
	case "sendback":
		teamReport.Status = "Send Back"
		if input.Comment != "" {
			comment := models.WeeklyReportComment{
				WeeklyReportRefID: &teamReport.ID,
				EditedByID:        &user.UserID,
				Comment:           input.Comment,
				CreatedAt:         now,
				UpdatedAt:         now,
			}
			if err := c.DB.Create(&comment).Error; err != nil {
				c.Json(w, http.StatusInternalServerError, "Failed to create comment", map[string]interface{}{"error": err.Error()})
				return
			}
		}

	}

	// --- Save report ---
	if err := c.DB.Save(&teamReport).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update team report", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Notify team members ---
	if teamReport.TeamInfo != nil {
		for _, ut := range teamReport.TeamInfo.UserTeams {
			memberID := ut.UserRef.UserID
			c.NotifyAndTrack(
				memberID,
				fmt.Sprintf("Team report has been %s", strings.ToLower(teamReport.Status)),
				fmt.Sprintf("Supervisor %s %s the team report for your team '%s'.", user.Username, strings.ToLower(teamReport.Status), teamReport.TeamInfo.Name),
				"Team Weekly Report",
				"TeamReport",
				&teamReport.ID,
				teamReport.Status,
				true,
			)
		}
	}

	// --- Build response ---
	progressItemsResp := make([]map[string]interface{}, 0, len(teamReport.ProgressItems))
	for _, item := range teamReport.ProgressItems {
		entity := map[string]interface{}{}
		if item.Entity != nil {
			entity = map[string]interface{}{
				"id":          item.Entity.ID,
				"entity_name": item.Entity.EntityName,
				"entity_type": item.Entity.EntityType,
			}
		}
		studentName := ""
		if item.CreatedBy != nil {
			studentName = item.CreatedBy.Profile.FirstName + " " + item.CreatedBy.Profile.LastName
		}

		progressItemsResp = append(progressItemsResp, map[string]interface{}{
			"id":              item.ID,
			"phase_name":      item.PhaseName,
			"progress_type":   item.ProgressType,
			"student_id":      item.CreatedByID,
			"student_name":    studentName,
			"verified_status": item.VerifiedStatus,
			"weight":          item.Weight,
			"performance":     item.Performance,
			"entity":          entity,
		})
	}

	resp := map[string]interface{}{
		"id":             teamReport.ID,
		"team_id":        teamReport.TeamInfoID,
		"team_name":      teamReport.TeamInfo.Name,
		"status":         teamReport.Status,
		"week_start":     teamReport.WeekStart,
		"week_end":       teamReport.WeekEnd,
		"progress_items": progressItemsResp,
		"reviewed_by": map[string]interface{}{
			"id":         user.UserID,
			"username":   user.Username,
			"first_name": user.Profile.FirstName,
			"last_name":  user.Profile.LastName,
		},
		"created_at": teamReport.CreatedAt,
		"updated_at": teamReport.UpdatedAt,
	}

	c.Json(w, http.StatusOK, fmt.Sprintf("Team report %s successfully", strings.ToLower(teamReport.Status)), map[string]interface{}{"data": resp})
}
