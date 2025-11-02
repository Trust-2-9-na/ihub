package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"web/services/assets/models"

	"github.com/gorilla/mux"
)

type CreateRoleInput struct {
	Name        string `json:"name"`        // e.g., "Mentor", "Supervisor"
	Description string `json:"description"` // optional explanation
}

// POST /api/roles
func (c *Construct) CreateRole(w http.ResponseWriter, r *http.Request) {
	// 1️⃣ Authenticate user
	user, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// 2️⃣ Authorize: Only SystemAdmin can create roles
	if strings.ToLower(user.Role.Name) != "systemadmin" {
		c.Json(w, http.StatusForbidden, "Only SystemAdmins can create roles", nil)
		return
	}

	// 3️⃣ Parse request body
	var input CreateRoleInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid request payload", map[string]interface{}{"error": err.Error()})
		return
	}

	// 4️⃣ Validate input
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		c.Json(w, http.StatusBadRequest, "Role name is required", nil)
		return
	}

	// 5️⃣ Check if role already exists
	var existingRole models.Role
	if err := c.DB.Where("LOWER(name) = ?", strings.ToLower(input.Name)).First(&existingRole).Error; err == nil {
		c.Json(w, http.StatusConflict, "Role already exists", map[string]interface{}{"role": existingRole})
		return
	}

	// 6️⃣ Create new role
	role := models.Role{
		Name:        input.Name,
		Description: input.Description,
	}

	if err := c.DB.Create(&role).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create role", map[string]interface{}{"error": err.Error()})
		return
	}

	// 7️⃣ Audit the creation
	roleID64 := uint64(role.RoleID)

	_ = c.LogAudit(
		user.UserID,
		fmt.Sprintf("Created new role '%s'", role.Name),
		nil,       // entity type
		&roleID64, // convert to *uint64
		nil,       // IP address
		nil,       // metadata
	)

	// 8️⃣ Return success response
	c.Json(w, http.StatusCreated, "Role created successfully", map[string]interface{}{
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

//changing roles api

func (c *Construct) ChangeUserRole(w http.ResponseWriter, r *http.Request) {
	// 1️⃣ Authenticate user
	admin, err := c.GetAuthenticatedUser(r)
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// 2️⃣ Permission check: Only SystemAdmins can change roles
	if strings.ToLower(admin.Role.Name) != "systemadmin" {
		c.Json(w, http.StatusForbidden, "Only SystemAdmins can change roles", nil)
		return
	}

	// 3️⃣ Get target user ID
	targetUserID, err := c.GetUintParam(r, "user_id")
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid user ID", nil)
		return
	}

	// 4️⃣ Fetch the target user
	var targetUser models.User
	if err := c.DB.Preload("Role").First(&targetUser, targetUserID).Error; err != nil {
		c.Json(w, http.StatusNotFound, "Target user not found", nil)
		return
	}

	// 5️⃣ Prevent changing role of another SystemAdmin
	if strings.ToLower(targetUser.Role.Name) == "systemadmin" && targetUser.UserID != admin.UserID {
		c.Json(w, http.StatusForbidden, "Cannot change role of another SystemAdmin", nil)
		return
	}

	// 6️⃣ Parse new role from request body
	var body struct {
		NewRole string `json:"new_role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.NewRole == "" {
		c.Json(w, http.StatusBadRequest, "Invalid role input", nil)
		return
	}

	// 7️⃣ Fetch the new role from DB
	var newRole models.Role
	if err := c.DB.Where("name = ?", body.NewRole).First(&newRole).Error; err != nil {
		c.Json(w, http.StatusNotFound, fmt.Sprintf("Role '%s' not found", body.NewRole), nil)
		return
	}

	// 8️⃣ Update user's role
	previousRole := targetUser.Role.Name
	if err := c.DB.Model(&targetUser).Update("role_id", newRole.RoleID).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update user role", map[string]interface{}{"error": err.Error()})
		return
	}

	// 9️⃣ Audit log with previous role
	action := fmt.Sprintf("Changed role of user #%d from '%s' to '%s'", targetUserID, previousRole, body.NewRole)
	entity := "User"
	_ = c.LogAudit(
		admin.UserID,
		action,
		&entity,
		&targetUserID,
		nil, // IP optional
		map[string]interface{}{
			"previous_role": previousRole,
			"new_role":      body.NewRole,
			"performed_by":  admin.UserID,
		},
	)

	// 🔟 Respond success
	c.Json(w, http.StatusOK, "User role updated successfully", map[string]interface{}{
		"user_id":       targetUserID,
		"previous_role": previousRole,
		"new_role":      body.NewRole,
	})
}
