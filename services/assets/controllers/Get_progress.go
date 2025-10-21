package controllers

import (
	"fmt"
	"net/http"
	"strings"
	"time"
	"web/services/assets/models"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func (c *Construct) GetProgressEntities(w http.ResponseWriter, r *http.Request) {
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
	// NOTE: preload TeamDetails on Items (ProgressItem.TeamDetails) so we can derive team info
	query := c.DB.Model(&models.ProgressEntity{}).
		Preload("EntityCohort").
		Preload("Items.AssignedTo.Profile").
		Preload("Items.CreatedBy.Profile").
		Preload("Items.TeamDetails.UserTeams.UserRef.Profile"). // load team -> users -> profiles for each item
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
	case "supervisor", "mentor":
		cohortIDs := c.getAssignedCohorts(currentUser.UserID, strings.Title(role))
		if len(cohortIDs) == 0 {
			c.Json(w, http.StatusOK, "No cohorts found", map[string]interface{}{"entities": []interface{}{}})
			return
		}
		query = query.Where("cohort_id IN ?", cohortIDs)
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

		// --- Derive team info from items (first item that has TeamDetails) ---
		var teamInfo map[string]interface{} = nil

		for _, item := range e.Items {
			if item.TeamDetails != nil {
				team := item.TeamDetails

				// build members list from UserTeams
				members := make([]map[string]interface{}, 0, len(team.UserTeams))
				for _, ut := range team.UserTeams {
					m := ut.UserRef
					first, last := "", ""
					if m.Profile.FirstName != "" || m.Profile.LastName != "" {
						first, last = m.Profile.FirstName, m.Profile.LastName
					}
					members = append(members, map[string]interface{}{
						"id":         m.UserID,
						"first_name": first,
						"last_name":  last,
						"email":      m.Email,
						"role":       ut.Role, // include role from UserTeam
					})
				}

				// build submission summary for this team
				submissionSummary := func(items []models.ProgressItem, teamRefID uint64) map[string]interface{} {
					submitted := make([]uint64, 0)
					for _, it := range items {
						if it.TeamRefID != nil && *it.TeamRefID == teamRefID {
							submitted = append(submitted, it.ID)
						}
					}
					return map[string]interface{}{
						"submitted_item_ids": submitted,
						"count":              len(submitted),
					}
				}(e.Items, team.TeamID)

				teamInfo = map[string]interface{}{
					"team_id":            team.TeamID,
					"team_name":          team.Name,
					"member_count":       len(team.UserTeams),
					"members":            members,
					"submission_summary": submissionSummary,
				}

				break // only capture the first team found for the entity
			}
		}

		if teamInfo != nil {
			entityMap["team"] = teamInfo
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

			// If item has TeamDetails, include a simple team summary on the item too
			var itemTeam map[string]interface{} = nil
			if item.TeamDetails != nil {
				t := item.TeamDetails

				// build members list from UserTeams
				members := make([]map[string]interface{}, 0, len(t.UserTeams))
				for _, ut := range t.UserTeams {
					m := ut.UserRef
					first, last := "", ""
					if m.Profile.FirstName != "" || m.Profile.LastName != "" {
						first, last = m.Profile.FirstName, m.Profile.LastName
					}
					members = append(members, map[string]interface{}{
						"id":         m.UserID,
						"first_name": first,
						"last_name":  last,
						"email":      m.Email,
						"role":       ut.Role, // role from UserTeam
					})
				}

				itemTeam = map[string]interface{}{
					"team_id":      t.TeamID,
					"team_name":    t.Name,
					"member_count": len(t.UserTeams),
					"members":      members,
				}
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
				"team":            itemTeam,
				"weekly_reports":  reportsResp,
			})
		}

		entityMap["items"] = itemsResp
		resp = append(resp, entityMap)
	}

	// --- AUDIT & LOG ACCESS (kept as your original) ---
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
