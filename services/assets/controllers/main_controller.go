package controllers

import "net/http"

func (c *Construct) Index(w http.ResponseWriter, r *http.Request) {
	c.Json(w, http.StatusOK, "ok", map[string]interface{}{
		"message": "Welcome to the DSI 25d goLang Framework, you are currently accessing the [Asset Management Service]",
	})
}
