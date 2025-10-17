package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"web/services/assets/middlewares"
	"web/services/assets/models"

	"github.com/gorilla/mux"
)

// ---------------------------------
// COHORT APIS //
// -----------------------------------
// ------------------------------------
// ** Supervisor create Cohort API **
// ------------------------------------
func (c *Construct) CreateCohort(w http.ResponseWriter, r *http.Request) {
	// --- 1. Get authenticated user from context ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Profile").Preload("Role.Permissions").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 2. Parse request body ---
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		StartDate   string `json:"start_date"`
		EndDate     string `json:"end_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 3. Validate input ---
	if strings.TrimSpace(input.Name) == "" {
		c.Json(w, http.StatusBadRequest, "Cohort name is required", nil)
		return
	}

	// --- 4. Optional: Check if user has "manage_cohorts" permission ---
	hasPermission := false
	for _, perm := range user.Role.Permissions {
		if strings.EqualFold(perm.Name, "manage_cohorts") {
			hasPermission = true
			break
		}
	}
	if !hasPermission {
		c.Json(w, http.StatusForbidden, "Forbidden: missing required permission", nil)
		return
	}

	// --- 5. Create cohort ---
	cohort := models.Cohort{
		Name:        input.Name,
		Description: input.Description,
		StartDate:   input.StartDate,
		EndDate:     input.EndDate,
		CreatedBy:   user.UserUUID,
	}

	if err := c.DB.Create(&cohort).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create cohort", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 6. Notify & Track creation ---
	c.NotifyAndTrack(
		user.UserID,
		"Cohort Created",
		fmt.Sprintf("Cohort '%s' has been successfully created.", cohort.Name),
		"Cohort Creation",
		"Cohort",
		&cohort.CohortID,
		"Created",
		false,
	)

	// --- 7. Audit trail ---
	c.DB.Create(&models.SystemHistory{
		EntityType: "Cohort",
		EntityID:   &cohort.CohortID,
		Action:     "Created",
		Status:     func() *string { s := "Active"; return &s }(),
		Comment: func() *string {
			s := fmt.Sprintf("Cohort '%s' created by %s %s", input.Name, user.Profile.FirstName, user.Profile.LastName)
			return &s
		}(),
		ChangedByID: user.UserID,
		CreatedAt:   time.Now(),
	})

	// --- 8. Optional background job ---
	job, _ := c.StartJob("cohort_reminder_setup", map[string]interface{}{"cohort_id": cohort.CohortID})
	msg := "Reminder job scheduled"
	_ = c.EndJob(job, "Completed", &msg)

	// --- 9. Build response ---
	resp := map[string]interface{}{
		"cohort_id":   cohort.CohortID,
		"name":        cohort.Name,
		"description": cohort.Description,
		"start_date":  cohort.StartDate,
		"end_date":    cohort.EndDate,
		"created_by": map[string]string{
			"first_name": user.Profile.FirstName,
			"last_name":  user.Profile.LastName,
			"full_name":  user.Profile.FirstName + " " + user.Profile.LastName,
		},
		"created_at": cohort.CreatedAt,
	}

	c.Json(w, http.StatusCreated, "Cohort created successfully", map[string]interface{}{"cohort": resp})
}

// ------------------------------------------------
// ** all users View-list Cohort API **
func (c *Construct) GetCohorts(w http.ResponseWriter, r *http.Request) {
	// --- 1. Get authenticated user from context ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Role.Permissions").Preload("Profile").
		Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "User not found", nil)
		return
	}

	// --- 2. Pagination parameters ---
	page := 1
	pageSize := 10
	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 {
			pageSize = parsed
		}
	}
	offset := (page - 1) * pageSize

	// --- 3. Base query ---
	var cohorts []models.Cohort
	query := c.DB.Preload("Users.Profile").Preload("Users.Role").
		Preload("Creator.Profile").
		Order("created_at desc").
		Offset(offset).Limit(pageSize)

	// --- 4. Role-based access ---
	switch user.Role.Name {
	case "Supervisor", "Mentor", "Student":
		roleName := user.Role.Name
		query = query.Joins("JOIN cohort_users cu ON cu.cohort_cohort_id = cohorts.cohort_id").
			Where("cu.user_user_id = ? AND cu.role = ?", user.UserID, roleName)
	case "OpsAdmin", "SystemAdmin":
		// Admins can see all cohorts
	default:
		c.Json(w, http.StatusForbidden, "Role not allowed", nil)
		return
	}

	// --- 5. Execute query ---
	if err := query.Find(&cohorts).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch cohorts", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 6. Build response ---
	var result []map[string]interface{}
	for _, cohort := range cohorts {
		supervisors := []map[string]interface{}{}
		mentors := []map[string]interface{}{}
		students := []map[string]interface{}{}

		for _, u := range cohort.Users {
			fullName := u.Profile.FirstName + " " + u.Profile.LastName
			userInfo := map[string]interface{}{
				"user_id":   u.UserID,
				"full_name": fullName,
				"role":      u.Role.Name,
			}
			switch u.Role.Name {
			case "Supervisor":
				supervisors = append(supervisors, userInfo)
			case "Mentor":
				mentors = append(mentors, userInfo)
			case "Student":
				students = append(students, userInfo)
			}
		}

		// --- Fetch approved proposals for students ---
		var proposals []models.Proposal
		studentIDs := []uint64{}
		for _, s := range students {
			if id, ok := s["user_id"].(uint64); ok {
				studentIDs = append(studentIDs, id)
			}
		}
		if len(studentIDs) > 0 {
			c.DB.Preload("SubmittedBy.Profile").
				Where("submitted_by_id IN ? AND status = ?", studentIDs, "Approved").
				Find(&proposals)
		}

		proposalsResp := []map[string]interface{}{}
		for _, p := range proposals {
			proposalsResp = append(proposalsResp, map[string]interface{}{
				"proposal_id":  p.ProposalID,
				"title":        p.Title,
				"abstract":     p.Abstract,
				"category":     p.Category,
				"subfield":     p.Subfield,
				"status":       p.Status,
				"submitted_by": p.SubmittedBy.Profile.FirstName + " " + p.SubmittedBy.Profile.LastName,
				"document_url": p.DocumentURL,
			})
		}

		result = append(result, map[string]interface{}{
			"cohort_id":   cohort.CohortID,
			"name":        cohort.Name,
			"description": cohort.Description,
			"start_date":  cohort.StartDate,
			"end_date":    cohort.EndDate,
			"created_by": map[string]string{
				"full_name": cohort.Creator.Profile.FirstName + " " + cohort.Creator.Profile.LastName,
			},
			"supervisors": supervisors,
			"mentors":     mentors,
			"students":    students,
			"proposals":   proposalsResp,
			"created_at":  cohort.CreatedAt,
			"user_role":   user.Role.Name,
		})
	}

	// --- 7. Track access ---
	c.DB.Create(&models.SystemHistory{
		EntityType:  "Cohort",
		EntityID:    nil,
		Action:      "View",
		Status:      ptrString("Accessed"),
		Comment:     ptrString(fmt.Sprintf("%s (%s) viewed cohort listings", user.Profile.FirstName+" "+user.Profile.LastName, user.Role.Name)),
		ChangedByID: user.UserID,
		CreatedAt:   time.Now(),
	})

	c.NotifyAndTrack(
		user.UserID,
		"Viewed Cohorts",
		fmt.Sprintf("%s viewed cohort listings", user.Profile.FirstName+" "+user.Profile.LastName),
		"Access Log",
		"Cohort",
		nil,
		"Viewed",
		false,
	)

	// --- 8. Send response with pagination info ---
	resp := map[string]interface{}{
		"page":         page,
		"page_size":    pageSize,
		"cohorts":      result,
		"cohort_count": len(result),
	}

	c.Json(w, http.StatusOK, "Cohorts retrieved successfully", resp)
}

func ptrString(s string) *string {
	return &s
}

// --------------------------------------------------------------------------
// ** Admin Delete Cohort API **
// --------------------------------------------------------------------------
func (c *Construct) DeleteCohorts(w http.ResponseWriter, r *http.Request) {
	// --- 1. Parse request body ---
	var body struct {
		CohortIDs []uint64 `json:"cohort_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}
	if len(body.CohortIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "No cohort IDs provided", nil)
		return
	}

	// --- 2. Get logged-in user from middleware context ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Role").Preload("Profile").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 3. Fetch cohorts ---
	var cohorts []models.Cohort
	if err := c.DB.Where("cohort_id IN ?", body.CohortIDs).Find(&cohorts).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch cohorts", map[string]interface{}{"error": err.Error()})
		return
	}
	if len(cohorts) == 0 {
		c.Json(w, http.StatusNotFound, "No cohorts found for provided IDs", nil)
		return
	}

	// --- 4. Role-based permission check ---
	var allowed []uint64
	for _, cohort := range cohorts {
		isCreator := cohort.CreatedBy == user.UserUUID
		isAdmin := strings.EqualFold(user.Role.Name, "OpsAdmin") || strings.EqualFold(user.Role.Name, "SystemAdmin")

		if isCreator || isAdmin {
			allowed = append(allowed, cohort.CohortID)
		} else {
			// Track unauthorized attempt
			_ = c.LogAudit(user.UserID, "unauthorized_delete", func() *string { s := "cohort"; return &s }(), &cohort.CohortID, nil, map[string]interface{}{
				"name": cohort.Name,
			})
			c.DB.Create(&models.SystemHistory{
				EntityType: "Cohort",
				EntityID:   &cohort.CohortID,
				Action:     "Delete Attempt",
				Status:     func() *string { s := "Denied"; return &s }(),
				Comment: func() *string {
					s := fmt.Sprintf("%s (%s) tried to delete cohort '%s' without permission", user.Profile.FirstName, user.Role.Name, cohort.Name)
					return &s
				}(),
				ChangedByID: user.UserID,
				CreatedAt:   time.Now(),
			})
		}
	}

	if len(allowed) == 0 {
		c.Json(w, http.StatusForbidden, "You do not have permission to delete any of the selected cohorts", nil)
		return
	}

	// --- 5. Delete cohorts ---
	if err := c.DB.Where("cohort_id IN ?", allowed).Delete(&models.Cohort{}).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to delete cohorts", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 6. Notifications, logs, and job cleanup ---
	for _, cohort := range cohorts {
		if !contains(allowed, cohort.CohortID) {
			continue
		}
		c.NotifyAndTrack(
			user.UserID,
			"Cohort Deleted",
			fmt.Sprintf("Cohort '%s' has been permanently deleted.", cohort.Name),
			"Delete",         // category / action type
			"Cohort",         // entity type
			&cohort.CohortID, // entity ID
			"Deleted",
			true, // status or outcome
		)

		c.DB.Create(&models.SystemHistory{
			EntityType: "Cohort",
			EntityID:   &cohort.CohortID,
			Action:     "Delete",
			Status:     func() *string { s := "Success"; return &s }(),
			Comment: func() *string {
				s := fmt.Sprintf("Cohort '%s' deleted by %s (%s)", cohort.Name, user.Profile.FirstName, user.Role.Name)
				return &s
			}(),
			ChangedByID: user.UserID,
			CreatedAt:   time.Now(),
		})

		job, _ := c.StartJob("cohort_delete_cleanup", map[string]interface{}{"cohort_id": cohort.CohortID})
		msg := "Cleanup job completed"
		_ = c.EndJob(job, "Completed", &msg)
	}

	c.Json(w, http.StatusOK, fmt.Sprintf("%d cohort(s) deleted successfully", len(allowed)), map[string]interface{}{"deleted_ids": allowed})
}

