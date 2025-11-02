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

// getAssignCohorts returns cohort IDs assigned to a mentor, supervisor, or student
func (c *Construct) getAssignCohorts(userID uint64, roleName string) []uint64 {
	var cohortIDs []uint64

	switch roleName {
	case "Mentor":
		c.DB.Table("cohort_users").
			Select("cohort_cohort_id").
			Where("user_user_id = ? AND role = ? AND deleted_at IS NULL", userID, "Mentor").
			Scan(&cohortIDs)
	case "Supervisor":
		c.DB.Table("cohort_users").
			Select("cohort_cohort_id").
			Where("user_user_id = ? AND role = ? AND deleted_at IS NULL", userID, "Supervisor").
			Scan(&cohortIDs)
	case "Student":
		c.DB.Table("cohort_users").
			Select("cohort_cohort_id").
			Where("user_user_id = ? AND role = ? AND deleted_at IS NULL", userID, "Student").
			Scan(&cohortIDs)
	}

	return cohortIDs
}

// CreateWeeklyReport allows a student to submit a weekly report
func (c *Construct) CreateWeeklyReport(w http.ResponseWriter, r *http.Request) {
	type inputStruct struct {
		CohortID        uint64   `json:"cohort_id"`
		TeamID          *uint64  `json:"team_id,omitempty"`
		WeekStartStr    string   `json:"week_start"`
		WeekEndStr      string   `json:"week_end"`
		WorkDone        string   `json:"work_done"`
		PlannedWork     string   `json:"planned_work"`
		NextWeek        string   `json:"next_week"`
		Challenges      string   `json:"challenges"`
		ProgressItemIDs []uint64 `json:"progress_item_ids,omitempty"`
		DocumentURL     *string  `json:"document_url,omitempty"`
	}

	var input inputStruct
	var documentURL *string

	// --- Authenticate user FIRST (before processing file upload) ---
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// --- Parse request body ---
	contentType := r.Header.Get("Content-Type")

	switch {
	case strings.HasPrefix(contentType, "application/json"):
		// Parse JSON body
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid JSON payload", map[string]interface{}{"error": err.Error()})
			return
		}

		// Validate document URL if provided
		if input.DocumentURL != nil {
			ext := strings.ToLower(filepath.Ext(*input.DocumentURL))
			allowedExts := map[string]bool{".pdf": true, ".csv": true}
			if !allowedExts[ext] {
				c.Json(w, http.StatusBadRequest, "Invalid document URL. Only PDF or CSV allowed.", nil)
				return
			}
			documentURL = input.DocumentURL
		}

	case strings.HasPrefix(contentType, "multipart/form-data"):
		// Parse multipart form
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			c.Json(w, http.StatusBadRequest, "Failed to parse form data", map[string]interface{}{"error": err.Error()})
			return
		}

		// Parse form fields
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
		// Check Content-Type for debugging
		log.Printf("[DEBUG] Content-Type: %s, Form has file: %v\n", r.Header.Get("Content-Type"), r.MultipartForm != nil && r.MultipartForm.File != nil)
		if r.MultipartForm != nil && r.MultipartForm.File != nil {
			log.Printf("[DEBUG] Available file fields: %v\n", len(r.MultipartForm.File))
			for fieldName := range r.MultipartForm.File {
				log.Printf("[DEBUG] Found file field: %s\n", fieldName)
			}
		}

		file, handler, err := r.FormFile("document")
		if err != nil {
			// Log if file is expected but not found (optional file, so just log)
			log.Printf("[INFO] No document file provided or error getting file 'document': %v\n", err)
		} else {
			log.Printf("[INFO] File upload received: %s (size: %d bytes)\n", handler.Filename, handler.Size)
			defer file.Close()

			allowedExts := map[string]bool{".pdf": true, ".csv": true}
			ext := strings.ToLower(filepath.Ext(handler.Filename))
			if !allowedExts[ext] {
				c.Json(w, http.StatusBadRequest, "Invalid file type. Only PDF or CSV allowed.", nil)
				return
			}

			buff := make([]byte, 512)
			if _, err := file.Read(buff); err != nil {
				log.Printf("[ERROR] Failed to read uploaded file header: %v\n", err)
				c.Json(w, http.StatusInternalServerError, "Failed to read uploaded file", map[string]interface{}{"error": err.Error()})
				return
			}
			file.Seek(0, io.SeekStart)

			mimeType := http.DetectContentType(buff)
			if mimeType != "application/pdf" && mimeType != "text/csv" && mimeType != "application/vnd.ms-excel" {
				c.Json(w, http.StatusBadRequest, fmt.Sprintf("Invalid MIME type: %s. Only PDF or CSV allowed.", mimeType), nil)
				return
			}

			// Create upload directory with proper error handling
			dateDir := time.Now().Format("20060102")
			uploadDir := filepath.Join("uploads", "reports", dateDir)
			// Clean the path to handle Windows path issues
			uploadDir = filepath.Clean(uploadDir)

			// Ensure parent directories exist with proper permissions (0755)
			if err := os.MkdirAll(uploadDir, 0755); err != nil {
				log.Printf("[ERROR] Failed to create upload directory '%s': %v\n", uploadDir, err)
				c.Json(w, http.StatusInternalServerError, "Failed to create upload directory", map[string]interface{}{"error": err.Error()})
				return
			}

			// Verify directory was created and is writable
			dirInfo, err := os.Stat(uploadDir)
			if err != nil {
				log.Printf("[ERROR] Upload directory does not exist after creation: '%s': %v\n", uploadDir, err)
				c.Json(w, http.StatusInternalServerError, "Failed to create upload directory", map[string]interface{}{"error": "Directory creation failed"})
				return
			}
			if !dirInfo.IsDir() {
				log.Printf("[ERROR] Upload path exists but is not a directory: '%s'\n", uploadDir)
				c.Json(w, http.StatusInternalServerError, "Failed to create upload directory", map[string]interface{}{"error": "Path exists but is not a directory"})
				return
			}

			// Generate filename using authenticated user's ID
			filename := fmt.Sprintf("report_%d_%d%s", user.UserID, time.Now().Unix(), ext)
			filePath := filepath.Join(uploadDir, filename)
			filePath = filepath.Clean(filePath)

			// Create file with proper error handling
			dest, err := os.Create(filePath)
			if err != nil {
				log.Printf("[ERROR] Failed to create file '%s': %v\n", filePath, err)
				log.Printf("[ERROR] Working directory: %s\n", func() string {
					wd, _ := os.Getwd()
					return wd
				}())
				c.Json(w, http.StatusInternalServerError, "Failed to save uploaded file", map[string]interface{}{"error": err.Error()})
				return
			}
			defer dest.Close()

			// Copy file content with error handling
			if _, err := io.Copy(dest, file); err != nil {
				log.Printf("[ERROR] Failed to write file content to '%s': %v\n", filePath, err)
				// Clean up the file if write failed
				os.Remove(filePath)
				c.Json(w, http.StatusInternalServerError, "Failed to save uploaded file", map[string]interface{}{"error": err.Error()})
				return
			}

			// Use forward slashes for URL (works on all platforms)
			url := fmt.Sprintf("/%s", filepath.ToSlash(filePath))
			documentURL = &url
			log.Printf("[INFO] Successfully saved uploaded file: %s (URL: %s)\n", filePath, url)
		}

	default:
		c.Json(w, http.StatusBadRequest, "Unsupported Content-Type. Use application/json or multipart/form-data.", nil)
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

	// Log document URL status before creating report
	if documentURL != nil {
		log.Printf("[INFO] Creating report with document URL: %s\n", *documentURL)
	} else {
		log.Printf("[INFO] Creating report without document (documentURL is nil)\n")
	}

	// Create weekly report
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

	if err := c.DB.Create(&report).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create weekly report", map[string]interface{}{"error": err.Error()})
		return
	}

	// Link progress items
	if len(input.ProgressItemIDs) > 0 {
		var items []models.ProgressItem
		c.DB.Where("id IN ?", input.ProgressItemIDs).Find(&items)
		if len(items) > 0 {
			c.DB.Model(&report).Association("ProgressItems").Append(items)
		}
	}

	// Notify supervisors
	var supervisors []models.User
	c.DB.Joins("JOIN cohort_users cs ON cs.user_user_id = users.user_id").
		Where("cs.cohort_cohort_id = ? AND cs.role = ? AND cs.deleted_at IS NULL", input.CohortID, "Supervisor").
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

	// Respond
	c.Json(w, http.StatusCreated, "Weekly report submitted successfully", map[string]interface{}{
		"data": map[string]interface{}{
			"id":             report.ID,
			"student_id":     report.StudentID,
			"cohort_id":      report.ReportCohortID,
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
		DocumentURL     *string  `json:"document_url"`
	}
	var documentURL *string

	contentType := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(contentType, "application/json"):
		// Parse JSON body
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid JSON payload", map[string]interface{}{"error": err.Error()})
			return
		}

		// Validate document URL if provided
		if input.DocumentURL != nil {
			urlLower := strings.ToLower(*input.DocumentURL)
			allowedExts := map[string]bool{".pdf": true, ".csv": true}
			ext := filepath.Ext(urlLower)
			if !allowedExts[ext] {
				c.Json(w, http.StatusBadRequest, "Invalid document URL. Only PDF or CSV allowed.", nil)
				return
			}
			documentURL = input.DocumentURL
		}

	case strings.HasPrefix(contentType, "multipart/form-data"):
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			c.Json(w, http.StatusBadRequest, "Failed to parse form data", map[string]interface{}{"error": err.Error()})
			return
		}

		// Parse optional "data" field as JSON
		if data := r.FormValue("data"); data != "" {
			if err := json.Unmarshal([]byte(data), &input); err != nil {
				c.Json(w, http.StatusBadRequest, "Invalid JSON in 'data' field", nil)
				return
			}
		}

		// Handle optional file upload
		file, fileHeader, err := r.FormFile("document")
		if err == nil {
			defer file.Close()

			ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
			allowedExts := map[string]bool{".pdf": true, ".csv": true}
			if !allowedExts[ext] {
				c.Json(w, http.StatusBadRequest, "Invalid file type: only PDF or CSV allowed", nil)
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

	default:
		c.Json(w, http.StatusBadRequest, "Unsupported Content-Type. Use JSON or multipart/form-data", nil)
		return
	}

	// 3️⃣ Authenticate user
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
	if report.ReportCohortID == nil || !c.UserHasCohortAccess(user.UserID, *report.ReportCohortID) {
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

	// 7️⃣ Add comment
	if input.Comment != nil && *input.Comment != "" {
		comment := models.WeeklyReportComment{
			WeeklyReportRefID: &report.ID,
			EditedByID:        &user.UserID,
			Comment:           *input.Comment,
			CreatedAt:         time.Now(),
			UpdatedAt:         time.Now(),
		}
		if err := c.DB.Create(&comment).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to add comment", map[string]interface{}{"error": err.Error()})
			return
		}
		updated = true

		if report.StudentID != user.UserID {
			c.NotifyAndTrack(
				report.StudentID,
				"New Comment on Weekly Report",
				fmt.Sprintf("%s commented on your weekly report: %s", user.Username, *input.Comment),
				"Comment",
				"WeeklyReport",
				&report.ID,
				"Commented",
				false,
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

	// 🔟 Respond
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
	if !c.UserHasCohortAccess(user.UserID, *report.ReportCohortID) {
		c.Json(w, http.StatusForbidden, "You do not have access to this report", nil)
		return
	}

	comment := models.WeeklyReportComment{
		WeeklyReportRefID: &report.ID,
		EditedByID:        &user.UserID,
		Comment:           input.Comment,
		CreatedAt:         time.Now(),
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
			false,
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

// PATCH /weekly-reports/approve?id=123
func (c *Construct) ApproveOrSendBackWeeklyReport(w http.ResponseWriter, r *http.Request) {
	// --- Parse report ID ---
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

	// --- Parse action ---
	var input struct {
		Action  string `json:"action"`            // "approve" or "sendback"
		Comment string `json:"comment,omitempty"` // optional for send back
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON payload", map[string]interface{}{"error": err.Error()})
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
		c.Json(w, http.StatusForbidden, "Only supervisors can approve or send back reports", nil)
		return
	}

	// --- Fetch report with progress items ---
	var report models.WeeklyReport
	if err := c.DB.Preload("ProgressItems").
		Preload("Student.Profile").
		Preload("ReportCohortInfo").
		First(&report, reportID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Report not found", nil)
		return
	}

	// --- Check supervisor access ---
	if !c.UserHasCohortAccess(user.UserID, *report.ReportCohortID) {
		c.Json(w, http.StatusForbidden, "You do not have access to this report", nil)
		return
	}

	now := time.Now()
	report.ReviewedByID = &user.UserID
	report.UpdatedAt = now

	// --- Update progress items based on action ---
	for _, item := range report.ProgressItems {
		switch input.Action {
		case "approve":
			item.VerifiedStatus = "Verified"
			item.Performance = calculateItemPerformance(item.StudentStatus, item.VerifiedStatus)
			if err := c.DB.Save(item).Error; err != nil {
				c.Json(w, http.StatusInternalServerError, "Failed to update progress item", map[string]interface{}{"error": err.Error()})
				return
			}

			// Recalculate entity performance for approved items
			if item.ProgressEntityRefID != nil {
				if err := c.UpdateEntityWeightedPerformance(*item.ProgressEntityRefID); err != nil {
					log.Println("Warning: failed to update entity performance for item", item.ID, err)
				}
			}

		case "sendback":
			// Do not change VerifiedStatus or Performance
		}
	}

	// --- Update report status ---
	switch input.Action {
	case "approve":
		report.Status = "Approved"
	case "sendback":
		report.Status = "Send Back"
		// Optional comment for send back
		if input.Comment != "" {
			comment := models.WeeklyReportComment{
				WeeklyReportRefID: &report.ID,
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
	if err := c.DB.Save(&report).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update report", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Notify student ---
	c.NotifyAndTrack(
		report.StudentID,
		fmt.Sprintf("Your weekly report has been %s", strings.ToLower(report.Status)),
		fmt.Sprintf("Supervisor %s %s your weekly report.", user.Username, strings.ToLower(report.Status)),
		"Weekly Report Review",
		"WeeklyReport",
		&report.ID,
		report.Status,
		true,
	)

	// --- Build progress items response ---
	progressItemsResp := make([]map[string]interface{}, 0, len(report.ProgressItems))
	for _, item := range report.ProgressItems {
		entity := map[string]interface{}{}
		if item.ProgressEntityRef != nil {
			entity = map[string]interface{}{
				"id":          item.ProgressEntityRef.ID,
				"entity_name": item.ProgressEntityRef.EntityName,
				"entity_type": item.ProgressEntityRef.EntityType,
			}
		}
		progressItemsResp = append(progressItemsResp, map[string]interface{}{
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

	// --- Build final response ---
	resp := map[string]interface{}{
		"id":             report.ID,
		"cohort_id":      report.ReportCohortID,
		"cohort_name":    report.ReportCohortInfo.Name,
		"student_id":     report.StudentID,
		"student_name":   studentName,
		"week_start":     report.WeekStart,
		"week_end":       report.WeekEnd,
		"status":         report.Status,
		"work_done":      report.WorkDone,
		"planned_work":   report.PlannedWork,
		"next_week":      report.NextWeek,
		"challenges":     report.Challenges,
		"progress_items": progressItemsResp,
		"reviewed_by": map[string]interface{}{
			"id":         user.UserID,
			"username":   user.Username,
			"first_name": user.Profile.FirstName,
			"last_name":  user.Profile.LastName,
		},
		"created_at": report.CreatedAt,
		"updated_at": report.UpdatedAt,
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
	includeArchived := r.URL.Query().Get("include_archived") == "true" // Optional: include archived reports
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
		Preload("ReportCohortInfo").
		Preload("ProgressItems").
		Preload("ProgressItems.ProgressEntityRef").
		Preload("Comments.EditedBy.Profile").
		// Only return individual reports (no team_id)
		Where("team_info_id IS NULL").
		Order("created_at DESC")

	// Filter out archived reports by default (unless explicitly requested)
	if !includeArchived {
		query = query.Where("is_archived = ?", false)
	}

	role := strings.ToLower(user.Role.Name)
	switch role {
	case "student":
		// Students see only their own individual reports
		query = query.Where("student_id = ?", user.UserID)

	case "mentor":
		// Fetch student IDs assigned to this mentor
		var assignedStudentIDs []uint64
		c.DB.Model(&models.MentorStudentAssignment{}).
			Where("mentor_ref_id = ? AND deleted_at IS NULL", user.UserID).
			Pluck("student_ref_id", &assignedStudentIDs)

		if len(assignedStudentIDs) == 0 {
			c.Json(w, http.StatusOK, "No assigned students found", map[string]interface{}{"data": []interface{}{}})
			return
		}

		// Filter individual reports (no team_id) for assigned students
		query = query.Where("student_id IN ?", assignedStudentIDs)

	case "supervisor":
		// Supervisors see individual reports (no team_id) for their assigned cohorts
		cohortIDs := c.getAssignedCohorts(user.UserID, "Supervisor")
		if len(cohortIDs) == 0 {
			c.Json(w, http.StatusOK, "No assigned cohorts found", map[string]interface{}{"data": []interface{}{}})
			return
		}
		query = query.Where("report_cohort_id IN ?", cohortIDs)

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
				"student_status":  item.StudentStatus,
				"verified_status": item.VerifiedStatus,
				"weight":          item.Weight,
				"performance":     item.Performance,
				"progress_type":   item.ProgressType,
				"entity": map[string]interface{}{
					"id":          item.ProgressEntityRefID,
					"entity_name": item.ProgressEntityRef.EntityName,
					"entity_type": item.ProgressEntityRef.EntityType,
				},
			})
		}

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

	// Check permissions upfront for archive/unarchive actions
	if input.Action == "archive" || input.Action == "unarchive" {
		if user.Role.Name != "Supervisor" && user.Role.Name != "OpsAdmin" && user.Role.Name != "SystemAdmin" {
			c.Json(w, http.StatusForbidden, "Only supervisors/admins can archive/unarchive reports", nil)
			return
		}
	}

	var updatedReports []models.WeeklyReport
	var processedReportIDs []uint64

	for _, report := range reports {
		switch input.Action {
		case "archive":
			report.IsArchived = true
			report.UpdatedAt = time.Now()
			if err := c.DB.Save(&report).Error; err != nil {
				c.Json(w, http.StatusInternalServerError, fmt.Sprintf("Failed to archive report #%d", report.ID), map[string]interface{}{"error": err.Error()})
				return
			}

			// Reload report to get latest state
			var updatedReport models.WeeklyReport
			if err := c.DB.Preload("Student.Profile").Preload("ReviewedBy.Profile").Preload("ReportCohortInfo").
				Where("id = ?", report.ID).First(&updatedReport).Error; err == nil {
				updatedReports = append(updatedReports, updatedReport)
				processedReportIDs = append(processedReportIDs, report.ID)
			}

			c.NotifyAndTrack(user.UserID, "Weekly Report Archived",
				fmt.Sprintf("Weekly report #%d was archived", report.ID),
				"Weekly Report Archive", "WeeklyReport", &report.ID, "Archived",
				false,
			)

		case "unarchive":
			report.IsArchived = false
			report.UpdatedAt = time.Now()
			if err := c.DB.Save(&report).Error; err != nil {
				c.Json(w, http.StatusInternalServerError, fmt.Sprintf("Failed to unarchive report #%d", report.ID), map[string]interface{}{"error": err.Error()})
				return
			}

			// Reload report to get latest state
			var updatedReport models.WeeklyReport
			if err := c.DB.Preload("Student.Profile").Preload("ReviewedBy.Profile").Preload("ReportCohortInfo").
				Where("id = ?", report.ID).First(&updatedReport).Error; err == nil {
				updatedReports = append(updatedReports, updatedReport)
				processedReportIDs = append(processedReportIDs, report.ID)
			}

			c.NotifyAndTrack(user.UserID, "Weekly Report Unarchived",
				fmt.Sprintf("Weekly report #%d was unarchived", report.ID),
				"Weekly Report Unarchive", "WeeklyReport", &report.ID, "Unarchived",
				false,
			)

		case "delete":
			if user.Role.Name == "Student" {
				if report.StudentID != user.UserID {
					c.Json(w, http.StatusForbidden, fmt.Sprintf("You can only delete your own reports. Report #%d belongs to another student", report.ID), nil)
					return
				}
				// Students "soft delete" = archive
				report.IsArchived = true
				report.UpdatedAt = time.Now()
				if err := c.DB.Save(&report).Error; err != nil {
					c.Json(w, http.StatusInternalServerError, fmt.Sprintf("Failed to archive report #%d", report.ID), map[string]interface{}{"error": err.Error()})
					return
				}

				// Reload archived report
				var updatedReport models.WeeklyReport
				if err := c.DB.Preload("Student.Profile").Preload("ReviewedBy.Profile").Preload("ReportCohortInfo").
					Where("id = ?", report.ID).First(&updatedReport).Error; err == nil {
					updatedReports = append(updatedReports, updatedReport)
					processedReportIDs = append(processedReportIDs, report.ID)
				}

				c.NotifyAndTrack(user.UserID, "Weekly Report Archived",
					fmt.Sprintf("You archived your weekly report #%d", report.ID),
					"Weekly Report Archive", "WeeklyReport", &report.ID, "Archived",
					false,
				)
			} else if user.Role.Name == "Supervisor" || user.Role.Name == "OpsAdmin" || user.Role.Name == "SystemAdmin" {
				// Store report ID before deletion
				reportID := report.ID

				// Delete related records first to avoid foreign key constraint violations
				// 1. Delete many-to-many relationship with progress items
				if err := c.DB.Exec("DELETE FROM weekly_report_progress_items WHERE weekly_report_id = ?", reportID).Error; err != nil {
					c.Json(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete progress items relationships for report #%d", reportID), map[string]interface{}{"error": err.Error()})
					return
				}

				// 2. Delete comments associated with this report
				if err := c.DB.Where("weekly_report_ref_id = ?", reportID).Delete(&models.WeeklyReportComment{}).Error; err != nil {
					c.Json(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete comments for report #%d", reportID), map[string]interface{}{"error": err.Error()})
					return
				}

				// 3. Now delete the report itself
				if err := c.DB.Unscoped().Delete(&report).Error; err != nil {
					c.Json(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete report #%d", reportID), map[string]interface{}{"error": err.Error()})
					return
				}
				processedReportIDs = append(processedReportIDs, reportID)

				c.NotifyAndTrack(user.UserID, "Weekly Report Deleted Permanently",
					fmt.Sprintf("Weekly report #%d was permanently deleted", reportID),
					"Weekly Report Deletion", "WeeklyReport", &reportID, "Deleted",
					true,
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

	// Build response with updated reports
	resp := make([]map[string]interface{}, 0, len(updatedReports))
	for _, report := range updatedReports {
		reportData := map[string]interface{}{
			"id":               report.ID,
			"report_cohort_id": report.ReportCohortID,
			"student_id":       report.StudentID,
			"week_start":       report.WeekStart,
			"week_end":         report.WeekEnd,
			"work_done":        report.WorkDone,
			"planned_work":     report.PlannedWork,
			"next_week":        report.NextWeek,
			"challenges":       report.Challenges,
			"status":           report.Status,
			"is_archived":      report.IsArchived,
			"created_at":       report.CreatedAt,
			"updated_at":       report.UpdatedAt,
		}
		if report.Student.Profile.ProfileID != 0 {
			reportData["student"] = map[string]interface{}{
				"user_id":    report.Student.UserID,
				"first_name": report.Student.Profile.FirstName,
				"last_name":  report.Student.Profile.LastName,
			}
		}
		resp = append(resp, reportData)
	}

	// For delete action, include deleted report IDs
	if input.Action == "delete" {
		c.Json(w, http.StatusOK, fmt.Sprintf("Action '%s' performed on selected reports successfully", input.Action), map[string]interface{}{
			"deleted_report_ids": processedReportIDs,
			"data":               resp,
		})
	} else {
		c.Json(w, http.StatusOK, fmt.Sprintf("Action '%s' performed on selected reports successfully", input.Action), map[string]interface{}{
			"data": resp,
		})
	}
}
