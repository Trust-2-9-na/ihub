package controllers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"web/services/assets/models"

	"github.com/gorilla/mux"
)

type CreateRoleInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// POST /api/roles
func (c *Construct) CreateRole(w http.ResponseWriter, r *http.Request) {
	var input CreateRoleInput

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "error", map[string]interface{}{
			"error": "invalid request payload",
		})
		return
	}

	role := models.Role{
		Name:        input.Name,
		Description: input.Description,
	}

	if err := c.DB.Create(&role).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "error", map[string]interface{}{
			"error": "failed to create role",
		})
		return
	}

	c.Json(w, http.StatusCreated, "success", map[string]interface{}{
		"role": role,
	})
}

// get all roles in the system API

func (c *Construct) GetRoles(w http.ResponseWriter, r *http.Request) {
	var roles []models.Role

	if err := c.DB.Find(&roles).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "error", map[string]interface{}{
			"error": "failed to fetch roles",
		})
		return
	}

	c.Json(w, http.StatusOK, "success", map[string]interface{}{
		"roles": roles,
	})
}

// delete a role API

func (c *Construct) DeleteRole(w http.ResponseWriter, r *http.Request) {
	// Get role_id from path params: /api/role/{id}
	vars := mux.Vars(r)
	roleIDStr := vars["id"]
	if roleIDStr == "" {
		c.Json(w, http.StatusBadRequest, "Missing role_id", nil)
		return
	}

	roleID, err := strconv.ParseUint(roleIDStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid role_id", map[string]interface{}{"error": err.Error()})
		return
	}

	// Check if role is assigned to any users
	var count int64
	if err := c.DB.Model(&models.User{}).Where("role_id = ?", roleID).Count(&count).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to check role usage", map[string]interface{}{"error": err.Error()})
		return
	}
	if count > 0 {
		c.Json(w, http.StatusBadRequest, "Role is assigned to users and cannot be deleted", nil)
		return
	}

	// Delete role
	if err := c.DB.Delete(&models.Role{}, roleID).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to delete role", map[string]interface{}{"error": err.Error()})
		return
	}

	c.Json(w, http.StatusOK, "Role deleted successfully", nil)
}

// Update a Specific role

type UpdateRoleInput struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

func (c *Construct) UpdateRole(w http.ResponseWriter, r *http.Request) {
	// Get role_id from query param: /updaterole?id=2
	roleID := r.URL.Query().Get("id")
	if roleID == "" {
		c.Json(w, http.StatusBadRequest, "Missing role_id", nil)
		return
	}

	id, err := strconv.ParseUint(roleID, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid role_id", nil)
		return
	}

	var input UpdateRoleInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	var role models.Role
	if err := c.DB.First(&role, id).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Role not found", nil)
		return
	}

	if input.Name != nil {
		role.Name = strings.ToLower(strings.TrimSpace(*input.Name))
	}
	if input.Description != nil {
		role.Description = *input.Description
	}

	if err := c.DB.Save(&role).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update role", map[string]interface{}{"error": err.Error()})
		return
	}

	c.Json(w, http.StatusOK, "Role updated successfully", map[string]interface{}{"role": role})
}
