package controllers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"web/services/assets/middlewares"
	"web/services/assets/models"

	"errors"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/mux"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ==========================================================================================
// Events Management POST API
// ------------------------------------------------------------------------------------------
func (c *Construct) CreateEvent(w http.ResponseWriter, r *http.Request) {
	// --- 1. Authenticate user ---
	viewerUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || viewerUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Profile").Preload("Role").
		Where("user_uuid = ?", viewerUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 2. Restrict to SystemAdmin & OpsAdmin only ---
	role := strings.ToLower(user.Role.Name)
	if role != "systemadmin" && role != "opsadmin" {
		c.Json(w, http.StatusForbidden, "Only SystemAdmin and OpsAdmin can create events", nil)
		return
	}

	// --- 3. Parse request body ---
	var input struct {
		Title         string    `json:"title"`
		Description   *string   `json:"description"`
		EventType     string    `json:"event_type"`
		StartTime     time.Time `json:"start_time"`
		EndTime       time.Time `json:"end_time"`
		Location      string    `json:"location"`
		CohortRefID   *uint64   `json:"cohort_id,omitempty"`
		Visibility    string    `json:"visibility,omitempty"`     // Public, Private, CohortOnly, RoleBased
		TargetRoles   []string  `json:"target_roles,omitempty"`   // used if RoleBased
		TargetCohorts []uint64  `json:"target_cohorts,omitempty"` // used if CohortOnly
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 4. Validate required fields ---
	if strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.EventType) == "" ||
		input.StartTime.IsZero() || input.EndTime.IsZero() || strings.TrimSpace(input.Location) == "" {
		c.Json(w, http.StatusBadRequest, "Missing required fields", nil)
		return
	}

	// --- 5. Validate event type ----
	// Predefined valid event types
	predefinedTypes := map[string]bool{
		models.EventTypeWorkshop: true,
		models.EventTypeDemo:     true,
		models.EventTypePitchDay: true,
		models.EventTypeSeminar:  true,
	}

	// Check if it's predefined or custom
	isPredefined := predefinedTypes[input.EventType]
	if !isPredefined {
		// Optional: sanitize custom input
		customType := strings.TrimSpace(strings.Title(strings.ToLower(input.EventType)))
		if len(customType) < 3 || len(customType) > 50 {
			c.Json(w, http.StatusBadRequest, "Invalid custom event type length (must be 3–50 chars)", nil)
			return
		}
		// (Optional) Persist custom event type for future selection
		var existingType models.EventTypeRegistry
		if err := c.DB.Where("LOWER(name) = ?", strings.ToLower(customType)).First(&existingType).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				newType := models.EventTypeRegistry{Name: customType, CreatedByID: user.UserID}
				c.DB.Create(&newType)
			}
		}
		input.EventType = customType
	}

	// --- 6. Validate visibility ---
	validVisibility := map[string]bool{
		models.VisibilityPublic:     true,
		models.VisibilityPrivate:    true,
		models.VisibilityCohortOnly: true,
		models.VisibilityRoleBased:  true,
	}
	if !validVisibility[input.Visibility] {
		c.Json(w, http.StatusBadRequest, "Invalid visibility option", nil)
		return
	}

	// --- 7. Ensure visibility-specific fields ---
	if input.Visibility == models.VisibilityRoleBased && len(input.TargetRoles) == 0 {
		c.Json(w, http.StatusBadRequest, "Target roles required for RoleBased visibility", nil)
		return
	}
	if input.Visibility == models.VisibilityCohortOnly && len(input.TargetCohorts) == 0 {
		c.Json(w, http.StatusBadRequest, "Target cohorts required for CohortOnly visibility", nil)
		return
	}

	// --- 8. Validate time consistency ---
	if input.EndTime.Before(input.StartTime) {
		c.Json(w, http.StatusBadRequest, "End time must be after start time", nil)
		return
	}

	// --- 9. Marshal JSON fields ---
	targetRolesJSON, _ := json.Marshal(input.TargetRoles)
	targetCohortsJSON, _ := json.Marshal(input.TargetCohorts)

	// --- 10. Create event record ---
	event := models.Event{
		Title:         input.Title,
		Description:   input.Description,
		EventType:     input.EventType,
		StartTime:     input.StartTime,
		EndTime:       input.EndTime,
		Location:      input.Location,
		CreatedByID:   user.UserID,
		CohortRefID:   input.CohortRefID,
		Visibility:    input.Visibility,
		TargetRoles:   datatypes.JSON(targetRolesJSON),
		TargetCohorts: datatypes.JSON(targetCohortsJSON),
	}
	if err := c.DB.Create(&event).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create event", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 11. Notify relevant users based on visibility ---
	var notifyUsers []models.User

	switch input.Visibility {
	case models.VisibilityPublic:
		// Notify all students, mentors, supervisors, admins
		if err := c.DB.Preload("Profile").Preload("Role").
			Joins("JOIN roles ON roles.role_id = users.role_id").
			Where("roles.name IN ?", []string{"Student", "Mentor", "Supervisor", "SystemAdmin", "OpsAdmin"}).
			Where("users.is_active = ?", true).
			Find(&notifyUsers).Error; err != nil {
			log.Printf("[ERROR] Failed to fetch users for public event notifications: %v", err)
		} else {
			log.Printf("[INFO] Notifying %d active users for public event", len(notifyUsers))
		}

	case models.VisibilityCohortOnly:
		if len(input.TargetCohorts) > 0 {
			if err := c.DB.Raw(`
            SELECT DISTINCT u.* FROM users u
            JOIN cohort_users cu ON cu.user_user_id = u.user_id
            WHERE cu.cohort_cohort_id IN ?
        `, input.TargetCohorts).Scan(&notifyUsers).Error; err != nil {
				log.Printf("[ERROR] Failed to fetch cohort users: %v", err)
			}
		}

	case models.VisibilityRoleBased:
		if len(input.TargetRoles) > 0 {
			// join roles table so we can filter by name
			if err := c.DB.Preload("Profile").Preload("Role").
				Joins("JOIN roles ON roles.role_id = users.role_id").
				Where("roles.name IN ?", input.TargetRoles).
				Find(&notifyUsers).Error; err != nil {
				log.Printf("[ERROR] Failed to fetch users for role-based event: %v", err)
			}
		}

	}

	// Send notifications
	for _, u := range notifyUsers {
		go c.NotifyAndTrack(
			u.UserID,
			fmt.Sprintf("New Event: %s", event.Title),
			fmt.Sprintf("Event '%s' has been scheduled by %s %s", event.Title, user.Profile.FirstName, user.Profile.LastName),
			"Event Management",
			"Event",
			&event.ID,
			"Created",
			false,
		)
	}

	// --- 12. Audit trail ---
	c.DB.Create(&models.SystemHistory{
		EntityType: "Event",
		EntityID:   &event.ID,
		Action:     "Created",
		Status:     func() *string { s := "Active"; return &s }(),
		Comment: func() *string {
			s := fmt.Sprintf("Event '%s' created by %s %s", event.Title, user.Profile.FirstName, user.Profile.LastName)
			return &s
		}(),
		ChangedByID: user.UserID,
		CreatedAt:   time.Now(),
	})

	// --- 13. Response payload ---
	resp := map[string]interface{}{
		"event_id":       event.ID,
		"title":          event.Title,
		"description":    event.Description,
		"event_type":     event.EventType,
		"start_time":     event.StartTime,
		"end_time":       event.EndTime,
		"location":       event.Location,
		"cohort_id":      event.CohortRefID,
		"visibility":     event.Visibility,
		"target_roles":   input.TargetRoles,
		"target_cohorts": input.TargetCohorts,
		"created_by": map[string]string{
			"first_name": user.Profile.FirstName,
			"last_name":  user.Profile.LastName,
			"full_name":  user.Profile.FirstName + " " + user.Profile.LastName,
		},
		"created_at": event.CreatedAt,
	}

	c.Json(w, http.StatusCreated, "Event created successfully", map[string]interface{}{"event": resp})
}

