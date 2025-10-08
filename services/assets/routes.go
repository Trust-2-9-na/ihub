package assets

import (
	"net/http"
	"web/libs/database"
	"web/services/assets/controllers"
	"web/services/assets/middlewares"

	"github.com/gorilla/mux"
	"gorm.io/gorm"
)

func NewRouter(r *mux.Router, DB *gorm.DB) {
	c := &controllers.Construct{DB: DB}
	// -----------------------------
	// PUBLIC ROUTES
	// -----------------------------

	dbService := database.New()
	db := dbService.DB()

	r.HandleFunc("/assets", c.Index).Methods("GET")
	api := r.PathPrefix("/api").Subrouter()

	api.HandleFunc("/login", c.Login).Methods("POST")
	api.HandleFunc("/signup/student", c.SignupStudent).Methods("POST")
	api.HandleFunc("/signup/mentor", c.SignupMentor).Methods("POST")
	api.HandleFunc("/signup/supervisor", c.SignupSupervisor).Methods("POST")

	//---------------------------------------------------
	// ADMIN ROUTES (SystemAdmin + OpsAdmin)
	// --------------------------------------------------
	admin := api.PathPrefix("/admin").Subrouter()

	// Logs
	admin.Handle("/logs/audit", middlewares.RoleAuthorization(db, []string{"SystemAdmin", "OpsAdmin"}, "view_logs")(http.HandlerFunc(c.GetAuditLogs))).Methods("GET")
	admin.Handle("/logs/jobs", middlewares.RoleAuthorization(db, []string{"SystemAdmin"}, "view_logs")(http.HandlerFunc(c.GetJobLogs))).Methods("GET")

	// Users & Roles
	admin.Handle("/users", middlewares.RoleAuthorization(db, []string{"SystemAdmin", "OpsAdmin"}, "manage_supervisors")(http.HandlerFunc(c.GetUsers))).Methods("GET")
	admin.Handle("/students", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "assign_users")(http.HandlerFunc(c.GetStudents))).Methods("GET")
	admin.Handle("/supervisors", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_supervisors")(http.HandlerFunc(c.GetSupervisors))).Methods("GET")
	admin.Handle("/mentors", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_mentors")(http.HandlerFunc(c.GetMentors))).Methods("GET")
	admin.Handle("/users/{uuid}/status", middlewares.RoleAuthorization(db, []string{"SystemAdmin"}, "manage_system")(http.HandlerFunc(c.ToggleUserStatus))).Methods("PATCH")

	admin.Handle("/roles", middlewares.RoleAuthorization(db, []string{"SystemAdmin"}, "manage_roles")(http.HandlerFunc(c.CreateRole))).Methods("POST")
	admin.Handle("/roles", middlewares.RoleAuthorization(db, []string{"SystemAdmin"}, "manage_roles")(http.HandlerFunc(c.GetRoles))).Methods("GET")
	admin.Handle("/roles/{id}", middlewares.RoleAuthorization(db, []string{"SystemAdmin"}, "manage_roles")(http.HandlerFunc(c.UpdateRole))).Methods("PUT")
	admin.Handle("/roles/{id}", middlewares.RoleAuthorization(db, []string{"SystemAdmin"}, "manage_roles")(http.HandlerFunc(c.DeleteRole))).Methods("DELETE")

	// Notifications
	admin.Handle("/notifications", middlewares.RoleAuthorization(db, []string{"SystemAdmin", "OpsAdmin"})(http.HandlerFunc(c.GetNotifications))).Methods("GET")
	admin.Handle("/notifications/mark", middlewares.RoleAuthorization(db, []string{"SystemAdmin", "OpsAdmin"}, "manage_system")(http.HandlerFunc(c.MarkNotificationRead))).Methods("PATCH")
	admin.Handle("/notifications/mark-all", middlewares.RoleAuthorization(db, []string{"SystemAdmin", "OpsAdmin"}, "manage_system")(http.HandlerFunc(c.MarkAllNotificationsRead))).Methods("PATCH")
	admin.Handle("/notifications", middlewares.RoleAuthorization(db, []string{"SystemAdmin", "OPsAdmin"}, "manage_system")(http.HandlerFunc(c.DeleteNotification))).Methods("DELETE")
	admin.Handle("/notifications", middlewares.RoleAuthorization(db, []string{"SystemAdmin"}, "manage_system")(http.HandlerFunc(c.AdminDeleteNotification))).Methods("DELETE")
	admin.Handle("/notifications", middlewares.RoleAuthorization(db, []string{"SystemAdmin"}, "manage_system")(http.HandlerFunc(c.CreateNotificationHandler))).Methods("POST")

	// Cohorts
	admin.Handle("/cohorts", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_cohorts")(http.HandlerFunc(c.CreateCohort))).Methods("POST")
	admin.Handle("/cohorts", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"}, "manage_cohorts")(http.HandlerFunc(c.GetCohorts))).Methods("GET")
	admin.Handle("/cohorts/{cohort_id}", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_cohorts")(http.HandlerFunc(c.UpdateCohort))).Methods("PUT")
	admin.Handle("/cohorts", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_cohorts")(http.HandlerFunc(c.DeleteCohorts))).Methods("DELETE")
	admin.Handle("/cohorts/archive", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_cohorts")(http.HandlerFunc(c.ArchiveCohort))).Methods("PATCH")
	admin.Handle("/cohorts/restore", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_cohorts")(http.HandlerFunc(c.RestoreCohort))).Methods("PATCH")

	// Cohort Assignments
	admin.Handle("/students/remove", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "assign_users")(http.HandlerFunc(c.RemoveStudentFromCohort))).Methods("DELETE")
	admin.Handle("/assign/supervisors", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "assign_users")(http.HandlerFunc(c.AssignSupervisorToCohort))).Methods("POST")
	admin.Handle("/reassign/supervisors", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "assign_users")(http.HandlerFunc(c.AssignSupervisorToCohort))).Methods("PUT")
	admin.Handle("/unassign/supervisors", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "assign_users")(http.HandlerFunc(c.UnassignSupervisorFromCohort))).Methods("DELETE")
	admin.Handle("/cohort/supervisors", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "assign_users")(http.HandlerFunc(c.GetCohortSupervisors))).Methods("GET")

	// Tracking
	admin.Handle("/tracking/cohorts", middlewares.RoleAuthorization(db, []string{"Supervisor", "Mentor"}, "view_reports")(http.HandlerFunc(c.CohortTrackingHistory))).Methods("GET")
	admin.Handle("/tracking/proposals", middlewares.RoleAuthorization(db, []string{"Supervisor", "Mentor"}, "view_reports")(http.HandlerFunc(c.ProposalTrackingHistory))).Methods("GET")

	// Reviews
	admin.Handle("/reviews", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_proposals")(http.HandlerFunc(c.AddReview))).Methods("POST")
	admin.Handle("/reviews", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_proposals")(http.HandlerFunc(c.GetReviews))).Methods("GET")
	admin.Handle("/reviews/comments", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "Student"})(http.HandlerFunc(c.GetMyReviews))).Methods("GET")
	admin.Handle("/reviews", middlewares.RoleAuthorization(db, []string{"OPsAdmin"}, "manage_proposal_reviews")(http.HandlerFunc(c.UpdateReviewByProposal))).Methods("PUT")
	admin.Handle("/reviews", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"}, "manage_proposals")(http.HandlerFunc(c.DeleteReview))).Methods("DELETE")

	// Proposals
	admin.Handle("/proposals", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "Supervisor"}, "manage_proposals")(http.HandlerFunc(c.GetProposals))).Methods("GET")
	admin.Handle("/proposals/{proposal_id}", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_proposals")(http.HandlerFunc(c.GetOwnProposals))).Methods("GET")
	admin.Handle("/proposals/archive", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_proposals")(http.HandlerFunc(c.ArchiveRestoreProposals))).Methods("PATCH")
	admin.Handle("/proposals/approved", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_proposals")(http.HandlerFunc(c.GetApprovedProposals))).Methods("GET")
	admin.Handle("/proposals/rejected", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_proposals")(http.HandlerFunc(c.GetRejectedProposals))).Methods("GET")
	admin.Handle("/proposals/archived", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_proposals")(http.HandlerFunc(c.GetArchivedProposals))).Methods("GET")
	admin.Handle("/submission-windows", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_proposals")(http.HandlerFunc(c.CreateSubmissionWindow))).Methods("POST")
	admin.Handle("/submission-windows", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"}, "manage_proposals")(http.HandlerFunc(c.GetSubmissionWindows))).Methods("GET")

	// -----------------------------
	// SUPERVISOR ROUTES
	// -----------------------------
	supervisor := api.PathPrefix("/supervisor").Subrouter()
	supervisor.Use(middlewares.RoleAuthorization(db, []string{"Supervisor"}))

	supervisor.Handle("/users", middlewares.RoleAuthorization(db, []string{"Supervisor"}, "view_reports")(http.HandlerFunc(c.GetUsers))).Methods("GET")
	supervisor.Handle("/students", middlewares.RoleAuthorization(db, []string{"Supervisor"}, "view_reports")(http.HandlerFunc(c.GetStudents))).Methods("GET")
	supervisor.Handle("/profile", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.UpdateProfile))).Methods("PUT")
	supervisor.Handle("/profile", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetProfile))).Methods("GET")

	supervisor.Handle("/notifications", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetNotifications))).Methods("GET")
	supervisor.Handle("/notifications/mark", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.MarkNotificationRead))).Methods("PATCH")
	supervisor.Handle("/notifications/mark-all", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.MarkAllNotificationsRead))).Methods("PATCH")
	supervisor.Handle("/notifications", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.DeleteNotification))).Methods("DELETE")
	supervisor.Handle("/notifications", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.CreateNotificationHandler))).Methods("POST")

	supervisor.Handle("/cohorts", middlewares.RoleAuthorization(db, []string{"Supervisor"}, "view_reports")(http.HandlerFunc(c.GetCohorts))).Methods("GET")
	supervisor.Handle("/tracking/cohorts", middlewares.RoleAuthorization(db, []string{"Supervisor"}, "view_reports")(http.HandlerFunc(c.CohortTrackingHistory))).Methods("GET")
	supervisor.Handle("/tracking/proposals", middlewares.RoleAuthorization(db, []string{"Supervisor"}, "view_reports")(http.HandlerFunc(c.ProposalTrackingHistory))).Methods("GET")

	// Cohort Mentor Assignments
	supervisor.Handle("/cohort/{id}/supervisors", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetCohortSupervisors))).Methods("GET")

	// -----------------------------
	// MENTOR ROUTES
	// -----------------------------
	mentor := api.PathPrefix("/mentor").Subrouter()
	mentor.Use(middlewares.RoleAuthorization(db, []string{"Mentor"}))

	mentor.Handle("/users", middlewares.RoleAuthorization(db, []string{"Mentor"}, "view_reports")(http.HandlerFunc(c.GetUsers))).Methods("GET")
	mentor.Handle("/students", middlewares.RoleAuthorization(db, []string{"Mentor"}, "view_reports")(http.HandlerFunc(c.GetStudents))).Methods("GET")
	mentor.Handle("/profile", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.UpdateProfile))).Methods("PUT")
	mentor.Handle("/profile", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.GetProfile))).Methods("GET")

	mentor.Handle("/notifications", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.GetNotifications))).Methods("GET")
	mentor.Handle("/notifications/mark", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.MarkNotificationRead))).Methods("PATCH")
	mentor.Handle("/notifications/mark-all", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.MarkAllNotificationsRead))).Methods("PATCH")
	mentor.Handle("/notifications", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.DeleteNotification))).Methods("DELETE")

	mentor.Handle("/cohorts", middlewares.RoleAuthorization(db, []string{"Mentor"}, "view_reports")(http.HandlerFunc(c.GetCohorts))).Methods("GET")
	mentor.Handle("/tracking/cohorts", middlewares.RoleAuthorization(db, []string{"Mentor"}, "view_reports")(http.HandlerFunc(c.CohortTrackingHistory))).Methods("GET")

	// -----------------------------
	// STUDENT ROUTES
	// -----------------------------
	student := api.PathPrefix("/student").Subrouter()
	student.Use(middlewares.RoleAuthorization(db, []string{"Student"}))

	student.Handle("/profile", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetProfile))).Methods("GET")
	student.Handle("/profile", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.UpdateProfile))).Methods("PUT")
	student.Handle("/profile/avatar", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.UpdateAvatar))).Methods("PATCH")

	student.Handle("/notifications", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetNotifications))).Methods("GET")
	student.Handle("/notifications/mark", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.MarkNotificationRead))).Methods("PATCH")
	student.Handle("/notifications/mark-all", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.MarkAllNotificationsRead))).Methods("PATCH")
	student.Handle("/notifications", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.DeleteNotification))).Methods("DELETE")

	student.Handle("/cohorts", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetCohorts))).Methods("GET")
	student.Handle("/proposals", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.CreateProposal))).Methods("POST")
	student.Handle("/proposals", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetOwnProposals))).Methods("GET")
	student.Handle("/proposals/{proposal_id}", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.UpdateProposal))).Methods("PUT")
	student.Handle("/proposals/archived", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetArchivedProposals))).Methods("GET")
	student.Handle("/proposals/archive", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.ArchiveRestoreProposals))).Methods("PATCH")
	student.Handle("/reviews", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetReviews))).Methods("GET")
	student.Handle("/submission-windows", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetSubmissionWindows))).Methods("GET")
	student.Handle("/proposals", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.DeleteProposal))).Methods("DELETE")
	student.Handle("/proposals/approved", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetApprovedProposals))).Methods("GET")
	student.Handle("/proposals/rejected", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetRejectedProposals))).Methods("GET")
	student.Handle("/reviews", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.DeleteReview))).Methods("DELETE")
	student.Handle("/tracking/proposals", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.ProposalTrackingHistory))).Methods("GET")
	student.Handle("/tracking/cohorts", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.CohortTrackingHistory))).Methods("GET")

}
