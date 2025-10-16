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

// getAssignCohorts returns cohort IDs assigned to a mentor or supervisor
func (c *Construct) getAssignCohorts(userID uint64, roleName string) []uint64 {
	var cohortIDs []uint64

	switch roleName {
	case "Mentor":
		c.DB.Table("cohort_mentors").
			Select("cohort_id").
			Where("mentor_id = ?", userID).
			Scan(&cohortIDs)
	case "Supervisor":
		c.DB.Table("cohort_supervisors").
			Select("cohort_id").
			Where("supervisor_id = ?", userID).
			Scan(&cohortIDs)
	}

	return cohortIDs
}

// CreateWeeklyReport allows a student to submit a weekly report

func (c *Construct) CreateWeeklyReport(w http.ResponseWriter, r *http.Request) {
	type inputStruct struct {
		CohortID        uint64   `json:"cohort_id"`
		WeekStartStr    string   `json:"week_start"` // date string YYYY-MM-DD
		WeekEndStr      string   `json:"week_end"`   // date string YYYY-MM-DD
		WorkDone        string   `json:"work_done"`
		PlannedWork     string   `json:"planned_work"`
		NextWeek        string   `json:"next_week"`
		Challenges      string   `json:"challenges"`
		ProgressItemIDs []uint64 `json:"progress_item_ids"`
	}

	var input inputStruct
	var documentURL *string

	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid JSON payload", map[string]interface{}{"error": err.Error()})
			return
		}
	} else if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			c.Json(w, http.StatusBadRequest, "Failed to parse form data", map[string]interface{}{"error": err.Error()})
			return
		}

		input.CohortID, _ = strconv.ParseUint(r.FormValue("cohort_id"), 10, 64)
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
	} else {
		c.Json(w, http.StatusBadRequest, "Unsupported Content-Type. Use application/json or multipart/form-data.", nil)
		return
	}

	// Authenticate
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// Parse week start/end dates
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

	// Create Weekly Report
	report := models.WeeklyReport{
		CohortID:    &input.CohortID,
		StudentID:   user.UserID,
		WeekStart:   weekStart,
		WeekEnd:     weekEnd,
		WorkDone:    input.WorkDone,
		PlannedWork: input.PlannedWork,
		NextWeek:    input.NextWeek,
		Challenges:  input.Challenges,
		Status:      "Pending Verification",
		DocumentURL: documentURL,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := c.DB.Create(&report).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create weekly report", map[string]interface{}{"error": err.Error()})
		return
	}

	// Link Progress Items
	if len(input.ProgressItemIDs) > 0 {
		var items []models.ProgressItem
		c.DB.Where("id IN ?", input.ProgressItemIDs).Find(&items)
		if len(items) > 0 {
			c.DB.Model(&report).Association("ProgressItems").Append(items)
		}
	}

	// Notify supervisors
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
		)
	}

	// Response
	c.Json(w, http.StatusCreated, "Weekly report submitted successfully", map[string]interface{}{
		"data": map[string]interface{}{
			"id":             report.ID,
			"student_id":     report.StudentID,
			"cohort_id":      report.CohortID,
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

// updating weekly reports

// UpdateWeeklyReport allows a student to update their weekly report or any cohort member to add comments
func (c *Construct) UpdateWeeklyReport(w http.ResponseWriter, r *http.Request) {
	// 1️⃣ Parse report ID
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		c.Json(w, http.StatusBadRequest, "Report ID is required", nil)
		return
	}
	reportID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid report ID", nil)
		return
	}

	// 2️⃣ Parse input
	var input struct {
		WorkDone        *string  `json:"work_done"`
		PlannedWork     *string  `json:"planned_work"`
		NextWeek        *string  `json:"next_week"`
		Challenges      *string  `json:"challenges"`
		ProgressItemIDs []uint64 `json:"progress_item_ids"`
		Comment         *string  `json:"comment"`
	}
	var documentURL *string

	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid JSON payload", map[string]interface{}{"error": err.Error()})
			return
		}
	} else if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			c.Json(w, http.StatusBadRequest, "Failed to parse form data", map[string]interface{}{"error": err.Error()})
			return
		}
		if data := r.FormValue("data"); data != "" {
			if err := json.Unmarshal([]byte(data), &input); err != nil {
				c.Json(w, http.StatusBadRequest, "Invalid JSON in 'data' field", nil)
				return
			}
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
			savePath := filepath.Join(uploadDir, fmt.Sprintf("report_%d_%d%s", reportID, time.Now().Unix(), ext))
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
	} else {
		c.Json(w, http.StatusBadRequest, "Unsupported Content-Type. Use JSON or multipart/form-data", nil)
		return
	}

	// 3️⃣ Authenticate
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// 4️⃣ Fetch report
	var report models.WeeklyReport
	if err := c.DB.Preload("ProgressItems").Preload("Comments").First(&report, reportID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Report not found", nil)
		return
	}

	// 5️⃣ Check cohort access
	if report.CohortID == nil || !c.UserHasCohortAccess(user.UserID, *report.CohortID) {
		c.Json(w, http.StatusForbidden, "You do not have access to this report", nil)
		return
	}

	updated := false

	// 6️⃣ Student updates
	if report.StudentID == user.UserID {
		if input.WorkDone != nil {
			report.WorkDone = *input.WorkDone
			updated = true
		}
		if input.PlannedWork != nil {
			report.PlannedWork = *input.PlannedWork
			updated = true
		}
		if input.NextWeek != nil {
			report.NextWeek = *input.NextWeek
			updated = true
		}
		if input.Challenges != nil {
			report.Challenges = *input.Challenges
			updated = true
		}
		if len(input.ProgressItemIDs) > 0 {
			var items []models.ProgressItem
			c.DB.Where("id IN ?", input.ProgressItemIDs).Find(&items)
			c.DB.Model(&report).Association("ProgressItems").Replace(items)
			updated = true
		}
		if documentURL != nil {
			report.DocumentURL = documentURL
			updated = true
		}
	}

	// 7️⃣ Add comment (any cohort member)
	if input.Comment != nil && *input.Comment != "" {
		comment := models.WeeklyReportComment{
			ReportID:   report.ID,
			EditedByID: &user.UserID,
			Comment:    *input.Comment,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		if err := c.DB.Create(&comment).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to add comment", map[string]interface{}{"error": err.Error()})
			return
		}
		updated = true

		// Notify student if commenter is not the owner
		if report.StudentID != user.UserID {
			c.NotifyAndTrack(
				report.StudentID,
				"New Comment on Weekly Report",
				fmt.Sprintf("%s commented on your weekly report: %s", user.Username, *input.Comment),
				"Comment",
				"WeeklyReport",
				&report.ID,
				"Commented",
			)
		}
	}

	// 8️⃣ Save report
	if updated {
		report.UpdatedAt = time.Now()
		if err := c.DB.Save(&report).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to update report", map[string]interface{}{"error": err.Error()})
			return
		}
	}

	// 9️⃣ Fetch updated report
	if err := c.DB.Preload("ProgressItems").Preload("Comments.User.Profile").Preload("Student").First(&report, report.ID).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch updated report", map[string]interface{}{"error": err.Error()})
		return
	}

	//  🔟 Respond
	c.Json(w, http.StatusOK, "Weekly report updated successfully", map[string]interface{}{
		"data": report,
	})
}

