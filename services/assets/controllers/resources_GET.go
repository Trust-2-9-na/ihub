package controllers

import (
	"net/http"
	"strings"

	"strconv"

	"web/services/assets/middlewares"
	"web/services/assets/models"

	"github.com/gorilla/mux"
)

// ==========================================================================================
//
//	API to GET Resources
//
// ------------------------------------------------------------------------------------------
func (c *Construct) GetResources(w http.ResponseWriter, r *http.Request) {
	// --- 1️⃣ Authenticate user ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Where("user_uuid = ?", userUUID).
		Preload("Profile").Preload("Role").First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "User not found", nil)
		return
	}

	// --- 2️⃣ Base query ---
	query := c.DB.Model(&models.Resource{}).
		Preload("Uploader.Profile").
		Preload("Uploader.Role").
		Preload("ResourceCohort.Users.Profile").
		Preload("ResourceTeam.UserTeams.UserRef.Profile").
		Preload("SharedWithUser.Profile")

	// Optional filters
	if cohortIDStr := r.URL.Query().Get("cohort_id"); cohortIDStr != "" {
		if id, err := strconv.ParseUint(cohortIDStr, 10, 64); err == nil {
			query = query.Where("resource_cohort_id = ?", id)
		}
	}
	if teamIDStr := r.URL.Query().Get("team_id"); teamIDStr != "" {
		if id, err := strconv.ParseUint(teamIDStr, 10, 64); err == nil {
			query = query.Where("resource_team_id = ?", id)
		}
	}
	if userIDStr := r.URL.Query().Get("user_id"); userIDStr != "" {
		if id, err := strconv.ParseUint(userIDStr, 10, 64); err == nil {
			query = query.Where("uploader_id = ?", id)
		}
	}

	var resources []models.Resource
	if err := query.Find(&resources).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch resources", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 3️⃣ Filter by visibility / access ---
	filtered := []map[string]interface{}{}
	for _, res := range resources {
		canView := false

		// ✅ Admins can see everything
		if strings.EqualFold(user.Role.Name, "SystemAdmin") || strings.EqualFold(user.Role.Name, "OpsAdmin") {
			canView = true
		} else {
			switch res.Visibility {
			case models.Public:
				canView = true
			case models.Private:
				if res.UploaderID == user.UserID {
					canView = true
				}
				if res.SharedWithUserID != nil && *res.SharedWithUserID == user.UserID {
					canView = true
				}
				if res.ResourceTeam != nil {
					for _, ut := range res.ResourceTeam.UserTeams {
						if ut.UserRefID == user.UserID {
							canView = true
							break
						}
					}
				}
			}

			if !canView && res.ResourceCohort != nil {
				for _, u := range res.ResourceCohort.Users {
					if u.UserID == user.UserID {
						canView = true
						break
					}
				}
			}

			if !canView && res.ResourceTeam != nil {
				for _, ut := range res.ResourceTeam.UserTeams {
					if ut.UserRefID == user.UserID {
						canView = true
						break
					}
				}
			}
		}

		if !canView {
			continue
		}

		// --- 4️⃣ Build structured response ---
		resp := map[string]interface{}{
			"id":            res.ID,
			"title":         res.Title,
			"description":   res.Description,
			"resource_type": res.ResourceType,
			"file_path":     res.FilePath,
			"url":           res.URL,
			"visibility":    res.Visibility,
			"created_at":    res.CreatedAt,
			"uploader": map[string]interface{}{
				"id":         res.UploaderID,
				"first_name": res.Uploader.Profile.FirstName,
				"last_name":  res.Uploader.Profile.LastName,
				"role":       res.Uploader.Role.Name,
			},
		}

		if res.ResourceCohort != nil {
			resp["cohort"] = map[string]interface{}{
				"id":   res.ResourceCohort.CohortID,
				"name": res.ResourceCohort.Name,
			}
		}

		if res.SharedWithUser != nil {
			resp["shared_with"] = map[string]interface{}{
				"id":         res.SharedWithUser.UserID,
				"first_name": res.SharedWithUser.Profile.FirstName,
				"last_name":  res.SharedWithUser.Profile.LastName,
			}
		}

		if res.ResourceTeam != nil {
			teamResp := map[string]interface{}{
				"id":          res.ResourceTeam.TeamID,
				"name":        res.ResourceTeam.Name,
				"description": res.ResourceTeam.Description,
			}
			if res.ResourceTeam.CohortDetails != nil {
				teamResp["cohort"] = map[string]interface{}{
					"id":   res.ResourceTeam.CohortDetails.CohortID,
					"name": res.ResourceTeam.CohortDetails.Name,
				}
			}
			resp["team"] = teamResp
		}

		filtered = append(filtered, resp)
	}

	// --- 5️⃣ Audit for system/ops admins ---
	if strings.EqualFold(user.Role.Name, "SystemAdmin") || strings.EqualFold(user.Role.Name, "OpsAdmin") {
		c.LogAudit(user.UserID, "view", ptrString("resources"), nil, nil, map[string]interface{}{
			"count": len(filtered),
		})
	}

	c.Json(w, http.StatusOK, "Resources fetched successfully", map[string]interface{}{
		"resources": filtered,
	})
}

