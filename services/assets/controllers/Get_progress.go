package controllers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"web/services/assets/models"

	"gorm.io/datatypes"

	"gorm.io/gorm"
)

func (c *Construct) GetStudentProgressEntities(w http.ResponseWriter, r *http.Request) {
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

	// --- Base Query ---
	query := c.DB.Model(&models.ProgressEntity{}).
		Preload("EntityCohort").
		Preload("Items.AssignedTo.Profile").
		Preload("Items.CreatedBy.Profile").
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

	// --- Role-based Filtering ---
	switch role {
	case "opsadmin", "systemadmin":
		if entityID > 0 {
			query = query.Where("id = ?", entityID)
		} else if cohortID > 0 {
			query = query.Where("cohort_id = ?", cohortID)
		}
	case "supervisor":
		cohortIDs := c.getAssignedCohorts(currentUser.UserID, "Supervisor")
		if len(cohortIDs) == 0 {
			c.Json(w, http.StatusOK, "No cohorts found", map[string]interface{}{"entities": []interface{}{}})
			return
		}
		query = query.Where("cohort_id IN ?", cohortIDs)
	case "mentor":
		cohortIDs := c.getAssignedCohorts(currentUser.UserID, "Mentor")
		if len(cohortIDs) == 0 {
			c.Json(w, http.StatusOK, "No cohorts found", map[string]interface{}{"entities": []interface{}{}})
			return
		}

		query = query.Where("cohort_id IN ?", cohortIDs)

		// Filter only progress items for students assigned to this mentor
		var studentIDs []uint64
		c.DB.Model(&models.CohortUser{}).
			Where("role = ? AND user_user_id = ?", "StudentMentor", currentUser.UserID).
			Pluck("member_id", &studentIDs)

		if len(studentIDs) == 0 {
			c.Json(w, http.StatusOK, "No students assigned", map[string]interface{}{"entities": []interface{}{}})
			return
		}

		query = query.Preload("Items", "created_by_id IN ?", studentIDs)

	case "student":
		cohortIDs := c.getAssignedCohorts(currentUser.UserID, "Student")
		if len(cohortIDs) == 0 {
			c.Json(w, http.StatusOK, "No cohorts found", map[string]interface{}{"entities": []interface{}{}})
			return
		}
		query = query.Where("cohort_id IN ?", cohortIDs).
			Preload("Items", "created_by_id = ?", currentUser.UserID)
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

		// --- Items ---
		itemsResp := make([]map[string]interface{}, 0, len(e.Items))
		for _, item := range e.Items {
			assignedTo := map[string]interface{}{}
			if item.AssignedTo != nil {
				assignedTo = map[string]interface{}{
					"id":         item.AssignedTo.UserID,
					"first_name": item.AssignedTo.Profile.FirstName,
					"last_name":  item.AssignedTo.Profile.LastName,
					"email":      item.AssignedTo.Email,
				}
			}

			createdBy := map[string]interface{}{}
			if item.CreatedBy != nil {
				createdBy = map[string]interface{}{
					"id":         item.CreatedBy.UserID,
					"first_name": item.CreatedBy.Profile.FirstName,
					"last_name":  item.CreatedBy.Profile.LastName,
					"email":      item.CreatedBy.Email,
				}
			}

			reportsResp := make([]map[string]interface{}, 0)
			for _, r := range item.WeeklyReports {
				linkedItemIDs := make([]uint64, 0)
				for _, p := range r.ProgressItems {
					linkedItemIDs = append(linkedItemIDs, p.ID)
				}

				status := r.Status
				if status == "Pending" && len(r.Comments) > 0 {
					for _, cmt := range r.Comments {
						if strings.ToLower(cmt.Comment) == "needs revision" {
							status = "Needs Revision"
							break
						}
					}
				}

				reportsResp = append(reportsResp, map[string]interface{}{
					"id":              r.ID,
					"student_id":      r.StudentID,
					"status":          status,
					"week_start":      r.WeekStart,
					"week_end":        r.WeekEnd,
					"linked_items":    linkedItemIDs,
					"student_status":  item.StudentStatus,
					"verified_status": item.VerifiedStatus,
				})
			}

			itemsResp = append(itemsResp, map[string]interface{}{
				"id":              item.ID,
				"phase_name":      item.PhaseName,
				"student_status":  item.StudentStatus,
				"verified_status": item.VerifiedStatus,
				"weight":          item.Weight,
				"performance":     item.Performance,
				"progress_type":   item.ProgressType,
				"is_archived":     item.IsArchived,
				"assigned_to":     assignedTo,
				"created_by":      createdBy,
				"weekly_reports":  reportsResp,
			})
		}

		entityMap["items"] = itemsResp
		resp = append(resp, entityMap)
	}

	// --- AUDIT & LOG ACCESS ---
	go func() {
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.RemoteAddr
		}
		ua := r.UserAgent()
		meta := fmt.Sprintf(`{"ip":"%s","user_agent":"%s"}`, ip, ua)

		message := fmt.Sprintf(
			"%s (%s) viewed progress entities (page: %d, limit: %d, cohort_id: %d, entity_id: %d)",
			currentUser.Username, role, page, limit, cohortID, entityID,
		)

		c.NotifyAndTrack(
			currentUser.UserID,
			"Progress Entities Viewed",
			message,
			"view",
			"ProgressEntity",
			nil,
			"success",
			false,
		)

		audit := &models.AuditLog{
			UserID:    currentUser.UserID,
			Action:    "view",
			Entity:    ptrString("ProgressEntity"),
			EntityID:  nil,
			Metadata:  datatypes.JSON([]byte(meta)),
			CreatedAt: time.Now(),
		}
		_ = c.DB.Create(audit).Error
	}()

	// --- Response ---
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
	cohortID, _ := c.GetUintParam(r, "cohort_id")
	page, limit := c.GetPaginationParams(r)
	offset := (page - 1) * limit

	reportStatus := r.URL.Query().Get("report_status")
	var weekStart, weekEnd time.Time
	if ws := r.URL.Query().Get("week_start"); ws != "" {
		weekStart, _ = time.Parse("2006-01-02", ws)
	}
	if we := r.URL.Query().Get("week_end"); we != "" {
		weekEnd, _ = time.Parse("2006-01-02", we)
	}

	// --- Base query ---
	query := c.DB.Model(&models.ProgressEntity{}).
		Preload("EntityCohort").
		Preload("Items.TeamDetails.UserTeams.UserRef.Profile"). // team members
		Preload("Items.CreatedBy.Profile").
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
		}).
		Joins("JOIN progress_items ON progress_items.entity_id = progress_entities.id").
		Where("progress_items.team_ref_id IS NOT NULL")

	// --- Role-based filtering ---
	switch role {
	case "opsadmin", "systemadmin":
		if cohortID > 0 {
			query = query.Where("progress_entities.cohort_id = ?", cohortID)
		}

	case "supervisor":
		assignedCohorts := c.getAssignedCohorts(currentUser.UserID, "Supervisor")
		query = query.Joins("JOIN teams ON teams.team_id = progress_items.team_ref_id").
			Where("(teams.created_by_id = ? OR teams.cohort_ref_id IN ?)", currentUser.UserID, assignedCohorts)

	case "student":
		var teamIDs []uint64
		c.DB.Model(&models.UserTeam{}).Where("user_user_id = ?", currentUser.UserID).Pluck("team_team_id", &teamIDs)
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

	// --- Count total ---
	var total int64
	if err := query.Distinct("progress_entities.id").Count(&total).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to count records", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Fetch entities ---
	var entities []models.ProgressEntity
	if err := query.Offset(offset).Limit(limit).Find(&entities).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch data", map[string]interface{}{"error": err.Error()})
		return
	}

	totalPages := int((total + int64(limit) - 1) / int64(limit))

	// --- Build response ---
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

			// --- Weekly reports ---
			reportsResp := make([]map[string]interface{}, 0)
			for _, r := range item.WeeklyReports {
				linkedItemIDs := make([]uint64, 0)
				for _, p := range r.ProgressItems {
					linkedItemIDs = append(linkedItemIDs, p.ID)
				}

				status := r.Status
				if status == "Pending" && len(r.Comments) > 0 {
					for _, cmt := range r.Comments {
						if strings.ToLower(cmt.Comment) == "needs revision" {
							status = "Needs Revision"
							break
						}
					}
				}

				reportsResp = append(reportsResp, map[string]interface{}{
					"id":              r.ID,
					"status":          status,
					"week_start":      r.WeekStart,
					"week_end":        r.WeekEnd,
					"linked_items":    linkedItemIDs,
					"verified_status": item.VerifiedStatus,
				})
			}

			// --- Item response including all fields ---
			itemsResp = append(itemsResp, map[string]interface{}{
				"id":              item.ID,
				"phase_name":      item.PhaseName,
				"team_id":         team.TeamID,
				"team_name":       team.Name,
				"member_count":    len(team.UserTeams),
				"members":         members,
				"weight":          item.Weight,
				"performance":     item.Performance,
				"student_status":  item.StudentStatus,
				"verified_status": item.VerifiedStatus,
				"weekly_reports":  reportsResp,
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