// adding comments on reports

func (c *Construct) AddOrReplyReportComment(w http.ResponseWriter, r *http.Request) {
	reportID, err := c.GetUintParam(r, "id")
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid report ID", nil)
		return
	}

	var input struct {
		Comment  string  `json:"comment"`
		ParentID *uint64 `json:"parent_id,omitempty"` // for reply threading
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Comment == "" {
		c.Json(w, http.StatusBadRequest, "Comment is required", nil)
		return
	}

	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// Fetch the report
	var report models.WeeklyReport
	if err := c.DB.First(&report, reportID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Report not found", nil)
		return
	}

	// Check cohort access
	if !c.UserHasCohortAccess(user.UserID, *report.CohortID) {
		c.Json(w, http.StatusForbidden, "You do not have access to this report", nil)
		return
	}

	comment := models.WeeklyReportComment{
		ReportID:   report.ID,
		EditedByID: &user.UserID,
		Comment:    input.Comment,
		CreatedAt:  time.Now(),
	}
	if err := c.DB.Create(&comment).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to add comment", map[string]interface{}{"error": err.Error()})
		return
	}

	// Notify the report owner if commenter is not the owner
	if report.StudentID != user.UserID {
		c.NotifyAndTrack(
			report.StudentID,
			"New Comment on Weekly Report",
			fmt.Sprintf("%s commented: %s", user.Username, input.Comment),
			"Weekly Report Comment",
			"WeeklyReport",
			&report.ID,
			"",
		)
	}

	c.Json(w, http.StatusOK, "Comment added successfully", map[string]interface{}{
		"comment": comment,
	})
}

