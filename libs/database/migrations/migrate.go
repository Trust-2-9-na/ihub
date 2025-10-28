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
	// 6️⃣ UserTeams
	if err := db.AutoMigrate(&models.UserTeam{}); err != nil {
		log.Printf("Migration failed for User Teams: %v", err)
		return err
	}

	// Add 'role' column if it doesn't exist
	if err := db.Exec(`
    ALTER TABLE user_teams 
    ADD COLUMN IF NOT EXISTS role VARCHAR(20) DEFAULT 'Member'
`).Error; err != nil {
		log.Fatalf("Failed to add 'role' column: %v", err)
	}

	// Add 'joined_at' column if it doesn't exist
	if err := db.Exec(`
    ALTER TABLE user_teams 
    ADD COLUMN IF NOT EXISTS joined_at TIMESTAMP DEFAULT now()
`).Error; err != nil {
		log.Fatalf("Failed to add 'joined_at' column: %v", err)
	}

	// 7️⃣ Cohorts
	if err := db.AutoMigrate(&models.Cohort{}); err != nil {
		log.Printf("Migration failed for Cohorts: %v", err)
		return err
	}
	// Cohort users (many-to-many)
	if err := db.AutoMigrate(&models.CohortUser{}); err != nil {
		log.Fatalf("Migration failed for CohortUsers: %v", err)
	}

	// Add 'role' column if it doesn't exist
	if err := db.Exec(`
    ALTER TABLE cohort_users 
    ADD COLUMN IF NOT EXISTS role VARCHAR(50)
`).Error; err != nil {
		log.Fatalf("Failed to add 'role' column to cohort_users: %v", err)
	}

	// Add 'created_by' column if it doesn't exist
	if err := db.Exec(`
    ALTER TABLE cohort_users 
    ADD COLUMN IF NOT EXISTS created_by BIGINT
`).Error; err != nil {
		log.Fatalf("Failed to add 'created_by' column to cohort_users: %v", err)
	}

	// Add 'created_at' column if it doesn't exist
	if err := db.Exec(`
    ALTER TABLE cohort_users 
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT NOW()
`).Error; err != nil {
		log.Fatalf("Failed to add 'created_at' column to cohort_users: %v", err)
	}

	// Add 'deleted_at' column if it doesn't exist
	if err := db.Exec(`
    ALTER TABLE cohort_users 
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP
`).Error; err != nil {
		log.Fatalf("Failed to add 'deleted_at' column to cohort_users: %v", err)
	}

	// 2️⃣ Create unique index to prevent duplicate active assignments
	if err := db.Exec(`
    CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_user_per_cohort
    ON cohort_users(cohort_cohort_id, user_user_id)
    WHERE deleted_at IS NULL;
`).Error; err != nil {
		log.Fatalf("Failed to create unique index for cohort assignments: %v", err)
	}

	log.Println("Migration for CohortUsers table completed successfully")

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

	//17 migrate weekly comments
	if err := db.AutoMigrate(&models.WeeklyReportComment{}); err != nil {
		log.Printf("Migration failed for Weekly Report comments: %v", err)
		return err
	}
	log.Println("Weekly_report_comment table created successfully")

	//18 migrate weekly comments
	if err := db.AutoMigrate(&models.MentorFeedback{}); err != nil {
		log.Printf("Migration failed for mentor feedback: %v", err)
		return err
	}
	log.Println("mentor feedback table created successfully")

	// 19 migrate email verification

	if err := db.AutoMigrate(&models.EmailVerification{}); err != nil {
		log.Printf("Migration failed for email verification: %v", err)
		return err
	}
	log.Println("email verification table created successfully")

	// 20 password requests migration

	if err := db.AutoMigrate(&models.PasswordRequest{}); err != nil {
		log.Printf("Migration failed for password request: %v", err)
		return err
	}
	log.Println("password request table created successfully")
	// 21 migrate student mentors
	if err := db.AutoMigrate(&models.MentorStudentAssignment{}); err != nil {
		log.Printf("Migration failed for mentor students: %v", err)
		return err
	}
	log.Println("mentor student table created successfully")

	// 22	 migrate password Reset

	if err := db.AutoMigrate(&models.PasswordResetToken{}); err != nil {
		log.Printf("Migration failed for password reset: %v", err)
		return err
	}
	log.Println("password reset table created successfully")

	// 23 migrate lookup models

	if err := db.AutoMigrate(
		&models.Category{},
		&models.Program{},
		&models.School{},
		&models.Expertise{},
		&models.Subfield{},
	); err != nil {
		log.Printf("Migration failed for lookup tables: %v", err)
		return err
	}

	// 24 migrate events
	if err := db.AutoMigrate(&models.Event{}); err != nil {
		log.Printf("Migration failed for events: %v", err)
		return err
	}
	log.Println("event  table created successfully")

	// 25 migrate events
	if err := db.AutoMigrate(&models.EventAttendance{}); err != nil {
		log.Printf("Migration failed for event Attendance: %v", err)
		return err
	}
	log.Println("event attendance table created successfully")

	// 26 migrate event types registry
	if err := db.AutoMigrate(&models.EventTypeRegistry{}); err != nil {
		log.Printf("Migration failed for event Registry: %v", err)
		return err
	}
	log.Println("event registry table created successfully")

	// 27 migrate resources
	if err := db.AutoMigrate(&models.Resource{}); err != nil {
		log.Printf("Migration failed for resources: %v", err)
		return err
	}
	log.Println("resource table created successfully")

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
		Email:         "sysadmin@domain.com",
		PasswordHash:  sysPassword,
		RoleID:        sysAdminRole.RoleID,
		IsActive:      true,
		EmailVerified: true, // mark as verified
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
		Email:         "tnachokwe@gmail.com",
		PasswordHash:  opsPassword,
		RoleID:        opsAdminRole.RoleID,
		IsActive:      true,
		EmailVerified: true, // mark as verified
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
