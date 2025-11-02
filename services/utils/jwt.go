package utils

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"
	"web/services/assets/models"

	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

// JwtKey ideally comes from env variable
var JwtKey = []byte("INfr!78InnOHUBkEY@2502")

// Claims struct used in JWT
type Claims struct {
	UserUUID    string `json:"user_uuid"`
	Role        string `json:"role"`
	SessionUUID string `json:"session_uuid"` // 🔹 Added for session linkage
	jwt.RegisteredClaims
}

// GenerateJWT generates a JWT token tied to a session
func GenerateJWT(userUUID string, role string, sessionUUID string) (string, error) {
	expirationTime := time.Now().Add(1 * time.Hour) // 1h lifespan

	claims := &Claims{
		UserUUID:    userUUID,
		Role:        role,
		SessionUUID: sessionUUID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(JwtKey)
}

// ValidateJWT validates a token and returns claims
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

// ValidateSessionFromJWT cross-checks a token with a session record
func ValidateSessionFromJWT(db *gorm.DB, tokenStr string) (*models.Session, *Claims, error) {
	claims, err := ValidateJWT(tokenStr)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid token: %v", err)
	}

	var session models.Session
	if err := db.Where("session_uuid = ? AND is_active = ?", claims.SessionUUID, true).First(&session).Error; err != nil {
		return nil, claims, fmt.Errorf("session not found or inactive")
	}

	if session.ExpiresAt.Before(time.Now()) {
		return nil, claims, fmt.Errorf("session expired")
	}

	return &session, claims, nil
}

// GenerateRandomString returns a secure random string of given length
func GenerateRandomString(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("invalid length")
	}

	b := make([]byte, (length*3)/4)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}

	s := base64.URLEncoding.EncodeToString(b)
	return s[:length], nil
}
