package controllers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"web/services/assets/models"
)

// audity get apis
// GetAuditLogs handles GET /api/logs/audit?limit=100&action=login

func (c *Construct) GetAuditLogs(w http.ResponseWriter, r *http.Request) {
	var logs []models.AuditLog

	// Default limit
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	// Optional action filter
	action := r.URL.Query().Get("action")

	query := c.DB.Order("created_at DESC").Limit(limit)
	if action != "" {
		query = query.Where("action = ?", action)
	}

	if err := query.Find(&logs).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch audit logs", map[string]interface{}{"error": err.Error()})
		return
	}

	c.Json(w, http.StatusOK, "Audit logs retrieved successfully", map[string]interface{}{"logs": logs})
}

// jobs get apis
func (c *Construct) GetJobLogs(w http.ResponseWriter, r *http.Request) {
	var logs []models.JobLog
	if err := c.DB.Order("created_at DESC").Limit(100).Find(&logs).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Could not fetch job logs", map[string]interface{}{"error": err.Error()})
		return
	}

	decodedLogs := make([]map[string]interface{}, len(logs))
	for i, log := range logs {
		var meta map[string]interface{}
		if len(log.Metadata) > 0 {
			// Unmarshal raw JSON bytes
			if err := json.Unmarshal(log.Metadata, &meta); err != nil {
				meta = nil
			}
		}

		decodedLogs[i] = map[string]interface{}{
			"id":         log.ID,
			"job_name":   log.JobName,
			"status":     log.Status,
			"start_time": log.StartTime,
			"end_time":   log.EndTime,
			"message":    log.Message,
			"metadata":   meta,
			"created_at": log.CreatedAt,
		}
	}

	c.Json(w, http.StatusOK, "Job logs retrieved successfully", map[string]interface{}{"logs": decodedLogs})
}
