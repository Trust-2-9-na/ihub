package controllers

// ================================================= TEAMS APIs +++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"web/services/assets/middlewares"
	"web/services/assets/models"

	"gorm.io/gorm"
)

//=====================================Assigning Team Members ========================================

// CreateTeamInput represents the expected request body

type CreateTeamInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	CohortRefID uint64   `json:"cohort_ref_id"` // reference to the cohort
	LeaderID    uint64   `json:"leader_id"`     // must be a student in cohort
	MemberIDs   []uint64 `json:"member_ids"`    // must be students in cohort
}

// CreateTeam allows a supervisor to create a team
func (c *Construct) CreateTeam(w http.ResponseWriter, r *http.Request) {
	// --- Get logged-in user ---
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if strings.ToLower(currentUser.Role.Name) != "supervisor" {
		c.Json(w, http.StatusForbidden, "Only supervisors can create teams", nil)
		return
	}

	// --- Parse input ---
	var input CreateTeamInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Check supervisor has access to this cohort ---
	if !c.UserHasCohortAccess(currentUser.UserID, input.CohortRefID) {
		c.Json(w, http.StatusForbidden, "You are not assigned to this cohort or cohort does not exist", nil)
		return
	}

	// --- Check for duplicate team name in the same cohort ---
	var existingTeam models.Team
	if err := c.DB.Where("name = ? AND cohort_ref_id = ?", input.Name, input.CohortRefID).First(&existingTeam).Error; err == nil {
		c.Json(w, http.StatusBadRequest, "A team with this name already exists in the cohort", nil)
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.Json(w, http.StatusInternalServerError, "Failed to check existing team names", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Validate leader and members are in the cohort ---
	allStudentIDs := append(input.MemberIDs, input.LeaderID)
	var validStudentIDs []uint64
	err = c.DB.Model(&models.CohortUser{}).
		Where("user_user_id IN ? AND cohort_cohort_id = ? AND role = ?", allStudentIDs, input.CohortRefID, "Student").
		Pluck("user_user_id", &validStudentIDs).Error
	if err != nil || len(validStudentIDs) != len(allStudentIDs) {
		c.Json(w, http.StatusBadRequest, "One or more users are not students in the cohort", nil)
		return
	}

	// --- Validate team size ---
	totalMembers := len(allStudentIDs)
	if totalMembers < 2 {
		c.Json(w, http.StatusBadRequest, "A team must have at least 2 members including the leader", nil)
		return
	}
	if totalMembers > 4 {
		c.Json(w, http.StatusBadRequest, "A team cannot have more than 4 members including the leader", nil)
		return
	}

	// --- Start transaction ---
	tx := c.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			c.Json(w, http.StatusInternalServerError, "Unexpected error", map[string]interface{}{"panic": r})
		}
	}()

	// --- Create team ---
	team := models.Team{
		Name:        input.Name,
		Description: input.Description,
		CohortRefID: &input.CohortRefID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		CreatedByID: currentUser.UserID, // <-- assign creator supervisor
	}
	if err := tx.Create(&team).Error; err != nil {
		tx.Rollback()
		c.Json(w, http.StatusInternalServerError, "Failed to create team", nil)
		return
	}

	// --- Assign creator supervisor to the team ---
	userTeams := []models.UserTeam{
		{
			UserRefID: currentUser.UserID,
			TeamRefID: team.TeamID,
			Role:      "Supervisor",
			JoinedAt:  time.Now(),
		},
	}

	// --- Assign students including leader ---
	for _, uid := range allStudentIDs {
		role := models.Member
		if uid == input.LeaderID {
			role = models.TeamLeader
		}
		userTeams = append(userTeams, models.UserTeam{
			UserRefID: uid,
			TeamRefID: team.TeamID,
			Role:      string(role),
			JoinedAt:  time.Now(),
		})
	}

	if err := tx.Create(&userTeams).Error; err != nil {
		tx.Rollback()
		c.Json(w, http.StatusInternalServerError, "Failed to assign users to team", nil)
		return
	}

	tx.Commit()

	// --- Notify students ---
	var cohort models.Cohort
	_ = c.DB.First(&cohort, input.CohortRefID)

	for _, uid := range allStudentIDs {
		c.NotifyAndTrack(
			uid,
			"Added to Team",
			fmt.Sprintf("You have been added to team '%s' in cohort '%s'.", team.Name, cohort.Name),
			"TeamAssignment",
			"Team",
			&team.TeamID,
			"Added",
			true,
		)
	}

	c.Json(w, http.StatusOK, "Team created successfully", map[string]interface{}{
		"team_id":    team.TeamID,
		"name":       team.Name,
		"cohort_id":  team.CohortRefID,
		"leader_id":  input.LeaderID,
		"member_ids": input.MemberIDs,
		"creator_id": currentUser.UserID, // Include creator in response
	})
}