// helper
func contains(arr []uint64, id uint64) bool {
	for _, a := range arr {
		if a == id {
			return true
		}
	}
	return false
}

// ===============::::Cohorts Archiving:::=============
func (c *Construct) ArchiveCohort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortIDs []uint64 `json:"cohort_ids"`
		Action    string   `json:"action"` // optional
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.CohortIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "Invalid request: provide cohort_ids", nil)
		return
	}

	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Role").Preload("Profile").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// Fetch all cohorts
	var cohorts []models.Cohort
	if err := c.DB.Where("cohort_id IN ?", body.CohortIDs).Find(&cohorts).Error; err != nil || len(cohorts) == 0 {
		c.Json(w, http.StatusNotFound, "No cohorts found to archive", nil)
		return
	}

	for _, cohort := range cohorts {
		// Only OpsAdmin or SystemAdmin can archive
		if !(strings.EqualFold(user.Role.Name, "OpsAdmin") || strings.EqualFold(user.Role.Name, "SystemAdmin")) {
			continue
		}

		if err := c.DB.Model(&cohort).Update("is_archived", true).Error; err != nil {
			continue
		}

		c.NotifyAndTrack(
			user.UserID,
			"Cohort Archived",
			fmt.Sprintf("Cohort '%s' has been archived.", cohort.Name),
			"Archive",        // category / action type
			"Cohort",         // entity type
			&cohort.CohortID, // entity ID
			"Archived",
			true, // status or outcome
		)

	}

	c.Json(w, http.StatusOK, "Cohorts archived successfully", map[string]interface{}{"cohort_ids": body.CohortIDs})
}

