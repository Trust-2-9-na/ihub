package assets

import (
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

	// -----------------------------
	// ADMIN ROUTES
	// -----------------------------
	admin := api.PathPrefix("/admin").Subrouter()
	admin.Use(middlewares.RoleAuthorization(db, "admin"))
	admin.HandleFunc("/logs/audit", c.GetAuditLogs).Methods("GET")
	admin.HandleFunc("/logs/jobs", c.GetJobLogs).Methods("GET")
	admin.HandleFunc("/users", c.GetUsers).Methods("GET")
	admin.HandleFunc("/students", c.GetStudents).Methods("GET")
	admin.HandleFunc("/mentors", c.GetMentors).Methods("GET")
	admin.HandleFunc("/supervisors", c.GetSupervisors).Methods("GET")
	admin.HandleFunc("/users/{uuid}/status", c.ToggleUserStatus).Methods("PATCH")
	admin.HandleFunc("/roles", c.CreateRole).Methods("POST")
	admin.HandleFunc("/roles", c.GetRoles).Methods("GET")
	admin.HandleFunc("/roles/{id}", c.UpdateRole).Methods("PUT")
	admin.HandleFunc("/roles/{id}", c.DeleteRole).Methods("DELETE")
	admin.HandleFunc("/notifications", c.GetNotifications).Methods("GET")
	admin.HandleFunc("/notifications/mark", c.MarkNotificationRead).Methods("PATCH")
	admin.HandleFunc("/notifications/mark-all", c.MarkAllNotificationsRead).Methods("PATCH")
	admin.HandleFunc("/notifications", c.DeleteNotification).Methods("DELETE")
	admin.HandleFunc("/notifications", c.AdminDeleteNotification).Methods("DELETE")
	admin.HandleFunc("/notifications", c.CreateNotificationHandler).Methods("POST")
	admin.HandleFunc("/notifications", c.MarkAllNotificationsRead).Methods("PATCH")
	admin.HandleFunc("/cohorts", c.CreateCohort).Methods("POST")
	admin.HandleFunc("/cohorts", c.DeleteCohorts).Methods("DELETE")
	admin.HandleFunc("/cohorts", c.GetCohorts).Methods("GET")
	admin.HandleFunc("/cohorts/{cohort_id}", c.UpdateCohort).Methods("PUT")
	admin.HandleFunc("/cohorts/archive", c.ArchiveCohort).Methods("PATCH")
	admin.HandleFunc("/cohorts/restore", c.RestoreCohort).Methods("PATCH")
	admin.HandleFunc("/cohorts", c.CreateCohort).Methods("POST")
	admin.HandleFunc("/students/remove", c.RemoveStudentFromCohort).Methods("DELETE")
	admin.HandleFunc("/tracking/cohorts", c.CohortTrackingHistory).Methods("GET")
	admin.HandleFunc("/reviews", c.GetReviews).Methods("GET") // list all reviews
	admin.HandleFunc("/reviews/comments", c.GetMyReviews).Methods("GET")
	admin.HandleFunc("/reviews", c.UpdateReviewByProposal).Methods("PUT")
	admin.HandleFunc("/reviews", c.AddReview).Methods("POST")
	admin.HandleFunc("/reviews/{review_id}", c.DeleteReview).Methods("DELETE")
	admin.HandleFunc("/proposals", c.GetProposals).Methods("GET")                  // list all proposals
	admin.HandleFunc("/proposals/{proposal_id}", c.GetOwnProposals).Methods("GET") // view single proposal
	admin.HandleFunc("/proposals/archive", c.ArchiveRestoreProposals).Methods("PATCH")
	admin.HandleFunc("/proposals/approved", c.GetApprovedProposals).Methods("GET")
	admin.HandleFunc("/proposals/rejected", c.GetRejectedProposals).Methods("GET")
	admin.HandleFunc("/reviews/{review_id}", c.DeleteReview).Methods("DELETE")
	admin.HandleFunc("/proposals/archived", c.GetArchivedProposals).Methods("GET")
	admin.HandleFunc("/tracking/proposals", c.ProposalTrackingHistory).Methods("GET")

	// -----------------------------
	// SUPERVISOR ROUTES
	// -----------------------------
	supervisor := api.PathPrefix("/supervisor").Subrouter()
	supervisor.Use(middlewares.RoleAuthorization(db, "supervisor"))
	supervisor.HandleFunc("/users", c.GetUsers).Methods("GET")
	supervisor.HandleFunc("/students", c.GetStudents).Methods("GET")
	supervisor.HandleFunc("/profile", c.UpdateProfile).Methods("PUT")
	supervisor.HandleFunc("/profile", c.GetProfile).Methods("GET")
	supervisor.HandleFunc("/notifications", c.GetNotifications).Methods("GET")
	supervisor.HandleFunc("/notifications/mark", c.MarkNotificationRead).Methods("PATCH")
	supervisor.HandleFunc("/notifications/mark-all", c.MarkAllNotificationsRead).Methods("PATCH")
	supervisor.HandleFunc("/notifications", c.DeleteNotification).Methods("DELETE")
	supervisor.HandleFunc("/notifications", c.CreateNotificationHandler).Methods("POST")
	supervisor.HandleFunc("/cohorts", c.GetCohorts).Methods("GET")
	supervisor.HandleFunc("/tracking/cohorts", c.CohortTrackingHistory).Methods("GET") // list all proposals
	supervisor.HandleFunc("/tracking/proposals", c.ProposalTrackingHistory).Methods("GET")

	// -----------------------------
	// MENTOR ROUTES
	// -----------------------------
	mentor := api.PathPrefix("/mentor").Subrouter()
	mentor.Use(middlewares.RoleAuthorization(db, "mentor"))
	mentor.HandleFunc("/users", c.GetUsers).Methods("GET")
	mentor.HandleFunc("/students", c.GetStudents).Methods("GET")
	mentor.HandleFunc("/profile", c.UpdateProfile).Methods("PUT")
	mentor.HandleFunc("/profile", c.GetProfile).Methods("GET")
	mentor.HandleFunc("/notifications", c.GetNotifications).Methods("GET")
	mentor.HandleFunc("/notifications/mark", c.MarkNotificationRead).Methods("PATCH")
	mentor.HandleFunc("/notifications/mark-all", c.MarkAllNotificationsRead).Methods("PATCH")
	mentor.HandleFunc("/notifications", c.DeleteNotification).Methods("DELETE")
	mentor.HandleFunc("/cohorts", c.GetCohorts).Methods("GET")
	mentor.HandleFunc("/tracking/cohorts", c.CohortTrackingHistory).Methods("GET")
	// -----------------------------
	// STUDENT ROUTES
	// -----------------------------
	student := api.PathPrefix("/student").Subrouter()
	student.Use(middlewares.RoleAuthorization(db, "student"))
	student.HandleFunc("/profile", c.GetProfile).Methods("GET")
	student.HandleFunc("/profile", c.UpdateProfile).Methods("PUT")
	student.HandleFunc("/profile/avatar", c.UpdateAvatar).Methods("PATCH")
	student.HandleFunc("/notifications", c.GetNotifications).Methods("GET")
	student.HandleFunc("/notifications/mark", c.MarkNotificationRead).Methods("PATCH")
	student.HandleFunc("/notifications/mark-all", c.MarkAllNotificationsRead).Methods("PATCH")
	student.HandleFunc("/notifications", c.DeleteNotification).Methods("DELETE")
	student.HandleFunc("/cohorts", c.GetCohorts).Methods("GET")
	student.HandleFunc("/proposals", c.CreateProposal).Methods("POST") // submit new proposal
	student.HandleFunc("/proposals", c.GetOwnProposals).Methods("GET") // list own proposals
	student.HandleFunc("/proposals/{proposal_id}", c.UpdateProposal).Methods("PUT")
	student.HandleFunc("/proposals/archived", c.GetArchivedProposals).Methods("GET")
	student.HandleFunc("/proposals/archive", c.ArchiveRestoreProposals).Methods("PATCH")
	student.HandleFunc("/reviews", c.GetReviews).Methods("GET")
	student.HandleFunc("/submission-windows", c.GetSubmissionWindows).Methods("GET")
	student.HandleFunc("/proposals", c.DeleteProposal).Methods("DELETE")
	student.HandleFunc("/proposals/approved", c.GetApprovedProposals).Methods("GET")
	student.HandleFunc("/proposals/rejected", c.GetRejectedProposals).Methods("GET")
	student.HandleFunc("/reviews/{review_id}", c.DeleteReview).Methods("DELETE")
	student.HandleFunc("/tracking/proposals", c.ProposalTrackingHistory).Methods("GET")
	student.HandleFunc("/tracking/cohorts", c.CohortTrackingHistory).Methods("GET")

}