// ====================================== API for Updating a Team ================================================================
func (c *Construct) UpdateTeam(w http.ResponseWriter, r *http.Request) {
	// --- Get logged-in user ---
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if strings.ToLower(currentUser.Role.Name) != "supervisor" {
		c.Json(w, http.StatusForbidden, "Only supervisors can update teams", nil)
		return
	}

	// --- Parse input ---
	var req struct {
		TeamID    uint64   `json:"team_id"`
		Name      string   `json:"name"`
		Desc      string   `json:"description"`
		LeaderID  uint64   `json:"leader_id"`  // student leader
		MemberIDs []uint64 `json:"member_ids"` // student members
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request", nil)
		return
	}

	// --- Fetch existing team ---
	var team models.Team
	if err := c.DB.Preload("UserTeams").Where("team_id = ?", req.TeamID).First(&team).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Team not found", nil)
		return
	}

	// --- Check for duplicate team name in the same cohort ---
	var duplicate models.Team
	if err := c.DB.Where("name = ? AND cohort_ref_id = ? AND team_id != ?", req.Name, team.CohortRefID, req.TeamID).
		First(&duplicate).Error; err == nil {
		c.Json(w, http.StatusBadRequest, "Another team with this name already exists in the cohort", nil)
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.Json(w, http.StatusInternalServerError, "Failed to check existing team names", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Validate leader and members are students in the cohort ---
	allStudentIDs := append(req.MemberIDs, req.LeaderID)
	var validStudentIDs []uint64
	if err := c.DB.Model(&models.CohortUser{}).
		Where("user_user_id IN ? AND cohort_cohort_id = ? AND role = ?", allStudentIDs, *team.CohortRefID, "Student").
		Pluck("user_user_id", &validStudentIDs).Error; err != nil || len(validStudentIDs) != len(allStudentIDs) {
		c.Json(w, http.StatusBadRequest, "One or more users are not students in the cohort", nil)
		return
	}

	// --- Validate team size ---
	totalMembers := len(allStudentIDs)
	if totalMembers < 2 {
		c.Json(w, http.StatusBadRequest, "A team must have at least 2 members including the leader", nil)
		return
	}
	if totalMembers > 4 {
		c.Json(w, http.StatusBadRequest, "A team cannot have more than 4 members including the leader", nil)
		return
	}

	// --- Update team info ---
	team.Name = req.Name
	team.Description = req.Desc
	team.UpdatedAt = time.Now()
	if err := c.DB.Save(&team).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update team info", nil)
		return
	}

	// --- Fetch current UserTeams ---
	existingMembers := make(map[uint64]models.UserTeam)
	var userTeams []models.UserTeam
	c.DB.Where("team_ref_id = ?", team.TeamID).Find(&userTeams)
	for _, ut := range userTeams {
		existingMembers[ut.UserRefID] = ut
	}

	// --- Update leader ---
	if leaderUT, exists := existingMembers[req.LeaderID]; exists {
		c.DB.Model(&leaderUT).Update("role", string(models.TeamLeader))
	} else {
		c.DB.Create(&models.UserTeam{
			UserRefID: req.LeaderID,
			TeamRefID: team.TeamID,
			Role:      string(models.TeamLeader),
			JoinedAt:  time.Now(),
		})
	}
	c.NotifyAndTrack(req.LeaderID, "Team Leadership Assigned",
		fmt.Sprintf("You are now the leader of team '%s'.", team.Name),
		"TeamUpdate", "Team", &team.TeamID, "Updated", true)

	// --- Add new student members ---
	// --- Add or update student members ---
	for _, mid := range req.MemberIDs {
		if ut, exists := existingMembers[mid]; exists {
			// Only update role if it's not the leader
			if ut.Role != string(models.TeamLeader) {
				c.DB.Model(&ut).Update("role", string(models.Member))
			}
		} else {
			c.DB.Create(&models.UserTeam{
				UserRefID: mid,
				TeamRefID: team.TeamID,
				Role:      string(models.Member),
				JoinedAt:  time.Now(),
			})
			c.NotifyAndTrack(mid, "Added to Team",
				fmt.Sprintf("You have been added to team '%s'.", team.Name),
				"TeamUpdate", "Team", &team.TeamID, "Added", true)
		}
		delete(existingMembers, mid)
	}

	// --- Remove old members not in request (excluding leader and supervisors) ---
	// Remove old members not in request (excluding leader)
	for oldID, ut := range existingMembers {
		if ut.Role == string(models.TeamLeader) {
			continue
		}
		c.DB.Delete(&ut)
		c.NotifyAndTrack(oldID, "Removed from Team",
			fmt.Sprintf("You have been removed from team '%s'.", team.Name),
			"TeamUpdate", "Team", &team.TeamID, "Removed", true)
	}

	// --- Respond ---
	c.Json(w, http.StatusOK, "Team updated successfully", map[string]interface{}{
		"team_id":    team.TeamID,
		"name":       team.Name,
		"leader_id":  req.LeaderID,
		"member_ids": req.MemberIDs,
		"creator_id": team.CreatedByID,
	})
}

