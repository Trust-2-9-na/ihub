package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"web/services/assets/models"

	"github.com/gorilla/mux"
)

//---------------------------------
// COHORT APIS //
//-----------------------------------
//------------------------------------
// ** Supervisor create Cohort API **
//------------------------------------
// CreateCohort handles creating a new cohort and returns creator's full name

func (c *Construct) CreateCohort(w http.ResponseWriter, r *http.Request) {
	// Get logged-in user's UUID from context
	userUUIDCtx := r.Context().Value("user_uuid")
	if userUUIDCtx == nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	userUUID := userUUIDCtx.(string)

	// Fetch user and preload profile for full name
	var user models.User
	if err := c.DB.Preload("Profile").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	// Parse input JSON
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

	// Create cohort
	cohort := models.Cohort{
		Name:        input.Name,
		Description: input.Description,
		StartDate:   input.StartDate,
		EndDate:     input.EndDate,
		CreatedBy:   user.UserUUID, // store UUID internally
	}

	if err := c.DB.Create(&cohort).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create cohort", map[string]interface{}{"error": err.Error()})
		return
	}

	// Optional: notification for creator
	_ = c.CreateNotification(user.UserID, "New Cohort Created", "Cohort "+input.Name+" has been added.")

	// Optional: audit log
	_ = c.LogAudit(user.UserID, "create", func() *string { s := "cohort"; return &s }(), &cohort.CohortID, nil, map[string]interface{}{
		"name": input.Name,
	})

	// Optional: job log (e.g., reminders)
	job, _ := c.StartJob("cohort_reminder_setup", map[string]interface{}{"cohort_id": cohort.CohortID})
	msg := "Reminder job scheduled"
	_ = c.EndJob(job, "Completed", &msg)

	// Prepare response with full name of creator
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

//------------------------------------------------
// ** all users View-list Cohort API **
//-------------------------------------

func (c *Construct) GetCohorts(w http.ResponseWriter, r *http.Request) {
	var cohorts []models.Cohort
	if err := c.DB.Find(&cohorts).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch cohorts", map[string]interface{}{"error": err.Error()})
		return
	}

	var result []map[string]interface{}
	for _, cohort := range cohorts {
		// Fetch creator's name
		var creator models.User
		if err := c.DB.Preload("Profile").Where("user_uuid = ?", cohort.CreatedBy).First(&creator).Error; err != nil {
			continue // skip if creator not found
		}

		result = append(result, map[string]interface{}{
			"cohort_id":   cohort.CohortID,
			"name":        cohort.Name,
			"description": cohort.Description,
			"start_date":  cohort.StartDate,
			"end_date":    cohort.EndDate,
			"created_by": map[string]string{
				"first_name": creator.Profile.FirstName,
				"last_name":  creator.Profile.LastName,
			},
			"created_at": cohort.CreatedAt,
		})
	}

	c.Json(w, http.StatusOK, "Cohorts retrieved successfully", map[string]interface{}{"cohorts": result})
}

//--------------------------------------------
// ** Supervisor && Admin Delete Cohort API **
//--------------------------------------------

