package controllers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"web/services/assets/middlewares"
	"web/services/assets/models"

	"gorm.io/gorm"
)

// Assigning Students to Mentors

func (c *Construct) AssignStudentsToMentor(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortID   uint64   `json:"cohort_id"`
		MentorID   uint64   `json:"mentor_id"`
		StudentIDs []uint64 `json:"student_ids"`
	}

	// Decode request
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}
	if body.CohortID == 0 || body.MentorID == 0 || len(body.StudentIDs) == 0 {
		c.Json(w, http.StatusBadRequest, "cohort_id, mentor_id, and student_ids are required", nil)
		return
	}

	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, err.Error(), nil)
		return
	}

	// Optional: Check supervisor access to cohort
	var sup models.CohortUser
	err = c.DB.Where("cohort_cohort_id = ? AND user_user_id = ? AND role = ?", body.CohortID, currentUser.UserID, "Supervisor").
		First(&sup).Error
	if err != nil {
		c.Json(w, http.StatusForbidden, "Only supervisors assigned to this cohort can assign students", nil)
		return
	}

	tx := c.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			c.Json(w, http.StatusInternalServerError, "Unexpected error", map[string]interface{}{"panic": r})
		}
	}()

	assigned := []uint64{}
	skipped := []uint64{}

	for _, studentID := range body.StudentIDs {
		var existing models.MentorStudentAssignment
		err := tx.Where("cohort_ref_id = ? AND mentor_ref_id = ? AND student_ref_id = ?",
			body.CohortID, body.MentorID, studentID).First(&existing).Error
		if err == nil {
			skipped = append(skipped, studentID)
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			tx.Rollback()
			c.Json(w, http.StatusInternalServerError, "Failed to check existing assignment", map[string]interface{}{"error": err.Error()})
			return
		}

		assign := models.MentorStudentAssignment{
			CohortRefID:  body.CohortID,
			MentorRefID:  body.MentorID,
			StudentRefID: studentID,
			CreatedBy:    &currentUser.UserID,
		}

		if err := tx.Create(&assign).Error; err != nil {
			tx.Rollback()
			c.Json(w, http.StatusInternalServerError, "Failed to assign student", map[string]interface{}{"error": err.Error()})
			return
		}

		assigned = append(assigned, studentID)

		// Notify student
		c.NotifyAndTrack(studentID, "Mentor Assignment",
			fmt.Sprintf("You have been assigned to mentor '%d' in cohort '%d'", body.MentorID, body.CohortID),
			"Assignment", "Cohort", &body.CohortID, "", true)
	}

	if err := tx.Commit().Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Transaction commit failed", map[string]interface{}{"error": err.Error()})
		return
	}

	c.Json(w, http.StatusOK, "Students assigned to mentor successfully", map[string]interface{}{
		"cohort_id": body.CohortID,
		"mentor_id": body.MentorID,
		"assigned":  assigned,
		"skipped":   skipped,
	})
}

// Assigning Supervisors to Teams

