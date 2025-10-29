package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"web/services/assets/models"
	"web/services/utils"

	"google.golang.org/api/idtoken"
	"gorm.io/gorm"
)

func (c *Construct) GoogleSignupStudent(w http.ResponseWriter, r *http.Request) {
	c.googleSignupHandler(w, r, "Student")
}

func (c *Construct) GoogleSignupMentor(w http.ResponseWriter, r *http.Request) {
	c.googleSignupHandler(w, r, "Mentor")
}

func (c *Construct) GoogleSignupSupervisor(w http.ResponseWriter, r *http.Request) {
	c.googleSignupHandler(w, r, "Supervisor")
}

func (c *Construct) googleSignupHandler(w http.ResponseWriter, r *http.Request, roleName string) {
	var req struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IDToken == "" {
		c.Json(w, http.StatusBadRequest, "Missing Google ID token", nil)
		return
	}

	// Verify Google token
	payload, err := idtoken.Validate(r.Context(), req.IDToken, os.Getenv("GOOGLE_CLIENT_ID"))
	if err != nil {
		c.Json(w, http.StatusUnauthorized, "Invalid Google token", map[string]interface{}{"error": err.Error()})
		return
	}

	email := fmt.Sprintf("%v", payload.Claims["email"])
	firstName := fmt.Sprintf("%v", payload.Claims["given_name"])
	lastName := fmt.Sprintf("%v", payload.Claims["family_name"])

	// Check if user already exists
	var user models.User
	err = c.DB.Preload("Profile").Preload("Role").Where("email = ?", email).First(&user).Error
	if err == nil {
		// ✅ User exists → create a new session and JWT
		sessionUUID, _ := utils.GenerateRandomString(32)
		session := models.Session{
			SessionUUID:     sessionUUID,
			SessionUserID:   user.UserID,
			SessionUserUUID: user.UserUUID,
			IsActive:        true,
			ExpiresAt:       time.Now().Add(24 * time.Hour),
			UserAgent:       r.UserAgent(),
			IPAddress:       r.RemoteAddr,
			LastActiveAt:    time.Now(),
		}
		if err := c.DB.Create(&session).Error; err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to create session", nil)
			return
		}

		token, err := utils.GenerateJWT(user.UserUUID, user.Role.Name, session.SessionUUID)
		if err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to generate JWT", nil)
			return
		}

		// Set HttpOnly cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "session_id",
			Value:    session.SessionUUID,
			Path:     "/",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   24 * 3600,
		})

		c.Json(w, http.StatusOK, fmt.Sprintf("%s login successful via Google", roleName), map[string]interface{}{
			"token": token,
			"user": map[string]interface{}{
				"user_id":    user.UserID,
				"first_name": user.Profile.FirstName,
				"last_name":  user.Profile.LastName,
				"email":      user.Email,
				"role":       user.Role.Name,
			},
			"session_id": session.SessionUUID,
		})
		return
	} else if err != gorm.ErrRecordNotFound {
		c.Json(w, http.StatusInternalServerError, "Database error", map[string]interface{}{"error": err.Error()})
		return
	}

	// ✅ User does not exist → create new user
	var newUser *models.User
	createRoleProfile := func(userID uint64) error {
		switch strings.ToLower(roleName) {
		case "student":
			return c.DB.Create(&models.StudentProfile{UserID: userID}).Error
		case "mentor":
			return c.DB.Create(&models.MentorProfile{UserID: userID}).Error
		case "supervisor":
			return c.DB.Create(&models.SupervisorProfile{UserID: userID}).Error
		default:
			return fmt.Errorf("unknown role: %s", roleName)
		}
	}

	c.signupUserWithRole(
		w,
		roleName,
		firstName,
		lastName,
		email,
		"", // no password for Google
		func(userID uint64) error {
			if err := createRoleProfile(userID); err != nil {
				return err
			}
			var u models.User
			if err := c.DB.Preload("Profile").Preload("Role").First(&u, userID).Error; err != nil {
				return err
			}
			newUser = &u
			return nil
		},
		true, // isGoogle
	)

	if newUser == nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create user", nil)
		return
	}

	// Create session for new user
	sessionUUID, _ := utils.GenerateRandomString(32)
	session := models.Session{
		SessionUUID:     sessionUUID,
		SessionUserID:   newUser.UserID,
		SessionUserUUID: newUser.UserUUID,
		IsActive:        true,
		ExpiresAt:       time.Now().Add(24 * time.Hour),
		UserAgent:       r.UserAgent(),
		IPAddress:       r.RemoteAddr,
		LastActiveAt:    time.Now(),
	}
	if err := c.DB.Create(&session).Error; err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to create session", nil)
		return
	}

	token, err := utils.GenerateJWT(newUser.UserUUID, newUser.Role.Name, session.SessionUUID)
	if err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to generate JWT", nil)
		return
	}

	// Set HttpOnly cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    session.SessionUUID,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   24 * 3600,
	})

	c.Json(w, http.StatusOK, fmt.Sprintf("%s signup/login successful via Google", roleName), map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"user_id":    newUser.UserID,
			"first_name": newUser.Profile.FirstName,
			"last_name":  newUser.Profile.LastName,
			"email":      newUser.Email,
			"role":       newUser.Role.Name,
		},
		"session_id": session.SessionUUID,
	})
}
