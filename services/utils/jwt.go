package utils

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var JwtKey = []byte("INfr!78InnOHUBkEY@2502") // Ideally move this to an env variable

// Claims struct used in JWT
type Claims struct {
	UserID uint64 `json:"user_id"`
	Role   string `json:"role"` // Admin, Student, Mentor, Supervisor
	jwt.RegisteredClaims
}

// GenerateJWT generates a JWT token for a user
func GenerateJWT(userID uint64, role string) (string, error) {
	expirationTime := time.Now().Add(1 * time.Hour) // Token valid for 1 hour

	claims := &Claims{
		UserID: userID,
		Role:   role,
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
