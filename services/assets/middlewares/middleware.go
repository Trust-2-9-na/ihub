package middlewares

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"web/services/utils"
)

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

			claims, err := utils.ValidateJWT(tokenString)
			if err != nil {
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			if claims.Role == "" {
				http.Error(w, "Access denied: role missing", http.StatusForbidden)
				return
			}

			// Check allowed roles
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

			// Store UserUUID in context
			ctx := context.WithValue(r.Context(), "user_uuid", claims.UserUUID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
