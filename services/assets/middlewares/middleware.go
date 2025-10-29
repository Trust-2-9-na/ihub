package middlewares

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
	"web/services/assets/models"
	"web/services/utils"

	"gorm.io/gorm"
)

// Define a custom type for context keys
type contextKey string

const userUUIDKey contextKey = "user_uuid"
const sessionUUIDKey contextKey = "session_uuid"

// RoleAuthorizationWithSession validates JWT + session, checks roles & optional permissions
func RoleAuthorization(db *gorm.DB, allowedRoles []string, requiredPermissions ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Missing Authorization Header", http.StatusUnauthorized)
				return
			}

			tokenString := strings.Replace(authHeader, "Bearer ", "", 1)

			// 1️⃣ Validate JWT
			claims, err := utils.ValidateJWT(tokenString)
			if err != nil {
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			// 2️⃣ Fetch user from DB
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

			// 3️⃣ Validate session cookie
			cookie, err := r.Cookie("session_id")
			if err != nil {
				http.Error(w, "Session not found", http.StatusUnauthorized)
				return
			}

			var session models.Session
			if err := db.Where("session_uuid = ? AND is_active = ?", cookie.Value, true).First(&session).Error; err != nil {
				http.Error(w, "Invalid session", http.StatusUnauthorized)
				return
			}

			// Check session expiration
			if time.Now().After(session.ExpiresAt) {
				// Optionally deactivate expired session
				db.Model(&session).Update("is_active", false)
				http.Error(w, "Session expired", http.StatusUnauthorized)
				return
			}

			// 4️⃣ Role check
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

			// 5️⃣ Permission check (optional)
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

			// 6️⃣ Optional: Update session last active timestamp for sliding expiration
			db.Model(&session).Update("last_active_at", time.Now())

			// 7️⃣ Store user & session info in context
			ctx := context.WithValue(r.Context(), userUUIDKey, user.UserUUID)
			ctx = context.WithValue(ctx, sessionUUIDKey, session.SessionUUID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Helper functions to retrieve values from context
func GetUserUUIDFromContext(ctx context.Context) (string, bool) {
	uuid, ok := ctx.Value(userUUIDKey).(string)
	return uuid, ok
}

func GetSessionUUIDFromContext(ctx context.Context) (string, bool) {
	sessionID, ok := ctx.Value(sessionUUIDKey).(string)
	return sessionID, ok
}