//==========================================================================================
//                                API to GET a Single Resource
//------------------------------------------------------------------------------------------

// GET /api/resources/{id}
func (c *Construct) GetResourceByID(w http.ResponseWriter, r *http.Request) {
	// --- 1️⃣ Get resource ID ---
	vars := mux.Vars(r)
	idStr := vars["resource_id"]
	resourceID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid resource ID", nil)
		return
	}

	// --- 2️⃣ Authenticated user ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Where("user_uuid = ?", userUUID).
		Preload("Profile").Preload("Role").First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "User not found", nil)
		return
	}

	// --- 3️⃣ Fetch resource with relations ---
	var res models.Resource
	if err := c.DB.Preload("Uploader.Profile").
		Preload("Uploader.Role").
		Preload("ResourceCohort.Users.Profile").
		Preload("ResourceTeam.UserTeams.UserRef.Profile").
		Preload("SharedWithUser.Profile").
		First(&res, resourceID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Resource not found", nil)
		return
	}

	// --- 4️⃣ Check access / visibility ---
	canView := false

	// ✅ Admins bypass all visibility restrictions
	if strings.EqualFold(user.Role.Name, "SystemAdmin") || strings.EqualFold(user.Role.Name, "OpsAdmin") {
		canView = true
	} else {
		switch res.Visibility {
		case models.Public:
			canView = true
		case models.Private:
			if res.UploaderID == user.UserID {
				canView = true
			}
			if res.SharedWithUserID != nil && *res.SharedWithUserID == user.UserID {
				canView = true
			}
			if res.ResourceTeam != nil {
				for _, ut := range res.ResourceTeam.UserTeams {
					if ut.UserRefID == user.UserID {
						canView = true
						break
					}
				}
			}
		}

		// Cohort access
		if !canView && res.ResourceCohort != nil {
			for _, u := range res.ResourceCohort.Users {
				if u.UserID == user.UserID {
					canView = true
					break
				}
			}
		}

		// Team access
		if !canView && res.ResourceTeam != nil {
			for _, ut := range res.ResourceTeam.UserTeams {
				if ut.UserRefID == user.UserID {
					canView = true
					break
				}
			}
		}
	}

	if !canView {
		c.Json(w, http.StatusForbidden, "You do not have access to this resource", nil)
		return
	}

	// --- 5️⃣ Build response ---
	resp := map[string]interface{}{
		"id":            res.ID,
		"title":         res.Title,
		"description":   res.Description,
		"resource_type": res.ResourceType,
		"file_path":     res.FilePath,
		"url":           res.URL,
		"visibility":    res.Visibility,
		"created_at":    res.CreatedAt,
		"uploader": map[string]interface{}{
			"id":         res.UploaderID,
			"first_name": res.Uploader.Profile.FirstName,
			"last_name":  res.Uploader.Profile.LastName,
			"role":       res.Uploader.Role.Name,
		},
	}

	if res.ResourceCohort != nil {
		resp["cohort"] = map[string]interface{}{
			"id":   res.ResourceCohort.CohortID,
			"name": res.ResourceCohort.Name,
		}
	}

	if res.SharedWithUser != nil {
		resp["shared_with"] = map[string]interface{}{
			"id":         res.SharedWithUser.UserID,
			"first_name": res.SharedWithUser.Profile.FirstName,
			"last_name":  res.SharedWithUser.Profile.LastName,
		}
	}

	if res.ResourceTeam != nil {
		teamResp := map[string]interface{}{
			"id":          res.ResourceTeam.TeamID,
			"name":        res.ResourceTeam.Name,
			"description": res.ResourceTeam.Description,
		}
		if res.ResourceTeam.CohortDetails != nil {
			teamResp["cohort"] = map[string]interface{}{
				"id":   res.ResourceTeam.CohortDetails.CohortID,
				"name": res.ResourceTeam.CohortDetails.Name,
			}
		}
		resp["team"] = teamResp
	}

	// --- 6️⃣ Audit & return ---
	c.LogAudit(user.UserID, "view", ptrString("resource"), &res.ID, nil, nil)
	c.Json(w, http.StatusOK, "Resource fetched successfully", map[string]interface{}{"resource": resp})
}

