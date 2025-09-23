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
	admin.HandleFunc("/users", c.GetUsers).Methods("GET")
	admin.HandleFunc("/users/{uuid}", c.DeleteUser).Methods("DELETE")
	admin.HandleFunc("/roles", c.CreateRole).Methods("POST")
	admin.HandleFunc("/roles", c.GetRoles).Methods("GET")
	admin.HandleFunc("/roles/{id}", c.UpdateRole).Methods("PUT")
	admin.HandleFunc("/roles/{id}", c.DeleteRole).Methods("DELETE")
	admin.HandleFunc("/profile", c.DeleteProfile).Methods("DELETE")

	// -----------------------------
	// SUPERVISOR ROUTES
	// -----------------------------
	supervisor := api.PathPrefix("/supervisor").Subrouter()
	supervisor.Use(middlewares.RoleAuthorization("supervisor"))
	supervisor.HandleFunc("/users", c.GetUsers).Methods("GET")
	supervisor.HandleFunc("/profile/{uuid}", c.GetProfile).Methods("GET")

	// -----------------------------
	// MENTOR ROUTES
	// -----------------------------
	mentor := api.PathPrefix("/mentor").Subrouter()
	mentor.Use(middlewares.RoleAuthorization("mentor"))
	mentor.HandleFunc("/users", c.GetUsers).Methods("GET")
	mentor.HandleFunc("/profile/{uuid}", c.GetProfile).Methods("GET")

	// -----------------------------
	// STUDENT ROUTES
	// -----------------------------
	student := api.PathPrefix("/student").Subrouter()
	student.Use(middlewares.RoleAuthorization("student"))
	student.HandleFunc("/profile/{uuid}", c.GetProfile).Methods("GET")
	student.HandleFunc("/profile", c.UpdateProfile).Methods("PUT")
	student.HandleFunc("/profile/avatar", c.UpdateAvatar).Methods("PATCH")
}