func (c *Construct) AssignSupervisorsToTeam(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CohortID      uint64   `json:"cohort_id"`
		TeamName      string   `json:"team_name"`
		SupervisorIDs []uint64 `json:"supervisor_ids,omitempty"`
	}

	// --- Decode request ---
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid JSON body", map[string]interface{}{"error": err.Error()})
		return
	}
	if body.CohortID == 0 || body.TeamName == "" {
		c.Json(w, http.StatusBadRequest, "cohort_id and team_name are required", nil)
		return
	}

	// --- Get authenticated user ---
	currentUser, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// --- Check if user is a supervisor of this cohort ---
	var cohortSupervisor models.CohortUser
	if err := c.DB.Where("cohort_cohort_id = ? AND user_user_id = ? AND role = ?", body.CohortID, currentUser.UserID, "Supervisor").
		First(&cohortSupervisor).Error; err != nil {
		c.Json(w, http.StatusForbidden, "Only supervisors assigned to this cohort can create teams", nil)
		return
	}

	// --- Start transaction ---
	tx := c.DB.Begin()

	// --- Create team ---
	team := models.Team{
		Name:        body.TeamName,
		CohortRefID: &body.CohortID,
		CreatedByID: currentUser.UserID,
	}
	if err := tx.Create(&team).Error; err != nil {
		tx.Rollback()
		c.Json(w, http.StatusInternalServerError, "Failed to create team", map[string]interface{}{"error": err.Error()})
		return
	}

	assignedSupervisors := []uint64{}

	// --- Assign creator supervisor as team leader ---
	leaderAssignment := models.UserTeam{
		TeamRefID: team.TeamID,
		UserRefID: currentUser.UserID,
		Role:      "Supervisor",
		JoinedAt:  time.Now(),
	}
	if err := tx.Create(&leaderAssignment).Error; err != nil {
		tx.Rollback()
		c.Json(w, http.StatusInternalServerError, "Failed to assign creator as supervisor", map[string]interface{}{"error": err.Error()})
		return
	}
	assignedSupervisors = append(assignedSupervisors, currentUser.UserID)

	// --- Assign additional supervisors if provided ---
	for _, supID := range body.SupervisorIDs {
		if supID == currentUser.UserID {
			continue
		}

		// Check if supervisor is part of this cohort
		var supCheck models.CohortUser
		if err := tx.Where("cohort_cohort_id = ? AND user_user_id = ? AND role = ?", body.CohortID, supID, "Supervisor").
			First(&supCheck).Error; err != nil {
			continue
		}

		assignment := models.UserTeam{
			TeamRefID: team.TeamID,
			UserRefID: supID,
			Role:      "Supervisor",
			JoinedAt:  time.Now(),
		}
		if err := tx.Create(&assignment).Error; err != nil {
			tx.Rollback()
			c.Json(w, http.StatusInternalServerError, "Failed to assign supervisor to team", map[string]interface{}{"error": err.Error()})
			return
		}
		assignedSupervisors = append(assignedSupervisors, supID)
	}

	tx.Commit()

	// --- Notify assigned supervisors ---
	for _, supID := range assignedSupervisors {
		c.NotifyAndTrack(supID, "Team Assignment",
			fmt.Sprintf("You have been assigned to team '%s' in cohort %d", team.Name, body.CohortID),
			"Assignment", "Team", &team.TeamID, "", true)
	}

	c.Json(w, http.StatusCreated, "Team created successfully", map[string]interface{}{
		"team_id":              team.TeamID,
		"team_name":            team.Name,
		"cohort_id":            body.CohortID,
		"assigned_supervisors": assignedSupervisors,
	})
}

//============================"""""""""======================="""""""""======================
//                      Get Students Assigned to Mentors
//============================"""""""""======================="""""""""======================

