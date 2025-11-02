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
)

// RegisterForEvents allows a user to register for multiple events at once
func (c *Construct) RegisterForEvents(w http.ResponseWriter, r *http.Request) {
	// --- 1. Get authenticated user ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Profile").Preload("Role").
		Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 2. Parse input ---
	var input struct {
		EventIDs []uint64 `json:"event_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	if len(input.EventIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "event_ids are required", nil)
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

	// --- 4. Register for each event ---
	var registered []map[string]interface{}
	var failed []map[string]interface{}

	for _, event := range events {
		allowed := false

		// --- Visibility check ---
		switch event.Visibility {
		case "Public":
			allowed = true
		case "Private":
			failed = append(failed, map[string]interface{}{"event_id": event.ID, "reason": "Event is private"})
		case "CohortOnly":
			if event.CohortRefID != nil {
				var count int64
				c.DB.Table("cohort_users").
					Where("cohort_cohort_id = ? AND user_user_id = ?", *event.CohortRefID, user.UserID).
					Count(&count)
				if count > 0 || user.Role.Name == "SystemAdmin" || user.Role.Name == "OpsAdmin" {
					allowed = true
				} else {
					failed = append(failed, map[string]interface{}{"event_id": event.ID, "reason": "Not in assigned cohort"})
				}
			} else {
				failed = append(failed, map[string]interface{}{"event_id": event.ID, "reason": "Event has no cohort assigned"})
			}
		case "RoleBased":
			var targetRoles []string
			if err := json.Unmarshal(event.TargetRoles, &targetRoles); err != nil {
				failed = append(failed, map[string]interface{}{"event_id": event.ID, "reason": "Failed to parse target roles"})
			} else {
				for _, r := range targetRoles {
					if strings.EqualFold(r, user.Role.Name) || user.Role.Name == "SystemAdmin" || user.Role.Name == "OpsAdmin" {
						allowed = true
						break
					}
				}
				if !allowed {
					failed = append(failed, map[string]interface{}{"event_id": event.ID, "reason": "Role not allowed"})
				}
			}
		default:
			failed = append(failed, map[string]interface{}{"event_id": event.ID, "reason": "Unknown visibility"})
		}

		if !allowed {
			continue
		}

		// --- Check if already registered ---
		var attendance models.EventAttendance
		if err := c.DB.Where("event_ref_id = ? AND attendee_ref_id = ?", event.ID, user.UserID).First(&attendance).Error; err == nil {
			failed = append(failed, map[string]interface{}{"event_id": event.ID, "reason": "Already registered"})
			continue
		}

		// --- Create registration ---
		newAttendance := models.EventAttendance{
			EventRefID:    event.ID,
			AttendeeRefID: user.UserID,
			RegisteredAt:  time.Now(),
			Attended:      false,
		}
		if err := c.DB.Create(&newAttendance).Error; err != nil {
			failed = append(failed, map[string]interface{}{"event_id": event.ID, "reason": "DB error"})
			continue
		}

		// --- Notify ---
		// --- Notify ---
		fullName := strings.TrimSpace(user.Profile.FirstName + " " + user.Profile.LastName)
		if fullName == "" {
			fullName = user.Username // fallback if name is empty
		}

		go c.NotifyAndTrack(
			user.UserID,
			"Event Registration",
			fmt.Sprintf("User %s registered for event '%s'", fullName, event.Title),
			"Event Management",
			"EventAttendance",
			&newAttendance.ID,
			"Registered",
			false,
		)

		// --- Append success ---
		registered = append(registered, map[string]interface{}{
			"event_id":      event.ID,
			"event_title":   event.Title,
			"attendee_id":   user.UserID,
			"attendee_name": fullName,
			"attendee_role": user.Role.Name,
			"registered_at": newAttendance.RegisteredAt,
		})
	}

	// --- 5. Response ---
	c.Json(w, http.StatusCreated, "Event registration processed", map[string]interface{}{
		"registered": registered,
		"failed":     failed,
	})
}

// Unregister from Events
func (c *Construct) UnregisterFromEvents(w http.ResponseWriter, r *http.Request) {
	// --- 1. Get authenticated user ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Profile").Preload("Role").
		Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- 2. Parse input ---
	var input struct {
		EventIDs []uint64 `json:"event_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	if len(input.EventIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "event_ids are required", nil)
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

	// --- 4. Attempt to unregister from each event ---
	var unregistered []map[string]interface{}
	var failed []map[string]interface{}

	for _, event := range events {
		// Check if user is registered
		var attendance models.EventAttendance
		err := c.DB.Where("event_ref_id = ? AND attendee_ref_id = ?", event.ID, user.UserID).First(&attendance).Error
		if err != nil {
			failed = append(failed, map[string]interface{}{"event_id": event.ID, "reason": "Not registered for this event"})
			continue
		}

		// Delete attendance
		if err := c.DB.Delete(&attendance).Error; err != nil {
			failed = append(failed, map[string]interface{}{"event_id": event.ID, "reason": "DB error"})
			continue
		}

		// Notify & track
		fullName := strings.TrimSpace(user.Profile.FirstName + " " + user.Profile.LastName)
		if fullName == "" {
			fullName = user.Username
		}

		go c.NotifyAndTrack(
			user.UserID,
			"Event Unregistration",
			fmt.Sprintf("User %s unregistered from event '%s'", fullName, event.Title),
			"Event Management",
			"EventAttendance",
			&attendance.ID,
			"Unregistered",
			false,
		)

		// Append success
		unregistered = append(unregistered, map[string]interface{}{
			"event_id":      event.ID,
			"event_title":   event.Title,
			"attendee_id":   user.UserID,
			"attendee_name": fullName,
			"attendee_role": user.Role.Name,
		})
	}

	// --- 5. Response ---
	c.Json(w, http.StatusOK, "Event unregistration processed", map[string]interface{}{
		"unregistered": unregistered,
		"failed":       failed,
	})
}

// MarkEventAttendance marks users as attended for a given event
func (c *Construct) MarkEventAttendance(w http.ResponseWriter, r *http.Request) {
	// --- 1. Authenticate admin ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var admin models.User
	if err := c.DB.Preload("Role").Where("user_uuid = ?", userUUID).First(&admin).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch user", nil)
		return
	}

	if admin.Role.Name != "SystemAdmin" && admin.Role.Name != "OpsAdmin" {
		c.Json(w, http.StatusForbidden, "Only admins can mark attendance", nil)
		return
	}

	// --- 2. Parse request body ---
	var input struct {
		EventID uint64   `json:"event_id"`
		UserIDs []uint64 `json:"user_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid input", map[string]interface{}{"error": err.Error()})
		return
	}

	if input.EventID == 0 || len(input.UserIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "event_id and user_ids are required", nil)
		return
	}

	// --- 3. Track successes and failures ---
	var marked []map[string]interface{}
	var failed []map[string]interface{}

	for _, uid := range input.UserIDs {
		var attendance models.EventAttendance
		if err := c.DB.Preload("AttendeeRef.Role").Preload("AttendeeRef.Profile").
			Where("event_ref_id = ? AND attendee_ref_id = ?", input.EventID, uid).
			First(&attendance).Error; err != nil {
			failed = append(failed, map[string]interface{}{"user_id": uid, "error": "Attendance record not found"})
			continue
		}

		now := time.Now()
		attendance.Attended = true
		attendance.CheckInAt = &now

		if err := c.DB.Save(&attendance).Error; err != nil {
			failed = append(failed, map[string]interface{}{"user_id": uid, "error": err.Error()})
			continue
		}

		// --- Notify & track ---
		go c.NotifyAndTrack(
			admin.UserID,
			"Attendance Marked",
			fmt.Sprintf("Attendance marked for user %d for event %d", uid, input.EventID),
			"Event Management",
			"EventAttendance",
			&attendance.ID,
			"Attended",
			false,
		)

		// --- Build marked info with full user details ---
		user := attendance.AttendeeRef
		fullName := strings.TrimSpace(user.Profile.FirstName + " " + user.Profile.LastName)
		if fullName == "" {
			fullName = user.Username
		}

		roleName := "Unknown"
		if user.Role.Name != "" {
			roleName = user.Role.Name
		}

		marked = append(marked, map[string]interface{}{
			"user_id":       uid,
			"attendee_name": fullName,
			"attendee_role": roleName,
			"attended_at":   now,
		})
	}

	// --- 4. Respond with summary ---
	status := http.StatusOK
	message := "Attendance marked successfully"
	if len(marked) == 0 {
		status = http.StatusBadRequest
		message = "No attendance could be marked"
	} else if len(failed) > 0 {
		message = "Some attendance records could not be marked"
	}

	c.Json(w, status, message, map[string]interface{}{
		"event_id": input.EventID,
		"marked":   marked,
		"failed":   failed,
	})
}

func (c *Construct) GetEventAttendees(w http.ResponseWriter, r *http.Request) {
	// --- 1. Parse filters from query parameters ---
	query := r.URL.Query()
	eventIDStr := query.Get("event_id")
	attendeeName := strings.TrimSpace(query.Get("attendee_name"))
	roleFilter := strings.TrimSpace(query.Get("attendee_role"))

	var attendances []models.EventAttendance
	dbQuery := c.DB.Preload("AttendeeRef.Role").Preload("AttendeeRef.Profile").Preload("EventRef")

	if eventIDStr != "" {
		eventID, err := strconv.ParseUint(eventIDStr, 10, 64)
		if err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid event_id", nil)
			return
		}
		dbQuery = dbQuery.Where("event_ref_id = ?", eventID)
	}

	if err := dbQuery.Find(&attendances).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch attendees", nil)
		return
	}

	// --- 2. Build response with filters ---
	var attendeeList []map[string]interface{}
	for _, a := range attendances {
		user := a.AttendeeRef
		ev := a.EventRef

		fullName := user.Username
		if user.Profile.FirstName != "" || user.Profile.LastName != "" {
			fullName = strings.TrimSpace(user.Profile.FirstName + " " + user.Profile.LastName)
		}

		roleName := "Unknown"
		if user.Role.Name != "" {
			roleName = user.Role.Name
		}

		// Apply optional filters
		if attendeeName != "" && !strings.Contains(strings.ToLower(fullName), strings.ToLower(attendeeName)) {
			continue
		}
		if roleFilter != "" && !strings.EqualFold(roleName, roleFilter) {
			continue
		}

		attendeeList = append(attendeeList, map[string]interface{}{
			"attendee_id":   user.UserID,
			"attendee_name": fullName,
			"attendee_role": roleName,
			"registered_at": a.RegisteredAt,
			"attended":      a.Attended,
			"check_in_at":   a.CheckInAt,
			"event_id":      ev.ID,
			"event_title":   ev.Title,
			"event_type":    ev.EventType,
			"start_time":    ev.StartTime,
			"end_time":      ev.EndTime,
			"location":      ev.Location,
		})
	}

	c.Json(w, http.StatusOK, "Event attendees fetched", map[string]interface{}{
		"attendees": attendeeList,
	})
}

//get attended users

func (c *Construct) GetAttendedUsers(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	eventIDStr := query.Get("event_id")

	dbQuery := c.DB.
		Preload("AttendeeRef.Role").
		Preload("AttendeeRef.Profile").
		Preload("EventRef").
		Where("attended = ?", true)

	// Optional filtering by event_id
	if eventIDStr != "" {
		eventID, err := strconv.ParseUint(eventIDStr, 10, 64)
		if err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid event_id", nil)
			return
		}
		dbQuery = dbQuery.Where("event_ref_id = ?", eventID)
	}

	var attendances []models.EventAttendance
	if err := dbQuery.Find(&attendances).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch attended users", nil)
		return
	}

	var attendedList []map[string]interface{}
	for _, a := range attendances {
		user := a.AttendeeRef
		ev := a.EventRef

		fullName := strings.TrimSpace(user.Profile.FirstName + " " + user.Profile.LastName)
		if fullName == "" {
			fullName = user.Username
		}

		attendedList = append(attendedList, map[string]interface{}{
			"user_id":       user.UserID,
			"full_name":     fullName,
			"role":          user.Role.Name,
			"attended_at":   a.CheckInAt,
			"event_id":      ev.ID,
			"event_title":   ev.Title,
			"event_type":    ev.EventType,
			"location":      ev.Location,
			"start_time":    ev.StartTime,
			"end_time":      ev.EndTime,
		})
	}

	c.Json(w, http.StatusOK, "List of attendees fetched successfully", map[string]interface{}{
		"attended": attendedList,
	})
}


// Get Absent Users

func (c *Construct) GetAbsentUsers(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	eventIDStr := query.Get("event_id")

	dbQuery := c.DB.
		Preload("AttendeeRef.Role").
		Preload("AttendeeRef.Profile").
		Preload("EventRef").
		Where("attended = ?", false)

	// Optional filtering by event_id
	if eventIDStr != "" {
		eventID, err := strconv.ParseUint(eventIDStr, 10, 64)
		if err != nil {
			c.Json(w, http.StatusBadRequest, "Invalid event_id", nil)
			return
		}
		dbQuery = dbQuery.Where("event_ref_id = ?", eventID)
	}

	var attendances []models.EventAttendance
	if err := dbQuery.Find(&attendances).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch absent users", nil)
		return
	}

	var absentList []map[string]interface{}
	for _, a := range attendances {
		user := a.AttendeeRef
		ev := a.EventRef

		fullName := strings.TrimSpace(user.Profile.FirstName + " " + user.Profile.LastName)
		if fullName == "" {
			fullName = user.Username
		}

		absentList = append(absentList, map[string]interface{}{
			"user_id":       user.UserID,
			"full_name":     fullName,
			"role":          user.Role.Name,
			"registered_at": a.RegisteredAt,
			"event_id":      ev.ID,
			"event_title":   ev.Title,
			"event_type":    ev.EventType,
			"location":      ev.Location,
			"start_time":    ev.StartTime,
			"end_time":      ev.EndTime,
		})
	}

	c.Json(w, http.StatusOK, "List of absent users fetched successfully", map[string]interface{}{
		"absent": absentList,
	})
}
