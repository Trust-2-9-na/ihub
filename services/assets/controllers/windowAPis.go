package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"web/services/assets/models"
	"strings"
)
//============Creating proposal submission window with dynamic filtering ============

func (c *Construct) CreateSubmissionWindow(w http.ResponseWriter, r *http.Request) {
    // ---------------------------
    // Parse JSON payload
    // ---------------------------
    var payload struct {
        Title       string  `json:"title"`
        StartDate   string  `json:"start_date"` // "2006-01-02"
        Deadline    string  `json:"deadline"`
        School      *string `json:"school,omitempty"`
        Program     *string `json:"program,omitempty"`
        YearOfStudy *string `json:"year_of_study,omitempty"`
    }

    if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
        c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
        return
    }

    // ---------------------------
    // Validate dates
    // ---------------------------
    start, err := time.Parse("2006-01-02", payload.StartDate)
    if err != nil {
        c.Json(w, http.StatusBadRequest, "Invalid start_date format", nil)
        return
    }

    deadline, err := time.Parse("2006-01-02", payload.Deadline)
    if err != nil {
        c.Json(w, http.StatusBadRequest, "Invalid deadline format", nil)
        return
    }

    if payload.Title == "" || deadline.Before(start) {
        c.Json(w, http.StatusBadRequest, "Invalid title or dates", nil)
        return
    }

    // ---------------------------
    // Get supervisor/admin info
    // ---------------------------
    userUUIDCtx := r.Context().Value("user_uuid")
    if userUUIDCtx == nil {
        c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
        return
    }
    userUUID := userUUIDCtx.(string)

    var user models.User
    if err := c.DB.Preload("Role").Preload("Profile").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
        c.Json(w, http.StatusInternalServerError, "Could not fetch user", map[string]interface{}{"error": err.Error()})
        return
    }

    role := strings.ToLower(user.Role.Name)
    if role != "supervisor" && role != "admin" {
        c.Json(w, http.StatusForbidden, "Only supervisors or admins can create submission windows", nil)
        return
    }

    // ---------------------------
    // Create the window
    // ---------------------------
    window := models.ProposalSubmissionWindow{
        Title:       payload.Title,
        StartDate:   start,
        Deadline:    deadline,
        CreatedByID: user.UserID,
        School:      payload.School,      // <- must exist in the model
        Program:     payload.Program,     // <- must exist in the model
        YearOfStudy: payload.YearOfStudy, // <- must exist in the model
    }

    if err := c.DB.Create(&window).Error; err != nil {
        c.Json(w, http.StatusInternalServerError, "Failed to create submission window", map[string]interface{}{"error": err.Error()})
        return
    }

    // ---------------------------
    // Dynamic student filtering
    // ---------------------------
    query := c.DB.Preload("Profile").Joins("JOIN student_profiles sp ON sp.user_id = users.user_id")

    if payload.School != nil && *payload.School != "" {
        query = query.Where("sp.school = ?", *payload.School)
    }
    if payload.Program != nil && *payload.Program != "" {
        query = query.Where("sp.program = ?", *payload.Program)
    }
    if payload.YearOfStudy != nil && *payload.YearOfStudy != "" {
        query = query.Where("sp.year_of_study = ?", *payload.YearOfStudy)
    }

    query = query.Where("users.deleted_at IS NULL")

    var students []models.User
    if err := query.Find(&students).Error; err != nil {
        c.Json(w, http.StatusInternalServerError, "Failed to fetch students", map[string]interface{}{"error": err.Error()})
        return
    }

    // ---------------------------
    // Send notifications
    // ---------------------------
    for _, student := range students {
        message := fmt.Sprintf(
            "Dear %s, a new proposal submission window '%s' is now open.\nSubmit your proposal between %s and %s.",
            student.Profile.FirstName,
            window.Title,
            start.Format("02 Jan 2006"),
            deadline.Format("02 Jan 2006"),
        )
        _ = c.CreateNotification(student.UserID, "Proposal Submission Open", message)
    }

    // ---------------------------
    // Audit log
    // ---------------------------
    _ = c.LogAudit(user.UserID, "create_submission_window", nil, &window.WindowID, nil, map[string]interface{}{
        "title": window.Title,
    })

    // ---------------------------
    // Response
    // ---------------------------
    resp := map[string]interface{}{
        "window_id":  window.WindowID,
        "title":      window.Title,
        "start_date": window.StartDate.Format("2006-01-02"),
        "deadline":   window.Deadline.Format("2006-01-02"),
        "created_by": user.Profile.FirstName + " " + user.Profile.LastName,
        "created_at": window.CreatedAt.Format("2006-01-02 15:04:05"),
        "updated_at": window.UpdatedAt.Format("2006-01-02 15:04:05"),
        "school":     payload.School,
        "program":    payload.Program,
        "year_of_study": payload.YearOfStudy,
        "sent_to":    len(students),
    }

    c.Json(w, http.StatusCreated, "Submission window created successfully", resp)
}

