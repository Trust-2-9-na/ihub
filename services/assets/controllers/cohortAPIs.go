package controllers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
	"web/services/assets/models"

	"github.com/gorilla/mux"
)

//---------------------------------
// COHORT APIS //
//-----------------------------------
//------------------------------------
// ** Supervisor create Cohort API **
//------------------------------------

func (c *Construct) CreateCohort(w http.ResponseWriter, r *http.Request) {
	// Get logged-in user UUID from context
	userUUIDCtx := r.Context().Value("user_uuid")
	if userUUIDCtx == nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	userUUID := userUUIDCtx.(string)

	// Fetch user to get numeric ID and profile
	var user models.User
	if err := c.DB.Preload("Profile").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	var input struct {
		Name        string    `json:"name"`
		Description string    `json:"description"`
		StartDate   time.Time `json:"start_date"`
		EndDate     time.Time `json:"end_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

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

	// 🔔 Notification for the creator
	_ = c.CreateNotification(user.UserID, "New Cohort Created", "Cohort "+input.Name+" has been added.")

	// 📜 Audit log
	_ = c.LogAudit(user.UserID, "create", func() *string { s := "cohort"; return &s }(), &cohort.CohortID, nil, map[string]interface{}{
		"name": input.Name,
	})

	// ⚙️ Job log
	job, _ := c.StartJob("cohort_reminder_setup", map[string]interface{}{"cohort_id": cohort.CohortID})
	msg := "Reminder job scheduled"
	_ = c.EndJob(job, "Completed", &msg)

	// Return response including creator's first + last name
	resp := map[string]interface{}{
		"cohort_id":   cohort.CohortID,
		"name":        cohort.Name,
		"description": cohort.Description,
		"start_date":  cohort.StartDate,
		"end_date":    cohort.EndDate,
		"created_by": map[string]string{
			"first_name": user.Profile.FirstName,
			"last_name":  user.Profile.LastName,
		},
		"created_at": cohort.CreatedAt,
	}

	c.Json(w, http.StatusCreated, "Cohort created successfully", map[string]interface{}{"cohort": resp})
}

//-------------------------------------
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

func (c *Construct) DeleteCohort(w http.ResponseWriter, r *http.Request) {
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

	// Fetch cohort
	var cohort models.Cohort
	if err := c.DB.First(&cohort, cohortID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Cohort not found", nil)
		return
	}

	// Fetch logged-in user info for logs and notifications
	var user models.User
	if err := c.DB.Preload("Profile").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	// Delete cohort
	if err := c.DB.Delete(&cohort).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to delete cohort", map[string]interface{}{"error": err.Error()})
		return
	}

	// 🔔 Notification
	_ = c.CreateNotification(user.UserID, "Cohort Deleted", "Cohort "+cohort.Name+" has been deleted.")

	// 📜 Audit log
	_ = c.LogAudit(user.UserID, "delete", func() *string { s := "cohort"; return &s }(), &cohort.CohortID, nil, map[string]interface{}{
		"name": cohort.Name,
	})

	// ⚙️ Job log
	job, _ := c.StartJob("cohort_delete_cleanup", map[string]interface{}{"cohort_id": cohort.CohortID})
	msg := "Cleanup job completed"
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
		"created_by": map[string]string{
			"first_name": creator.Profile.FirstName,
			"last_name":  creator.Profile.LastName,
		},
	}

	c.Json(w, http.StatusOK, "Cohort deleted successfully", map[string]interface{}{"cohort": resp})
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
		Name        *string    `json:"name,omitempty"`
		Description *string    `json:"description,omitempty"`
		StartDate   *time.Time `json:"start_date,omitempty"`
		EndDate     *time.Time `json:"end_date,omitempty"`
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
