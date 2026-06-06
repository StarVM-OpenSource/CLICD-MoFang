package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"clicd/internal/config"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token    string `json:"token"`
	Username string `json:"username"`
}

type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

func jsonResponse(w http.ResponseWriter, status int, resp APIResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

func tokenFromRequest(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}

	cookie, err := r.Cookie("clicd_token")
	if err == nil {
		return cookie.Value
	}

	return ""
}

func isValidToken(tokenString string) bool {
	_, ok := claimsFromToken(tokenString)
	return ok
}

func claimsFromToken(tokenString string) (jwt.MapClaims, bool) {
	if tokenString == "" {
		return nil, false
	}
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(config.AppConfig.JWTSecret), nil
	})
	if err != nil || !token.Valid {
		return nil, false
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	return claims, ok
}

func claimsFromRequest(r *http.Request) (jwt.MapClaims, bool) {
	return claimsFromToken(tokenFromRequest(r))
}

func isSubUserRequest(r *http.Request) bool {
	claims, ok := claimsFromRequest(r)
	if !ok {
		return false
	}
	_, ok = claims["sub_user"]
	return ok
}

func isAuthenticatedRequest(r *http.Request) bool {
	return isValidToken(tokenFromRequest(r))
}

// HandleLogin processes login requests
func HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}

	ip := clientIP(r)
	ua := r.Header.Get("User-Agent")

	if req.Username != config.AppConfig.AdminUser {
		RecordLoginLog(req.Username, ip, ua, false)
		jsonResponse(w, http.StatusUnauthorized, APIResponse{Success: false, Message: "Invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(config.AppConfig.AdminPassHash), []byte(req.Password)); err != nil {
		RecordLoginLog(req.Username, ip, ua, false)
		jsonResponse(w, http.StatusUnauthorized, APIResponse{Success: false, Message: "Invalid credentials"})
		return
	}

	RecordLoginLog(req.Username, ip, ua, true)

	// Generate JWT token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"username": req.Username,
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
		"iat":      time.Now().Unix(),
	})

	tokenString, err := token.SignedString([]byte(config.AppConfig.JWTSecret))
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to generate token"})
		return
	}

	jsonResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Data: LoginResponse{
			Token:    tokenString,
			Username: req.Username,
		},
	})
}

// HandleChangePassword processes password change requests
func HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}

	if len(req.NewPassword) < 8 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "New password must be at least 8 characters"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(config.AppConfig.AdminPassHash), []byte(req.OldPassword)); err != nil {
		jsonResponse(w, http.StatusUnauthorized, APIResponse{Success: false, Message: "Current password is incorrect"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to hash password"})
		return
	}

	config.AppConfig.AdminPassHash = string(hash)
	if err := config.SaveConfig(); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to save configuration"})
		return
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Password changed successfully"})
}

// HandleCheckAuth checks if the user is authenticated
func HandleCheckAuth(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Authenticated"})
}

// AuthMiddleware extracts JWT from cookies or Authorization header
func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenString := tokenFromRequest(r)
		if !isValidToken(tokenString) && !isValidApiKeyRequest(r) {
			jsonResponse(w, http.StatusUnauthorized, APIResponse{Success: false, Message: "Authentication required"})
			return
		}

		next(w, r)
	}
}

// AdminMiddleware requires a valid administrator token and rejects sub-user tokens.
func AdminMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if isSubUserRequest(r) {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Administrator permission required"})
			return
		}
		next(w, r)
	})
}