//==============================GETALL======================================
func (c *Construct) GetSubmissionWindows(w http.ResponseWriter, r *http.Request) {
    userUUIDCtx := r.Context().Value("user_uuid")
    if userUUIDCtx == nil {
        c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
        return
    }
    userUUID := userUUIDCtx.(string)

    var user models.User
    if err := c.DB.Preload("Role").Preload("Profile").
        Joins("LEFT JOIN student_profiles sp ON sp.user_id = users.user_id").
        Where("user_uuid = ?", userUUID).
        First(&user).Error; err != nil {
        c.Json(w, http.StatusInternalServerError, "Failed to fetch user", map[string]interface{}{"error": err.Error()})
        return
    }

    role := strings.ToLower(user.Role.Name)
    var windows []models.ProposalSubmissionWindow

    switch role {
    case "admin":
        // Admin can see all windows
        if err := c.DB.Preload("CreatedBy.Profile").Find(&windows).Error; err != nil {
            c.Json(w, http.StatusInternalServerError, "Failed to fetch windows", map[string]interface{}{"error": err.Error()})
            return
        }

    case "supervisor":
        // Supervisor only sees what they created
        if err := c.DB.Preload("CreatedBy.Profile").Where("created_by_id = ?", user.UserID).Find(&windows).Error; err != nil {
            c.Json(w, http.StatusInternalServerError, "Failed to fetch windows", map[string]interface{}{"error": err.Error()})
            return
        }

    case "student":
        // Students see windows relevant to them
        var studentProfile models.StudentProfile
        if err := c.DB.Where("user_id = ?", user.UserID).First(&studentProfile).Error; err != nil {
            c.Json(w, http.StatusInternalServerError, "Failed to fetch student profile", map[string]interface{}{"error": err.Error()})
            return
        }

        // Get all windows
        var allWindows []models.ProposalSubmissionWindow
        if err := c.DB.Preload("CreatedBy.Profile").Find(&allWindows).Error; err != nil {
            c.Json(w, http.StatusInternalServerError, "Failed to fetch windows", map[string]interface{}{"error": err.Error()})
            return
        }

        // Filter manually — if window has filter values, match against student's profile
        for _, w := range allWindows {
            match := true

            if w.School != nil && *w.School != "" && studentProfile.School != *w.School {
                match = false
            }
            if w.Program != nil && *w.Program != "" && studentProfile.Program != *w.Program {
                match = false
            }
            if w.YearOfStudy != nil && *w.YearOfStudy != "" && studentProfile.YearOfStudy != *w.YearOfStudy {
                match = false
            }

            if match {
                windows = append(windows, w)
            }
        }

    default:
        c.Json(w, http.StatusForbidden, "Unauthorized role", nil)
        return
    }

    // Format response
    var resp []map[string]interface{}
    for _, win := range windows {
        resp = append(resp, map[string]interface{}{
            "window_id":  win.WindowID,
            "title":      win.Title,
            "start_date": win.StartDate.Format("2006-01-02"),
            "deadline":   win.Deadline.Format("2006-01-02"),
            "created_by": win.CreatedBy.Profile.FirstName + " " + win.CreatedBy.Profile.LastName,
            "created_at": win.CreatedAt.Format("2006-01-02 15:04:05"),
            "updated_at": win.UpdatedAt.Format("2006-01-02 15:04:05"),
            "school":     win.School,
            "program":    win.Program,
            "year":       win.YearOfStudy,
        })
    }

   c.Json(w, http.StatusOK, "Submission windows fetched successfully", map[string]interface{}{
    "windows": resp,
})

}