func (c *Construct) GetMentorStudentAssignments(w http.ResponseWriter, r *http.Request) {
	// Authenticate user
	userUUID, ok := middlewares.GetUserUUIDFromContext(r.Context())
	if !ok || userUUID == "" {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var user models.User
	if err := c.DB.Preload("Role").Where("user_uuid = ?", userUUID).First(&user).Error; err != nil {
		c.Json(w, http.StatusUnauthorized, "User not found", nil)
		return
	}

	// Check if role is loaded (RoleID will be 0 if not loaded)
	if user.RoleID == 0 || user.Role.Name == "" {
		c.Json(w, http.StatusUnauthorized, "User role not found", nil)
		return
	}

	roleName := strings.ToLower(strings.TrimSpace(user.Role.Name))

	// Get optional query parameters
	cohortIDStr := r.URL.Query().Get("cohort_id")
	mentorIDStr := r.URL.Query().Get("mentor_id")

	var cohortID, mentorID uint64
	if cohortIDStr != "" {
		if id, err := strconv.ParseUint(cohortIDStr, 10, 64); err == nil {
			cohortID = id
		}
	}
	if mentorIDStr != "" {
		if id, err := strconv.ParseUint(mentorIDStr, 10, 64); err == nil {
			mentorID = id
		}
	}

	// Query structure to get mentor-student assignments
	var assignments []struct {
		AssignmentID  uint64
		CohortID      uint64
		CohortName    string
		MentorID      uint64
		MentorName    string
		MentorEmail   string
		StudentID     uint64
		StudentName   string
		StudentEmail  string
		CreatedByID   uint64
		CreatedByName string
		CreatedAt     time.Time
	}

	query := c.DB.Table("mentor_student_assignments as msa").
		Select(`
			msa.id as assignment_id,
			c.cohort_id,
			c.name as cohort_name,
			mentor.user_id as mentor_id,
			COALESCE(CONCAT(mp.first_name, ' ', mp.last_name), mentor.username) as mentor_name,
			mentor.email as mentor_email,
			student.user_id as student_id,
			COALESCE(CONCAT(sp.first_name, ' ', sp.last_name), student.username) as student_name,
			student.email as student_email,
			assigner.user_id as created_by_id,
			COALESCE(CONCAT(ap.first_name, ' ', ap.last_name), assigner.username) as created_by_name,
			msa.created_at as created_at
		`).
		Joins("JOIN cohorts c ON c.cohort_id = msa.cohort_ref_id").
		Joins("JOIN users mentor ON mentor.user_id = msa.mentor_ref_id").
		Joins("LEFT JOIN user_profiles mp ON mp.user_id = mentor.user_id").
		Joins("JOIN users student ON student.user_id = msa.student_ref_id").
		Joins("LEFT JOIN user_profiles sp ON sp.user_id = student.user_id").
		Joins("LEFT JOIN users assigner ON assigner.user_id = msa.created_by").
		Joins("LEFT JOIN user_profiles ap ON ap.user_id = assigner.user_id").
		Where("msa.deleted_at IS NULL")

	// Apply filters
	if cohortID > 0 {
		query = query.Where("msa.cohort_ref_id = ?", cohortID)
	}
	if mentorID > 0 {
		query = query.Where("msa.mentor_ref_id = ?", mentorID)
	}

	// Role-based visibility
	switch roleName {
	case "mentor":
		// Mentor sees only their own assignments
		query = query.Where("msa.mentor_ref_id = ?", user.UserID)

	case "supervisor":
		// Supervisor sees assignments in cohorts they supervise
		query = query.Where("c.cohort_id IN (?)",
			c.DB.Table("cohort_users").Select("cohort_cohort_id").
				Where("user_user_id = ? AND role = ?", user.UserID, "Supervisor"),
		)

	case "student":
		// Student sees only their own assignments
		query = query.Where("msa.student_ref_id = ?", user.UserID)

	case "opsadmin", "systemadmin":
		// Full visibility - no additional filter needed
	default:
		// Log the actual role name for debugging
		errorMsg := fmt.Sprintf("Access denied: role '%s' is not authorized for this action", user.Role.Name)
		c.Json(w, http.StatusForbidden, errorMsg, nil)
		return
	}

	// Execute query
	if err := query.Order("msa.created_at DESC").Scan(&assignments).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to fetch mentor-student assignments", map[string]interface{}{"error": err.Error()})
		return
	}

	// Helper for formatting names
	formatName := func(name string) string {
		name = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(name, "_", " ")))
		words := strings.Fields(name)
		for i, w := range words {
			if len(w) > 0 {
				words[i] = strings.ToUpper(string(w[0])) + strings.ToLower(w[1:])
			}
		}
		return strings.Join(words, " ")
	}

	// Fetch proposals for all students in their respective cohorts
	type ProposalInfo struct {
		ProposalID    uint64
		Title         string
		Category      string
		Status        string
		SubmittedByID uint64
		CohortID      *uint64
	}

	// Get unique student-cohort pairs
	studentCohortPairs := make(map[string]bool)
	for _, a := range assignments {
		key := fmt.Sprintf("%d-%d", a.StudentID, a.CohortID)
		studentCohortPairs[key] = true
	}

	// Query proposals for these student-cohort pairs
	var proposals []ProposalInfo
	if len(studentCohortPairs) > 0 {
		var studentIDs []uint64
		var cohortIDs []uint64
		for _, a := range assignments {
			studentIDs = append(studentIDs, a.StudentID)
			cohortIDs = append(cohortIDs, a.CohortID)
		}

		// Remove duplicates
		uniqueStudentIDs := make(map[uint64]bool)
		uniqueCohortIDs := make(map[uint64]bool)
		for _, id := range studentIDs {
			uniqueStudentIDs[id] = true
		}
		for _, id := range cohortIDs {
			uniqueCohortIDs[id] = true
		}

		var studentIDList []uint64
		var cohortIDList []uint64
		for id := range uniqueStudentIDs {
			studentIDList = append(studentIDList, id)
		}
		for id := range uniqueCohortIDs {
			cohortIDList = append(cohortIDList, id)
		}

		c.DB.Table("proposals").
			Select("proposals.proposal_id, proposals.title, proposals.category, proposals.status, proposals.submitted_by_id, proposals.proposal_cohort_id as cohort_id").
			Where("proposals.submitted_by_id IN ? AND proposals.proposal_cohort_id IN ? AND proposals.deleted_at IS NULL", studentIDList, cohortIDList).
			Order("proposals.created_at DESC").
			Scan(&proposals)
	}

	// Group proposals by student-cohort combination
	proposalsByStudentCohort := make(map[string][]ProposalInfo)
	for _, p := range proposals {
		if p.CohortID != nil {
			key := fmt.Sprintf("%d-%d", p.SubmittedByID, *p.CohortID)
			proposalsByStudentCohort[key] = append(proposalsByStudentCohort[key], p)
		}
	}

	// Group assignments by mentor
	mentorMap := make(map[uint64]map[string]interface{})
	studentKeys := make(map[string]bool) // Track unique student-cohort pairs to avoid duplicates

	for _, a := range assignments {
		mentorKey := a.MentorID
		cohortKey := fmt.Sprintf("%d", a.CohortID)
		studentCohortKey := fmt.Sprintf("%d-%d", a.StudentID, a.CohortID)

		// Initialize mentor entry if not exists
		if mentorMap[mentorKey] == nil {
			mentorMap[mentorKey] = make(map[string]interface{})
			mentorMap[mentorKey]["mentor"] = map[string]interface{}{
				"id":    a.MentorID,
				"name":  formatName(a.MentorName),
				"email": a.MentorEmail,
			}
			mentorMap[mentorKey]["cohorts"] = make(map[string]interface{})
		}

		cohorts := mentorMap[mentorKey]["cohorts"].(map[string]interface{})

		// Initialize cohort entry if not exists
		if cohorts[cohortKey] == nil {
			cohorts[cohortKey] = map[string]interface{}{
				"cohort_id":   a.CohortID,
				"cohort_name": a.CohortName,
				"students":    []map[string]interface{}{},
			}
		}

		cohort := cohorts[cohortKey].(map[string]interface{})
		students := cohort["students"].([]map[string]interface{})

		// Skip if we've already added this student-cohort combination
		if !studentKeys[studentCohortKey] {
			// Add student to list
			studentInfo := map[string]interface{}{
				"id":          a.StudentID,
				"name":        formatName(a.StudentName),
				"email":       a.StudentEmail,
				"assigned_at": a.CreatedAt,
				"proposals":   []map[string]interface{}{},
			}

			// Add assigned_by info for admins/supervisors/mentors
			if roleName != "student" {
				studentInfo["assigned_by"] = map[string]interface{}{
					"id":   a.CreatedByID,
					"name": formatName(a.CreatedByName),
				}
			}

			// Add proposals for this student in this cohort
			if studentProposals, exists := proposalsByStudentCohort[studentCohortKey]; exists {
				proposalsList := make([]map[string]interface{}, 0, len(studentProposals))
				for _, p := range studentProposals {
					proposalsList = append(proposalsList, map[string]interface{}{
						"proposal_id": p.ProposalID,
						"title":       p.Title,
						"category":    p.Category,
						"status":      p.Status,
					})
				}
				studentInfo["proposals"] = proposalsList
			}

			students = append(students, studentInfo)
			studentKeys[studentCohortKey] = true
			cohort["students"] = students
			cohorts[cohortKey] = cohort
			mentorMap[mentorKey]["cohorts"] = cohorts
		}
	}

	// Convert map to list
	result := make([]map[string]interface{}, 0, len(mentorMap))
	for _, mentorData := range mentorMap {
		// Convert cohorts map to list
		cohortsMap := mentorData["cohorts"].(map[string]interface{})
		cohortsList := make([]map[string]interface{}, 0, len(cohortsMap))
		for _, cohortData := range cohortsMap {
			cohortsList = append(cohortsList, cohortData.(map[string]interface{}))
		}
		mentorData["cohorts"] = cohortsList
		result = append(result, mentorData)
	}

	c.Json(w, http.StatusOK, "Mentor-student assignments fetched successfully", map[string]interface{}{
		"assignments": result,
		"count":       len(result),
	})
}
