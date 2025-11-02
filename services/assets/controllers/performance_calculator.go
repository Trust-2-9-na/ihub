package controllers

import (
	"log"
	"math"
	"strings"
	"web/services/assets/models"
)

func (c *Construct) UpdateEntityWeightedPerformance(entityID uint64) error {
	var items []models.ProgressItem
	// Query items by entity ID - use explicit column name to ensure it works
	// Exclude archived and soft-deleted items from calculation
	if err := c.DB.Where("progress_entity_ref_id = ? AND is_archived = ?", entityID, false).
		Find(&items).Error; err != nil {
		log.Printf("❌ Error querying items for entity %d: %v\n", entityID, err)
		return err
	}
	if len(items) == 0 {
		log.Printf("⚠️ No progress items found for entity %d (excluding archived)\n", entityID)
		return nil
	}

	// Count team vs individual items for logging
	teamCount := 0
	for _, item := range items {
		if item.TeamRefID != nil {
			teamCount++
		}
	}
	log.Printf("📊 Found %d progress items for entity %d (team: %d, individual: %d)\n",
		len(items), entityID, teamCount, len(items)-teamCount)

	// --- Normalize weights within verified items ---
	if err := c.NormalizeEntityWeights(entityID); err != nil {
		log.Printf("⚠️ Weight normalization failed for entity %d: %v\n", entityID, err)
	}

	// --- Map teamID -> list of items for that team ---
	// Include ALL items (not just verified) for calculation
	teamItemsMap := make(map[uint64][]models.ProgressItem)
	var individualItems []models.ProgressItem

	for _, item := range items {
		// Include all items for status and performance calculation
		if item.TeamRefID != nil {
			teamItemsMap[*item.TeamRefID] = append(teamItemsMap[*item.TeamRefID], item)
		} else {
			individualItems = append(individualItems, item)
		}
	}

	// --- Calculate team performance ---
	// Include all items, but verified items get full weight, pending verification get reduced weight
	teamPerformances := make([]float64, 0)
	for teamID, teamItems := range teamItemsMap {
		var total float64
		var weightSum float64

		for _, ti := range teamItems {
			w := ti.Weight
			if w <= 0 {
				w = 1
			}

			// Calculate effective weight: verified items get full weight, pending verification get 50% weight
			effectiveWeight := w
			if !strings.EqualFold(ti.VerifiedStatus, "Verified") {
				effectiveWeight = w * 0.5 // Reduce weight for unverified items
			}

			total += ti.Performance * effectiveWeight
			weightSum += effectiveWeight
		}

		if weightSum > 0 {
			teamPerf := total / weightSum
			teamPerformances = append(teamPerformances, teamPerf)
		} else {
			teamPerformances = append(teamPerformances, 0)
		}

		log.Printf("✅ Team %d performance: %.2f\n", teamID, teamPerformances[len(teamPerformances)-1])
	}

	// --- Calculate individual item performance ---
	// Include all items, but verified items get full weight, pending verification get reduced weight
	var individualTotal float64
	var individualWeight float64
	for _, ii := range individualItems {
		w := ii.Weight
		if w <= 0 {
			w = 1
		}

		// Calculate effective weight: verified items get full weight, pending verification get 50% weight
		effectiveWeight := w
		if !strings.EqualFold(ii.VerifiedStatus, "Verified") {
			effectiveWeight = w * 0.5 // Reduce weight for unverified items
		}

		individualTotal += ii.Performance * effectiveWeight
		individualWeight += effectiveWeight
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
	// Status is now based on ALL items, not just verified ones
	status := deriveEntityStatus(items)
	if entityPerformance >= 1.0 {
		entityPerformance = 1.0
		// Only mark as completed if all verified items are completed
		if status == models.StatusCompleted {
			status = models.StatusCompleted
		}
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

// deriveEntityStatus determines the overall entity status based on ALL item statuses.
// If any item exists (even pending verification), status changes to "In Progress"
func deriveEntityStatus(items []models.ProgressItem) string {
	// If no items exist, return Pending
	if len(items) == 0 {
		return models.StatusPending
	}

	// As soon as ANY item is submitted, status should be "In Progress"
	// Check if we have any items that indicate progress
	var verified []models.ProgressItem
	hasAnyItems := false

	for _, item := range items {
		hasAnyItems = true
		if strings.EqualFold(item.VerifiedStatus, "Verified") {
			verified = append(verified, item)
		}
	}

	// If we have items but no verified ones yet, still show "In Progress"
	// This ensures status changes immediately when items are submitted
	if !hasAnyItems {
		return models.StatusPending
	}

	// If we have verified items, use them for more accurate status
	if len(verified) > 0 {
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
		case allCompleted && len(verified) == len(items):
			// All items are verified and completed
			return models.StatusCompleted
		case hasInProgress || hasCompleted:
			return models.StatusInProgress
		default:
			return models.StatusInProgress // Even if pending, if items exist, show in progress
		}
	}

	// If we have items but none are verified yet, status is "In Progress"
	// This ensures immediate status update when items are submitted
	return models.StatusInProgress
}

// NormalizeEntityWeights adjusts verified items so that their total verified weight = 1.
func (c *Construct) NormalizeEntityWeights(entityID uint64) error {
	var verifiedItems []models.ProgressItem
	if err := c.DB.
		Where("progress_entity_ref_id = ? AND LOWER(verified_status) = ?", entityID, "verified").
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