func (c *Construct) DeleteCohorts(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
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

	// Get logged-in user UUID
	userUUIDCtx := r.Context().Value("user_uuid")
	if userUUIDCtx == nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	userUUID := userUUIDCtx.(string)

	// Fetch logged-in user info for logs/notifications
	var user models.User
	if err := c.DB.Preload("Profile").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	// Fetch cohorts to be deleted
	var cohorts []models.Cohort
	if err := c.DB.Where("cohort_id IN ?", body.CohortIDs).Find(&cohorts).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch cohorts", map[string]interface{}{"error": err.Error()})
		return
	}
	if len(cohorts) == 0 {
		c.Json(w, http.StatusNotFound, "No cohorts found for provided IDs", nil)
		return
	}

	// Delete cohorts
	if err := c.DB.Where("cohort_id IN ?", body.CohortIDs).Delete(&models.Cohort{}).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to delete cohorts", map[string]interface{}{"error": err.Error()})
		return
	}

	// Prepare response with creator full names
	resp := []map[string]interface{}{}
	for _, cohort := range cohorts {
		var creator models.User
		if err := c.DB.Preload("Profile").Where("user_uuid = ?", cohort.CreatedBy).First(&creator).Error; err != nil {
			creator.Profile.FirstName = "Unknown"
			creator.Profile.LastName = ""
		}

		resp = append(resp, map[string]interface{}{
			"cohort_id":   cohort.CohortID,
			"name":        cohort.Name,
			"description": cohort.Description,
			"created_by": map[string]string{
				"first_name": creator.Profile.FirstName,
				"last_name":  creator.Profile.LastName,
				"full_name":  creator.Profile.FirstName + " " + creator.Profile.LastName,
			},
		})
	}

	// Notifications & audit log for each deleted cohort
	for _, cohort := range cohorts {
		_ = c.CreateNotification(user.UserID, "Cohort Deleted", "Cohort "+cohort.Name+" has been deleted.")
		_ = c.LogAudit(user.UserID, "delete", func() *string { s := "cohort"; return &s }(), &cohort.CohortID, nil, map[string]interface{}{
			"name": cohort.Name,
		})
		// Optional: job cleanup
		job, _ := c.StartJob("cohort_delete_cleanup", map[string]interface{}{"cohort_id": cohort.CohortID})
		msg := "Cleanup job completed"
		_ = c.EndJob(job, "Completed", &msg)
	}

	c.Json(w, http.StatusOK, fmt.Sprintf("%d cohort(s) deleted successfully", len(cohorts)), map[string]interface{}{"cohorts": resp})
}

//--------------------------------------------
//  Update Cohort API **
//--------------------------------------------

func (c *Construct) UpdateCohort(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	cohortIDStr := vars["cohort_id"]
	cohortID, err := strconv.ParseUint(cohortIDStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid cohort ID", nil)
		return
	}

	// Get logged-in user UUID
	userUUIDCtx := r.Context().Value("user_uuid")
	if userUUIDCtx == nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	userUUID := userUUIDCtx.(string)

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

	// Fetch cohort
	var cohort models.Cohort
	if err := c.DB.First(&cohort, cohortID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Cohort not found", nil)
		return
	}

	// Fetch user info
	var user models.User
	if err := c.DB.Preload("Profile").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	// Apply updates
	if input.Name != nil {
		cohort.Name = *input.Name
	}
	if input.Description != nil {
		cohort.Description = *input.Description
	}
	if input.StartDate != nil {
		cohort.StartDate = *input.StartDate
	}
	if input.EndDate != nil {
		cohort.EndDate = *input.EndDate
	}

	if err := c.DB.Save(&cohort).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update cohort", map[string]interface{}{"error": err.Error()})
		return
	}

	// 🔔 Notification
	_ = c.CreateNotification(user.UserID, "Cohort Updated", "Cohort "+cohort.Name+" has been updated.")

	// 📜 Audit log
	_ = c.LogAudit(user.UserID, "update", func() *string { s := "cohort"; return &s }(), &cohort.CohortID, nil, map[string]interface{}{
		"name": cohort.Name,
	})

	// ⚙️ Job log
	job, _ := c.StartJob("cohort_update_notifications", map[string]interface{}{"cohort_id": cohort.CohortID})
	msg := "Update notification job completed"
	_ = c.EndJob(job, "Completed", &msg)

	// Return response including creator's name
	var creator models.User
	if err := c.DB.Preload("Profile").Where("user_uuid = ?", cohort.CreatedBy).First(&creator).Error; err != nil {
		creator.Profile.FirstName = "Unknown"
		creator.Profile.LastName = ""
	}

	resp := map[string]interface{}{
		"cohort_id":   cohort.CohortID,
		"name":        cohort.Name,
		"description": cohort.Description,
		"start_date":  cohort.StartDate,
		"end_date":    cohort.EndDate,
		"created_by": map[string]string{
			"first_name": creator.Profile.FirstName,
			"last_name":  creator.Profile.LastName,
		},
		"updated_at": cohort.UpdatedAt,
	}

	c.Json(w, http.StatusOK, "Cohort updated successfully", map[string]interface{}{"cohort": resp})
}