//GET EVENTS

func (c *Construct) GetEvents(w http.ResponseWriter, r *http.Request) {
	// --- 1. Authenticate user ---
	viewerUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || viewerUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Profile").Preload("Role").Where("user_uuid = ?", viewerUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	userRole := strings.ToLower(user.Role.Name)

	var events []models.Event
	query := c.DB.Preload("CreatedBy.Profile").Preload("CohortRef")

	// --- Optional: filter by event_type ---
	eventType := strings.TrimSpace(r.URL.Query().Get("event_type"))
	if eventType != "" {
		query = query.Where("LOWER(event_type) = ?", strings.ToLower(eventType))
	}

	if userRole == "systemadmin" || userRole == "opsadmin" {
		// Admins see all events
		if err := query.Order("start_time DESC").Find(&events).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to fetch events", map[string]interface{}{"error": err.Error()})
			return
		}
	} else {
		// --- Regular users: fetch cohorts ---
		var cohortIDs []uint64
		c.DB.Table("cohort_users").Select("cohort_cohort_id").Where("user_user_id = ?", user.UserID).Scan(&cohortIDs)

		// --- Apply visibility rules ---
		if err := query.Where("visibility = ?", "Public").
			Or("visibility = ? AND cohort_ref_id IN ?", "CohortOnly", cohortIDs).
			Or("visibility = ? AND ? = ANY (target_roles::text[])", "RoleBased", user.Role.Name).
			Or("visibility = ? AND ?::text::jsonb <@ target_cohorts", "CohortOnly", cohortIDs). // JSON array contains cohort
			Order("start_time DESC").
			Find(&events).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to fetch events", map[string]interface{}{"error": err.Error()})
			return
		}
	}

	// --- Build response ---
	resp := make([]map[string]interface{}, len(events))
	for i, e := range events {
		resp[i] = map[string]interface{}{
			"event_id":       e.ID,
			"title":          e.Title,
			"description":    e.Description,
			"event_type":     e.EventType,
			"start_time":     e.StartTime,
			"end_time":       e.EndTime,
			"location":       e.Location,
			"visibility":     e.Visibility,
			"cohort_id":      e.CohortRefID,
			"target_roles":   e.TargetRoles,
			"target_cohorts": e.TargetCohorts,
			"created_by": map[string]string{
				"first_name": e.CreatedBy.Profile.FirstName,
				"last_name":  e.CreatedBy.Profile.LastName,
				"full_name":  e.CreatedBy.Profile.FirstName + " " + e.CreatedBy.Profile.LastName,
			},
			"created_at": e.CreatedAt,
		}
	}

	c.Json(w, http.StatusOK, "Events fetched successfully", map[string]interface{}{"events": resp})
}

