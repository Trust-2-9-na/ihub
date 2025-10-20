package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
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
		// ✅ User exists → login directly
		token, err := utils.GenerateJWT(fmt.Sprint(user.UserID), user.Role.Name)
		if err != nil {
			c.Json(w, http.StatusInternalServerError, "Failed to generate JWT", nil)
			return
		}

		c.Json(w, http.StatusOK, fmt.Sprintf("%s login successful via Google", roleName), map[string]interface{}{
			"token": token,
			"user": map[string]interface{}{
				"user_id":    user.UserID,
				"first_name": user.Profile.FirstName,
				"last_name":  user.Profile.LastName,
				"email":      user.Email,
				"role":       user.Role.Name,
			},
		})
		return
	} else if err != gorm.ErrRecordNotFound {
		c.Json(w, http.StatusInternalServerError, "Database error", map[string]interface{}{"error": err.Error()})
		return
	}

	// ✅ User does not exist → create new user via signupUserWithRole
	var newUser *models.User
	createRoleProfile := func(userID uint64) error {
		// Create role-specific profile
		switch strings.ToLower(roleName) {
		case "student":
			student := models.StudentProfile{
				UserID:      userID,
				School:      "",
				Program:     "",
				YearOfStudy: "",
			}
			return c.DB.Create(&student).Error
		case "mentor":
			mentor := models.MentorProfile{
				UserID:       userID,
				Department:   nil,
				Organization: "",
				Expertise:    "",
				YearsExp:     0,
			}
			return c.DB.Create(&mentor).Error
		case "supervisor":
			supervisor := models.SupervisorProfile{
				UserID:       userID,
				Department:   nil,
				Organization: "",
				Expertise:    "",
				YearsExp:     0,
			}
			return c.DB.Create(&supervisor).Error
		default:
			return fmt.Errorf("unknown role: %s", roleName)
		}
	}

	// Wrap signup to capture the created user
	c.signupUserWithRole(
		w,
		roleName,
		firstName,
		lastName,
		email,
		"", // no password for Google
		func(userID uint64) error {
			err := createRoleProfile(userID)
			if err != nil {
				return err
			}

			// Fetch the newly created user with profile and role
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

	// Generate JWT for new user
	token, err := utils.GenerateJWT(fmt.Sprint(newUser.UserID), newUser.Role.Name)
	if err != nil {
		c.Json(w, http.StatusInternalServerError, "Failed to generate JWT", nil)
		return
	}

	// Return JWT and user info
	c.Json(w, http.StatusOK, fmt.Sprintf("%s signup/login successful via Google", roleName), map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"user_id":    newUser.UserID,
			"first_name": newUser.Profile.FirstName,
			"last_name":  newUser.Profile.LastName,
			"email":      newUser.Email,
			"role":       newUser.Role.Name,
		},
	})
}