// UserHasCohortAccess checks if a user belongs to a cohort

func (c *Construct) UserHasCohortAccess(userID, cohortID uint64) bool {
	var count int64
	c.DB.Model(&models.CohortUser{}).
		Where("user_user_id = ? AND cohort_cohort_id = ?", userID, cohortID).
		Count(&count)
	return count > 0
}

// Reports approval or rejection

// PATCH /weekly-reports/approve?id=123
func (c *Construct) ApproveOrRejectWeeklyReport(w http.ResponseWriter, r *http.Request) {
	// 1️⃣ Parse report ID from URL vars
	vars := mux.Vars(r)
	idStr, ok := vars["id"]
	if !ok || idStr == "" {
		c.Json(w, http.StatusBadRequest, "Report ID is required", nil)
		return
	}
	reportID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid report ID", nil)
		return
	}

	// 2️⃣ Parse action input
	var input struct {
		Action  string `json:"action"`            // "approve" or "reject"
		Comment string `json:"comment,omitempty"` // optional for rejection
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON payload", nil)
		return
	}
	input.Action = strings.ToLower(input.Action)
	if input.Action != "approve" && input.Action != "reject" {
		c.Json(w, http.StatusBadRequest, "Action must be 'approve' or 'reject'", nil)
		return
	}

	// 3️⃣ Authenticate supervisor
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	if strings.ToLower(user.Role.Name) != "supervisor" {
		c.Json(w, http.StatusForbidden, "Only supervisors can approve or reject reports", nil)
		return
	}

	// 4️⃣ Fetch report with student, cohort, and progress items
	var report models.WeeklyReport
	if err := c.DB.Preload("ProgressItems").
		Preload("Student.Profile").
		Preload("Cohort").
		First(&report, reportID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Report not found", nil)
		return
	}

	// 5️⃣ Check supervisor access to cohort
	if !c.UserHasCohortAccess(user.UserID, *report.CohortID) {
		c.Json(w, http.StatusForbidden, "You do not have access to this report", nil)
		return
	}

	// 6️⃣ Update report status and progress items
	report.ReviewedByID = &user.UserID
	now := time.Now()
	report.UpdatedAt = now

	// Update VerifiedStatus and recalc Performance
	for _, item := range report.ProgressItems {
		if item == nil {
			continue
		}

		// Update VerifiedStatus and recalc Performance
		// --- Update report status and progress items ---
		now := time.Now()
		report.UpdatedAt = now
		report.ReviewedByID = &user.UserID

		for _, item := range report.ProgressItems {
			if item == nil {
				continue
			}

			switch input.Action {
			case "approve":
				item.VerifiedStatus = "Verified"

				// Recalculate item performance based on student status
				item.Performance = calculateItemPerformance(item.StudentStatus, item.VerifiedStatus)

			case "reject":
				item.VerifiedStatus = "Pending Verification"
				item.Performance = 0
			}

			if err := c.DB.Save(item).Error; err != nil {
				c.Json(w, http.StatusInternalServerError, "Failed to update progress item", map[string]interface{}{"error": err.Error()})
				return
			}

			// Normalize weights and recalc entity performance
			if err := c.UpdateEntityWeightedPerformance(*item.EntityID); err != nil {
				log.Println("Warning: failed to update entity performance for item", item.ID, err)
			}
		}

		// Finally, update report status
		report.Status = strings.Title(input.Action)
		if err := c.DB.Save(&report).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to update report", map[string]interface{}{"error": err.Error()})
			return
		}

	}

	switch input.Action {
	case "approve":
		report.Status = "Approved"
	case "reject":
		report.Status = "Rejected"

		// Add optional rejection comment
		if input.Comment != "" {
			comment := models.WeeklyReportComment{
				ReportID:   report.ID,
				EditedByID: &user.UserID,
				Comment:    input.Comment,
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			if err := c.DB.Create(&comment).Error; err != nil {
				c.Json(w, http.StatusInternalServerError, "Failed to create comment", map[string]interface{}{"error": err.Error()})
				return
			}
		}
	}

	// Save report
	if err := c.DB.Save(&report).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update report", map[string]interface{}{"error": err.Error()})
		return
	}

	// 7️⃣ Notify student
	c.NotifyAndTrack(
		report.StudentID,
		fmt.Sprintf("Your weekly report has been %s", strings.ToLower(report.Status)),
		fmt.Sprintf("Supervisor %s %s your weekly report.", user.Username, strings.ToLower(report.Status)),
		"Weekly Report Review",
		"WeeklyReport",
		&report.ID,
		report.Status,
	)

	// 8️⃣ Build response
	progressItems := make([]map[string]interface{}, 0, len(report.ProgressItems))
	for _, item := range report.ProgressItems {
		entity := map[string]interface{}{}
		if item.Entity != nil {
			entity = map[string]interface{}{
				"id":          item.Entity.ID,
				"entity_name": item.Entity.EntityName,
				"entity_type": item.Entity.EntityType,
			}
		}
		progressItems = append(progressItems, map[string]interface{}{
			"id":              item.ID,
			"phase_name":      item.PhaseName,
			"progress_type":   item.ProgressType,
			"student_status":  item.StudentStatus,
			"verified_status": item.VerifiedStatus,
			"weight":          item.Weight,
			"performance":     item.Performance,
			"entity":          entity,
		})
	}

	studentName := ""
	if report.Student.Profile.ProfileID != 0 {
		studentName = report.Student.Profile.FirstName + " " + report.Student.Profile.LastName
	}

	reviewedBy := map[string]interface{}{}
	if report.ReviewedByID != nil && report.ReviewedBy != nil {
		reviewedBy = map[string]interface{}{
			"id":         report.ReviewedBy.UserID,
			"username":   report.ReviewedBy.Username,
			"first_name": report.ReviewedBy.Profile.FirstName,
			"last_name":  report.ReviewedBy.Profile.LastName,
		}
	} else {
		reviewedBy = nil
	}

	resp := map[string]interface{}{
		"id":             report.ID,
		"cohort_id":      report.CohortID,
		"cohort_name":    report.Cohort.Name,
		"student_id":     report.StudentID,
		"student_name":   studentName,
		"week_start":     report.WeekStart,
		"week_end":       report.WeekEnd,
		"status":         report.Status,
		"work_done":      report.WorkDone,
		"planned_work":   report.PlannedWork,
		"next_week":      report.NextWeek,
		"challenges":     report.Challenges,
		"progress_items": progressItems,
		"reviewed_by":    reviewedBy,
		"created_at":     report.CreatedAt,
		"updated_at":     report.UpdatedAt,
	}

	c.Json(w, http.StatusOK, fmt.Sprintf("Report %s successfully", strings.ToLower(report.Status)), map[string]interface{}{"data": resp})
}

