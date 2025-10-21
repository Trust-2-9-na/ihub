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

	// --- Create team ---
	team := models.Team{
		Name:        input.Name,
		Description: input.Description,
		CohortRefID: &input.CohortRefID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := c.DB.Create(&team).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create team", nil)
		return
	}

	// --- Assign users to team ---
	userTeams := make([]models.UserTeam, 0, len(allStudentIDs))
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
	if err := c.DB.Create(&userTeams).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to assign users to team", nil)
		return
	}

	// --- Notify users ---
	var cohort models.Cohort
	_ = c.DB.First(&cohort, input.CohortRefID) // optional: get cohort name for notifications

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

	// --- Respond ---
	c.Json(w, http.StatusOK, "Team created successfully", map[string]interface{}{
		"team_id":    team.TeamID,
		"name":       team.Name,
		"cohort_id":  team.CohortRefID,
		"leader_id":  input.LeaderID,
		"member_ids": input.MemberIDs,
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
		LeaderID  uint64   `json:"leader_id"`
		MemberIDs []uint64 `json:"member_ids"`
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
	if err := c.DB.Where("name = ? AND cohort_ref_id = ? AND team_id != ?", req.Name, team.CohortRefID, req.TeamID).First(&duplicate).Error; err == nil {
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

	// --- Update leader ---
	var currentLeader models.UserTeam
	c.DB.Where("team_ref_id = ? AND role = ?", team.TeamID, models.TeamLeader).First(&currentLeader)
	if currentLeader.UserRefID != req.LeaderID {
		// Demote old leader if exists
		if currentLeader.UserRefID != 0 {
			c.DB.Model(&currentLeader).Update("role", string(models.Member))
			c.NotifyAndTrack(currentLeader.UserRefID, "Demoted from Team Leader",
				fmt.Sprintf("You are no longer the leader of team '%s' but remain a member.", team.Name),
				"TeamUpdate", "Team", &team.TeamID, "Updated", true)
		}

		// Promote or add new leader
		var newLeader models.UserTeam
		if err := c.DB.Where("team_ref_id = ? AND user_ref_id = ?", team.TeamID, req.LeaderID).First(&newLeader).Error; err == nil {
			c.DB.Model(&newLeader).Update("role", string(models.TeamLeader))
		} else {
			newLeader = models.UserTeam{
				UserRefID: req.LeaderID,
				TeamRefID: team.TeamID,
				Role:      string(models.TeamLeader),
				JoinedAt:  time.Now(),
			}
			c.DB.Create(&newLeader)
		}
		c.NotifyAndTrack(req.LeaderID, "Team Leadership Assigned",
			fmt.Sprintf("You are now the leader of team '%s'.", team.Name),
			"TeamUpdate", "Team", &team.TeamID, "Updated", true)
	}

	// --- Update members ---
	existingMembers := make(map[uint64]models.UserTeam)
	var userTeams []models.UserTeam
	c.DB.Where("team_ref_id = ?", team.TeamID).Find(&userTeams)
	for _, ut := range userTeams {
		existingMembers[ut.UserRefID] = ut
	}

	// Add new members
	for _, mid := range req.MemberIDs {
		if _, exists := existingMembers[mid]; !exists {
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

	// Remove old members not in request (except leader)
	for oldID, ut := range existingMembers {
		if oldID == req.LeaderID {
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

	// --- Base query with preloads ---
	query := c.DB.Preload("UserTeams.UserRef.Profile").Preload("CohortDetails")

	// --- Role-based filtering ---
	switch roleName {
	case "student":
		var teamIDs []uint64
		c.DB.Model(&models.UserTeam{}).Where("user_user_id = ?", user.UserID).Pluck("team_team_id", &teamIDs)
		if len(teamIDs) == 0 {
			c.Json(w, http.StatusOK, "No teams found", map[string]interface{}{
				"data":                 []interface{}{},
				"total_members_all":    0,
				"total_unique_members": 0,
			})
			return
		}
		query = query.Where("team_id IN ?", teamIDs)

	case "supervisor", "mentor":
		cohortIDs := c.getAssignedCohorts(user.UserID, strings.Title(roleName))
		if len(cohortIDs) == 0 {
			c.Json(w, http.StatusOK, "No teams found", map[string]interface{}{
				"data":                 []interface{}{},
				"total_members_all":    0,
				"total_unique_members": 0,
			})
			return
		}
		query = query.Where("cohort_ref_id IN ?", cohortIDs)

	case "opsadmin":
		// No filtering
	default:
		c.Json(w, http.StatusForbidden, "You are not allowed to view teams", nil)
		return
	}

	// --- Fetch teams ---
	var teams []models.Team
	if err := query.Find(&teams).Error; err != nil {
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

		// --- Notify and Track ---
		c.NotifyAndTrack(
			user.UserID,
			"Viewed Team",
			fmt.Sprintf("%s viewed team %s", user.Profile.FirstName, t.Name),
			"TeamView",
			"Team",
			&t.TeamID,
			"Viewed",
			false,
		)
	}

	c.Json(w, http.StatusOK, "Success", map[string]interface{}{
		"data":                 resp,
		"total_members_all":    totalMembersAllTeams,
		"total_unique_members": len(uniqueMembers),
	})
}

//========================================== Team Management ===============================================

func (c *Construct) ManageTeamSafe(w http.ResponseWriter, r *http.Request) {
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

	if strings.ToLower(user.Role.Name) != "supervisor" {
		c.Json(w, http.StatusForbidden, "Only supervisors can manage teams", nil)
		return
	}

	// --- Parse request payload ---
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

	// --- Fetch teams with userteams preloaded ---
	var teams []models.Team
	if err := c.DB.Preload("UserTeams.UserRef.Profile").
		Where("team_id IN ?", req.TeamIDs).Find(&teams).Error; err != nil || len(teams) == 0 {
		c.Json(w, http.StatusNotFound, "No teams found", nil)
		return
	}

	// --- Filter teams based on supervisor's assigned cohorts ---
	assignedCohorts := c.getAssignedCohorts(user.UserID, "Supervisor")
	validTeams := make([]models.Team, 0)
	for _, t := range teams {
		if t.CohortRefID != nil && slices.Contains(assignedCohorts, *t.CohortRefID) {
			validTeams = append(validTeams, t)
		}
	}
	if len(validTeams) == 0 {
		c.Json(w, http.StatusForbidden, "You are not allowed to manage these teams", nil)
		return
	}

	type teamActionResult struct {
		TeamID      uint64   `json:"team_id"`
		Name        string   `json:"name"`
		Action      string   `json:"action"`
		AffectedIDs []uint64 `json:"affected_ids,omitempty"`
		Message     string   `json:"message"`
	}
	var results []teamActionResult

	// --- Begin transaction ---
	tx := c.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			c.Json(w, http.StatusInternalServerError, "Unexpected error occurred", nil)
		}
	}()

	for _, t := range validTeams {
		var affected []uint64
		switch strings.ToLower(req.Action) {
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
						continue // skip leader
					}
					if err := tx.Delete(&ut).Error; err == nil {
						removed = append(removed, mID)
						c.NotifyAndTrack(mID, "Removed from Team",
							fmt.Sprintf("You have been removed from team %s by supervisor %s", t.Name, user.Profile.FirstName),
							"TeamMemberUpdate", "UserTeam", &t.TeamID, "MemberRemoved", false)
					}
				}
			}
			results = append(results, teamActionResult{TeamID: t.TeamID, Name: t.Name, Action: "remove_members", AffectedIDs: removed, Message: fmt.Sprintf("%d member(s) removed", len(removed))})

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
