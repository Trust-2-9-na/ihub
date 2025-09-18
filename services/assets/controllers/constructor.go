package controllers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"gorm.io/gorm"
)

type Construct struct {
	DB *gorm.DB
}

type customResponseWriter struct {
	w http.ResponseWriter
}

func (rw customResponseWriter) response(status int, message string, payload map[string]interface{}) {
	resp := map[string]interface{}{
		"status_code": status,
		"status":      message,
		"data":        payload,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		slog.Error(err.Error(), "status", http.StatusInternalServerError)
		rw.w.WriteHeader(status)
		rw.w.Write(data)
		return
	}
	rw.w.WriteHeader(status)
	rw.w.Write(data)
}

func (c *Construct) Json(w http.ResponseWriter, status int, message string, payload map[string]interface{}) {
	crw := customResponseWriter{w: w}
	crw.response(status, message, payload)
}
