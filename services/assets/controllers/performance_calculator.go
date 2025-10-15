package controllers

import (
	"log"
	"math"
	"strings"
	"web/services/assets/models"
)

// --- Recalculate a progress entity’s weighted performance and status ---
func (c *Construct) UpdateEntityWeightedPerformance(entityID uint64) error {
	var items []models.ProgressItem
	if err := c.DB.Where("entity_id = ?", entityID).Find(&items).Error; err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	// --- Normalize verified item weights first ---
	if err := c.NormalizeEntityWeights(entityID); err != nil {
		log.Println("Warning: failed to normalize weights:", err)
	}

	var (
		totalPerformance float64
		totalWeight      float64
	)

	// --- Compute weighted performance using only verified items ---
	for _, item := range items {
		if strings.ToLower(item.VerifiedStatus) != "verified" {
			continue
		}

		weight := item.Weight
		if weight <= 0 {
			weight = 1
		}

		totalPerformance += item.Performance * weight
		totalWeight += weight
	}

	entityPerformance := 0.0
	if totalWeight > 0 {
		entityPerformance = math.Round((totalPerformance/totalWeight)*100) / 100
	}

	// --- Update entity performance & derived status ---
	return c.DB.Model(&models.ProgressEntity{}).
		Where("id = ?", entityID).
		Updates(map[string]interface{}{
			"performance": entityPerformance,
			"status":      deriveEntityStatus(items),
		}).Error
}

// --- Derive entity status from verified item statuses ---
func deriveEntityStatus(items []models.ProgressItem) string {
	var verifiedItems []models.ProgressItem
	for _, item := range items {
		if item.VerifiedStatus == "Verified" {
			verifiedItems = append(verifiedItems, item)
		}
	}

	if len(verifiedItems) == 0 {
		return models.StatusPending
	}

	allCompleted := true
	anyInProgress := false
	anyCompleted := false

	for _, item := range verifiedItems {
		status := strings.ToLower(item.StudentStatus)
		switch status {
		case "completed":
			anyCompleted = true
		case "in progress":
			anyInProgress = true
			allCompleted = false
		case "pending":
			allCompleted = false
		default:
			allCompleted = false
		}
	}

	switch {
	case allCompleted:
		return models.StatusCompleted
	case anyInProgress || anyCompleted:
		return models.StatusInProgress // ✅ was "Pending" before
	default:
		return models.StatusPending
	}
}

// --- Normalize verified items so that total verified weight = 1 ---
func (c *Construct) NormalizeEntityWeights(entityID uint64) error {
	var verifiedItems []models.ProgressItem
	if err := c.DB.
		Where("entity_id = ? AND verified_status = ?", entityID, "Verified").
		Find(&verifiedItems).Error; err != nil {
		return err
	}

	if len(verifiedItems) == 0 {
		return nil
	}

	var total float64
	for _, item := range verifiedItems {
		if item.Weight <= 0 {
			item.Weight = 1
		}
		total += item.Weight
	}

	if total == 0 {
		return nil
	}

	for _, item := range verifiedItems {
		newWeight := item.Weight / total
		newWeight = math.Round(newWeight*100) / 100
		if err := c.DB.Model(&item).Update("weight", newWeight).Error; err != nil {
			log.Println("Weight normalization failed for item:", item.ID, "error:", err)
		}
	}

	return nil
}