//=========================== GET Teams API ==========================================

func (c *Construct) GetTeams(w http.ResponseWriter, r *http.Request) {
	// --- Get logged-in user ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Role").Preload("Profile").
		Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "User not found", nil)
		return
	}

	roleName := strings.ToLower(user.Role.Name)

	// --- Query params ---
	teamID := r.URL.Query().Get("team_id")
	teamName := strings.TrimSpace(r.URL.Query().Get("name"))
	page, limit := c.GetPaginationParams(r)
	offset := (page - 1) * limit

	// --- Base query ---
	query := c.DB.Model(&models.Team{}).
		Preload("UserTeams.UserRef.Profile").
		Preload("CohortDetails")

	// --- Filtering by query params ---
	if teamID != "" {
		query = query.Where("team_id = ?", teamID)
	}
	if teamName != "" {
		query = query.Where("LOWER(name) LIKE ?", "%"+strings.ToLower(teamName)+"%")
	}

	// --- Role-based filtering ---
	switch roleName {
	case "student":
		var teamIDs []uint64
		c.DB.Model(&models.UserTeam{}).
			Where("user_user_id = ?", user.UserID).
			Pluck("team_team_id", &teamIDs)

		if len(teamIDs) == 0 {
			c.Json(w, http.StatusOK, "No teams found", map[string]interface{}{
				"data":                 []interface{}{},
				"total_members_all":    0,
				"total_unique_members": 0,
				"page":                 page,
				"limit":                limit,
			})
			return
		}
		query = query.Where("team_id IN ?", teamIDs)

	case "supervisor":
		var createdTeamIDs []uint64
		c.DB.Model(&models.Team{}).Where("created_by_id = ?", user.UserID).Pluck("team_id", &createdTeamIDs)

		cohortIDs := c.getAssignedCohorts(user.UserID, "Supervisor")
		var cohortTeamIDs []uint64
		if len(cohortIDs) > 0 {
			c.DB.Model(&models.Team{}).Where("cohort_ref_id IN ?", cohortIDs).Pluck("team_id", &cohortTeamIDs)
		}

		teamIDMap := map[uint64]struct{}{}
		for _, id := range createdTeamIDs {
			teamIDMap[id] = struct{}{}
		}
		for _, id := range cohortTeamIDs {
			teamIDMap[id] = struct{}{}
		}

		var teamIDs []uint64
		for id := range teamIDMap {
			teamIDs = append(teamIDs, id)
		}

		if len(teamIDs) == 0 {
			c.Json(w, http.StatusOK, "No teams found", map[string]interface{}{
				"data":                 []interface{}{},
				"total_members_all":    0,
				"total_unique_members": 0,
				"page":                 page,
				"limit":                limit,
			})
			return
		}
		query = query.Where("team_id IN ?", teamIDs)
	case "mentor":
		// Get students assigned to this mentor in the cohorts they are assigned to
		var studentIDs []uint64
		c.DB.Model(&models.MentorStudentAssignment{}).
			Where("mentor_ref_id = ? AND cohort_ref_id IN ?", user.UserID, c.getAssignedCohorts(user.UserID, "Mentor")).
			Pluck("student_ref_id", &studentIDs)

		if len(studentIDs) == 0 {
			c.Json(w, http.StatusOK, "No teams found", map[string]interface{}{
				"data":                 []interface{}{},
				"total_members_all":    0,
				"total_unique_members": 0,
				"page":                 page,
				"limit":                limit,
			})
			return
		}

		// Get teams for these students
		var teamIDs []uint64
		c.DB.Model(&models.UserTeam{}).Where("user_user_id IN ?", studentIDs).Pluck("team_team_id", &teamIDs)
		if len(teamIDs) == 0 {
			c.Json(w, http.StatusOK, "No teams found", map[string]interface{}{
				"data":                 []interface{}{},
				"total_members_all":    0,
				"total_unique_members": 0,
				"page":                 page,
				"limit":                limit,
			})
			return
		}

		query = query.Where("team_id IN ?", teamIDs)

	case "opsadmin":
		// Full access
	default:
		c.Json(w, http.StatusForbidden, "You are not allowed to view teams", nil)
		return
	}

	// --- Count total ---
	var totalCount int64
	if err := query.Count(&totalCount).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to count teams", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Fetch teams with pagination ---
	var teams []models.Team
	if err := query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&teams).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch teams", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Build response ---
	type teamMemberResp struct {
		UserID    uint64 `json:"user_id"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Role      string `json:"role"`
	}

	type teamResp struct {
		TeamID      uint64           `json:"team_id"`
		Name        string           `json:"name"`
		Description string           `json:"description,omitempty"`
		Cohort      string           `json:"cohort,omitempty"`
		Members     []teamMemberResp `json:"members,omitempty"`
		MemberCount int              `json:"member_count"`
		CreatedAt   string           `json:"created_at"`
		UpdatedAt   string           `json:"updated_at"`
	}

	var resp []teamResp
	totalMembersAllTeams := 0
	uniqueMembers := make(map[uint64]struct{})

	for _, t := range teams {
		var members []teamMemberResp
		for _, ut := range t.UserTeams {
			u := ut.UserRef
			members = append(members, teamMemberResp{
				UserID:    u.UserID,
				FirstName: u.Profile.FirstName,
				LastName:  u.Profile.LastName,
				Role:      ut.Role,
			})
			uniqueMembers[u.UserID] = struct{}{}
		}

		cohortName := ""
		if t.CohortDetails != nil {
			cohortName = t.CohortDetails.Name
		}

		memberCount := len(members)
		totalMembersAllTeams += memberCount

		resp = append(resp, teamResp{
			TeamID:      t.TeamID,
			Name:        t.Name,
			Description: t.Description,
			Cohort:      cohortName,
			Members:     members,
			MemberCount: memberCount,
			CreatedAt:   t.CreatedAt.Format("2006-01-02 15:04"),
			UpdatedAt:   t.UpdatedAt.Format("2006-01-02 15:04"),
		})
	}

	c.Json(w, http.StatusOK, "Success", map[string]interface{}{
		"data":                 resp,
		"page":                 page,
		"limit":                limit,
		"total":                totalCount,
		"total_members_all":    totalMembersAllTeams,
		"total_unique_members": len(uniqueMembers),
	})
}

//========================================== Team Management ===============================================

func (c *Construct) ManageTeamSafe(w http.ResponseWriter, r *http.Request) {
	// --- Auth check ---
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Role").Preload("Profile").
		Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "User not found", nil)
		return
	}

	if strings.ToLower(user.Role.Name) != "supervisor" {
		c.Json(w, http.StatusForbidden, "Only supervisors can manage teams", nil)
		return
	}

	// --- Parse payload ---
	type manageTeamReq struct {
		TeamIDs   []uint64 `json:"team_ids"`
		Action    string   `json:"action"`     // archive, unarchive, delete, remove_members
		MemberIDs []uint64 `json:"member_ids"` // for remove_members
	}

	var req manageTeamReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request payload", nil)
		return
	}
	if len(req.TeamIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "No team IDs provided", nil)
		return
	}

	// --- Load target teams ---
	var teams []models.Team
	if err := c.DB.Preload("UserTeams.UserRef.Profile").
		Where("team_id IN ?", req.TeamIDs).Find(&teams).Error; err != nil || len(teams) == 0 {
		c.Json(w, http.StatusNotFound, "No teams found", nil)
		return
	}

	// --- Determine which teams the supervisor can manage ---
	assignedCohorts := c.getAssignedCohorts(user.UserID, "Supervisor")
	validTeams := make([]models.Team, 0)

	for _, t := range teams {
		// Check if user is assigned to team as supervisor
		var isTeamSupervisor bool
		c.DB.Model(&models.UserTeam{}).
			Where("team_ref_id = ? AND user_ref_id = ? AND role = ?", t.TeamID, user.UserID, "Supervisor").
			Select("count(*) > 0").Find(&isTeamSupervisor)

		if t.CreatedByID == user.UserID ||
			isTeamSupervisor ||
			(t.CohortRefID != nil && slices.Contains(assignedCohorts, *t.CohortRefID)) {
			validTeams = append(validTeams, t)
		}
	}

	if len(validTeams) == 0 {
		c.Json(w, http.StatusForbidden, "You are not allowed to manage these teams", nil)
		return
	}

	// --- Prepare response container ---
	type teamActionResult struct {
		TeamID      uint64   `json:"team_id"`
		Name        string   `json:"name"`
		Action      string   `json:"action"`
		AffectedIDs []uint64 `json:"affected_ids,omitempty"`
		Message     string   `json:"message"`
	}
	var results []teamActionResult

	tx := c.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			c.Json(w, http.StatusInternalServerError, "Unexpected error occurred", nil)
		}
	}()

	for _, t := range validTeams {
		var affected []uint64
		action := strings.ToLower(req.Action)

		switch action {
		case "archive":
			if t.IsArchived {
				results = append(results, teamActionResult{TeamID: t.TeamID, Name: t.Name, Action: "archive", Message: "Already archived"})
				continue
			}
			t.IsArchived = true
			if err := tx.Save(&t).Error; err != nil {
				tx.Rollback()
				c.Json(w, http.StatusInternalServerError, "Failed to archive team", nil)
				return
			}
			for _, ut := range t.UserTeams {
				affected = append(affected, ut.UserRefID)
				c.NotifyAndTrack(ut.UserRefID, "Team Archived",
					fmt.Sprintf("Team %s has been archived by supervisor %s", t.Name, user.Profile.FirstName),
					"TeamUpdate", "Team", &t.TeamID, "Archived", false)
			}
			results = append(results, teamActionResult{TeamID: t.TeamID, Name: t.Name, Action: "archive", AffectedIDs: affected, Message: "Team archived successfully"})

		case "unarchive":
			if !t.IsArchived {
				results = append(results, teamActionResult{TeamID: t.TeamID, Name: t.Name, Action: "unarchive", Message: "Team is not archived"})
				continue
			}
			t.IsArchived = false
			if err := tx.Save(&t).Error; err != nil {
				tx.Rollback()
				c.Json(w, http.StatusInternalServerError, "Failed to unarchive team", nil)
				return
			}
			for _, ut := range t.UserTeams {
				affected = append(affected, ut.UserRefID)
				c.NotifyAndTrack(ut.UserRefID, "Team Unarchived",
					fmt.Sprintf("Team %s has been unarchived by supervisor %s", t.Name, user.Profile.FirstName),
					"TeamUpdate", "Team", &t.TeamID, "Unarchived", false)
			}
			results = append(results, teamActionResult{TeamID: t.TeamID, Name: t.Name, Action: "unarchive", AffectedIDs: affected, Message: "Team unarchived successfully"})

		case "delete":
			// Check delete permissions (creator or assigned supervisor)
			var isTeamSupervisor bool
			c.DB.Model(&models.UserTeam{}).
				Where("team_ref_id = ? AND user_ref_id = ? AND role = ?", t.TeamID, user.UserID, "Supervisor").
				Select("count(*) > 0").Find(&isTeamSupervisor)

			if t.CreatedByID != user.UserID && !isTeamSupervisor {
				results = append(results, teamActionResult{
					TeamID:  t.TeamID,
					Name:    t.Name,
					Action:  "delete",
					Message: "You are not authorized to delete this team",
				})
				continue
			}

			for _, ut := range t.UserTeams {
				affected = append(affected, ut.UserRefID)
				c.NotifyAndTrack(ut.UserRefID, "Team Deleted",
					fmt.Sprintf("Team %s has been deleted by supervisor %s", t.Name, user.Profile.FirstName),
					"TeamDelete", "Team", &t.TeamID, "Deleted", false)
			}
			if err := tx.Delete(&t).Error; err != nil {
				tx.Rollback()
				c.Json(w, http.StatusInternalServerError, "Failed to delete team", nil)
				return
			}
			results = append(results, teamActionResult{TeamID: t.TeamID, Name: t.Name, Action: "delete", AffectedIDs: affected, Message: "Team deleted successfully"})

		case "remove_members":
			if len(req.MemberIDs) == 0 {
				results = append(results, teamActionResult{TeamID: t.TeamID, Name: t.Name, Action: "remove_members", Message: "No member IDs provided"})
				continue
			}
			var removed []uint64
			for _, mID := range req.MemberIDs {
				var ut models.UserTeam
				if err := tx.Where("user_ref_id = ? AND team_ref_id = ?", mID, t.TeamID).First(&ut).Error; err == nil {
					if ut.Role == "TeamLeader" {
						continue // can't remove leader
					}
					if err := tx.Delete(&ut).Error; err == nil {
						removed = append(removed, mID)
						c.NotifyAndTrack(mID, "Removed from Team",
							fmt.Sprintf("You have been removed from team %s by supervisor %s", t.Name, user.Profile.FirstName),
							"TeamMemberUpdate", "UserTeam", &t.TeamID, "MemberRemoved", false)
					}
				}
			}
			results = append(results, teamActionResult{
				TeamID:      t.TeamID,
				Name:        t.Name,
				Action:      "remove_members",
				AffectedIDs: removed,
				Message:     fmt.Sprintf("%d member(s) removed", len(removed)),
			})

		default:
			tx.Rollback()
			c.Json(w, http.StatusBadRequest, "Invalid action", nil)
			return
		}

		// --- Audit supervisor action ---
		c.NotifyAndTrack(user.UserID, fmt.Sprintf("Performed %s on Team", req.Action),
			fmt.Sprintf("Supervisor %s performed %s on team %s", user.Profile.FirstName, req.Action, t.Name),
			"TeamAudit", "Team", &t.TeamID, "ActionPerformed", true)
	}

	tx.Commit()
	c.Json(w, http.StatusOK, "Action completed successfully", map[string]interface{}{
		"results": results,
	})
}