// get weekly reports

func (c *Construct) GetWeeklyReports(w http.ResponseWriter, r *http.Request) {
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// --- Query Params ---
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

	reportStatus := r.URL.Query().Get("status")
	var weekStart, weekEnd time.Time
	if ws := r.URL.Query().Get("week_start"); ws != "" {
		weekStart, _ = time.Parse("2006-01-02", ws)
	}
	if we := r.URL.Query().Get("week_end"); we != "" {
		weekEnd, _ = time.Parse("2006-01-02", we)
	}

	var reports []models.WeeklyReport
	query := c.DB.
		Preload("Student.Profile").
		Preload("Student.Role").
		Preload("ReviewedBy.Profile").
		Preload("Cohort").
		Preload("ProgressItems.Entity").
		Preload("Comments.EditedBy.Profile").
		Order("created_at DESC")

	role := strings.ToLower(user.Role.Name)
	switch role {
	case "student":
		query = query.Where("student_id = ?", user.UserID)

	case "supervisor", "mentor":
		cohortIDs := c.getAssignedCohorts(user.UserID, strings.Title(role))
		if len(cohortIDs) == 0 {
			c.Json(w, http.StatusOK, "No assigned cohorts found", map[string]interface{}{"data": []interface{}{}})
			return
		}
		query = query.Where("cohort_id IN ?", cohortIDs)

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

	// Fetch total count for pagination
	var total int64
	query.Model(&models.WeeklyReport{}).Count(&total)

	if err := query.Limit(limit).Offset(offset).Find(&reports).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch reports", map[string]interface{}{"error": err.Error()})
		return
	}

	// Build response
	resp := make([]map[string]interface{}, 0, len(reports))
	for _, report := range reports {
		itemsResp := make([]map[string]interface{}, 0, len(report.ProgressItems))
		for _, item := range report.ProgressItems {
			itemsResp = append(itemsResp, map[string]interface{}{
				"id":              item.ID,
				"phase_name":      item.PhaseName,
				"student_status":  item.StudentStatus,  // ← replace 'status'
				"verified_status": item.VerifiedStatus, // supervisor verification
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
		// add itemsResp to report map here
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

		resp = append(resp, map[string]interface{}{
			"id":             report.ID,
			"student_id":     report.StudentID,
			"student_name":   report.Student.Profile.FullName(),
			"cohort_id":      report.CohortID,
			"cohort_name":    report.Cohort.Name,
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

	// Track action
	c.NotifyAndTrack(
		user.UserID,
		"Viewed Weekly Reports",
		fmt.Sprintf("%s viewed weekly reports list", user.Username),
		"Read",
		"WeeklyReport",
		nil,
		"Viewed",
	)

	c.Json(w, http.StatusOK, "Weekly reports fetched successfully", map[string]interface{}{
		"page":  page,
		"limit": limit,
		"total": total,
		"data":  resp,
	})
}

// deleting, archiving, or unarchiving reports

func (c *Construct) ManageWeeklyReports(w http.ResponseWriter, r *http.Request) {
	// Parse input
	var input struct {
		ReportIDs []uint64 `json:"report_ids"`
		Action    string   `json:"action"` // "archive", "unarchive", "delete"
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if len(input.ReportIDs) == 0 || input.Action == "" {
		c.Json(w, http.StatusBadRequest, "report_ids and action are required", nil)
		return
	}

	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var reports []models.WeeklyReport
	if err := c.DB.Where("id IN ?", input.ReportIDs).Find(&reports).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch reports", map[string]interface{}{"error": err.Error()})
		return
	}

	if len(reports) == 0 {
		c.Json(w, http.StatusNotFound, "No reports found for the given IDs", nil)
		return
	}

	for _, report := range reports {
		switch input.Action {
		case "archive":
			if user.Role.Name != "Supervisor" && user.Role.Name != "OpsAdmin" && user.Role.Name != "SystemAdmin" {
				c.Json(w, http.StatusForbidden, "Only supervisors/admins can archive reports", nil)
				return
			}
			report.IsArchived = true
			report.UpdatedAt = time.Now()
			c.DB.Save(&report)
			c.NotifyAndTrack(user.UserID, "Weekly Report Archived",
				fmt.Sprintf("Weekly report #%d was archived", report.ID),
				"Weekly Report Archive", "WeeklyReport", &report.ID, "Archived",
			)

		case "unarchive":
			if user.Role.Name != "Supervisor" && user.Role.Name != "OpsAdmin" && user.Role.Name != "SystemAdmin" {
				c.Json(w, http.StatusForbidden, "Only supervisors/admins can unarchive reports", nil)
				return
			}
			report.IsArchived = false
			report.UpdatedAt = time.Now()
			c.DB.Save(&report)
			c.NotifyAndTrack(user.UserID, "Weekly Report Unarchived",
				fmt.Sprintf("Weekly report #%d was unarchived", report.ID),
				"Weekly Report Unarchive", "WeeklyReport", &report.ID, "Unarchived",
			)

		case "delete":
			if user.Role.Name == "Student" {
				if report.StudentID != user.UserID {
					c.Json(w, http.StatusForbidden, "You can only delete your own reports", nil)
					return
				}
				// Students “soft delete” = archive
				report.IsArchived = true
				report.UpdatedAt = time.Now()
				c.DB.Save(&report)
				c.NotifyAndTrack(user.UserID, "Weekly Report Archived",
					fmt.Sprintf("You archived your weekly report #%d", report.ID),
					"Weekly Report Archive", "WeeklyReport", &report.ID, "Archived",
				)
			} else if user.Role.Name == "Supervisor" || user.Role.Name == "OpsAdmin" || user.Role.Name == "SystemAdmin" {
				c.DB.Unscoped().Delete(&report)
				c.NotifyAndTrack(user.UserID, "Weekly Report Deleted Permanently",
					fmt.Sprintf("Weekly report #%d was permanently deleted", report.ID),
					"Weekly Report Deletion", "WeeklyReport", &report.ID, "Deleted",
				)
			} else {
				c.Json(w, http.StatusForbidden, "You do not have permission to delete this report", nil)
				return
			}

		default:
			c.Json(w, http.StatusBadRequest, "Invalid action, must be 'archive', 'unarchive', or 'delete'", nil)
			return
		}
	}

	c.Json(w, http.StatusOK, fmt.Sprintf("Action '%s' performed on selected reports successfully", input.Action), nil)
}
