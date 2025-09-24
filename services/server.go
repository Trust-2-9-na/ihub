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

// Server wraps dependencies for our app
type Server struct {
	port int
	db   database.Service
}

// NewServer creates and configures a new HTTP server
func NewServer() *http.Server {
	// Load port from env, fallback to 9000
	port, err := strconv.Atoi(os.Getenv("PORT"))
	if err != nil || port == 0 {
		port = 9000
	}

	webService := &Server{
		port: port,
		db:   database.New(),
	}

	// Setup CORS
	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: true,
	})

	// Apply middleware to routes
	handler := corsHandler.Handler(webService.RegisterRoutes())

	// Return HTTP server config
	return &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      handler,
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
}
