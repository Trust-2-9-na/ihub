package controllers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"web/services/assets/models"

	"gorm.io/gorm"
)

func (c *Construct) GetStudentProgressItems(w http.ResponseWriter, r *http.Request) {
	// --- Auth ---
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, fmt.Sprintf("%v", err), nil)
		return
	}
	role := strings.ToLower(currentUser.Role.Name)

	// --- Query Parameters ---
	entityID, _ := c.GetUintParam(r, "id")

	var cohortID uint64
	if cohortStr := r.URL.Query().Get("cohort_id"); cohortStr != "" {
		fmt.Sscan(cohortStr, &cohortID)
	}

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

	reportStatus := r.URL.Query().Get("report_status")

	var weekStart, weekEnd time.Time
	if ws := r.URL.Query().Get("week_start"); ws != "" {
		weekStart, _ = time.Parse("2006-01-02", ws)
	}
	if we := r.URL.Query().Get("week_end"); we != "" {
		weekEnd, _ = time.Parse("2006-01-02", we)
	}

	// --- Check if archived items should be included ---
	includeArchived := r.URL.Query().Get("include_archived") == "true"

	// --- Base Query ---
	query := c.DB.Model(&models.ProgressEntity{}).
		Preload("EntityCohort").
		Preload("Items.AssignedTo.Profile").
		Preload("Items.CreatedBy.Profile").
		Preload("Items.CreatedBy.Role").
		Preload("Items.TeamDetails").
		Preload("Items.WeeklyReports", func(db *gorm.DB) *gorm.DB {
			if reportStatus != "" {
				db = db.Where("status = ?", reportStatus)
			}
			if !weekStart.IsZero() {
				db = db.Where("week_start >= ?", weekStart)
			}
			if !weekEnd.IsZero() {
				db = db.Where("week_end <= ?", weekEnd)
			}
			return db.Preload("ProgressItems").Preload("Comments")
		})

	// --- Role-based Filtering (use entity_cohort_id) ---
	switch role {
	case "opsadmin", "systemadmin":
		if entityID > 0 {
			query = query.Where("id = ?", entityID)
		} else if cohortID > 0 {
			query = query.Where("entity_cohort_id = ?", cohortID)
		}
		// Filter individual items only (exclude team items and archived items)
		preloadCondition := "team_ref_id IS NULL"
		if !includeArchived {
			preloadCondition += " AND is_archived = false"
		}
		query = query.Preload("Items", preloadCondition)

	case "supervisor":
		cohortIDs := c.getAssignedCohorts(currentUser.UserID, "Supervisor")
		if len(cohortIDs) == 0 {
			c.Json(w, http.StatusOK, "No cohorts found", map[string]interface{}{"entities": []interface{}{}})
			return
		}
		// Filter individual items only (exclude team items and archived items)
		preloadCondition := "team_ref_id IS NULL"
		if !includeArchived {
			preloadCondition += " AND is_archived = false"
		}
		query = query.Where("entity_cohort_id IN ?", cohortIDs).
			Preload("Items", preloadCondition)

	case "mentor":
		cohortIDs := c.getAssignedCohorts(currentUser.UserID, "Mentor")
		if len(cohortIDs) == 0 {
			c.Json(w, http.StatusOK, "No cohorts found", map[string]interface{}{"entities": []interface{}{}})
			return
		}
		query = query.Where("entity_cohort_id IN ?", cohortIDs)

		// Filter only progress items for students assigned to this mentor
		var studentIDs []uint64
		c.DB.Model(&models.MentorStudentAssignment{}).
			Where("mentor_ref_id = ? AND deleted_at IS NULL", currentUser.UserID).
			Pluck("student_ref_id", &studentIDs)

		if len(studentIDs) == 0 {
			c.Json(w, http.StatusOK, "No students assigned", map[string]interface{}{"entities": []interface{}{}})
			return
		}

		// Filter individual items only (exclude team items and archived items)
		preloadCondition := "created_by_id IN ? AND team_ref_id IS NULL"
		if !includeArchived {
			preloadCondition += " AND is_archived = false"
		}
		query = query.Preload("Items", preloadCondition, studentIDs)

	case "student":
		cohortIDs := c.getAssignedCohorts(currentUser.UserID, "Student")
		if len(cohortIDs) == 0 {
			c.Json(w, http.StatusOK, "No cohorts found", map[string]interface{}{"entities": []interface{}{}})
			return
		}

		// Filter individual items only (exclude team items and archived items)
		preloadCondition := "created_by_id = ? AND team_ref_id IS NULL"
		if !includeArchived {
			preloadCondition += " AND is_archived = false"
		}
		query = query.Where("entity_cohort_id IN ?", cohortIDs).
			Preload("Items", preloadCondition, currentUser.UserID)

	default:
		c.Json(w, http.StatusForbidden, "Unauthorized role", nil)
		return
	}

	// --- Fetch Entities ---
	var entities []models.ProgressEntity
	if err := query.Limit(limit).Offset(offset).Find(&entities).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch entities", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Build Response ---
	resp := make([]map[string]interface{}, 0, len(entities))
	for _, e := range entities {
		entityMap := map[string]interface{}{
			"id":            e.ID,
			"entity_name":   e.EntityName,
			"entity_type":   e.EntityType,
			"status":        e.Status,
			"performance":   e.Performance,
			"progress_type": e.ProgressType,
			"is_archived":   e.IsArchived,
			"cohort_id":     e.EntityCohortID,
		}

		itemsResp := make([]map[string]interface{}, 0, len(e.Items))
		for _, item := range e.Items {
			// Skip team items - they should be fetched via GetTeamProgressEntities
			if item.TeamRefID != nil {
				continue
			}

			assignedTo := map[string]interface{}{}
			if item.AssignedTo != nil {
				assignedTo = map[string]interface{}{
					"id":         item.AssignedTo.UserID,
					"first_name": item.AssignedTo.Profile.FirstName,
					"last_name":  item.AssignedTo.Profile.LastName,
					"email":      item.AssignedTo.Email,
				}
			}

			// Submitted by (creator) - full name and role
			submittedBy := map[string]interface{}{}
			if item.CreatedBy != nil {
				fullName := ""
				if item.CreatedBy.Profile.ProfileID != 0 {
					fullName = fmt.Sprintf("%s %s",
						strings.TrimSpace(item.CreatedBy.Profile.FirstName),
						strings.TrimSpace(item.CreatedBy.Profile.LastName))
					fullName = strings.TrimSpace(fullName)
				}
				if fullName == "" && item.CreatedBy.Username != "" {
					fullName = item.CreatedBy.Username
				}

				roleName := ""
				if item.CreatedBy.Role.RoleID != 0 {
					roleName = item.CreatedBy.Role.Name
				}

				submittedBy = map[string]interface{}{
					"id":         item.CreatedBy.UserID,
					"full_name":  fullName,
					"first_name": "",
					"last_name":  "",
					"email":      item.CreatedBy.Email,
					"role":       roleName,
				}
				if item.CreatedBy.Profile.ProfileID != 0 {
					submittedBy["first_name"] = item.CreatedBy.Profile.FirstName
					submittedBy["last_name"] = item.CreatedBy.Profile.LastName
				}
			}

			// Weekly reports collected above, but you asked not to return them yet
			itemsResp = append(itemsResp, map[string]interface{}{
				"id":              item.ID,
				"phase_name":      item.PhaseName,
				"student_status":  item.StudentStatus,
				"verified_status": item.VerifiedStatus,
				"weight":          item.Weight,
				"performance":     item.Performance,
				"progress_type":   item.ProgressType,
				"is_archived":     item.IsArchived,
				"is_team_task":    false, // This is an individual task
				"team_ref_id":     nil,
				"assigned_to":     assignedTo,
				"submitted_by":    submittedBy, // Full name and role
				"created_by":      submittedBy, // Keep for backward compatibility
				// "weekly_reports": reportsResp, // omitted as requested
			})
		}

		entityMap["items"] = itemsResp
		resp = append(resp, entityMap)
	}

	c.Json(w, http.StatusOK, "Fetched progress entities successfully", map[string]interface{}{
		"page":     page,
		"limit":    limit,
		"count":    len(resp),
		"entities": resp,
	})
}

