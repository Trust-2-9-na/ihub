package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandler200StatusCodes(t *testing.T) {
	c := Construct{}
	testCases := []struct {
		name    string
		status  int
		handler http.HandlerFunc
	}{
		{
			name:    "Test that controller responds with status code 200, and status codes match body and headers",
			status:  http.StatusOK,
			handler: c.Index,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			r, err := http.NewRequest("GET", "/assets/v1", nil)
			if err != nil {
				t.Errorf("expected error to be nil, but got %v", err)
			}
			w := httptest.NewRecorder()
			handler := http.HandlerFunc(tt.handler)
			handler.ServeHTTP(w, r)
			if w.Code != tt.status {
				t.Errorf("Expected response code to be 200, but got %d", w.Code)
			}
		})
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			r, err := http.NewRequest("GET", "/assets/v1", nil)
			if err != nil {
				t.Errorf("expected error to be nil, but got %v", err)
			}
			w := httptest.NewRecorder()
			handler := http.HandlerFunc(c.Index)
			handler.ServeHTTP(w, r)
			var payload map[string]interface{}
			err = json.NewDecoder(w.Body).Decode(&payload)
			if err != nil {
				t.Errorf("expected error to be nil, but got %v", err)
			}
			if payload["status_code"] != float64(w.Result().StatusCode) {
				t.Errorf("expected status code to be %.f, but got %.f", float64(w.Result().StatusCode), payload["status_code"])
			}
		})
	}
}
