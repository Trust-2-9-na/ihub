package utils

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JwtKey ideally comes from env variable
var JwtKey = []byte("INfr!78InnOHUBkEY@2502")

// Claims struct used in JWT
type Claims struct {
	UserUUID string `json:"user_id"` // changed from uint64
	Role     string `json:"role"`    // Admin, Student, Mentor, Supervisor
	jwt.RegisteredClaims
}

// GenerateJWT generates a JWT token for a user
func GenerateJWT(userUUID string, role string) (string, error) {
	expirationTime := time.Now().Add(1 * time.Hour)

	claims := &Claims{
		UserUUID: userUUID,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(JwtKey)
}

// ValidateJWT validates a token string and returns claims
func ValidateJWT(tokenStr string) (*Claims, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		return JwtKey, nil
	})
	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	return claims, nil
}

// GenerateRandomString returns a secure random string of the given length
func GenerateRandomString(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("invalid length")
	}

	// 3/4 * length because base64 encoding expands size by ~4/3
	b := make([]byte, (length*3)/4)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}

	s := base64.URLEncoding.EncodeToString(b)
	return s[:length], nil
}
