package assets

import (
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
	admin.Use(middlewares.RoleAuthorization("admin"))
	admin.HandleFunc("/logs/audit", c.GetAuditLogs).Methods("GET")
	admin.HandleFunc("/logs/jobs", c.GetJobLogs).Methods("GET")
	admin.HandleFunc("/users", c.GetUsers).Methods("GET")
	admin.HandleFunc("/students", c.GetStudents).Methods("GET")
	admin.HandleFunc("/mentors", c.GetMentors).Methods("GET")
	admin.HandleFunc("/supervisors", c.GetSupervisors).Methods("GET")
	admin.HandleFunc("/users/{uuid}", c.DeleteUser).Methods("DELETE")
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
	admin.HandleFunc("/cohorts", c.DeleteCohort).Methods("DELETE")
	admin.HandleFunc("/cohorts", c.GetCohorts).Methods("GET")
	admin.HandleFunc("/cohorts/{cohort_id}", c.UpdateCohort).Methods("PUT")

	// -----------------------------
	// SUPERVISOR ROUTES
	// -----------------------------
	supervisor := api.PathPrefix("/supervisor").Subrouter()
	supervisor.Use(middlewares.RoleAuthorization("supervisor"))
	supervisor.HandleFunc("/users", c.GetUsers).Methods("GET")
	supervisor.HandleFunc("/students", c.GetStudents).Methods("GET")
	supervisor.HandleFunc("/profile", c.UpdateProfile).Methods("PUT")
	supervisor.HandleFunc("/profile", c.GetProfile).Methods("GET")
	supervisor.HandleFunc("/notifications", c.GetNotifications).Methods("GET")
	supervisor.HandleFunc("/notifications/mark", c.MarkNotificationRead).Methods("PATCH")
	supervisor.HandleFunc("/notifications/mark-all", c.MarkAllNotificationsRead).Methods("PATCH")
	supervisor.HandleFunc("/notifications", c.DeleteNotification).Methods("DELETE")
	supervisor.HandleFunc("/notifications", c.CreateNotificationHandler).Methods("POST")
	supervisor.HandleFunc("/cohorts", c.CreateCohort).Methods("POST")
	supervisor.HandleFunc("/cohorts", c.DeleteCohort).Methods("DELETE")
	supervisor.HandleFunc("/cohorts/{cohort_id}", c.UpdateCohort).Methods("PUT")
	supervisor.HandleFunc("/cohorts", c.GetCohorts).Methods("GET")

	// -----------------------------
	// MENTOR ROUTES
	// -----------------------------
	mentor := api.PathPrefix("/mentor").Subrouter()
	mentor.Use(middlewares.RoleAuthorization("mentor"))
	mentor.HandleFunc("/users", c.GetUsers).Methods("GET")
	mentor.HandleFunc("/students", c.GetStudents).Methods("GET")
	mentor.HandleFunc("/profile", c.UpdateProfile).Methods("PUT")
	mentor.HandleFunc("/profile", c.GetProfile).Methods("GET")
	mentor.HandleFunc("/notifications", c.GetNotifications).Methods("GET")
	mentor.HandleFunc("/notifications/mark", c.MarkNotificationRead).Methods("PATCH")
	mentor.HandleFunc("/notifications/mark-all", c.MarkAllNotificationsRead).Methods("PATCH")
	mentor.HandleFunc("/notifications", c.DeleteNotification).Methods("DELETE")
	mentor.HandleFunc("/cohorts", c.GetCohorts).Methods("GET")

	// -----------------------------
	// STUDENT ROUTES
	// -----------------------------
	student := api.PathPrefix("/student").Subrouter()
	student.Use(middlewares.RoleAuthorization("student"))
	student.HandleFunc("/profile", c.GetProfile).Methods("GET")
	student.HandleFunc("/profile", c.UpdateProfile).Methods("PUT")
	student.HandleFunc("/profile/avatar", c.UpdateAvatar).Methods("PATCH")
	student.HandleFunc("/notifications", c.GetNotifications).Methods("GET")
	student.HandleFunc("/notifications/mark", c.MarkNotificationRead).Methods("PATCH")
	student.HandleFunc("/notifications/mark-all", c.MarkAllNotificationsRead).Methods("PATCH")
	student.HandleFunc("/notifications", c.DeleteNotification).Methods("DELETE")
	student.HandleFunc("/cohorts", c.GetCohorts).Methods("GET")

}
