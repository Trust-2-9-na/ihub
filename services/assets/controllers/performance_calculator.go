package controllers

import (
	"log"
	"math"
	"strings"
	"web/services/assets/models"
)

func (c *Construct) UpdateEntityWeightedPerformance(entityID uint64) error {
	var items []models.ProgressItem
	if err := c.DB.Where("entity_id = ?", entityID).Find(&items).Error; err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	// --- Normalize weights within verified items ---
	if err := c.NormalizeEntityWeights(entityID); err != nil {
		log.Printf("⚠️ Weight normalization failed for entity %d: %v\n", entityID, err)
	}

	// --- Map teamID -> list of items for that team ---
	teamItemsMap := make(map[uint64][]models.ProgressItem)
	var individualItems []models.ProgressItem

	for _, item := range items {
		if !strings.EqualFold(item.VerifiedStatus, "Verified") {
			continue
		}

		if item.TeamRefID != nil {
			teamItemsMap[*item.TeamRefID] = append(teamItemsMap[*item.TeamRefID], item)
		} else {
			individualItems = append(individualItems, item)
		}
	}

	// --- Calculate team performance ---
	teamPerformances := make([]float64, 0)
	for teamID, teamItems := range teamItemsMap {
		var total float64
		var weightSum float64

		for _, ti := range teamItems {
			w := ti.Weight
			if w <= 0 {
				w = 1
			}
			total += ti.Performance * w
			weightSum += w
		}

		if weightSum > 0 {
			teamPerf := total / weightSum
			teamPerformances = append(teamPerformances, teamPerf)
		} else {
			teamPerformances = append(teamPerformances, 0)
		}

		// Optional: store team performance on a TeamWeeklyReport or TeamProgressEntity if needed
		log.Printf("✅ Team %d performance: %.2f\n", teamID, teamItems[0].TeamRefID)
	}

	// --- Calculate individual item performance ---
	var individualTotal float64
	var individualWeight float64
	for _, ii := range individualItems {
		w := ii.Weight
		if w <= 0 {
			w = 1
		}
		individualTotal += ii.Performance * w
		individualWeight += w
	}
	individualPerf := 0.0
	if individualWeight > 0 {
		individualPerf = individualTotal / individualWeight
	}

	// --- Combine team and individual performances ---
	var allPerformances []float64
	allPerformances = append(allPerformances, teamPerformances...)
	if individualWeight > 0 {
		allPerformances = append(allPerformances, individualPerf)
	}

	// --- Compute overall entity performance ---
	entityPerformance := 0.0
	if len(allPerformances) > 0 {
		var sum float64
		for _, p := range allPerformances {
			sum += p
		}
		entityPerformance = sum / float64(len(allPerformances))
		entityPerformance = math.Round(entityPerformance*100) / 100
	}

	// --- Determine entity status ---
	status := deriveEntityStatus(items)
	if entityPerformance >= 1.0 {
		entityPerformance = 1.0
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

	log.Printf("✅ Entity %d overall performance updated: %.2f (%s)\n", entityID, entityPerformance, status)
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
