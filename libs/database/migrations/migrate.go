package migrations

import (
	"log"
	"strings"
	"web/libs/database"
	"web/services/assets/models"
	"web/services/utils"

	"gorm.io/gorm"
)

// Migrate runs database migrations and seeds default roles and admin
func Migrate() error {
	dbService := database.New()
	db := dbService.DB()

	// 1. Migrate Roles first
	if err := db.AutoMigrate(&models.Role{}); err != nil {
		log.Printf("Migration failed for Roles: %v", err)
		return err
	}

	// 2. Seed base roles
	if err := seedRoles(db); err != nil {
		log.Printf("Failed to seed roles: %v", err)
		return err
	}

	// 3. Migrate Users (depends on Roles)
	if err := db.AutoMigrate(&models.User{}); err != nil {
		log.Printf("Migration failed for Users: %v", err)
		return err
	}

	// 4. Migrate UserProfiles (after Users)
	if err := db.AutoMigrate(&models.UserProfile{}); err != nil {
		log.Printf("Migration failed for UserProfiles: %v", err)
		return err
	}

	// 5. Migrate role-specific profiles (after Users)
	if err := db.AutoMigrate(&models.StudentProfile{}, &models.MentorProfile{}, &models.SupervisorProfile{}); err != nil {
		log.Printf("Migration failed for role-specific profiles: %v", err)
		return err
	}

	// 6. Migrate Cohorts (independent table)
	if err := db.AutoMigrate(&models.Cohort{}); err != nil {
		log.Printf("Migration failed for Cohorts: %v", err)
		return err
	}

	// 7. Migrate Audit Logs
	if err := db.AutoMigrate(&models.AuditLog{}); err != nil {
		log.Printf("Migration failed for Audit Logs: %v", err)
		return err
	}

	// 8. Migrate Job Logs
	if err := db.AutoMigrate(&models.JobLog{}); err != nil {
		log.Printf("Migration failed for Job Logs: %v", err)
		return err
	}

	// 9. Migrate Proposals
	if err := db.AutoMigrate(&models.Proposal{}); err != nil {
		log.Printf("Migration failed for Proposals: %v", err)
		return err
	}

	// 10. Migrate Notifications
	if err := db.AutoMigrate(&models.Notification{}); err != nil {
		log.Printf("Migration failed for Notifications: %v", err)
		return err
	}

	log.Println("All migrations ran successfully!")

	// Seed admin user
	if err := seedAdmin(db); err != nil {
		log.Printf("Failed to seed admin: %v", err)
		return err
	}

	log.Println("Admin user seeded successfully!")
	return nil
}

// seedRoles ensures base roles exist in the system
func seedRoles(db *gorm.DB) error {
	roles := []models.Role{
		{Name: "Admin", Description: "System administrator"},
		{Name: "Student", Description: "A learner"},
		{Name: "Mentor", Description: "An expert"},
		{Name: "Supervisor", Description: "A supervisor"},
	}

	for _, r := range roles {
		var existing models.Role
		err := db.Where("LOWER(name) = ?", strings.ToLower(r.Name)).First(&existing).Error
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				if err := db.Create(&r).Error; err != nil {
					return err
				}
			} else {
				return err
			}
		} else if existing.Description == "" {
			existing.Description = r.Description
			if err := db.Save(&existing).Error; err != nil {
				return err
			}
		}
	}

	log.Println("Base roles seeded successfully!")
	return nil
}

// seedAdmin ensures the admin role and a default admin user exist
func seedAdmin(db *gorm.DB) error {
	var adminRole models.Role
	if err := db.Where("LOWER(name) = ?", "admin").First(&adminRole).Error; err != nil {
		return err
	}

	passwordHash, err := utils.HashPassword("Inno@2025")
	if err != nil {
		return err
	}

	admin := models.User{
		Username:     "infratel-hub",
		Email:        "infratel@domain.com",
		PasswordHash: passwordHash,
		RoleID:       adminRole.RoleID,
		IsActive:     true,
	}

	if err := db.Where("email = ?", admin.Email).FirstOrCreate(&admin).Error; err != nil {
		return err
	}

	// Create default profile for admin
	var profile models.UserProfile
	if err := db.Where("user_id = ?", admin.UserID).FirstOrCreate(&profile, models.UserProfile{
		UserID:    admin.UserID,
		FirstName: "Admin",
		LastName:  "Hub",
	}).Error; err != nil {
		return err
	}

	log.Println("Admin user and profile ready!")
	return nil
}
