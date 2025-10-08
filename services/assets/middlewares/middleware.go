package middlewares

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"web/services/assets/models"
	"web/services/utils"

	"gorm.io/gorm"
)

// Define a custom type for context keys
type contextKey string

const userUUIDKey contextKey = "user_uuid"

// RoleAuthorization validates JWT, checks allowed roles, active status, and optional permissions
func RoleAuthorization(db *gorm.DB, allowedRoles []string, requiredPermissions ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Missing Authorization Header", http.StatusUnauthorized)
				return
			}

			tokenString := strings.Replace(authHeader, "Bearer ", "", 1)

			// Validate JWT
			claims, err := utils.ValidateJWT(tokenString)
			if err != nil {
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			if claims.Role == "" {
				http.Error(w, "Access denied: role missing", http.StatusForbidden)
				return
			}

			// Fetch user from DB with permissions
			var user models.User
			if err := db.Preload("Role.Permissions").Where("user_uuid = ?", claims.UserUUID).First(&user).Error; err != nil {
				http.Error(w, "User not found", http.StatusUnauthorized)
				return
			}

			// Check if user is active
			if !user.IsActive {
				http.Error(w, "Account disabled", http.StatusForbidden)
				return
			}

			// Check allowed roles
			roleAllowed := false
			for _, role := range allowedRoles {
				if strings.EqualFold(user.Role.Name, role) {
					roleAllowed = true
					break
				}
			}
			if !roleAllowed {
				fmt.Printf("Access denied for role: %s\n", user.Role.Name)
				http.Error(w, "Forbidden: You don't have permission", http.StatusForbidden)
				return
			}

			// Check required permissions if provided
			if len(requiredPermissions) > 0 {
				hasPermission := false
				for _, perm := range user.Role.Permissions {
					for _, req := range requiredPermissions {
						if strings.EqualFold(perm.Name, req) {
							hasPermission = true
							break
						}
					}
					if hasPermission {
						break
					}
				}
				if !hasPermission {
					fmt.Printf("Access denied: missing required permission for role %s\n", user.Role.Name)
					http.Error(w, "Forbidden: missing required permission", http.StatusForbidden)
					return
				}
			}

			// Store UserUUID in context using custom key type
			ctx := context.WithValue(r.Context(), userUUIDKey, user.UserUUID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Helper function to retrieve UserUUID from context
func GetUserUUIDFromContext(ctx context.Context) (string, bool) {
	uuid, ok := ctx.Value(userUUIDKey).(string)
	return uuid, ok
}
