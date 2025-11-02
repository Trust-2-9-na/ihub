package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"web/libs/database/migrations"
	"web/services"
)

// Load environment variables from .env file
func init() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
}

// handleGracefulShutdown listens for OS signals and gracefully shuts down the server.
func handleGracefulShutdown(server *http.Server, shutdownComplete chan bool) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	log.Println("Shutdown signal received. Attempting graceful shutdown...")

	timeoutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(timeoutCtx); err != nil {
		log.Printf("Forced shutdown due to error: %v", err)
	} else {
		log.Println("Server shutdown completed gracefully.")
	}

	shutdownComplete <- true
}

func main() {
	runMigrations := flag.Bool("migrate", false, "Run database migrations")
	flag.Parse()

	if *runMigrations {
		if err := migrations.Migrate(); err != nil {
			log.Fatalf("Migration failed: %v", err)
		}
		log.Println("Migration completed successfully.")
		return
	}

	// Create server with CORS-enabled router
	server := services.NewServer()
	shutdownComplete := make(chan bool, 1)

	go handleGracefulShutdown(server, shutdownComplete)

	slog.Info("Server is running", "port", server.Addr)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}

	<-shutdownComplete
	log.Println("Application terminated.")
}