// =============~~~~Cohort Restoring~~~~==============
func (c *Construct) RestoreCohort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortIDs []uint64 `json:"cohort_ids"`
		Action    string   `json:"action"` // optional
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.CohortIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "Invalid request: provide cohort_ids", nil)
		return
	}

	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Role").Preload("Profile").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// Fetch all cohorts
	var cohorts []models.Cohort
	if err := c.DB.Where("cohort_id IN ?", body.CohortIDs).Find(&cohorts).Error; err != nil || len(cohorts) == 0 {
		c.Json(w, http.StatusNotFound, "No cohorts found to restore", nil)
		return
	}

	for _, cohort := range cohorts {
		if err := c.DB.Model(&cohort).Update("is_archived", false).Error; err != nil {
			continue
		}

		c.NotifyAndTrack(
			user.UserID,
			"Cohort Restored",
			fmt.Sprintf("Cohort '%s' has been restored.", cohort.Name),
			"Restore",        // category / action type
			"Cohort",         // entity type
			&cohort.CohortID, // entity ID
			"Restored",
			true, // status or outcome
		)

	}

	c.Json(w, http.StatusOK, "Cohorts restored successfully", map[string]interface{}{"cohort_ids": body.CohortIDs})
}

// ================X student removal from cohorts X==============
func (c *Construct) RemoveStudentFromCohort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortID  uint64 `json:"cohort_id"`
		StudentID uint64 `json:"student_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}
	if body.CohortID == 0 || body.StudentID == 0 {
		c.Json(w, http.StatusBadRequest, "Cohort ID and Student ID are required", nil)
		return
	}

	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var actingUser models.User
	if err := c.DB.Preload("Role").Preload("Profile").Where("user_uuid = ?", userUUID).First(&actingUser).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "User not found", nil)
		return
	}

	var cohort models.Cohort
	if err := c.DB.First(&cohort, "cohort_id = ?", body.CohortID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Cohort not found", nil)
		return
	}

	// --- Permission check ---
	isAdmin := strings.EqualFold(actingUser.Role.Name, "OpsAdmin") || strings.EqualFold(actingUser.Role.Name, "SystemAdmin")
	isCreator := cohort.CreatedBy == actingUser.UserUUID

	var cohortUser struct{ Role string }
	_ = c.DB.Raw(`SELECT role FROM cohort_users WHERE cohort_cohort_id = ? AND user_user_id = ?`, body.CohortID, actingUser.UserID).Scan(&cohortUser)
	isSupervisor := strings.EqualFold(cohortUser.Role, "Supervisor")

	if !(isAdmin || isCreator || isSupervisor) {
		c.Json(w, http.StatusForbidden, "You do not have permission to remove students from this cohort", nil)
		return
	}

	// Verify student exists in cohort
	var studentRecord struct{ Role string }
	if err := c.DB.Raw(`SELECT role FROM cohort_users WHERE cohort_cohort_id = ? AND user_user_id = ?`, body.CohortID, body.StudentID).Scan(&studentRecord).Error; err != nil || !strings.EqualFold(studentRecord.Role, "Student") {
		c.Json(w, http.StatusNotFound, "Student not found in this cohort", nil)
		return
	}

	// Remove student
	if err := c.DB.Exec(`DELETE FROM cohort_users WHERE cohort_cohort_id = ? AND user_user_id = ?`, body.CohortID, body.StudentID).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to remove student", map[string]interface{}{"error": err.Error()})
		return
	}

	// Fetch student info
	var student models.User
	c.DB.Preload("Profile").First(&student, body.StudentID)

	// Log removal
	_ = c.LogAudit(actingUser.UserID, "remove_student", ptrString("cohort_user"), &body.CohortID, nil, map[string]interface{}{
		"removed_student_id":   body.StudentID,
		"removed_student_name": student.Profile.FirstName + " " + student.Profile.LastName,
	})

	c.DB.Create(&models.SystemHistory{
		EntityType: "CohortUser",
		EntityID:   &body.CohortID,
		Action:     "Remove Student",
		Status:     ptrString("Success"),
		Comment: ptrString(fmt.Sprintf("%s (%s) removed %s (%s) from cohort '%s'",
			actingUser.Profile.FirstName, actingUser.Role.Name,
			student.Profile.FirstName, student.Profile.LastName,
			cohort.Name)),
		ChangedByID: actingUser.UserID,
		CreatedAt:   time.Now(),
	})

	// Notify student
	c.NotifyAndTrack(
		student.UserID,
		"Removed from Cohort",
		fmt.Sprintf("You have been removed from the cohort '%s' by %s (%s).",
			cohort.Name, actingUser.Profile.FirstName, actingUser.Role.Name),
		"Removal",        // category / action type
		"Cohort",         // entity type
		&cohort.CohortID, // entity ID
		"Removed",
		true, // status
	)

	c.Json(w, http.StatusOK, fmt.Sprintf("Student '%s %s' removed successfully from cohort '%s'",
		student.Profile.FirstName, student.Profile.LastName, cohort.Name), nil)
}

//-----------------------------------------------------------------------------------
//	Update Cohort API **
//===================================================================================

func (c *Construct) UpdateCohort(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	cohortIDStr := vars["cohort_id"]
	cohortID, err := strconv.ParseUint(cohortIDStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid cohort ID", nil)
		return
	}

	// --- Get logged-in user from middleware context ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Role").Preload("Profile").
		Where("user_uuid = ?", userUUID).
		First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "User not found", nil)
		return
	}

	// --- Decode input ---
	var input struct {
		Name        *string `json:"name,omitempty"`
		Description *string `json:"description,omitempty"`
		StartDate   *string `json:"start_date,omitempty"`
		EndDate     *string `json:"end_date,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Fetch cohort ---
	var cohort models.Cohort
	if err := c.DB.Preload("Users.Role").Preload("Users.Profile").
		First(&cohort, cohortID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Cohort not found", nil)
		return
	}

	// --- Track changes ---
	changes := map[string]interface{}{}
	if input.Name != nil && cohort.Name != *input.Name {
		changes["name"] = map[string]string{"old": cohort.Name, "new": *input.Name}
		cohort.Name = *input.Name
	}
	if input.Description != nil && cohort.Description != *input.Description {
		changes["description"] = map[string]string{"old": cohort.Description, "new": *input.Description}
		cohort.Description = *input.Description
	}
	if input.StartDate != nil && cohort.StartDate != *input.StartDate {
		changes["start_date"] = map[string]string{"old": cohort.StartDate, "new": *input.StartDate}
		cohort.StartDate = *input.StartDate
	}
	if input.EndDate != nil && cohort.EndDate != *input.EndDate {
		changes["end_date"] = map[string]string{"old": cohort.EndDate, "new": *input.EndDate}
		cohort.EndDate = *input.EndDate
	}

	if len(changes) == 0 {
		c.Json(w, http.StatusOK, "No changes made", nil)
		return
	}

	// --- Save updates ---
	if err := c.DB.Save(&cohort).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update cohort", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Audit log ---
	_ = c.LogAudit(user.UserID, "update", ptrString("cohort"), &cohort.CohortID, nil, changes)

	// --- System history ---
	changeSummary := []string{}
	for field, vals := range changes {
		if v, ok := vals.(map[string]string); ok {
			changeSummary = append(changeSummary, fmt.Sprintf("%s: '%s' → '%s'", field, v["old"], v["new"]))
		}
	}
	comment := fmt.Sprintf("%s (%s) updated cohort '%s' — %s",
		user.Profile.FirstName, user.Role.Name, cohort.Name, strings.Join(changeSummary, ", "))
	c.DB.Create(&models.SystemHistory{
		EntityType:  "Cohort",
		EntityID:    &cohort.CohortID,
		Action:      "Update",
		Status:      ptrString("Success"),
		Comment:     &comment,
		ChangedByID: user.UserID,
		CreatedAt:   time.Now(),
	})

	// --- Notification ---
	c.NotifyAndTrack(
		user.UserID,
		"Cohort Updated",
		fmt.Sprintf("Cohort '%s' has been updated.", cohort.Name),
		"Update",         // category (action type)
		"Cohort",         // entity type
		&cohort.CohortID, // entity ID
		"Success",
		false, // status or outcome
	)

	// --- Response ---
	resp := map[string]interface{}{
		"cohort_id":   cohort.CohortID,
		"name":        cohort.Name,
		"description": cohort.Description,
		"start_date":  cohort.StartDate,
		"end_date":    cohort.EndDate,
		"updated_at":  cohort.UpdatedAt,
		"updated_by": map[string]string{
			"first_name": user.Profile.FirstName,
			"last_name":  user.Profile.LastName,
			"role":       user.Role.Name,
		},
		"changes": changes,
	}

	c.Json(w, http.StatusOK, "Cohort updated successfully", map[string]interface{}{"cohort": resp})
}
