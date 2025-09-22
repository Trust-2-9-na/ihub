package migrations

import (
	"log"

	"web/libs/database"
	"web/services/assets/models"
)

// Migrate runs the database migrations in the correct order
func Migrate() error {
	db := database.New()

	// First migrate Roles and Cohorts, since Users and Proposals depend on them
	if err := db.DB().AutoMigrate(
		&models.Role{},
		&models.Cohort{},
	); err != nil {
		log.Printf("Migration failed for Roles/Cohorts: %v", err)
		return err
	}

	// Then migrate Users, which depends on Roles and Cohorts
	if err := db.DB().AutoMigrate(&models.User{}); err != nil {
		log.Printf("Migration failed for Users: %v", err)
		return err
	}
	// migration for userprofiles, which depends on user model
	if err := db.DB().AutoMigrate(&models.UserProfile{}); err != nil {
		log.Printf("Migration failed for User Profiles: %v", err)
		return err
	}

	// Finally migrate Proposals, which depend on Users
	if err := db.DB().AutoMigrate(&models.Proposal{}); err != nil {
		log.Printf("Migration failed for Proposals: %v", err)
		return err
	}

	log.Println("All migrations ran successfully!")
	return nil
}
