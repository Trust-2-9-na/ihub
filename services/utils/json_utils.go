package utils

import (
	"encoding/json"
)

func JSONString(data interface{}) *string {
	b, _ := json.Marshal(data)
	s := string(b)
	return &s
}
