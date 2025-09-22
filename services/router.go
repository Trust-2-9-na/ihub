package services

import (
	"net/http"
	"web/libs/handlers"
	"web/services/assets"

	//"web/services/utils"
	"github.com/gorilla/mux"
)

func (s *Server) RegisterRoutes() *mux.Router {
	router := mux.NewRouter()
	router.NotFoundHandler = http.HandlerFunc(handlers.NotFound)
	router.MethodNotAllowedHandler = http.HandlerFunc(handlers.MethodNotAllowed)
	assets.NewRouter(router, s.db.DB())
	return router
}
