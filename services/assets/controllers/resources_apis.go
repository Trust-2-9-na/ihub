package controllers

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"web/services/assets/middlewares"
	"web/services/assets/models"
	"web/services/utils"

	"github.com/gorilla/mux"
)

// ==========================================================================================
//
//	API to Create a Resource
//
// ------------------------------------------------------------------------------------------
func (c *Construct) CreateResource(w http.ResponseWriter, r *http.Request) {
	// --- 1️⃣ Authenticate user ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Where("user_uuid = ?", userUUID).
		Preload("Profile").
		Preload("Role").
		First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "User not found", nil)
		return
	}

	// --- 2️⃣ Parse input ---
	contentType := r.Header.Get("Content-Type")
	var title, description, resourceType, url string
	var cohortIDStr, sharedUserIDStr, teamIDStr string
	var file io.Reader
	var fileHeader *multipart.FileHeader

	if strings.HasPrefix(contentType, "application/json") {
		// JSON payload
		var input struct {
			Title            string `json:"title"`
			Description      string `json:"description"`
			ResourceType     string `json:"resource_type"`
			URL              string `json:"url"`
			ResourceCohortID string `json:"resource_cohort_id"`
			SharedWithUserID string `json:"shared_with_user_id"`
			ResourceTeamID   string `json:"resource_team_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid JSON payload", map[string]interface{}{"error": err.Error()})
			return
		}
		title = input.Title
		description = input.Description
		resourceType = input.ResourceType
		url = input.URL
		cohortIDStr = input.ResourceCohortID
		sharedUserIDStr = input.SharedWithUserID
		teamIDStr = input.ResourceTeamID

	} else if strings.HasPrefix(contentType, "multipart/form-data") {
		// Form-data
		if err := r.ParseMultipartForm(50 << 20); err != nil {
			c.Json(w, http.StatusBadRequest, "Failed to parse form", map[string]interface{}{"error": err.Error()})
			return
		}
		title = r.FormValue("title")
		description = r.FormValue("description")
		resourceType = r.FormValue("resource_type")
		url = r.FormValue("url")
		cohortIDStr = r.FormValue("resource_cohort_id")
		sharedUserIDStr = r.FormValue("shared_with_user_id")
		teamIDStr = r.FormValue("resource_team_id")

		var err error
		file, fileHeader, err = r.FormFile("file")
		if err != nil && err != http.ErrMissingFile {
			c.Json(w, http.StatusBadRequest, "Failed to read uploaded file", map[string]interface{}{"error": err.Error()})
			return
		}
	} else {
		c.Json(w, http.StatusUnsupportedMediaType, "Unsupported Content-Type", nil)
		return
	}

	if title == "" || resourceType == "" {
		c.Json(w, http.StatusBadRequest, "Title and resource type are required", nil)
		return
	}

	// --- 3️⃣ Parse IDs ---
	var cohortID, sharedUserID, teamID *uint64
	if cohortIDStr != "" {
		if id, err := strconv.ParseUint(cohortIDStr, 10, 64); err == nil {
			cohortID = &id
		}
	}
	if sharedUserIDStr != "" {
		if id, err := strconv.ParseUint(sharedUserIDStr, 10, 64); err == nil {
			sharedUserID = &id
		}
	}
	if teamIDStr != "" {
		if id, err := strconv.ParseUint(teamIDStr, 10, 64); err == nil {
			teamID = &id
		}
	}

	// --- 4️⃣ Validate only one scope ---
	targetCount := 0
	if cohortID != nil {
		targetCount++
	}
	if sharedUserID != nil {
		targetCount++
	}
	if teamID != nil {
		targetCount++
	}
	if targetCount > 1 {
		c.Json(w, http.StatusBadRequest, "Resource can only target one scope: cohort, team, or specific user", nil)
		return
	}

	// --- 5️⃣ Determine visibility ---
	visibility := models.Public // default
	if cohortID != nil || sharedUserID != nil || teamID != nil {
		visibility = models.Private
	}

	// --- 6️⃣ Handle file uploads ---
	var filePath string
	if file != nil && fileHeader != nil {
		defer func() {
			if f, ok := file.(io.Closer); ok {
				f.Close()
			}
		}()

		if err := utils.ValidateResourceFile(fileHeader, resourceType); err != nil {
			c.Json(w, http.StatusBadRequest, err.Error(), nil)
			return
		}

		uploadDir := fmt.Sprintf("uploads/resources/%s", strings.ToLower(resourceType))
		if err := os.MkdirAll(uploadDir, 0755); err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to create upload directory", nil)
			return
		}

		savePath := filepath.Join(uploadDir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), fileHeader.Filename))
		dst, err := os.Create(savePath)
		if err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to save file", nil)
			return
		}
		defer dst.Close()
		io.Copy(dst, file)
		filePath = savePath

	} else if resourceType == string(models.Link) {
		if url == "" || (!strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://")) {
			c.Json(w, http.StatusBadRequest, "Invalid or missing URL for link resource", nil)
			return
		}
	} else {
		c.Json(w, http.StatusBadRequest, "File upload required for this resource type", nil)
		return
	}

	// --- 7️⃣ Save resource to DB ---
	resource := models.Resource{
		Title:            title,
		Description:      description,
		ResourceType:     resourceType,
		URL:              url,
		FilePath:         filePath,
		UploaderID:       user.UserID,
		ResourceCohortID: cohortID,
		SharedWithUserID: sharedUserID,
		ResourceTeamID:   teamID,
		Visibility:       visibility,
	}

	if err := c.DB.Create(&resource).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to save resource", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 8️⃣ Preload relationships ---
	c.DB.
		Preload("Uploader.Profile").
		Preload("Uploader.Role").
		Preload("ResourceCohort").
		Preload("SharedWithUser.Profile").
		Preload("ResourceTeam.CohortDetails").
		First(&resource, resource.ID)

	// --- 9️⃣ Notify & Audit ---
	c.NotifyAndTrack(
		user.UserID,
		"Resource Uploaded",
		fmt.Sprintf("%s uploaded a new %s: '%s'", user.Profile.FirstName, resourceType, title),
		"Upload", "Resource", &resource.ID, "Success", false,
	)

	c.LogAudit(user.UserID, "create", ptrString("resource"), &resource.ID, nil, map[string]interface{}{
		"title": title, "resource_type": resourceType,
	})

	// --- 🔟 Response ---
	resp := map[string]interface{}{
		"id":            resource.ID,
		"title":         resource.Title,
		"description":   resource.Description,
		"resource_type": resource.ResourceType,
		"file_path":     resource.FilePath,
		"url":           resource.URL,
		"visibility":    resource.Visibility,
		"created_at":    resource.CreatedAt,
		"uploader": map[string]interface{}{
			"id":         user.UserID,
			"first_name": user.Profile.FirstName,
			"last_name":  user.Profile.LastName,
			"role":       user.Role.Name,
		},
	}

	if resource.ResourceCohort != nil {
		resp["cohort"] = map[string]interface{}{
			"id":   resource.ResourceCohort.CohortID,
			"name": resource.ResourceCohort.Name,
		}
	}

	if resource.ResourceTeam != nil {
		teamInfo := map[string]interface{}{
			"id":          resource.ResourceTeam.TeamID,
			"name":        resource.ResourceTeam.Name,
			"description": resource.ResourceTeam.Description,
		}
		if resource.ResourceTeam.CohortDetails != nil {
			teamInfo["cohort"] = map[string]interface{}{
				"id":   resource.ResourceTeam.CohortDetails.CohortID,
				"name": resource.ResourceTeam.CohortDetails.Name,
			}
		}
		resp["team"] = teamInfo
	}

	if resource.SharedWithUser != nil {
		resp["shared_with"] = map[string]interface{}{
			"id":         resource.SharedWithUser.UserID,
			"first_name": resource.SharedWithUser.Profile.FirstName,
			"last_name":  resource.SharedWithUser.Profile.LastName,
		}
	}

	c.Json(w, http.StatusCreated, "Resource uploaded successfully", map[string]interface{}{
		"resource": resp,
	})
}

//==========================================================================================
//                                API to Update a Resource
//------------------------------------------------------------------------------------------

func (c *Construct) UpdateResource(w http.ResponseWriter, r *http.Request) {
	// --- 1️⃣ Get resource ID ---
	vars := mux.Vars(r)
	resourceIDStr := vars["resource_id"]
	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 64)
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

	// --- 3️⃣ Fetch existing resource ---
	var resource models.Resource
	if err := c.DB.Preload("Uploader.Profile").Preload("Uploader.Role").
		Preload("ResourceCohort").Preload("SharedWithUser.Profile").
		Preload("ResourceTeam.CohortDetails").
		First(&resource, resourceID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Resource not found", nil)
		return
	}

	// --- 4️⃣ Parse form-data for updates ---
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		c.Json(w, http.StatusBadRequest, "Failed to parse form", map[string]interface{}{"error": err.Error()})
		return
	}

	title := r.FormValue("title")
	description := r.FormValue("description")
	resourceType := r.FormValue("resource_type")
	url := r.FormValue("url")

	cohortIDStr := r.FormValue("resource_cohort_id")
	sharedUserIDStr := r.FormValue("shared_with_user_id")
	teamIDStr := r.FormValue("resource_team_id")

	var cohortID, sharedUserID, teamID *uint64
	if cohortIDStr != "" {
		if id, err := strconv.ParseUint(cohortIDStr, 10, 64); err == nil {
			cohortID = &id
		}
	}
	if sharedUserIDStr != "" {
		if id, err := strconv.ParseUint(sharedUserIDStr, 10, 64); err == nil {
			sharedUserID = &id
		}
	}
	if teamIDStr != "" {
		if id, err := strconv.ParseUint(teamIDStr, 10, 64); err == nil {
			teamID = &id
		}
	}

	// --- 5️⃣ Validate only one scope ---
	targetCount := 0
	if cohortID != nil {
		targetCount++
	}
	if sharedUserID != nil {
		targetCount++
	}
	if teamID != nil {
		targetCount++
	}
	if targetCount > 1 {
		c.Json(w, http.StatusBadRequest, "Resource can only target one scope: cohort, team, or user", nil)
		return
	}

	// --- 6️⃣ Determine visibility ---
	visibility := models.Public
	if cohortID != nil || sharedUserID != nil || teamID != nil {
		visibility = models.Private
	}

	// --- 7️⃣ Handle file upload ---
	file, header, err := r.FormFile("file")
	if err == nil {
		defer file.Close()
		if err := utils.ValidateResourceFile(header, resourceType); err != nil {
			c.Json(w, http.StatusBadRequest, err.Error(), nil)
			return
		}

		uploadDir := fmt.Sprintf("uploads/resources/%s", strings.ToLower(resourceType))
		if err := os.MkdirAll(uploadDir, 0755); err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to create upload directory", nil)
			return
		}

		newFileName := fmt.Sprintf("%d_%s", time.Now().UnixNano(), header.Filename)
		savePath := filepath.Join(uploadDir, newFileName)

		dst, err := os.Create(savePath)
		if err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to save file", nil)
			return
		}
		defer dst.Close()
		io.Copy(dst, file)

		// Delete old file if exists
		if resource.FilePath != "" {
			os.Remove(resource.FilePath)
		}

		resource.FilePath = savePath
		resource.URL = "" // Clear URL if uploading a file
	} else if resourceType == string(models.Link) {
		if url == "" || (!strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://")) {
			c.Json(w, http.StatusBadRequest, "Invalid or missing URL for link resource", nil)
			return
		}
		resource.URL = url
		resource.FilePath = "" // Clear file if switching to link
	} else if resourceType != string(models.Link) && header == nil {
		c.Json(w, http.StatusBadRequest, "File upload required for this resource type", nil)
		return
	}

	// --- 8️⃣ Update fields ---
	if title != "" {
		resource.Title = title
	}
	if description != "" {
		resource.Description = description
	}
	if resourceType != "" {
		resource.ResourceType = resourceType
	}
	resource.ResourceCohortID = cohortID
	resource.SharedWithUserID = sharedUserID
	resource.ResourceTeamID = teamID
	resource.Visibility = visibility

	if err := c.DB.Save(&resource).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update resource", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 9️⃣ Reload relations ---
	c.DB.Preload("Uploader.Profile").Preload("Uploader.Role").
		Preload("ResourceCohort").Preload("SharedWithUser.Profile").
		Preload("ResourceTeam.CohortDetails").
		First(&resource, resource.ID)

	// --- 🔟 Notify & audit ---
	c.NotifyAndTrack(
		user.UserID,
		"Resource Updated",
		fmt.Sprintf("%s updated resource: '%s'", user.Profile.FirstName, resource.Title),
		"Update", "Resource", &resource.ID, "Success", false,
	)

	c.LogAudit(user.UserID, "update", ptrString("resource"), &resource.ID, nil, map[string]interface{}{
		"title": resource.Title, "resource_type": resource.ResourceType,
	})

	// --- 1️⃣1️⃣ Response ---
	resp := map[string]interface{}{
		"id":            resource.ID,
		"title":         resource.Title,
		"description":   resource.Description,
		"resource_type": resource.ResourceType,
		"file_path":     resource.FilePath,
		"url":           resource.URL,
		"visibility":    resource.Visibility,
		"created_at":    resource.CreatedAt,
		"uploader": map[string]interface{}{
			"id":         user.UserID,
			"first_name": user.Profile.FirstName,
			"last_name":  user.Profile.LastName,
			"role":       user.Role.Name,
		},
	}

	if resource.ResourceCohort != nil {
		resp["cohort"] = map[string]interface{}{
			"id":   resource.ResourceCohort.CohortID,
			"name": resource.ResourceCohort.Name,
		}
	}
	if resource.SharedWithUser != nil {
		resp["shared_with"] = map[string]interface{}{
			"id":         resource.SharedWithUser.UserID,
			"first_name": resource.SharedWithUser.Profile.FirstName,
			"last_name":  resource.SharedWithUser.Profile.LastName,
		}
	}
	if resource.ResourceTeam != nil {
		teamInfo := map[string]interface{}{
			"id":          resource.ResourceTeam.TeamID,
			"name":        resource.ResourceTeam.Name,
			"description": resource.ResourceTeam.Description,
		}
		if resource.ResourceTeam.CohortDetails != nil {
			teamInfo["cohort"] = map[string]interface{}{
				"id":   resource.ResourceTeam.CohortDetails.CohortID,
				"name": resource.ResourceTeam.CohortDetails.Name,
			}
		}
		resp["team"] = teamInfo
	}

	c.Json(w, http.StatusOK, "Resource updated successfully", map[string]interface{}{"resource": resp})
}
