package controllers

import (
	"log"
	"math"
	"strings"
	"web/services/assets/models"
)


// and updates its status automatically based on verified item progress.
func (c *Construct) UpdateEntityWeightedPerformance(entityID uint64) error {
	var items []models.ProgressItem
	if err := c.DB.Where("entity_id = ?", entityID).Find(&items).Error; err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	// --- Normalize verified weights before computing performance ---
	if err := c.NormalizeEntityWeights(entityID); err != nil {
		log.Printf("⚠️ Weight normalization failed for entity %d: %v\n", entityID, err)
	}

	var (
		totalPerformance float64
		totalWeight      float64
	)

	// --- Compute weighted performance (verified items only) ---
	for _, item := range items {
		if !strings.EqualFold(item.VerifiedStatus, "Verified") {
			continue
		}

		weight := item.Weight
		if weight <= 0 {
			weight = 1
		}

		// Each item's performance is already a percentage (0–100)
		totalPerformance += item.Performance * weight
		totalWeight += weight
	}

	entityPerformance := 0.0
	if totalWeight > 0 {
		entityPerformance = totalPerformance / totalWeight
		entityPerformance = math.Round(entityPerformance*100) / 100 // round to 2 decimals
	}

	// --- Determine entity status based on performance ---
	status := deriveEntityStatus(items)

	// Auto-mark as "Completed" if performance >= 100%
	if entityPerformance >= 100 {
		entityPerformance = 100
		status = models.StatusCompleted
	}

	// --- Update entity record ---
	if err := c.DB.Model(&models.ProgressEntity{}).
		Where("id = ?", entityID).
		Updates(map[string]interface{}{
			"performance": entityPerformance,
			"status":      status,
		}).Error; err != nil {
		return err
	}

	log.Printf("✅ Entity %d performance updated: %.2f%% (%s)\n", entityID, entityPerformance, status)
	return nil
}

// deriveEntityStatus determines the overall entity status based on verified item statuses.
func deriveEntityStatus(items []models.ProgressItem) string {
	var verified []models.ProgressItem
	for _, item := range items {
		if strings.EqualFold(item.VerifiedStatus, "Verified") {
			verified = append(verified, item)
		}
	}

	if len(verified) == 0 {
		return models.StatusPending
	}

	allCompleted := true
	hasInProgress := false
	hasCompleted := false

	for _, item := range verified {
		status := strings.ToLower(item.StudentStatus)
		switch status {
		case "completed":
			hasCompleted = true
		case "in progress":
			hasInProgress = true
			allCompleted = false
		case "pending", "not started":
			allCompleted = false
		default:
			allCompleted = false
		}
	}

	switch {
	case allCompleted:
		return models.StatusCompleted
	case hasInProgress || hasCompleted:
		return models.StatusInProgress
	default:
		return models.StatusPending
	}
}

// NormalizeEntityWeights adjusts verified items so that their total verified weight = 1.
func (c *Construct) NormalizeEntityWeights(entityID uint64) error {
	var verifiedItems []models.ProgressItem
	if err := c.DB.
		Where("entity_id = ? AND LOWER(verified_status) = ?", entityID, "verified").
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
		newWeight = math.Round(newWeight*100) / 100 // round to 2 decimals
		if err := c.DB.Model(&models.ProgressItem{}).
			Where("id = ?", item.ID).
			Update("weight", newWeight).Error; err != nil {
			log.Printf("⚠️ Failed to normalize weight for item %d: %v\n", item.ID, err)
		}
	}

	return nil
}
