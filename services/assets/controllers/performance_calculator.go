package controllers

import (
	"math"
	"strings"
	"web/services/assets/models"
)

// --- Update the weighted performance of a progress entity ---
func (c *Construct) UpdateEntityWeightedPerformance(entityID uint64) error {
	var items []models.ProgressItem
	if err := c.DB.Where("entity_id = ?", entityID).Find(&items).Error; err != nil {
		return err
	}

	if len(items) == 0 {
		return nil
	}

	// Ensure weights are normalized
	_ = c.NormalizeEntityWeights(entityID)

	var totalPerformance float64
	for _, item := range items {
		// Default weight to 1 if zero
		weight := item.Weight
		if weight == 0 {
			weight = 1
		}
		totalPerformance += item.Performance * weight
	}

	// Sum of weights
	var totalWeight float64
	for _, it := range items {
		if it.Weight == 0 {
			totalWeight += 1
		} else {
			totalWeight += it.Weight
		}
	}

	// Calculate weighted performance (round to 2 decimals)
	entityPerformance := 0.0
	if totalWeight > 0 {
		entityPerformance = math.Round((totalPerformance/totalWeight)*100) / 100
	}

	// Update entity in DB
	return c.DB.Model(&models.ProgressEntity{}).
		Where("id = ?", entityID).
		Updates(map[string]interface{}{
			"performance": entityPerformance,
			"status":      deriveEntityStatus(items),
		}).Error
}

// --- Derive entity status from its items ---
func deriveEntityStatus(items []models.ProgressItem) string {
	allCompleted := true
	anyInProgress := false

	for _, item := range items {
		switch strings.ToLower(item.Status) {
		case "in progress":
			anyInProgress = true
			allCompleted = false
		case "pending":
			allCompleted = false
		case "cancelled":
			// skip but affects completion logic
		}
	}

	switch {
	case allCompleted:
		return "Completed"
	case anyInProgress:
		return "In Progress"
	default:
		return "Pending"
	}
}

// --- Normalize item weights so that total weight = 1 ---
func (c *Construct) NormalizeEntityWeights(entityID uint64) error {
	var items []models.ProgressItem
	if err := c.DB.Where("entity_id = ?", entityID).Find(&items).Error; err != nil {
		return err
	}

	if len(items) == 0 {
		return nil
	}

	// Compute total weight
	var total float64
	for _, it := range items {
		if it.Weight == 0 {
			it.Weight = 1
		}
		total += it.Weight
	}

	if total == 0 {
		return nil
	}

	// Update normalized weight
	for _, it := range items {
		newWeight := it.Weight / total
		// Round to 2 decimals
		newWeight = math.Round(newWeight*100) / 100
		c.DB.Model(&it).Update("weight", newWeight)
	}

	return nil
}
