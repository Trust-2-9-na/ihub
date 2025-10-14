package controllers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"web/services/assets/models"
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

// creating reports

func (c *Construct) CreateWeeklyReport(w http.ResponseWriter, r *http.Request) {
	type inputStruct struct {
		CohortID        uint64    `json:"cohort_id"`
		WeekStart       time.Time `json:"week_start"`
		WeekEnd         time.Time `json:"week_end"`
		WorkDone        string    `json:"work_done"`
		PlannedWork     string    `json:"planned_work"`
		NextWeek        string    `json:"next_week"`
		Challenges      string    `json:"challenges"`
		ProgressItemIDs []uint64  `json:"progress_item_ids"`
	}

	var input inputStruct
	var documentURL *string

	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		// ------------------ JSON ------------------
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid JSON payload", map[string]interface{}{"error": err.Error()})
			return
		}
	} else if strings.HasPrefix(contentType, "multipart/form-data") {
		// ------------------ Multipart ------------------
		if err := r.ParseMultipartForm(10 << 20); err != nil { // 10 MB max
			c.Json(w, http.StatusBadRequest, "Failed to parse form data", map[string]interface{}{"error": err.Error()})
			return
		}

		// Parse fields
		input.CohortID, _ = strconv.ParseUint(r.FormValue("cohort_id"), 10, 64)
		input.WorkDone = r.FormValue("work_done")
		input.PlannedWork = r.FormValue("planned_work")
		input.NextWeek = r.FormValue("next_week")
		input.Challenges = r.FormValue("challenges")

		weekStart, err := time.Parse("2006-01-02", r.FormValue("week_start"))
		if err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid week_start format. Use YYYY-MM-DD.", nil)
			return
		}
		input.WeekStart = weekStart

		weekEnd, err := time.Parse("2006-01-02", r.FormValue("week_end"))
		if err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid week_end format. Use YYYY-MM-DD.", nil)
			return
		}
		input.WeekEnd = weekEnd

		// Parse progress item IDs (comma-separated)
		if ids := r.FormValue("progress_item_ids"); ids != "" {
			for _, idStr := range strings.Split(ids, ",") {
				if id, err := strconv.ParseUint(strings.TrimSpace(idStr), 10, 64); err == nil {
					input.ProgressItemIDs = append(input.ProgressItemIDs, id)
				}
			}
		}

		// Handle file upload
		file, handler, err := r.FormFile("document")
		if err == nil {
			defer file.Close()

			// Validate extension
			allowedExts := map[string]bool{".pdf": true, ".csv": true}
			ext := strings.ToLower(filepath.Ext(handler.Filename))
			if !allowedExts[ext] {
				c.Json(w, http.StatusBadRequest, "Invalid file type. Only PDF or CSV allowed.", nil)
				return
			}

			// Validate MIME type
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

			// Save file
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

	// ------------------ Authenticate ------------------
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// ------------------ Create Weekly Report ------------------
	report := models.WeeklyReport{
		CohortID:    &input.CohortID,
		StudentID:   user.UserID,
		WeekStart:   input.WeekStart,
		WeekEnd:     input.WeekEnd,
		WorkDone:    input.WorkDone,
		PlannedWork: input.PlannedWork,
		NextWeek:    input.NextWeek,
		Challenges:  input.Challenges,
		Status:      "Pending",
		DocumentURL: documentURL,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := c.DB.Create(&report).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create weekly report", map[string]interface{}{"error": err.Error()})
		return
	}

	// ------------------ Link Progress Items ------------------
	if len(input.ProgressItemIDs) > 0 {
		var items []models.ProgressItem
		c.DB.Where("id IN ?", input.ProgressItemIDs).Find(&items)
		if len(items) > 0 {
			c.DB.Model(&report).Association("ProgressItems").Append(items)
		}
	}

	// ------------------ Notify & Track ------------------
	c.NotifyAndTrack(
		user.UserID,
		"Weekly Report Submitted",
		fmt.Sprintf("Your weekly report for %s - %s has been submitted successfully.", input.WeekStart.Format("02 Jan"), input.WeekEnd.Format("02 Jan")),
		"Weekly Report Submission",
		"WeeklyReport",
		&report.ID,
		"Pending",
	)

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
			"Pending",
		)
	}

	// ------------------ Response ------------------
	c.Json(w, http.StatusCreated, "Weekly report submitted successfully", map[string]interface{}{
		"data": map[string]interface{}{
			"id":             report.ID,
			"student_id":     report.StudentID,
			"cohort_id":      report.CohortID,
			"status":         report.Status,
			"week_start":     report.WeekStart,
			"week_end":       report.WeekEnd,
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

func (c *Construct) UpdateWeeklyReport(w http.ResponseWriter, r *http.Request) {
	// 1️⃣ Parse report ID from query
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

	// 2️⃣ Detect content type and parse input
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
		// JSON request
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid JSON payload", nil)
			return
		}
	} else if strings.HasPrefix(contentType, "multipart/form-data") {
		// Multipart form (for file + JSON fields)
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			c.Json(w, http.StatusBadRequest, "Failed to parse form data", map[string]interface{}{"error": err.Error()})
			return
		}

		// Parse JSON from "data" field if provided
		if data := r.FormValue("data"); data != "" {
			if err := json.Unmarshal([]byte(data), &input); err != nil {
				c.Json(w, http.StatusBadRequest, "Invalid JSON in 'data' field", nil)
				return
			}
		}

		// Handle file upload (optional)
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

			// Save file
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
	if !c.UserHasCohortAccess(user.UserID, *report.CohortID) {
		c.Json(w, http.StatusForbidden, "You do not have access to this report", nil)
		return
	}

	updated := false

	// 6️⃣ Update student fields
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

		// Update progress items
		if len(input.ProgressItemIDs) > 0 {
			var items []models.ProgressItem
			c.DB.Where("id IN ?", input.ProgressItemIDs).Find(&items)
			c.DB.Model(&report).Association("ProgressItems").Replace(items)
			updated = true
		}

		// Update uploaded document
		if documentURL != nil {
			report.DocumentURL = documentURL
			updated = true
		}
	}

	// 7️⃣ Add comment (any cohort member)
	if input.Comment != nil && *input.Comment != "" {
		comment := models.WeeklyReportComment{
			ReportID:  report.ID,
			UserID:    user.UserID,
			Comment:   *input.Comment,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
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
			)
		}
	}

	// 8️⃣ Save updates
	if updated {
		report.UpdatedAt = time.Now()
		if err := c.DB.Save(&report).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to update report", map[string]interface{}{"error": err.Error()})
			return
		}
	}

	// 9️⃣ Fetch updated report for response
	if err := c.DB.Preload("ProgressItems").Preload("Comments.User.Profile").Preload("Student").First(&report, report.ID).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch updated report", map[string]interface{}{"error": err.Error()})
		return
	}

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
		ReportID:  report.ID,
		UserID:    user.UserID,
		Comment:   input.Comment,
		CreatedAt: time.Now(),
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

