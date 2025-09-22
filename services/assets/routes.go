package assets

import (
	"web/services/assets/controllers"
	"web/services/assets/middlewares"

	"github.com/gorilla/mux"
	"gorm.io/gorm"
)

// NewRouter sets up all routes for the application.
func NewRouter(r *mux.Router, DB *gorm.DB) {
	c := &controllers.Construct{
		DB: DB,
	}

	// Base /assets route
	r.PathPrefix("/assets").HandlerFunc(c.Index).Methods("GET")

	// Base /api
	apiRoute := r.PathPrefix("/api").Subrouter()

	// -----------------------------
	// ADMIN ROUTES
	// -----------------------------
	adminRoutes := apiRoute.PathPrefix("").Subrouter()
	adminRoutes.Use(middlewares.RoleAuthorization("Admin")) // match exact role string
	adminRoutes.HandleFunc("/users", c.GetUsers).Methods("GET")
	adminRoutes.HandleFunc("/users/{id}", c.UpdateUser).Methods("PUT")
	adminRoutes.HandleFunc("/users/{id}", c.DeleteUser).Methods("DELETE")
	adminRoutes.HandleFunc("/roles", c.CreateRole).Methods("POST")
	adminRoutes.HandleFunc("/roles", c.GetRoles).Methods("GET")
	adminRoutes.HandleFunc("/roles/{id}", c.UpdateRole).Methods("PUT")
	adminRoutes.HandleFunc("/roles/{id}", c.DeleteRole).Methods("DELETE")
	adminRoutes.HandleFunc("/profile", c.DeleteProfile).Methods("DELETE")

	// -----------------------------
	// SUPERVISOR ROUTES
	// -----------------------------
	supervisorRoutes := apiRoute.PathPrefix("").Subrouter()
	supervisorRoutes.Use(middlewares.RoleAuthorization("Supervisor"))
	supervisorRoutes.HandleFunc("/users", c.GetUsers).Methods("GET")
	supervisorRoutes.HandleFunc("/profile/{id}", c.GetProfile).Methods("GET")

	// -----------------------------
	// MENTOR ROUTES
	// -----------------------------
	mentorRoutes := apiRoute.PathPrefix("").Subrouter()
	mentorRoutes.Use(middlewares.RoleAuthorization("Mentor"))
	mentorRoutes.HandleFunc("/users", c.GetUsers).Methods("GET")
	mentorRoutes.HandleFunc("/profile/{id}", c.GetProfile).Methods("GET")

	// -----------------------------
	// STUDENT ROUTES
	// -----------------------------
	studentRoutes := apiRoute.PathPrefix("").Subrouter()
	studentRoutes.Use(middlewares.RoleAuthorization("Student"))
	studentRoutes.HandleFunc("/profile/{id}", c.GetProfile).Methods("GET")
	studentRoutes.HandleFunc("/profile", c.UpdateProfile).Methods("PUT")
	studentRoutes.HandleFunc("/profile/avatar", c.UpdateAvatar).Methods("PATCH")

	// -----------------------------
	// PUBLIC ROUTES
	// -----------------------------
	apiRoute.HandleFunc("/signup", c.Signup).Methods("POST")
	apiRoute.HandleFunc("/login", c.Login).Methods("POST")
}
