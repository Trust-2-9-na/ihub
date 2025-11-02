package services

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
	"web/libs/database"

	"github.com/rs/cors"
)

// Server wraps dependencies for the app.
type Server struct {
	port int
	db   database.Service
}

// NewServer creates and configures a new HTTP server.
func NewServer() *http.Server {
	// Load port from environment variable; fallback to 9000.
	port, err := strconv.Atoi(os.Getenv("PORT"))
	if err != nil || port == 0 {
		port = 9000
	}

	// Initialize the server dependencies.
	webService := &Server{
		port: port,
		db:   database.New(),
	}

	// Configure CORS middleware.
	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000"}, // Add your frontend origin(s)
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Requested-With"},
		AllowCredentials: true,
	})

	// Apply middleware to registered routes.
	handler := corsHandler.Handler(webService.RegisterRoutes())

	// Return a configured HTTP server instance.
	return &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      handler,
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
}
