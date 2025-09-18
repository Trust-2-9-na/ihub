package migrations

import (
	"web/libs/database"
)

func Migrate() error {
	db := database.New()
	return db.DB().AutoMigrate()
}
