package assets

import (
	"web/services/assets/controllers"

	"github.com/gorilla/mux"
	"gorm.io/gorm"
)

func NewRouter(r *mux.Router, DB *gorm.DB) {
	c := &controllers.Construct{
		DB: DB,
	}
	route := r.PathPrefix("/assets").Subrouter()
	route.HandleFunc("", c.Index).Methods("GET")
}