// ==========================================================================================
//
//	API to List Resources
//
// ------------------------------------------------------------------------------------------
// CanViewResource checks if a user can view a given resource
func CanViewResource(user models.User, res models.Resource) bool {
	// ✅ Admin bypass
	if strings.EqualFold(user.Role.Name, "SystemAdmin") || strings.EqualFold(user.Role.Name, "OpsAdmin") {
		return true
	}

	switch res.Visibility {
	case models.Public:
		return true
	case models.Private:
		if res.UploaderID == user.UserID {
			return true
		}
		if res.SharedWithUserID != nil && *res.SharedWithUserID == user.UserID {
			return true
		}
		if res.ResourceTeam != nil {
			for _, ut := range res.ResourceTeam.UserTeams {
				if ut.UserRefID == user.UserID {
					return true
				}
			}
		}
	}
	// Cohort access
	if res.ResourceCohort != nil {
		for _, u := range res.ResourceCohort.Users {
			if u.UserID == user.UserID {
				return true
			}
		}
	}
	// Team access
	if res.ResourceTeam != nil {
		for _, ut := range res.ResourceTeam.UserTeams {
			if ut.UserRefID == user.UserID {
				return true
			}
		}
	}
	return false
}

// ListResources handles paginated resource listing with access control
func (c *Construct) ListResources(w http.ResponseWriter, r *http.Request) {
	// --- 1️⃣ Authenticated user ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Where("user_uuid = ?", userUUID).
		Preload("Profile").Preload("Role").First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "User not found", nil)
		return
	}

	// --- 2️⃣ Pagination params ---
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	offset := (page - 1) * limit

	// --- 3️⃣ Base query with optional filters ---
	query := c.DB.Model(&models.Resource{}).
		Preload("Uploader.Profile").Preload("Uploader.Role").
		Preload("ResourceCohort.Users.Profile").
		Preload("ResourceTeam.UserTeams.UserRef.Profile").
		Preload("SharedWithUser.Profile")

	if cohortIDStr := r.URL.Query().Get("cohort_id"); cohortIDStr != "" {
		if id, err := strconv.ParseUint(cohortIDStr, 10, 64); err == nil {
			query = query.Where("resource_cohort_id = ?", id)
		}
	}
	if teamIDStr := r.URL.Query().Get("team_id"); teamIDStr != "" {
		if id, err := strconv.ParseUint(teamIDStr, 10, 64); err == nil {
			query = query.Where("resource_team_id = ?", id)
		}
	}
	if userIDStr := r.URL.Query().Get("user_id"); userIDStr != "" {
		if id, err := strconv.ParseUint(userIDStr, 10, 64); err == nil {
			query = query.Where("uploader_id = ?", id)
		}
	}

	var resources []models.Resource
	if err := query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&resources).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch resources", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 4️⃣ Filter resources by access ---
	filtered := []map[string]interface{}{}
	for _, res := range resources {
		// ✅ Admin bypass: skip CanViewResource check
		if !strings.EqualFold(user.Role.Name, "SystemAdmin") && !strings.EqualFold(user.Role.Name, "OpsAdmin") {
			if !CanViewResource(user, res) {
				continue
			}
		}

		resp := map[string]interface{}{
			"id":            res.ID,
			"title":         res.Title,
			"description":   res.Description,
			"resource_type": res.ResourceType,
			"file_path":     res.FilePath,
			"url":           res.URL,
			"visibility":    res.Visibility,
			"created_at":    res.CreatedAt,
			"uploader": map[string]interface{}{
				"id":         res.UploaderID,
				"first_name": res.Uploader.Profile.FirstName,
				"last_name":  res.Uploader.Profile.LastName,
				"role":       res.Uploader.Role.Name,
			},
		}

		if res.ResourceCohort != nil {
			resp["cohort"] = map[string]interface{}{
				"id":   res.ResourceCohort.CohortID,
				"name": res.ResourceCohort.Name,
			}
		}
		if res.SharedWithUser != nil {
			resp["shared_with"] = map[string]interface{}{
				"id":         res.SharedWithUser.UserID,
				"first_name": res.SharedWithUser.Profile.FirstName,
				"last_name":  res.SharedWithUser.Profile.LastName,
			}
		}
		if res.ResourceTeam != nil {
			teamResp := map[string]interface{}{
				"id":          res.ResourceTeam.TeamID,
				"name":        res.ResourceTeam.Name,
				"description": res.ResourceTeam.Description,
			}
			if res.ResourceTeam.CohortDetails != nil {
				teamResp["cohort"] = map[string]interface{}{
					"id":   res.ResourceTeam.CohortDetails.CohortID,
					"name": res.ResourceTeam.CohortDetails.Name,
				}
			}
			resp["team"] = teamResp
		}

		filtered = append(filtered, resp)
	}

	// --- 5️⃣ Audit & response ---
	c.LogAudit(user.UserID, "list", ptrString("resources"), nil, nil, map[string]interface{}{
		"count": len(filtered),
	})

	c.Json(w, http.StatusOK, "Resources fetched successfully", map[string]interface{}{
		"page":      page,
		"limit":     limit,
		"resources": filtered,
	})
}
