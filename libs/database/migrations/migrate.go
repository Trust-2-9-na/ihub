package migrations

import (
	"fmt"
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
	// migarate permisions
	if err := db.AutoMigrate(&models.Permission{}); err != nil {
		log.Printf("Migration failed for Permisions: %v", err)
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
	// Cohort users (many-to-many)
	// --- Create cohort_users table ---
	err = db.Exec(`
CREATE TABLE IF NOT EXISTS cohort_users (
    cohort_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    role VARCHAR(20) DEFAULT 'Student',
	created_by BIGINT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (cohort_id, user_id),
    CONSTRAINT fk_cohortusers_cohort FOREIGN KEY (cohort_id) REFERENCES cohorts(cohort_id) ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT fk_cohortusers_user FOREIGN KEY (user_id) REFERENCES users(user_id) ON DELETE CASCADE ON UPDATE CASCADE
);
`).Error
	if err != nil {
		log.Fatalf("Migration failed for CohortUsers table: %v", err)
	}

	// --- Create partial unique index to allow only one supervisor per cohort ---
	err = db.Exec(`
CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_supervisor_per_cohort
ON cohort_users(cohort_cohort_id)
WHERE role = 'Supervisor';
`).Error
	if err != nil {
		log.Fatalf("Failed to create partial unique index for supervisors: %v", err)
	}

	log.Println("Migration for cohort_users table completed successfully")

	if err != nil {
		log.Printf("Migration failed for CohortUsers: %v", err)
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
	// 13 tracking hiatory
	if err := db.AutoMigrate(&models.SystemHistory{}); err != nil {
		log.Printf("Migration failed for system tracking history: %v", err)
		return err
	}
	// 14 migrate Entity Progress

	if err := db.AutoMigrate(&models.ProgressEntity{}); err != nil {
		log.Printf("Migration failed for entity progress tracking: %v", err)
		return err
	}

	// 15 migrate item progress

	if err := db.AutoMigrate(&models.ProgressItem{}); err != nil {
		log.Printf("Migration failed for item progress tracking: %v", err)
		return err
	}

	// 16 migrate reports

	if err := db.AutoMigrate(&models.WeeklyReport{}); err != nil {
		log.Printf("Migration failed for weekly reports: %v", err)
		return err
	}
	log.Println("WeeklyReport table created successfully")

	// 17 migrate supervisor enforcements
	if err := db.AutoMigrate(&models.SupervisorEnforcement{}); err != nil {
		log.Printf("Migration failed for supervisor Enforcements: %v", err)
		return err
	}
	// 17. Weekly Report Comments
	err = db.Exec(`
CREATE TABLE IF NOT EXISTS weekly_report_comments (
    id BIGSERIAL PRIMARY KEY,
    report_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    comment TEXT,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_weekly_report_comments_report
        FOREIGN KEY (report_id)
        REFERENCES weekly_reports(id)
        ON DELETE CASCADE
        ON UPDATE CASCADE,
    CONSTRAINT fk_weekly_report_comments_user
        FOREIGN KEY (user_id)
        REFERENCES users(user_id)
        ON DELETE CASCADE
        ON UPDATE CASCADE
);
`).Error
	if err != nil {
		log.Fatalf("Migration failed for weekly_report_comments table: %v", err)
	}
	log.Println("weekly_report_comments table created successfully")

	log.Println("All migrations ran successfully!")

	// Seed admin user
	if err := SeedAll(db); err != nil {
		log.Printf("Failed to seed all users: %v", err)
		return err
	}

	log.Println("user seeded successfully!")
	return nil
}

func seedRoles(db *gorm.DB) error {
	roles := []models.Role{
		{Name: "SystemAdmin", Description: "Full system administrator access — manages entire system"},
		{Name: "OpsAdmin", Description: "Operational administrator — manages cohorts,proposals, assignments, and supervisors"},
		{Name: "Supervisor", Description: "Supervises students and reviews proposals"},
		{Name: "Mentor", Description: "Provides mentorship and technical guidance"},
		{Name: "Student", Description: "A learner submitting proposals and participating in cohorts"},
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

	log.Println("✅ Base roles seeded: SystemAdmin, OpsAdmin, Supervisor, Mentor, Student")
	return nil
}

// -----------------------------
// Seed Admins
// -----------------------------
func seedAdmins(db *gorm.DB) error {
	// --- System Admin ---
	var sysAdminRole models.Role
	if err := db.Where("LOWER(name) = ?", "systemadmin").First(&sysAdminRole).Error; err != nil {
		return fmt.Errorf("system admin role missing: %w", err)
	}

	sysPassword, err := utils.HashPassword("Inno@2025")
	if err != nil {
		return err
	}

	sysAdmin := models.User{
		Username:     "system-admin",
		Email:        "sysadmin@domain.com",
		PasswordHash: sysPassword,
		RoleID:       sysAdminRole.RoleID,
		IsActive:     true,
	}

	if err := db.Where("email = ?", sysAdmin.Email).FirstOrCreate(&sysAdmin).Error; err != nil {
		return err
	}

	if err := db.Where("user_id = ?", sysAdmin.UserID).FirstOrCreate(&models.UserProfile{
		UserID:    sysAdmin.UserID,
		FirstName: "System",
		LastName:  "Administrator",
	}).Error; err != nil {
		return err
	}

	// --- Ops Admin ---
	var opsAdminRole models.Role
	if err := db.Where("LOWER(name) = ?", "opsadmin").First(&opsAdminRole).Error; err != nil {
		return fmt.Errorf("ops admin role missing: %w", err)
	}

	opsPassword, err := utils.HashPassword("Ops@2025")
	if err != nil {
		return err
	}

	opsAdmin := models.User{
		Username:     "ops-admin",
		Email:        "opsadmin@domain.com",
		PasswordHash: opsPassword,
		RoleID:       opsAdminRole.RoleID,
		IsActive:     true,
	}

	if err := db.Where("email = ?", opsAdmin.Email).FirstOrCreate(&opsAdmin).Error; err != nil {
		return err
	}

	if err := db.Where("user_id = ?", opsAdmin.UserID).FirstOrCreate(&models.UserProfile{
		UserID:    opsAdmin.UserID,
		FirstName: "Operations",
		LastName:  "Administrator",
	}).Error; err != nil {
		return err
	}

	log.Println("✅ SystemAdmin and OpsAdmin users seeded successfully!")
	return nil
}

// -----------------------------
// Seed Permissions
// -----------------------------
func seedPermissions(db *gorm.DB) error {
	permissions := []models.Permission{
		{Name: "manage_system", Description: "Access to all system operations"},
		{Name: "manage_roles", Description: "Can create, update, delete roles"},
		{Name: "manage_cohorts", Description: "Can create, update, and delete cohorts"},
		{Name: "manage_proposals", Description: "can approve, add reviews, view reviews, and view proposals"},
		{Name: "assign_users", Description: "Can assign users to cohorts"},
		{Name: "view_reports", Description: "Can view analytics and reports"},
		{Name: "view_logs", Description: "Can View System Logs"},
		{Name: "manage_supervisors", Description: "Can manage supervisors"},
		{Name: "assign_mentors", Description: "Can Assign Mentors to Cohorts"},
		{Name: "manage_mentors", Description: "Can manage mentors"},
	}

	for _, p := range permissions {
		var existing models.Permission
		err := db.Where("LOWER(name) = ?", strings.ToLower(p.Name)).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			if err := db.Create(&p).Error; err != nil {
				return err
			}
		}
	}

	log.Println("✅ Base permissions seeded successfully!")
	return nil
}

// ----------------------------------------------------------------------
// mapping roles and permissions
//----------------------------------------------------------------------

func seedRolePermissions(db *gorm.DB) error {
	// Define which permissions each role should have
	rolePerms := map[string][]string{
		"SystemAdmin": {"manage_system", "manage_roles", "view_logs"},
		"OpsAdmin":    {"manage_cohorts", "manage_proposals", "assign_users", "manage_supervisors", "manage_mentors", "view_logs"},
		"Supervisor":  {"manage_proposals", "view_reports", "assign_mentors"},
		"Mentor":      {"view_reports"},
		"Student":     {},
	}

	for roleName, perms := range rolePerms {
		var role models.Role
		if err := db.Where("LOWER(name) = ?", strings.ToLower(roleName)).First(&role).Error; err != nil {
			return fmt.Errorf("role %s not found: %w", roleName, err)
		}

		for _, permName := range perms {
			var perm models.Permission
			if err := db.Where("LOWER(name) = ?", strings.ToLower(permName)).First(&perm).Error; err != nil {
				return fmt.Errorf("permission %s not found: %w", permName, err)
			}

			// Associate permission with role if not already assigned
			if err := db.Model(&role).Association("Permissions").Append(&perm); err != nil {
				return fmt.Errorf("failed to assign permission %s to role %s: %w", permName, roleName, err)
			}
		}
	}

	log.Println("🔗 Role-permission mappings seeded successfully!")
	return nil
}

// -----------------------------
// Seed All
// -----------------------------
func SeedAll(db *gorm.DB) error {

	if err := seedAdmins(db); err != nil {
		return err
	}
	if err := seedPermissions(db); err != nil {
		log.Fatalf("Failed to seed permissions: %v", err)
	}

	if err := seedRoles(db); err != nil {
		log.Fatalf("Failed to seed roles: %v", err)
	}

	if err := seedRolePermissions(db); err != nil {
		log.Fatalf("Failed to seed role-permissions: %v", err)
	}

	log.Println("🎉 All base data seeded successfully!")
	return nil
}