// GET Team Progress
func (c *Construct) GetTeamProgressEntities(w http.ResponseWriter, r *http.Request) {
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, fmt.Sprintf("%v", err), nil)
		return
	}
	role := strings.ToLower(currentUser.Role.Name)

	// Optional cohort filter (path or query)
	cohortID, _ := c.GetUintParam(r, "cohort_id")
	if cohortID == 0 {
		if q := r.URL.Query().Get("cohort_id"); q != "" {
			if v, err := strconv.ParseUint(q, 10, 64); err == nil {
				cohortID = v
			}
		}
	}

	page, limit := c.GetPaginationParams(r)
	offset := (page - 1) * limit

	reportStatus := r.URL.Query().Get("report_status")
	includeArchived := r.URL.Query().Get("include_archived") == "true" // Optional: include archived items
	var weekStart, weekEnd time.Time
	if ws := r.URL.Query().Get("week_start"); ws != "" {
		weekStart, _ = time.Parse("2006-01-02", ws)
	}
	if we := r.URL.Query().Get("week_end"); we != "" {
		weekEnd, _ = time.Parse("2006-01-02", we)
	}

	// Base query
	query := c.DB.Model(&models.ProgressEntity{}).
		Preload("EntityCohort").
		Preload("Items.TeamDetails.UserTeams.UserRef.Profile").
		Preload("Items.CreatedBy.Profile").
		Preload("Items.CreatedBy.Role").
		Preload("Items.WeeklyReports", func(db *gorm.DB) *gorm.DB {
			if reportStatus != "" {
				db = db.Where("status = ?", reportStatus)
			}
			if !weekStart.IsZero() {
				db = db.Where("week_start >= ?", weekStart)
			}
			if !weekEnd.IsZero() {
				db = db.Where("week_end <= ?", weekEnd)
			}
			return db.Preload("ProgressItems").Preload("Comments").Preload("Student.Profile")
		}).
		// NOTE: correct join column
		Joins("JOIN progress_items ON progress_items.progress_entity_ref_id = progress_entities.id").
		Where("progress_items.team_ref_id IS NOT NULL")

	// Filter out archived items by default (unless explicitly requested)
	if !includeArchived {
		query = query.Where("progress_items.is_archived = ?", false)
	}

	// Role-based filtering
	switch role {
	case "opsadmin", "systemadmin":
		if cohortID > 0 {
			// NOTE: correct entity cohort column
			query = query.Where("progress_entities.entity_cohort_id = ?", cohortID)
		}

	case "supervisor":
		assignedCohorts := c.getAssignedCohorts(currentUser.UserID, "Supervisor")
		query = query.Joins("JOIN teams ON teams.team_id = progress_items.team_ref_id").
			Where("(teams.created_by_id = ? OR teams.cohort_ref_id IN ?)", currentUser.UserID, assignedCohorts)

	case "student":
		var teamIDs []uint64
		c.DB.Model(&models.UserTeam{}).
			Where("user_user_id = ?", currentUser.UserID).
			Pluck("team_team_id", &teamIDs)
		if len(teamIDs) == 0 {
			c.Json(w, http.StatusOK, "No teams found", map[string]interface{}{
				"count": 0, "page": page, "limit": limit, "total_pages": 0, "entities": []interface{}{},
			})
			return
		}
		query = query.Where("progress_items.team_ref_id IN ?", teamIDs)

	case "mentor":
		c.Json(w, http.StatusForbidden, "Mentors cannot view team progress", nil)
		return

	default:
		c.Json(w, http.StatusForbidden, "Unauthorized role", nil)
		return
	}

	// Count
	var total int64
	if err := query.Distinct("progress_entities.id").Count(&total).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to count records", map[string]interface{}{"error": err.Error()})
		return
	}

	// Fetch
	var entities []models.ProgressEntity
	if err := query.Offset(offset).Limit(limit).Find(&entities).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch data", map[string]interface{}{"error": err.Error()})
		return
	}
	totalPages := int((total + int64(limit) - 1) / int64(limit))

	// Build response
	resp := make([]map[string]interface{}, 0, len(entities))
	for _, e := range entities {
		entityMap := map[string]interface{}{
			"id":          e.ID,
			"entity_name": e.EntityName,
			"entity_type": e.EntityType,
			"status":      e.Status,
			"is_archived": e.IsArchived,
			"cohort_id":   e.EntityCohortID,
		}

		itemsResp := make([]map[string]interface{}, 0, len(e.Items))
		for _, item := range e.Items {
			if item.TeamDetails == nil {
				continue
			}
			team := item.TeamDetails

			members := make([]map[string]interface{}, 0, len(team.UserTeams))
			for _, ut := range team.UserTeams {
				u := ut.UserRef
				members = append(members, map[string]interface{}{
					"id":         u.UserID,
					"first_name": u.Profile.FirstName,
					"last_name":  u.Profile.LastName,
					"email":      u.Email,
					"role":       ut.Role,
				})
			}

			// Submitted by (creator) - full name and role
			submittedBy := map[string]interface{}{}
			if item.CreatedBy != nil {
				fullName := ""
				if item.CreatedBy.Profile.ProfileID != 0 {
					fullName = fmt.Sprintf("%s %s",
						strings.TrimSpace(item.CreatedBy.Profile.FirstName),
						strings.TrimSpace(item.CreatedBy.Profile.LastName))
					fullName = strings.TrimSpace(fullName)
				}
				if fullName == "" && item.CreatedBy.Username != "" {
					fullName = item.CreatedBy.Username
				}

				roleName := ""
				if item.CreatedBy.Role.RoleID != 0 {
					roleName = item.CreatedBy.Role.Name
				}

				// Also check team role if user is part of the team
				roleInTeam := ""
				if team.CreatedByID == item.CreatedBy.UserID {
					roleInTeam = "TeamLeader"
				} else {
					for _, ut := range team.UserTeams {
						if ut.UserRefID == item.CreatedBy.UserID && ut.Role != "" {
							roleInTeam = ut.Role
							break
						}
					}
				}

				submittedBy = map[string]interface{}{
					"id":         item.CreatedBy.UserID,
					"full_name":  fullName,
					"first_name": "",
					"last_name":  "",
					"email":      item.CreatedBy.Email,
					"role":       roleName,   // User's system role (Student, Mentor, etc.)
					"team_role":  roleInTeam, // Role within the team (TeamLeader, Member, etc.)
				}
				if item.CreatedBy.Profile.ProfileID != 0 {
					submittedBy["first_name"] = item.CreatedBy.Profile.FirstName
					submittedBy["last_name"] = item.CreatedBy.Profile.LastName
				}
			}

			itemsResp = append(itemsResp, map[string]interface{}{
				"id":              item.ID,
				"phase_name":      item.PhaseName,
				"progress_type":   item.ProgressType,
				"team_id":         team.TeamID,
				"team_name":       team.Name,
				"team_ref_id":     item.TeamRefID,
				"is_team_task":    true, // This is a team task
				"member_count":    len(team.UserTeams),
				"members":         members,
				"submitted_by":    submittedBy, // Full name and role
				"created_by":      submittedBy, // Keep for backward compatibility
				"weight":          item.Weight,
				"performance":     item.Performance,
				"student_status":  item.StudentStatus,
				"verified_status": item.VerifiedStatus,
				"is_archived":     item.IsArchived,
				// weekly_reports intentionally omitted or include if needed
			})
		}

		entityMap["items"] = itemsResp
		resp = append(resp, entityMap)
	}

	c.Json(w, http.StatusOK, "Fetched team progress successfully", map[string]interface{}{
		"count":       total,
		"page":        page,
		"limit":       limit,
		"total_pages": totalPages,
		"entities":    resp,
	})
}

// GetPaginationParams extracts ?page and ?limit query params with defaults
func (c *Construct) GetPaginationParams(r *http.Request) (page, limit int) {
	query := r.URL.Query()
	page, limit = 1, 10

	if p := query.Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if l := query.Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	return page, limit
}

//