// GET Events By ID

func (c *Construct) GetEventByID(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr := vars["id"]
	eventID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid event ID", nil)
		return
	}

	viewerUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || viewerUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Profile").Preload("Role").Where("user_uuid = ?", viewerUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	var event models.Event
	if err := c.DB.Preload("CreatedBy.Profile").Preload("CohortRef").First(&event, eventID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Event not found", nil)
		return
	}
	userRole := strings.ToLower(user.Role.Name)

	// --- Visibility check ---
	if userRole != "systemadmin" && userRole != "opsadmin" {
		var cohortIDs []uint64
		c.DB.Table("cohort_users").Select("cohort_cohort_id").Where("user_user_id = ?", user.UserID).Scan(&cohortIDs)

		allowed := false
		switch event.Visibility {
		case "Public":
			allowed = true
		case "CohortOnly":
			for _, cid := range cohortIDs {
				if event.CohortRefID != nil && *event.CohortRefID == cid {
					allowed = true
					break
				}
				if event.TargetCohorts != nil {
					var tcs []uint64
					_ = json.Unmarshal(event.TargetCohorts, &tcs)
					for _, tc := range tcs {
						if tc == cid {
							allowed = true
							break
						}
					}
				}
			}
		case "RoleBased":
			var roles []string
			if event.TargetRoles != nil {
				_ = json.Unmarshal(event.TargetRoles, &roles)
				for _, r := range roles {
					if strings.EqualFold(r, user.Role.Name) {
						allowed = true
						break
					}

				}
			}
		}

		if !allowed {
			c.Json(w, http.StatusForbidden, "You are not allowed to view this event", nil)
			return
		}
	}

	// --- Response ---
	resp := map[string]interface{}{
		"event_id":       event.ID,
		"title":          event.Title,
		"description":    event.Description,
		"event_type":     event.EventType,
		"start_time":     event.StartTime,
		"end_time":       event.EndTime,
		"location":       event.Location,
		"visibility":     event.Visibility,
		"cohort_id":      event.CohortRefID,
		"target_roles":   event.TargetRoles,
		"target_cohorts": event.TargetCohorts,
		"created_by": map[string]string{
			"first_name": event.CreatedBy.Profile.FirstName,
			"last_name":  event.CreatedBy.Profile.LastName,
			"full_name":  event.CreatedBy.Profile.FirstName + " " + event.CreatedBy.Profile.LastName,
		},
		"created_at": event.CreatedAt,
	}

	c.Json(w, http.StatusOK, "Event fetched successfully", map[string]interface{}{"event": resp})
}

