package middlewares

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"web/services/utils"

	"github.com/golang-jwt/jwt/v5"
	"github.com/joho/godotenv"
)

var jwtSecret string

func init() {
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env file not loaded")
	}
	jwtSecret = os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Println("Warning: JWT_SECRET not set, using fallback key")
		jwtSecret = "INfr!78InnOHUBkEY@2502"
	}
}

// Claims defines the JWT payload structure
type Claims struct {
	UserID uint64 `json:"user_id"`
	Role   string `json:"role"` // Expected: Admin, Student, Mentor, Supervisor
	jwt.RegisteredClaims
}

// RoleAuthorization validates JWT and checks for allowed roles
func RoleAuthorization(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Missing Authorization Header", http.StatusUnauthorized)
				return
			}

			tokenString := strings.Replace(authHeader, "Bearer ", "", 1)

			// Use the same Claims struct as in utils
			claims := &utils.Claims{}

			token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
				return []byte(utils.JwtKey), nil // or jwtKey if imported directly
			})

			if err != nil || !token.Valid {
				http.Error(w, "Invalid Token", http.StatusUnauthorized)
				return
			}

			if claims.Role == "" {
				http.Error(w, "Access denied: role missing", http.StatusForbidden)
				return
			}

			// Check if role is allowed
			allowed := false
			for _, role := range allowedRoles {
				if strings.EqualFold(claims.Role, role) {
					allowed = true
					break
				}
			}

			if !allowed {
				fmt.Printf("Access denied for role: %s\n", claims.Role)
				http.Error(w, "Forbidden: You don't have permission", http.StatusForbidden)
				return
			}

			// Store UserID in context
			ctx := context.WithValue(r.Context(), "user_id", claims.UserID)
			r = r.WithContext(ctx)

			next.ServeHTTP(w, r)
		})
	}
}

// ContextWithUserID stores user ID in context
func ContextWithUserID(ctx context.Context, userID uint64) context.Context {
	return context.WithValue(ctx, contextKey("user_id"), userID)
}

// UserIDFromContext retrieves user ID from context
func UserIDFromContext(ctx context.Context) uint64 {
	if val, ok := ctx.Value(contextKey("user_id")).(uint64); ok {
		return val
	}
	return 0
}

// contextKey prevents collisions in context keys
type contextKey string
