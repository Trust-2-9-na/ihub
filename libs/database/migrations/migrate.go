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

	// 1️⃣ Roles
	if err := db.AutoMigrate(&models.Role{}); err != nil {
		log.Printf("Migration failed for Roles: %v", err)
		return err
	}

	if err := seedRoles(db); err != nil {
		log.Printf("Failed to seed roles: %v", err)
		return err
	}

	// 2️⃣ Users
	if err := db.AutoMigrate(&models.User{}); err != nil {
		log.Printf("Migration failed for Users: %v", err)
		return err
	}

	// 3️⃣ User Profiles
	if err := db.AutoMigrate(&models.UserProfile{}); err != nil {
		log.Printf("Migration failed for UserProfiles: %v", err)
		return err
	}

	// 4️⃣ Role-specific profiles
	if err := db.AutoMigrate(
		&models.StudentProfile{},
		&models.MentorProfile{},
		&models.SupervisorProfile{},
	); err != nil {
		log.Printf("Migration failed for role-specific profiles: %v", err)
		return err
	}

	// 5️⃣ Teams
	if err := db.AutoMigrate(&models.Team{}); err != nil {
		log.Printf("Migration failed for Teams: %v", err)
		return err
	}
	// 6 user teams
	err := db.Exec(`
CREATE TABLE IF NOT EXISTS user_teams (
    user_id BIGINT NOT NULL,
    team_id BIGINT NOT NULL,
    role VARCHAR(20) DEFAULT 'Member',
    joined_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, team_id),
    CONSTRAINT fk_userteams_user FOREIGN KEY (user_id) REFERENCES users(user_id) ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT fk_userteams_team FOREIGN KEY (team_id) REFERENCES teams(team_id) ON DELETE CASCADE ON UPDATE CASCADE
);
`).Error
	if err != nil {
		log.Printf("Migration failed for UserTeams: %v", err)
		return err
	}

	// 7️⃣ Cohorts
	if err := db.AutoMigrate(&models.Cohort{}); err != nil {
		log.Printf("Migration failed for Cohorts: %v", err)
		return err
	}

	// 8️⃣ Audit Logs
	if err := db.AutoMigrate(&models.AuditLog{}); err != nil {
		log.Printf("Migration failed for Audit Logs: %v", err)
		return err
	}
	// submission windows
	if err := db.AutoMigrate(&models.ProposalSubmissionWindow{}); err != nil {
		log.Printf("Migration failed for submission windows: %v", err)
		return err
	}

	// 9️⃣ Job Logs
	if err := db.AutoMigrate(&models.JobLog{}); err != nil {
		log.Printf("Migration failed for Job Logs: %v", err)
		return err
	}

	// 🔟 Proposals
	if err := db.AutoMigrate(&models.Proposal{}); err != nil {
		log.Printf("Migration failed for Proposals: %v", err)
		return err
	}

	// 1️⃣1️⃣ Proposal Reviews (if Proposals exists)
	if db.Migrator().HasTable(&models.Proposal{}) {
		if err := db.AutoMigrate(&models.ProposalReview{}); err != nil {
			log.Printf("Migration failed for Proposal Reviews: %v", err)
			return err
		}
	} else {
		log.Println("⚠️ Skipping ProposalReviews migration: proposals table not found")
	}

	// 1️⃣2️⃣ Notifications
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
