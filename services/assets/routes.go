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

	// Serve static files from uploads directory
	r.PathPrefix("/uploads/").Handler(http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads/"))))

	r.HandleFunc("/assets", c.Index).Methods("GET")
	api := r.PathPrefix("/api").Subrouter()

	api.HandleFunc("/login", c.Login).Methods("POST")
	api.HandleFunc("/logout", c.Logout).Methods("POST")
	api.HandleFunc("/signup/student", c.SignupStudent).Methods("POST")
	api.HandleFunc("/signup/mentor", c.SignupMentor).Methods("POST")

	// google authentication routes
	api.HandleFunc("/signup/student/google", c.GoogleSignupStudent).Methods("POST")
	api.HandleFunc("/signup/mentor/google", c.GoogleSignupMentor).Methods("POST")
	api.HandleFunc("/signup/supervisor/google", c.GoogleSignupSupervisor).Methods("POST")

	// ---------------- AUTH / PASSWORD ----------------
	api.HandleFunc("/password/forgot", c.ForgotPassword).Methods("POST") // No auth, user provides email
	api.HandleFunc("/password/reset", c.ResetPassword).Methods("POST")   // No auth, user provides token & new password

	// email verification routes
	api.HandleFunc("/verify-email", c.VerifyEmail).Methods("GET")                     // Clickable link in email
	api.HandleFunc("/resend-verification", c.ResendVerificationEmail).Methods("POST") // Request new token
	api.HandleFunc("/email-verified", c.CheckEmailVerified).Methods("GET")

	// reference data routes
	api.HandleFunc("/lookups", c.GetAllLookups).Methods("GET")            // fetch all or filtered by types e.g category, program etc.
	api.HandleFunc("/lookups", c.CreateLookup).Methods("POST")            // create new lookup entry
	api.HandleFunc("/lookups/{type}/{id}", c.UpdateLookup).Methods("PUT") // update a specific lookup

	//---------------------------------------------------
	// ADMIN ROUTES (SystemAdmin + OpsAdmin)
	// --------------------------------------------------
	admin := api.PathPrefix("/admin").Subrouter()

	// Logs
	admin.Handle("/logs/audit", middlewares.RoleAuthorization(db, []string{"SystemAdmin", "OpsAdmin"}, "view_logs")(http.HandlerFunc(c.GetAuditLogs))).Methods("GET")
	admin.Handle("/logs/jobs", middlewares.RoleAuthorization(db, []string{"SystemAdmin"}, "view_logs")(http.HandlerFunc(c.GetJobLogs))).Methods("GET")

	// Change password
	admin.Handle("/password/change", middlewares.RoleAuthorization(db, []string{"SystemAdmin", "OpsAdmin"})(http.HandlerFunc(c.ChangePassword))).Methods("POST")

	// Users & Roles
	admin.Handle("/users", middlewares.RoleAuthorization(db, []string{"SystemAdmin", "OpsAdmin"})(http.HandlerFunc(c.GetUsers))).Methods("GET")
	admin.Handle("/user/create", middlewares.RoleAuthorization(db, []string{"SystemAdmin"}, "manage_system")(http.HandlerFunc(c.AdminCreateUser))).Methods("POST")
	admin.Handle("/user/create", middlewares.RoleAuthorization(db, []string{"SystemAdmin"}, "manage_system")(http.HandlerFunc(c.UpdateUserByAdmin))).Methods("PUT")
	admin.Handle("/students", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.GetStudents))).Methods("GET")
	admin.Handle("/supervisors", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_supervisors")(http.HandlerFunc(c.GetSupervisors))).Methods("GET")
	admin.Handle("/mentors", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_mentors")(http.HandlerFunc(c.GetMentors))).Methods("GET")
	admin.Handle("/users/status", middlewares.RoleAuthorization(db, []string{"SystemAdmin"}, "manage_system")(http.HandlerFunc(c.ToggleUserStatus))).Methods("PATCH")
	admin.Handle("/profile", middlewares.RoleAuthorization(db, []string{"SystemAdmin", "OpsAdmin"})(http.HandlerFunc(c.UpdateProfile))).Methods("PUT")
	admin.Handle("/profile/avatar", middlewares.RoleAuthorization(db, []string{"SyetemAdmin", "OpsAdmin"})(http.HandlerFunc(c.GetProfile))).Methods("POST")
	admin.Handle("/profile", middlewares.RoleAuthorization(db, []string{"SyetemAdmin", "OpsAdmin"})(http.HandlerFunc(c.GetProfile))).Methods("GET")

	// change user role
	admin.Handle("/user/{user_id}/roles", middlewares.RoleAuthorization(db, []string{"SystemAdmin"}, "manage_system")(http.HandlerFunc(c.ChangeUserRole))).Methods("PATCH")

	admin.Handle("/roles", middlewares.RoleAuthorization(db, []string{"SystemAdmin"}, "manage_roles")(http.HandlerFunc(c.CreateRole))).Methods("POST")
	admin.Handle("/roles", middlewares.RoleAuthorization(db, []string{"SystemAdmin"}, "manage_roles")(http.HandlerFunc(c.GetRoles))).Methods("GET")
	admin.Handle("/roles/{id}", middlewares.RoleAuthorization(db, []string{"SystemAdmin"}, "manage_roles")(http.HandlerFunc(c.UpdateRole))).Methods("PUT")
	admin.Handle("/roles/{id}", middlewares.RoleAuthorization(db, []string{"SystemAdmin"}, "manage_roles")(http.HandlerFunc(c.DeleteRole))).Methods("DELETE")

	// team management
	admin.Handle("/team", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.GetTeams))).Methods("GET")

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
	admin.Handle("/assign/supervisors", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "assign_users")(http.HandlerFunc(c.AssignSupervisorsToCohort))).Methods("POST")
	admin.Handle("/reassign/supervisors", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "assign_users")(http.HandlerFunc(c.ReassignSupervisorsToCohort))).Methods("PUT")
	admin.Handle("/unassign/supervisors", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "assign_users")(http.HandlerFunc(c.UnassignSupervisorsFromCohort))).Methods("DELETE")
	admin.Handle("/cohort/supervisors", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.GetCohortSupervisors))).Methods("GET")
	admin.Handle("/cohort/mentors", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.GetCohortMentors))).Methods("GET")
	admin.Handle("/cohort/members", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.GetCohortMembers))).Methods("GET")
	admin.Handle("/mentor/students", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.GetMentorStudentAssignments))).Methods("GET")

	// Tracking
	admin.Handle("/tracking/cohorts", middlewares.RoleAuthorization(db, []string{"Supervisor", "Mentor"}, "view_reports")(http.HandlerFunc(c.CohortTrackingHistory))).Methods("GET")
	admin.Handle("/tracking/proposals", middlewares.RoleAuthorization(db, []string{"Supervisor", "Mentor"}, "view_reports")(http.HandlerFunc(c.ProposalTrackingHistory))).Methods("GET")
	admin.Handle("/tracking", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "view_logs")(http.HandlerFunc(c.GetSystemHistory))).Methods("GET")

	// reports
	admin.Handle("/weekly/reports", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.GetWeeklyReports))).Methods("GET")

	//progress tracking
	admin.Handle("/tracking/progress", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_cohorts")(http.HandlerFunc(c.CreateCohortProgressEntity))).Methods("POST")
	admin.Handle("/progress", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_cohorts")(http.HandlerFunc(c.AddProgressItem))).Methods("POST")
	admin.Handle("/tracking/progress/{id}", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_cohorts")(http.HandlerFunc(c.UpdateProgressEntity))).Methods("PUT")
	admin.Handle("/progress/{id}", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_cohorts")(http.HandlerFunc(c.UpdateProgressItem))).Methods("PUT")
	admin.Handle("/tracking/progress", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.GetStudentProgressItems))).Methods("GET")
	admin.Handle("/progress", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.GetCohortProgressEntities))).Methods("GET")
	admin.Handle("/item/progress", middlewares.RoleAuthorization(db, []string{"OpsAdmin"})(http.HandlerFunc(c.GetTeamProgressEntities))).Methods("GET")
	admin.Handle("/mange/entities", middlewares.RoleAuthorization(db, []string{"OpsAdmin"})(http.HandlerFunc(c.ManageProgressEntities))).Methods("DELETE")
	admin.Handle("/manage/items", middlewares.RoleAuthorization(db, []string{"OpsAdmin"})(http.HandlerFunc(c.ManageProgressItems))).Methods("DELETE")

	// Reviews
	admin.Handle("/reviews", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_proposals")(http.HandlerFunc(c.AddReview))).Methods("POST")
	admin.Handle("/reviews", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_proposals")(http.HandlerFunc(c.GetReviews))).Methods("GET")
	admin.Handle("/reviews/comments", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "Student"})(http.HandlerFunc(c.GetMyReviews))).Methods("GET")
	admin.Handle("/reviews", middlewares.RoleAuthorization(db, []string{"OPsAdmin"}, "manage_proposals")(http.HandlerFunc(c.UpdateReviewByProposal))).Methods("PUT")
	admin.Handle("/reviews", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"}, "manage_proposals")(http.HandlerFunc(c.DeleteReview))).Methods("DELETE")

	// Proposals
	admin.Handle("/proposals", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "Supervisor"}, "manage_proposals")(http.HandlerFunc(c.GetProposals))).Methods("GET")
	admin.Handle("/proposals/{proposal_id}", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_proposals")(http.HandlerFunc(c.GetOwnProposals))).Methods("GET")
	admin.Handle("/proposals/archive", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_proposals")(http.HandlerFunc(c.ArchiveRestoreProposals))).Methods("PATCH")
	admin.Handle("/proposals/approved", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_proposals")(http.HandlerFunc(c.GetApprovedProposals))).Methods("GET")
	admin.Handle("/proposals/rejected", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_proposals")(http.HandlerFunc(c.GetRejectedProposals))).Methods("GET")
	admin.Handle("/proposals/archived", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_proposals")(http.HandlerFunc(c.GetArchivedProposals))).Methods("GET")

	// submission windows
	admin.Handle("/submission-windows", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_proposals")(http.HandlerFunc(c.CreateSubmissionWindow))).Methods("POST")
	admin.Handle("/submission-windows", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.GetSubmissionWindows))).Methods("GET")
	admin.Handle("/submission-windows/{id}", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_proposals")(http.HandlerFunc(c.UpdateSubmissionWindow))).Methods("PUT")
	admin.Handle("/submission-windows", middlewares.RoleAuthorization(db, []string{"OpsAdmin"}, "manage_proposals")(http.HandlerFunc(c.ManageSubmissionWindows))).Methods("DELETE")

	//events management routes
	admin.Handle("/events", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.CreateEvent))).Methods("POST")
	admin.Handle("/events", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.GetEvents))).Methods("GET")         // Get All events
	admin.Handle("/events/{id}", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.GetEventByID))).Methods("GET") // Get Events BY ID
	admin.Handle("/manage/events", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.ManageEvents))).Methods("DELETE")
	admin.Handle("/event/attendees", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.GetEventAttendees))).Methods("GET") // Get All attendees
	admin.Handle("/mark/attendees", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.MarkEventAttendance))).Methods("PUT")
	admin.Handle("/event/register", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.RegisterForEvents))).Methods("POST")
	admin.Handle("/event/unregister", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.UnregisterFromEvents))).Methods("POST")
	admin.Handle("/event/attended", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.GetAttendedUsers))).Methods("GET")
	admin.Handle("/event/absent", middlewares.RoleAuthorization(db, []string{"OpsAdmin", "SystemAdmin"})(http.HandlerFunc(c.GetAbsentUsers))).Methods("GET")

	// Resources management routes
	admin.Handle("/resources", middlewares.RoleAuthorization(db, []string{"OpsAdmin"})(http.HandlerFunc(c.CreateResource))).Methods("POST")
	admin.Handle("/resources/{id}", middlewares.RoleAuthorization(db, []string{"OpsAdmin"})(http.HandlerFunc(c.UpdateResource))).Methods("PUT")
	admin.Handle("/resources/{resource_id}", middlewares.RoleAuthorization(db, []string{"OpsAdmin"})(http.HandlerFunc(c.GetResourceByID))).Methods("GET") // Get A resource by ID
	admin.Handle("/resources", middlewares.RoleAuthorization(db, []string{"OpsAdmin"})(http.HandlerFunc(c.GetResources))).Methods("GET")                  // GEt ALl resources
	admin.Handle("/list/resources", middlewares.RoleAuthorization(db, []string{"OpsAdmin"})(http.HandlerFunc(c.ListResources))).Methods("GET")            // List resources per cohort, user, or team
	admin.Handle("/manage/resources", middlewares.RoleAuthorization(db, []string{"OpsAdmin"})(http.HandlerFunc(c.ManageResources))).Methods("DELETE")     // manage system resources

	// Downloads Routes for Admins
	admin.Handle("/download/resources/{resource_id}", middlewares.RoleAuthorization(db, []string{"OpsAdmin"})(http.HandlerFunc(c.DownloadResource))).Methods("GET")   // Download a Resource
	admin.Handle("/list/download/resources", middlewares.RoleAuthorization(db, []string{"OpsAdmin"})(http.HandlerFunc(c.ListDownloadableResources))).Methods("GET")   // Get download list
	admin.Handle("/download/proposal/{proposal_id}", middlewares.RoleAuthorization(db, []string{"OpsAdmin"})(http.HandlerFunc(c.DownloadProposal))).Methods("GET")    // Download a Proposal
	admin.Handle("/list/download/proposals", middlewares.RoleAuthorization(db, []string{"OpsAdmin"})(http.HandlerFunc(c.ListDownloadableProposals))).Methods("GET")   // Get download list
	admin.Handle("/download/reports/{report_id}", middlewares.RoleAuthorization(db, []string{"OpsAdmin"})(http.HandlerFunc(c.DownloadWeeklyReport))).Methods("GET")   // Download a Report (uploaded document)
	admin.Handle("/export/reports/{report_id}", middlewares.RoleAuthorization(db, []string{"OpsAdmin"})(http.HandlerFunc(c.ExportWeeklyReportData))).Methods("GET")   // Export report data (JSON)
	admin.Handle("/list/download/reports", middlewares.RoleAuthorization(db, []string{"OpsAdmin"})(http.HandlerFunc(c.ListDownloadableWeeklyReports))).Methods("GET") // Get download list

	// -----------------------------
	// SUPERVISOR ROUTES
	// -----------------------------
	supervisor := api.PathPrefix("/supervisor").Subrouter()
	supervisor.Use(middlewares.RoleAuthorization(db, []string{"Supervisor"}))

	// Change password
	supervisor.Handle("/password/change", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.ChangePassword))).Methods("POST")
	// get mentors

	supervisor.Handle("/mentors", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetMentors))).Methods("GET")

	supervisor.Handle("/users", middlewares.RoleAuthorization(db, []string{"Supervisor"}, "view_reports")(http.HandlerFunc(c.GetUsers))).Methods("GET")
	supervisor.Handle("/students", middlewares.RoleAuthorization(db, []string{"Supervisor"}, "view_reports")(http.HandlerFunc(c.GetStudents))).Methods("GET")
	supervisor.Handle("/profile", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.UpdateProfile))).Methods("PUT")
	supervisor.Handle("/profile", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetProfile))).Methods("GET")
	supervisor.Handle("/profile/avatar", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetProfile))).Methods("POST")

	// Events

	supervisor.Handle("/events", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetEvents))).Methods("GET")
	supervisor.Handle("/event/register", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.RegisterForEvents))).Methods("POST")
	supervisor.Handle("/event/unregister", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.UnregisterFromEvents))).Methods("POST")

	supervisor.Handle("/notifications", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetNotifications))).Methods("GET")
	supervisor.Handle("/notifications/mark", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.MarkNotificationRead))).Methods("PATCH")
	supervisor.Handle("/notifications/mark-all", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.MarkAllNotificationsRead))).Methods("PATCH")
	supervisor.Handle("/notifications", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.DeleteNotification))).Methods("DELETE")
	supervisor.Handle("/notifications", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.CreateNotificationHandler))).Methods("POST")

	supervisor.Handle("/cohorts", middlewares.RoleAuthorization(db, []string{"Supervisor"}, "view_reports")(http.HandlerFunc(c.GetCohorts))).Methods("GET")
	supervisor.Handle("/tracking/cohorts", middlewares.RoleAuthorization(db, []string{"Supervisor"}, "view_reports")(http.HandlerFunc(c.CohortTrackingHistory))).Methods("GET")
	supervisor.Handle("/tracking/proposals", middlewares.RoleAuthorization(db, []string{"Supervisor"}, "view_reports")(http.HandlerFunc(c.ProposalTrackingHistory))).Methods("GET")

	//progress tracking
	supervisor.Handle("/tracking/progress", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.CreateCohortProgressEntity))).Methods("POST")
	supervisor.Handle("/progress", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.AddProgressItem))).Methods("POST")
	supervisor.Handle("/tracking/progress", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetStudentProgressItems))).Methods("GET")
	supervisor.Handle("/tracking/progress/{id}", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.UpdateProgressEntity))).Methods("PUT")
	supervisor.Handle("/progress/{id}", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.UpdateProgressItem))).Methods("PUT")
	supervisor.Handle("/tracking/progress", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.ManageProgressItems))).Methods("DELETE")
	supervisor.Handle("/progress", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetCohortProgressEntities))).Methods("GET")

	// Cohort Mentor Assignments
	supervisor.Handle("/cohort/supervisors", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetCohortSupervisors))).Methods("GET")
	supervisor.Handle("/cohort/members", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetCohortMembers))).Methods("GET")
	supervisor.Handle("/assign/mentors", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.AssignMentorsToCohort))).Methods("POST")
	supervisor.Handle("/reassign/mentors", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.ReassignMentors))).Methods("PUT")
	supervisor.Handle("/unassign/mentors", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.UnassignMentors))).Methods("DELETE")
	supervisor.Handle("/cohort/mentors", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetCohortMentors))).Methods("GET")

	//Student Mentor Assignments
	supervisor.Handle("/assign", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.AssignStudentsToMentor))).Methods("POST")
	supervisor.Handle("/assign", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.AssignSupervisorsToTeam))).Methods("POST")
	supervisor.Handle("/mentor/students", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetMentorStudentAssignments))).Methods("GET")

	// team management
	supervisor.Handle("/team", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.CreateTeam))).Methods("POST")
	supervisor.Handle("/team", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.UpdateTeam))).Methods("PUT")
	supervisor.Handle("/team", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetTeams))).Methods("GET")
	supervisor.Handle("/team", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.ManageTeamSafe))).Methods("DELETE")

	// Individual Students reports
	supervisor.Handle("/weekly/reports", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.AddOrReplyReportComment))).Methods("POST")
	supervisor.Handle("/weekly/reports", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetWeeklyReports))).Methods("GET")
	supervisor.Handle("/weekly/reports", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.ManageWeeklyReports))).Methods("DELETE")
	supervisor.Handle("/reports/{id}", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.ApproveOrSendBackWeeklyReport))).Methods("PATCH")

	// Team Reports and progress items

	supervisor.Handle("/team/reports", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetWeeklyReportsByTeam))).Methods("GET")
	supervisor.Handle("/team/reports/{id}", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.ApproveOrSendBackTeamReport))).Methods("PATCH")
	supervisor.Handle("/team/progress", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetTeamProgressEntities))).Methods("GET")
	supervisor.Handle("/manage/reports", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.ManageTeamReports))).Methods("DELETE")

	// mentor feedback
	supervisor.Handle("/mentor/feedback", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetMentorFeedback))).Methods("GET")
	supervisor.Handle("/mentor/feedback", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.ManageMentorFeedbacks))).Methods("DELETE")

	// Resources management routes
	supervisor.Handle("/resources", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.CreateResource))).Methods("POST")
	supervisor.Handle("/resources/{id}", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.UpdateResource))).Methods("PUT")
	supervisor.Handle("/resources/{resource_id}", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetResourceByID))).Methods("GET") // Get A resource by ID
	supervisor.Handle("/resources", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.GetResources))).Methods("GET")                  // GEt ALl resources
	supervisor.Handle("/list/resources", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.ListResources))).Methods("GET")            // List resources per cohort, user, or team

	// Download Routes
	supervisor.Handle("/list/download/resources", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.ListDownloadableResources))).Methods("GET")   // Retrieve download list
	supervisor.Handle("/download/proposal/{proposal_id}", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.DownloadProposal))).Methods("GET")    // Download a Proposal
	supervisor.Handle("/list/download/proposals", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.ListDownloadableProposals))).Methods("GET")   // retrieve download list
	supervisor.Handle("/download/reports/{report_id}", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.DownloadWeeklyReport))).Methods("GET")   // Download a Report (uploaded document)
	supervisor.Handle("/export/reports/{report_id}", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.ExportWeeklyReportData))).Methods("GET")   // Export report data (JSON)
	supervisor.Handle("/list/download/reports", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.ListDownloadableWeeklyReports))).Methods("GET") // retrieve download list
	supervisor.Handle("/download/resources/{resource_id}", middlewares.RoleAuthorization(db, []string{"Supervisor"})(http.HandlerFunc(c.DownloadResource))).Methods("GET")   // Download a Resource

	// -----------------------------
	// MENTOR ROUTES
	// -----------------------------
	mentor := api.PathPrefix("/mentor").Subrouter()
	mentor.Use(middlewares.RoleAuthorization(db, []string{"Mentor"}))

	// Change password
	mentor.Handle("/password/change", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.ChangePassword))).Methods("POST")

	mentor.Handle("/users", middlewares.RoleAuthorization(db, []string{"Mentor"}, "view_reports")(http.HandlerFunc(c.GetUsers))).Methods("GET")
	mentor.Handle("/students", middlewares.RoleAuthorization(db, []string{"Mentor"}, "view_reports")(http.HandlerFunc(c.GetStudents))).Methods("GET")
	mentor.Handle("/profile", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.UpdateProfile))).Methods("PUT")
	mentor.Handle("/profile", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.GetProfile))).Methods("GET")
	mentor.Handle("/profile/avatar", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.GetProfile))).Methods("POST")

	mentor.Handle("/notifications", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.GetNotifications))).Methods("GET")
	mentor.Handle("/notifications/mark", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.MarkNotificationRead))).Methods("PATCH")
	mentor.Handle("/notifications/mark-all", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.MarkAllNotificationsRead))).Methods("PATCH")
	mentor.Handle("/notifications", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.DeleteNotification))).Methods("DELETE")

	//progress tracking
	mentor.Handle("/tracking/progress", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.AddProgressItem))).Methods("POST")
	mentor.Handle("/tracking/progress", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.GetStudentProgressItems))).Methods("GET")
	mentor.Handle("/progress/{id}", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.UpdateProgressItem))).Methods("PUT")
	mentor.Handle("/progress", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.GetCohortProgressEntities))).Methods("GET")

	mentor.Handle("/cohorts", middlewares.RoleAuthorization(db, []string{"Mentor"}, "view_reports")(http.HandlerFunc(c.GetCohorts))).Methods("GET")
	mentor.Handle("/tracking/cohorts", middlewares.RoleAuthorization(db, []string{"Mentor"}, "view_reports")(http.HandlerFunc(c.CohortTrackingHistory))).Methods("GET")
	mentor.Handle("/cohort/mentors", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.GetCohortMentors))).Methods("GET")
	mentor.Handle("/cohort/members", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.GetCohortMembers))).Methods("GET")
	mentor.Handle("/students", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.GetMentorStudentAssignments))).Methods("GET")

	// mentor feedback routes
	mentor.Handle("/mentor/feedback", middlewares.RoleAuthorization(db, []string{"mentor"})(http.HandlerFunc(c.CreateMentorFeedback))).Methods("POST")
	mentor.Handle("/mentor/feedback", middlewares.RoleAuthorization(db, []string{"mentor"})(http.HandlerFunc(c.GetMentorFeedback))).Methods("GET")
	mentor.Handle("/mentor/feedback", middlewares.RoleAuthorization(db, []string{"mentor"})(http.HandlerFunc(c.UpdateMentorFeedback))).Methods("PUT")
	mentor.Handle("/mentor/feedback", middlewares.RoleAuthorization(db, []string{"mentor"})(http.HandlerFunc(c.ManageMentorFeedbacks))).Methods("DELETE")

	// team management
	mentor.Handle("/team", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.GetTeams))).Methods("GET")
	mentor.Handle("/team/reports", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.GetWeeklyReportsByTeam))).Methods("GET")
	mentor.Handle("/team/progress", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.GetTeamProgressEntities))).Methods("GET")

	// reports
	mentor.Handle("/weekly/reports", middlewares.RoleAuthorization(db, []string{"mentor"})(http.HandlerFunc(c.AddOrReplyReportComment))).Methods("POST")
	mentor.Handle("/weekly/reports", middlewares.RoleAuthorization(db, []string{"mentor"})(http.HandlerFunc(c.GetWeeklyReports))).Methods("GET")

	// events

	mentor.Handle("/events", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.GetEvents))).Methods("GET")
	mentor.Handle("/event/register", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.RegisterForEvents))).Methods("POST")
	mentor.Handle("/event/unregister", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.UnregisterFromEvents))).Methods("POST")

	// Resources management routes
	mentor.Handle("/resources", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.CreateResource))).Methods("POST")
	mentor.Handle("/resources/{id}", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.UpdateResource))).Methods("PUT")
	mentor.Handle("/resources/{resource_id}", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.GetResourceByID))).Methods("GET") // Get A resource by ID
	mentor.Handle("/resources", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.GetResources))).Methods("GET")                  // GEt ALl resources
	mentor.Handle("/list/resources", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.ListResources))).Methods("GET")            // List resources per cohort, user, or team

	// Downloads routes
	mentor.Handle("/download/resources/{resource_id}", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.DownloadResource))).Methods("GET")   // Download a Resource
	mentor.Handle("/list/download/resources", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.ListDownloadableResources))).Methods("GET")   // Get download list
	mentor.Handle("/download/proposal/{proposal_id}", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.DownloadProposal))).Methods("GET")    // Download a Proposal
	mentor.Handle("/list/download/proposals", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.ListDownloadableProposals))).Methods("GET")   // Get download list
	mentor.Handle("/download/reports/{report_id}", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.DownloadWeeklyReport))).Methods("GET")   // Download a Report
	mentor.Handle("/list/download/reports", middlewares.RoleAuthorization(db, []string{"Mentor"})(http.HandlerFunc(c.ListDownloadableWeeklyReports))).Methods("GET") // Get download list

	// -----------------------------
	// STUDENT ROUTES
	// -----------------------------
	student := api.PathPrefix("/student").Subrouter()
	student.Use(middlewares.RoleAuthorization(db, []string{"Student"}))

	// Change password
	student.Handle("/password/change", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.ChangePassword))).Methods("POST")

	student.Handle("/profile", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetProfile))).Methods("GET")
	student.Handle("/profile", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.UpdateProfile))).Methods("PUT")
	student.Handle("/profile/avatar", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.UpdateAvatar))).Methods("PATCH")
	student.Handle("/profile/avatar", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetProfile))).Methods("POST")

	student.Handle("/notifications", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetNotifications))).Methods("GET")
	student.Handle("/notifications/mark", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.MarkNotificationRead))).Methods("PATCH")
	student.Handle("/notifications/mark-all", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.MarkAllNotificationsRead))).Methods("PATCH")
	student.Handle("/notifications", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.DeleteNotification))).Methods("DELETE")
	// cohorts
	student.Handle("/cohorts", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetCohorts))).Methods("GET")
	student.Handle("/cohort/members", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetCohortMembers))).Methods("GET")
	student.Handle("/tracking/progress", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.AddProgressItem))).Methods("POST")
	student.Handle("/progress/{id}", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.UpdateProgressItem))).Methods("PUT")
	student.Handle("/tracking/progress", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetStudentProgressItems))).Methods("GET")
	student.Handle("/tracking/progress", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.ManageProgressItems))).Methods("DELETE")

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

	// mentor feedback
	student.Handle("/mentor/feedback", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetMentorFeedback))).Methods("GET")
	student.Handle("/mentor/feedback", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.ManageMentorFeedbacks))).Methods("DELETE")

	// mentor assignments
	student.Handle("/mentor/assignments", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetMentorStudentAssignments))).Methods("GET")

	// team checks
	student.Handle("/team", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetTeams))).Methods("GET")

	// Individual Student reports

	student.Handle("/weekly/reports", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.CreateWeeklyReport))).Methods("POST")
	student.Handle("/weekly/reports", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.UpdateWeeklyReport))).Methods("PUT")
	student.Handle("/weekly/reports", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.AddOrReplyReportComment))).Methods("POST")
	student.Handle("/weekly/reports", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetWeeklyReports))).Methods("GET")

	// Team progress Items
	student.Handle("/team/progress", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.AddTeamProgressItem))).Methods("POST")
	student.Handle("/team/progress/{id}", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.UpdateTeamProgressItem))).Methods("PUT")
	student.Handle("/team/progress", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetTeamProgressEntities))).Methods("GET")

	// Team Reports

	student.Handle("/team/reports", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.CreateTeamWeeklyReport))).Methods("POST")
	student.Handle("/team/reports", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.UpdateTeamWeeklyReport))).Methods("PUT")
	student.Handle("/team/reports", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetWeeklyReportsByTeam))).Methods("GET")

	// events

	student.Handle("/events", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetEvents))).Methods("GET") // Get All Events
	student.Handle("/event/register", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.RegisterForEvents))).Methods("POST")
	student.Handle("/event/unregister", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.UnregisterFromEvents))).Methods("POST")

	// Resources management routes
	student.Handle("/resources", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.CreateResource))).Methods("POST")
	student.Handle("/resources/{id}", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.UpdateResource))).Methods("PUT")
	student.Handle("/resources/{resource_id}", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetResourceByID))).Methods("GET") // Get A resource by ID
	student.Handle("/resources", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetResources))).Methods("GET")                  // GEt ALl resources
	student.Handle("/list/resources", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.ListResources))).Methods("GET")            // List resources per

	student.Handle("/progress/entities", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.GetCohortProgressEntities))).Methods("GET")

	// Download Routes
	student.Handle("/download/resources/{resource_id}", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.DownloadResource))).Methods("GET")   // Download a Resource
	student.Handle("/list/download/resources", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.ListDownloadableResources))).Methods("GET")   // Get download list
	student.Handle("/download/proposal/{proposal_id}", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.DownloadProposal))).Methods("GET")    // Download a Proposal
	student.Handle("/list/download/proposals", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.ListDownloadableProposals))).Methods("GET")   // Get download list
	student.Handle("/download/reports/{report_id}", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.DownloadWeeklyReport))).Methods("GET")   // Download a Report (uploaded document)
	student.Handle("/export/reports/{report_id}", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.ExportWeeklyReportData))).Methods("GET")   // Export report data (JSON)
	student.Handle("/list/download/reports", middlewares.RoleAuthorization(db, []string{"Student"})(http.HandlerFunc(c.ListDownloadableWeeklyReports))).Methods("GET") // Get download list
}