// EDIT or UPDATE an Event
func (c *Construct) UpdateEvent(w http.ResponseWriter, r *http.Request) {
	// --- 1. Authenticate user ---
	viewerUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || viewerUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Profile").Preload("Role").
		Where("user_uuid = ?", viewerUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	role := strings.ToLower(user.Role.Name)
	if role != "systemadmin" && role != "opsadmin" {
		c.Json(w, http.StatusForbidden, "Only SystemAdmin and OpsAdmin can update events", nil)
		return
	}

	// --- 2. Get event ID ---
	eventIDStr := chi.URLParam(r, "id")
	if eventIDStr == "" {
		c.Json(w, http.StatusBadRequest, "Event ID is required", nil)
		return
	}

	var event models.Event
	if err := c.DB.First(&event, eventIDStr).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Event not found", nil)
		return
	}

	oldVisibility := event.Visibility

	// --- 3. Parse request body ---
	var input struct {
		Title         *string    `json:"title"`
		Description   *string    `json:"description"`
		EventType     *string    `json:"event_type"`
		StartTime     *time.Time `json:"start_time"`
		EndTime       *time.Time `json:"end_time"`
		Location      *string    `json:"location"`
		CohortRefID   *uint64    `json:"cohort_id"`
		Visibility    *string    `json:"visibility"`
		TargetRoles   []string   `json:"target_roles"`
		TargetCohorts []uint64   `json:"target_cohorts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 4. Validate enums ---
	if input.EventType != nil {
		predefinedTypes := map[string]bool{
			models.EventTypeWorkshop: true,
			models.EventTypeDemo:     true,
			models.EventTypePitchDay: true,
			models.EventTypeSeminar:  true,
		}

		eventType := strings.TrimSpace(*input.EventType)

		if !predefinedTypes[eventType] {
			// Allow custom event types — sanitize & validate
			customType := strings.Title(strings.ToLower(eventType))
			if len(customType) < 3 || len(customType) > 50 {
				c.Json(w, http.StatusBadRequest, "Invalid custom event type length (must be 3–50 chars)", nil)
				return
			}

			// Optional: Store custom event type in registry (for reuse)
			var existingType models.EventTypeRegistry
			if err := c.DB.Where("LOWER(name) = ?", strings.ToLower(customType)).
				First(&existingType).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					newType := models.EventTypeRegistry{
						Name:        customType,
						CreatedByID: user.UserID,
						CreatedAt:   time.Now(),
					}
					c.DB.Create(&newType)
				}
			}

			event.EventType = customType
		} else {
			event.EventType = eventType
		}
	}

	if input.Visibility != nil {
		validVisibility := map[string]bool{
			models.VisibilityPublic:     true,
			models.VisibilityPrivate:    true,
			models.VisibilityCohortOnly: true,
			models.VisibilityRoleBased:  true,
		}
		if !validVisibility[*input.Visibility] {
			c.Json(w, http.StatusBadRequest, "Invalid visibility option", nil)
			return
		}
		event.Visibility = *input.Visibility
	}

	// --- 5. Apply updates ---
	if input.Title != nil {
		event.Title = strings.TrimSpace(*input.Title)
	}
	if input.Description != nil {
		event.Description = input.Description
	}
	if input.StartTime != nil {
		event.StartTime = *input.StartTime
	}
	if input.EndTime != nil {
		event.EndTime = *input.EndTime
	}
	if input.Location != nil {
		event.Location = *input.Location
	}
	if input.CohortRefID != nil {
		event.CohortRefID = input.CohortRefID
	}

	// --- 6. Validate time logic ---
	if event.EndTime.Before(event.StartTime) {
		c.Json(w, http.StatusBadRequest, "End time must be after start time", nil)
		return
	}

	// --- 7. Visibility & audience management ---
	if event.Visibility == models.VisibilityRoleBased {
		if len(input.TargetRoles) == 0 {
			c.Json(w, http.StatusBadRequest, "Target roles required for RoleBased visibility", nil)
			return
		}
		targetRolesJSON, _ := json.Marshal(input.TargetRoles)
		event.TargetRoles = datatypes.JSON(targetRolesJSON)
		event.TargetCohorts = datatypes.JSON([]byte("[]")) // clear cohorts
	}

	if event.Visibility == models.VisibilityCohortOnly {
		if len(input.TargetCohorts) == 0 {
			c.Json(w, http.StatusBadRequest, "Target cohorts required for CohortOnly visibility", nil)
			return
		}
		targetCohortsJSON, _ := json.Marshal(input.TargetCohorts)
		event.TargetCohorts = datatypes.JSON(targetCohortsJSON)
		event.TargetRoles = datatypes.JSON([]byte("[]")) // clear roles
	}

	if event.Visibility == models.VisibilityPublic {
		event.TargetRoles = datatypes.JSON([]byte("[]"))
		event.TargetCohorts = datatypes.JSON([]byte("[]"))
	}

	// --- 8. Save changes ---
	if err := c.DB.Save(&event).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update event", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 9. Determine new audience & re-notify ---
	var notifyUsers []models.User

	switch event.Visibility {
	case models.VisibilityPublic:
		// Notify all
		c.DB.Preload("Profile").Preload("Role").
			Joins("JOIN roles ON roles.role_id = users.role_id").
			Where("roles.name IN ?", []string{"Student", "Mentor", "Supervisor", "SystemAdmin", "OpsAdmin"}).
			Where("users.is_active = ?", true).Find(&notifyUsers)

	case models.VisibilityCohortOnly:
		var targetCohorts []uint64
		_ = json.Unmarshal(event.TargetCohorts, &targetCohorts)
		if len(targetCohorts) > 0 {
			c.DB.Raw(`SELECT u.* FROM users u
				JOIN cohort_users cu ON cu.user_user_id = u.user_id
				WHERE cu.cohort_cohort_id IN ?`, targetCohorts).Scan(&notifyUsers)
		}

	case models.VisibilityRoleBased:
		var targetRoles []string
		_ = json.Unmarshal(event.TargetRoles, &targetRoles)
		if len(targetRoles) > 0 {
			c.DB.Preload("Profile").Preload("Role").
				Joins("JOIN roles r ON r.role_id = users.role_id").
				Where("LOWER(r.name) IN ?", targetRoles).Find(&notifyUsers)
		}
	}

	// Notify only if visibility changed or major details changed
	if oldVisibility != event.Visibility || input.Title != nil || input.StartTime != nil {
		for _, u := range notifyUsers {
			go c.NotifyAndTrack(
				u.UserID,
				fmt.Sprintf("Event Updated: %s", event.Title),
				fmt.Sprintf("Event '%s' has been updated by %s %s", event.Title, user.Profile.FirstName, user.Profile.LastName),
				"Event Management",
				"Event",
				&event.ID,
				"Updated",
				false,
			)
		}
	}

	// --- 10. Record system history ---
	c.DB.Create(&models.SystemHistory{
		EntityType: "Event",
		EntityID:   &event.ID,
		Action:     "Updated",
		Status:     func() *string { s := "Active"; return &s }(),
		Comment: func() *string {
			s := fmt.Sprintf("Event '%s' updated by %s %s", event.Title, user.Profile.FirstName, user.Profile.LastName)
			return &s
		}(),
		ChangedByID: user.UserID,
		CreatedAt:   time.Now(),
	})

	// --- 11. Respond ---
	c.Json(w, http.StatusOK, "Event updated successfully", map[string]interface{}{
		"event_id":       event.ID,
		"title":          event.Title,
		"description":    event.Description,
		"event_type":     event.EventType,
		"start_time":     event.StartTime,
		"end_time":       event.EndTime,
		"location":       event.Location,
		"visibility":     event.Visibility,
		"target_roles":   input.TargetRoles,
		"target_cohorts": input.TargetCohorts,
	})
}

// manage Events By Admins

func (c *Construct) ManageEvents(w http.ResponseWriter, r *http.Request) {
	// --- 1. Parse input ---
	var input struct {
		EventIDs []uint64 `json:"event_ids"`
		Action   string   `json:"action"` // "archive", "unarchive", "delete"
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if len(input.EventIDs) == 0 || input.Action == "" {
		c.Json(w, http.StatusBadRequest, "event_ids and action are required", nil)
		return
	}

	// --- 2. Authenticate user ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Role").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 3. Fetch events ---
	var events []models.Event
	if err := c.DB.Where("id IN ?", input.EventIDs).Find(&events).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch events", map[string]interface{}{"error": err.Error()})
		return
	}

	if len(events) == 0 {
		c.Json(w, http.StatusNotFound, "No events found for the given IDs", nil)
		return
	}

	// --- 4. Perform actions ---
	var affected []map[string]interface{}
	for _, event := range events {
		switch strings.ToLower(input.Action) {
		case "archive":
			if user.Role.Name != "SystemAdmin" && user.Role.Name != "OpsAdmin" {
				c.Json(w, http.StatusForbidden, "Only SystemAdmin and OpsAdmin can archive events", nil)
				return
			}
			event.IsArchived = true
			event.UpdatedAt = time.Now()
			c.DB.Save(&event)
			c.NotifyAndTrack(user.UserID, "Event Archived",
				fmt.Sprintf("Event '%s' was archived", event.Title),
				"Event Archive", "Event", &event.ID, "Archived", false)

			affected = append(affected, map[string]interface{}{
				"id":    event.ID,
				"title": event.Title,
				"state": "Archived",
			})

		case "unarchive":
			if user.Role.Name != "SystemAdmin" && user.Role.Name != "OpsAdmin" {
				c.Json(w, http.StatusForbidden, "Only SystemAdmin and OpsAdmin can unarchive events", nil)
				return
			}
			event.IsArchived = false
			event.UpdatedAt = time.Now()
			c.DB.Save(&event)
			c.NotifyAndTrack(user.UserID, "Event Unarchived",
				fmt.Sprintf("Event '%s' was unarchived", event.Title),
				"Event Unarchive", "Event", &event.ID, "Unarchived", false)

			affected = append(affected, map[string]interface{}{
				"id":    event.ID,
				"title": event.Title,
				"state": "Unarchived",
			})

		case "delete":
			if user.Role.Name != "SystemAdmin" && user.Role.Name != "OpsAdmin" {
				c.Json(w, http.StatusForbidden, "Only SystemAdmin and OpsAdmin can delete events", nil)
				return
			}
			c.DB.Unscoped().Delete(&event)
			c.NotifyAndTrack(user.UserID, "Event Deleted Permanently",
				fmt.Sprintf("Event '%s' was permanently deleted", event.Title),
				"Event Deletion", "Event", &event.ID, "Deleted", true)

			affected = append(affected, map[string]interface{}{
				"id":    event.ID,
				"title": event.Title,
				"state": "Deleted",
			})

		default:
			c.Json(w, http.StatusBadRequest, "Invalid action, must be 'archive', 'unarchive', or 'delete'", nil)
			return
		}
	}

	// --- 5. Return structured response ---
	resp := map[string]interface{}{
		"action":   input.Action,
		"count":    len(affected),
		"affected": affected,
	}

	c.Json(w, http.StatusOK, fmt.Sprintf("Action '%s' performed successfully", input.Action), resp)
}
