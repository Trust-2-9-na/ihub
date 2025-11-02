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

type contextKey string

const userUUIDKey contextKey = "user_uuid"
const sessionUUIDKey contextKey = "session_uuid"

// RoleAuthorization validates JWT + session (cookie or X-Session-ID), checks roles and optional permissions
func RoleAuthorization(db *gorm.DB, allowedRoles []string, requiredPermissions ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 0) Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Missing Authorization Header", http.StatusUnauthorized)
				return
			}
			tokenString := strings.Replace(authHeader, "Bearer ", "", 1)

			// 1) Validate JWT
			claims, err := utils.ValidateJWT(tokenString)
			if err != nil {
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			// 2) Fetch user
			var user models.User
			if err := db.Preload("Role.Permissions").Where("user_uuid = ?", claims.UserUUID).First(&user).Error; err != nil {
				http.Error(w, "User not found", http.StatusUnauthorized)
				return
			}
			if !user.IsActive {
				http.Error(w, "Account disabled", http.StatusForbidden)
				return
			}

			// 3) Resolve session id from cookie OR header
			var sessionID string
			if c, err := r.Cookie("session_id"); err == nil && c.Value != "" {
				sessionID = c.Value
			}
			if sessionID == "" {
				// Per-tab session propagation from frontend
				sessionID = r.Header.Get("X-Session-ID")
			}
			if sessionID == "" {
				http.Error(w, "Session not found", http.StatusUnauthorized)
				return
			}

			// 4) Validate session
			var session models.Session
			if err := db.Where("session_uuid = ? AND is_active = ?", sessionID, true).First(&session).Error; err != nil {
				http.Error(w, "Invalid session", http.StatusUnauthorized)
				return
			}
			if time.Now().After(session.ExpiresAt) {
				db.Model(&session).Update("is_active", false)
				http.Error(w, "Session expired", http.StatusUnauthorized)
				return
			}

			// 5) Ensure session belongs to the JWT user
			if session.SessionUserUUID != user.UserUUID {
				http.Error(w, "Session does not match token user", http.StatusUnauthorized)
				return
			}

			// 6) Role check
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

			// 7) Permission check (optional)
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

			// 8) Optional sliding expiration
			db.Model(&session).Update("last_active_at", time.Now())

			// 9) Stash into context
			ctx := context.WithValue(r.Context(), userUUIDKey, user.UserUUID)
			ctx = context.WithValue(ctx, sessionUUIDKey, session.SessionUUID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetUserUUIDFromContext(ctx context.Context) (string, bool) {
	uuid, ok := ctx.Value(userUUIDKey).(string)
	return uuid, ok
}

func GetSessionUUIDFromContext(ctx context.Context) (string, bool) {
	sessionID, ok := ctx.Value(sessionUUIDKey).(string)
	return sessionID, ok
}
