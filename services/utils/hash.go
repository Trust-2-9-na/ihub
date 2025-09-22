package utils

import (
	"golang.org/x/crypto/bcrypt"
)

// HashPassword hashes a plain password
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 12) // 12 is the cost
	return string(bytes), err
}

// CheckPasswordHash compares plain password with hashed password
func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
