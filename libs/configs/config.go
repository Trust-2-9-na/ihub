package configs

import (
	"os"

	_ "github.com/joho/godotenv/autoload"
)

var (
	Database = os.Getenv("DB_DATABASE")
	Password = os.Getenv("DB_PASSWORD")
	Username = os.Getenv("DB_USERNAME")
	Port     = os.Getenv("DB_PORT")
	Host     = os.Getenv("DB_HOST")
	Schema   = os.Getenv("DB_SCHEMA")
)