// Suopervisors to enforce taks item's status

func (c *Construct) EnforceProgressItems(w http.ResponseWriter, r *http.Request) {
	var input struct {
		WeeklyReportID uint64   `json:"weekly_report_id"`
		ItemIDs        []uint64 `json:"item_ids"`
		NewStatus      string   `json:"new_status"`
		Reason         string   `json:"reason"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if strings.ToLower(user.Role.Name) != "supervisor" && strings.ToLower(user.Role.Name) != "opsadmin" {
		c.Json(w, http.StatusForbidden, "Only supervisors or admins can enforce items", nil)
		return
	}

	var items []models.ProgressItem
	if err := c.DB.Where("id IN ?", input.ItemIDs).Find(&items).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch items", nil)
		return
	}

	if len(items) == 0 {
		c.Json(w, http.StatusNotFound, "No progress items found", nil)
		return
	}

	for _, item := range items {
		oldStatus := item.Status
		item.Status = input.NewStatus

		// Update DB
		c.DB.Save(&item)

		// Create Supervisor Enforcement record
		enforcement := models.SupervisorEnforcement{
			ProgressItemID: item.ID,
			ReportID:       input.WeeklyReportID, // weekly report that triggered the enforcement
			EnforcedByID:   user.UserID,          // updated field name
			OldStatus:      oldStatus,
			NewStatus:      input.NewStatus,
			Reason:         input.Reason,
			CreatedAt:      time.Now(),
		}

		c.DB.Create(&enforcement)

		// Recalculate entity weighted performance
		if item.EntityID != nil {
			c.UpdateEntityWeightedPerformance(*item.EntityID)
		} else {
			fmt.Println("Warning: item.EntityID is nil, skipping weighted performance update")
		}

		// Notify student
		if item.AssignedToID != nil {
			c.NotifyAndTrack(
				*item.AssignedToID,
				"Progress Item Status Enforced",
				fmt.Sprintf("Supervisor enforced status '%s' on '%s' (old: %s)", input.NewStatus, item.PhaseName, oldStatus),
				"Supervisor Enforcement",
				"ProgressItem",
				&item.ID,
				input.NewStatus,
			)
		}
	}

	c.Json(w, http.StatusOK, "Progress items enforced successfully", map[string]interface{}{
		"count": len(items),
	})
}

// get weekly reports

func (c *Construct) GetWeeklyReports(w http.ResponseWriter, r *http.Request) {
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var reports []models.WeeklyReport
	query := c.DB.
		Preload("Student.Profile").
		Preload("Student.Role").
		Preload("ReviewedBy.Profile").
		Preload("Cohort").
		Preload("LinkedItem.Entity").
		Order("created_at DESC")

	switch user.Role.Name {
	case "Student":
		// Students see all reports from their cohort(s)
		var cohortIDs []uint64
		c.DB.Table("cohort_users").
			Select("cohort_cohort_id").
			Where("user_user_id = ? AND role = ?", user.UserID, "Student").
			Scan(&cohortIDs)

		if len(cohortIDs) == 0 {
			c.Json(w, http.StatusOK, "No reports available", map[string]interface{}{"data": []models.WeeklyReport{}})
			return
		}
		query = query.Where("cohort_id IN ?", cohortIDs)

	case "Mentor", "Supervisor":
		// Mentors/Supervisors see reports from assigned cohorts
		cohortIDs := c.getAssignCohorts(user.UserID, user.Role.Name)
		if len(cohortIDs) == 0 {
			c.Json(w, http.StatusOK, "No assigned cohorts found", map[string]interface{}{"data": []models.WeeklyReport{}})
			return
		}
		query = query.Where("cohort_id IN ?", cohortIDs)

	case "OpsAdmin", "SystemAdmin":
		// Admins see all reports
	default:
		c.Json(w, http.StatusForbidden, "Unauthorized role", nil)
		return
	}

	if err := query.Find(&reports).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch reports", map[string]interface{}{"error": err.Error()})
		return
	}

	// 🔁 Manually hydrate comments with user info
	for i := range reports {
		var comments []models.WeeklyReportComment
		if err := c.DB.Where("report_id = ?", reports[i].ID).Order("created_at ASC").Find(&comments).Error; err == nil {
			for j := range comments {
				var commenter models.User
				if err := c.DB.Select("user_id, username, email").Preload("Profile").
					Where("user_id = ?", comments[j].UserID).First(&commenter).Error; err == nil {
					comments[j].User = &commenter
				}
			}
			reports[i].Comments = comments
		}
	}

	// 🔔 Track viewing action
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
		"data": reports,
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
