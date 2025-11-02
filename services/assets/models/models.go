package models

import (
	"web/libs/database"
)

type Log struct {
	database.Base
	Activity string `json:"activity"`
}
