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

var lookupModels = map[string]interface{}{
	"category":  &models.Category{},
	"program":   &models.Program{},
	"school":    &models.School{},
	"expertise": &models.Expertise{},
	"subfield":  &models.Subfield{},
}

func makeSlice(model interface{}) interface{} {
	switch model.(type) {
	case *models.Category:
		return &[]models.Category{}
	case *models.Program:
		return &[]models.Program{}
	case *models.School:
		return &[]models.School{}
	case *models.Expertise:
		return &[]models.Expertise{}
	case *models.Subfield:
		return &[]models.Subfield{}
	default:
		return &[]interface{}{}
	}
}

func makeNewLookupInstance(model interface{}, name string) interface{} {
	switch model.(type) {
	case *models.Category:
		return &models.Category{Name: name}
	case *models.Program:
		return &models.Program{Name: name}
	case *models.School:
		return &models.School{Name: name}
	case *models.Expertise:
		return &models.Expertise{Name: name}
	case *models.Subfield:
		return &models.Subfield{Name: name}
	default:
		return nil
	}
}

func normalizeLookupType(raw string) string {
	switch strings.ToLower(raw) {
	case "categories":
		return "category"
	case "programs":
		return "program"
	case "schools":
		return "school"
	case "expertises":
		return "expertise"
	case "subfields":
		return "subfield"
	default:
		return strings.ToLower(raw)
	}
}

func (c *Construct) CreateLookup(w http.ResponseWriter, r *http.Request) {
	var inputs []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&inputs); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid input format. Expected an array of {type, name} objects.", map[string]interface{}{"error": err.Error()})
		return
	}

	var created []map[string]string
	var failed []map[string]string

	for _, input := range inputs {
		lookupType := normalizeLookupType(input.Type)
		name := strings.TrimSpace(input.Name)

		if name == "" {
			failed = append(failed, map[string]string{"type": input.Type, "error": "Missing name"})
			continue
		}

		model, ok := lookupModels[lookupType]
		if !ok {
			failed = append(failed, map[string]string{"type": input.Type, "error": "Invalid lookup type"})
			continue
		}

		// Create a *fresh instance* of the right type
		newInstance := makeNewLookupInstance(model, name)

		// Try insert (ignore existing)
		if err := c.DB.FirstOrCreate(newInstance, map[string]interface{}{"name": name}).Error; err != nil {
			failed = append(failed, map[string]string{"type": input.Type, "name": name, "error": err.Error()})
			continue
		}

		// Log Audit
		entity := fmt.Sprintf("%s Lookup", lookupType)
		_ = c.LogAudit(0, fmt.Sprintf("Created new %s: %s", lookupType, name), &entity, nil, nil, nil)

		created = append(created, map[string]string{"type": input.Type, "name": name})
	}

	status := http.StatusCreated
	message := "Lookups processed successfully"
	if len(failed) > 0 && len(created) == 0 {
		status = http.StatusBadRequest
		message = "All lookups failed"
	} else if len(failed) > 0 {
		status = http.StatusMultiStatus
		message = "Some lookups failed"
	}

	c.Json(w, status, message, map[string]interface{}{
		"created": created,
		"failed":  failed,
	})
}

// GET /api/lookups?types=categories,programs

func (c *Construct) GetAllLookups(w http.ResponseWriter, r *http.Request) {
	// Parse requested types from query param
	typesParam := r.URL.Query().Get("types")
	var requested []string
	if typesParam != "" {
		requested = strings.Split(typesParam, ",")
	}

	// Helper to check if a string exists in a slice
	contains := func(slice []string, val string) bool {
		for _, s := range slice {
			if s == val {
				return true
			}
		}
		return false
	}

	result := map[string]interface{}{}

	for key, model := range lookupModels {
		// Skip if specific types requested and current key is not included
		if len(requested) > 0 && !contains(requested, key+"s") {
			continue
		}

		slice := makeSlice(model) // helper that creates pointer to slice
		if err := c.DB.Find(slice).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to fetch lookups", map[string]interface{}{"error": err.Error()})
			return
		}

		result[key+"s"] = slice
	}

	c.Json(w, http.StatusOK, "Lookup data fetched successfully", map[string]interface{}{"data": result})
}

// PUT /api/lookups/{type}/{id}
// PUT /api/lookups/{type}/{id}
func (c *Construct) UpdateLookup(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	rawType := vars["type"]
	idStr := vars["id"]

	// Validate ID
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid ID format", map[string]interface{}{"error": err.Error()})
		return
	}

	// Normalize lookup type
	lookupType := normalizeLookupType(rawType)
	model, ok := lookupModels[lookupType]
	if !ok {
		c.Json(w, http.StatusBadRequest, "Invalid lookup type", map[string]interface{}{"type": rawType})
		return
	}

	// Parse input body
	var input struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		c.Json(w, http.StatusBadRequest, "Invalid input", map[string]interface{}{"error": err.Error()})
		return
	}

	newName := strings.TrimSpace(input.Name)
	if newName == "" {
		c.Json(w, http.StatusBadRequest, "Name cannot be empty", nil)
		return
	}

	// Perform the update
	if err := c.DB.Model(model).Where("id = ?", id).Update("name", newName).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to update lookup", map[string]interface{}{"error": err.Error()})
		return
	}

	// --- Notify and Track ---
	entity := fmt.Sprintf("%s Lookup", strings.Title(lookupType))
	title := "Lookup Updated"
	message := fmt.Sprintf("%s '%s' was updated successfully", entity, newName)

	go c.NotifyAndTrack(
		0, // no specific user — could be admin/system
		title,
		message,
		entity,
		"Lookup",
		&id,
		"Updated",
		false, // no email
	)

	// Success response
	c.Json(w, http.StatusOK, "Lookup updated successfully", map[string]interface{}{
		"id":   id,
		"type": lookupType,
		"name": newName,
	})
}
