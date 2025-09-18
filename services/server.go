package services

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
	"web/libs/database"
)

type Server struct {
	port int
	db database.Service
}

func NewServer() *http.Server {
	port, _ := strconv.Atoi(os.Getenv("PORT"))
	webService := &Server{
		port: port,
		db: database.New(),
	}
	return &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: webService.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
}