// GetCohortOverview fetches proposals, students, and teams for a single cohort
func (c *Construct) GetCohortOverview(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	cohortIDStr := vars["cohort_id"]
	cohortID, err := strconv.ParseUint(cohortIDStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid cohort ID", nil)
		return
	}

	// Get logged-in user UUID
	userUUIDCtx := r.Context().Value("user_uuid")
	if userUUIDCtx == nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	userUUID := userUUIDCtx.(string)

	// Fetch user and role
	var user models.User
	if err := c.DB.Preload("Role").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	roleName := strings.ToLower(user.Role.Name)
	if roleName != "supervisor" && roleName != "admin" {
		c.Json(w, http.StatusForbidden, "Unauthorized", nil)
		return
	}

	// --- Fetch cohort info ---
	var cohort models.Cohort
	if err := c.DB.Preload("Creator.Profile").First(&cohort, cohortID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Cohort not found", nil)
		return
	}

	// --- Fetch proposals ---
	var proposals []models.Proposal
	if err := c.DB.Preload("SubmittedBy.Profile").
		Preload("Team.Users.Profile").
		Preload("Window").
		Where("cohort_id = ?", cohortID).
		Find(&proposals).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch proposals", map[string]interface{}{"error": err.Error()})
		return
	}

	proposalsResp := []map[string]interface{}{}
	for _, p := range proposals {
		submittedBy := map[string]string{"full_name": p.SubmittedBy.Profile.FirstName + " " + p.SubmittedBy.Profile.LastName}
		teamUsers := []map[string]string{}
		if p.Team != nil {
			for _, u := range p.Team.Users {
				teamUsers = append(teamUsers, map[string]string{
					"full_name": u.Profile.FirstName + " " + u.Profile.LastName,
					"username":  u.Username,
				})
			}
		}
		proposalsResp = append(proposalsResp, map[string]interface{}{
			"proposal_id":  p.ProposalID,
			"title":        p.Title,
			"abstract":     p.Abstract,
			"status":       p.Status,
			"archived":     p.Archived,
			"submitted_by": submittedBy,
			"team":         teamUsers,
			"window_title": func() string {
				if p.Window != nil {
					return p.Window.Title
				}
				return ""
			}(),
			"document_url": p.DocumentURL,
			"created_at":   p.CreatedAt,
			"updated_at":   p.UpdatedAt,
		})
	}

	// --- Fetch students in cohort ---
	var students []models.User
	if err := c.DB.Preload("Profile").Where("cohort_id = ? AND role_id = ?", cohortID, 2).Find(&students).Error; err != nil { // 2 = student role
		c.Json(w, http.StatusInternalServerError, "Failed to fetch students", map[string]interface{}{"error": err.Error()})
		return
	}
	studentsResp := []map[string]interface{}{}
	for _, s := range students {
		studentsResp = append(studentsResp, map[string]interface{}{
			"user_id":   s.UserID,
			"username":  s.Username,
			"full_name": s.Profile.FirstName + " " + s.Profile.LastName,
			"email":     s.Email,
		})
	}

	// --- Fetch teams in cohort ---
	var teams []models.Team
	if err := c.DB.Preload("Users.Profile").Where("cohort_id = ?", cohortID).Find(&teams).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch teams", map[string]interface{}{"error": err.Error()})
		return
	}
	teamsResp := []map[string]interface{}{}
	for _, t := range teams {
		usersResp := []map[string]string{}
		for _, u := range t.Users {
			usersResp = append(usersResp, map[string]string{
				"full_name": u.Profile.FirstName + " " + u.Profile.LastName,
				"username":  u.Username,
			})
		}
		teamsResp = append(teamsResp, map[string]interface{}{
			"team_id": t.TeamID,
			"name":    t.Name,
			"users":   usersResp,
		})
	}

	// --- Build final response ---
	resp := map[string]interface{}{
		"cohort": map[string]interface{}{
			"cohort_id":   cohort.CohortID,
			"name":        cohort.Name,
			"description": cohort.Description,
			"start_date":  cohort.StartDate,
			"end_date":    cohort.EndDate,
			"created_by": map[string]string{
				"full_name": cohort.Creator.Profile.FirstName + " " + cohort.Creator.Profile.LastName,
			},
		},
		"counts": map[string]int{
			"proposals": len(proposalsResp),
			"students":  len(studentsResp),
			"teams":     len(teamsResp),
		},
		"proposals": proposalsResp,
		"students":  studentsResp,
		"teams":     teamsResp,
	}

	c.Json(w, http.StatusOK, "Cohort overview fetched successfully", resp)
}
