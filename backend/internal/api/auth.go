package api

import (
	"context"
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

type authContextKey struct{}

type AuthContext struct {
	Type           string
	Username       string
	ApiKeyID       string
	ApiKeyName     string
	Actor          string
	Scopes         []string
	ContainerUUIDs []string
}

const (
	authTypeAdmin   = "admin"
	authTypeSubUser = "sub_user"
	authTypeAPIKey  = "api_key"
)

func withAuthContext(r *http.Request, auth AuthContext) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), authContextKey{}, auth))
}

func authContextFromRequest(r *http.Request) (AuthContext, bool) {
	ctx, ok := r.Context().Value(authContextKey{}).(AuthContext)
	return ctx, ok
}

func requestActor(r *http.Request) string {
	if ctx, ok := authContextFromRequest(r); ok && ctx.Actor != "" {
		return ctx.Actor
	}
	if claims, ok := claimsFromRequest(r); ok {
		if subUser, _ := claims["sub_user"].(string); subUser != "" {
			return "user:" + subUser
		}
		if username, _ := claims["username"].(string); username != "" {
			return username
		}
	}
	return "admin"
}

func hasScope(r *http.Request, scope string) bool {
	ctx, ok := authContextFromRequest(r)
	if !ok {
		return true
	}
	switch ctx.Type {
	case authTypeAdmin:
		return true
	case authTypeSubUser:
		return subUserScopeAllowed(scope)
	case authTypeAPIKey:
		return scopeAllowed(ctx.Scopes, scope)
	default:
		return false
	}
}

func subUserScopeAllowed(scope string) bool {
	switch scope {
	case "container:read", "container:power", "container:reinstall", "container:network",
		"dashboard:read", "image:read", "task:read", "snapshot:read", "snapshot:create", "snapshot:delete", "snapshot:restore", "snapshot:schedule",
		"terminal:ssh", "terminal:vnc":
		return true
	default:
		return false
	}
}

func hasAnyScope(r *http.Request, scopes ...string) bool {
	for _, scope := range scopes {
		if hasScope(r, scope) {
			return true
		}
	}
	return false
}

func scopeAllowed(scopes []string, required string) bool {
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "*" || scope == "admin:*" || scope == required {
			return true
		}
		if strings.HasSuffix(scope, ":*") {
			prefix := strings.TrimSuffix(scope, "*")
			if strings.HasPrefix(required, prefix) {
				return true
			}
		}
	}
	return false
}

func requireScope(w http.ResponseWriter, r *http.Request, scope string) bool {
	if hasScope(r, scope) {
		return true
	}
	jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Insufficient API key scope"})
	return false
}

func ScopeMiddleware(scope string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireScope(w, r, scope) {
			return
		}
		next(w, r)
	}
}

func AnyScopeMiddleware(scopes []string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if hasAnyScope(r, scopes...) {
			next(w, r)
			return
		}
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Insufficient API key scope"})
	}
}

func auditRequest(r *http.Request, action, target, detail string, success bool, errMsg string) {
	config.AddAuditLogFull(action, target, detail, requestActor(r), clientIP(r), r.UserAgent(), success, errMsg)
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
	if !ok {
		return nil, false
	}

	// For sub-user tokens, check token_version against stored version (password rotation invalidation)
	if subUser, _ := claims["sub_user"].(string); subUser != "" {
		tokenVersionFloat, hasVersion := claims["token_version"].(float64)
		tokenVersion := int(tokenVersionFloat)
		foundSubUser := false
		for i := range config.AppConfig.SubUsers {
			if config.AppConfig.SubUsers[i].Username == subUser {
				foundSubUser = true
				stored := config.AppConfig.SubUsers[i].TokenVersion
				// If stored version > 0, require token_version to match exactly.
				// This also rejects legacy tokens that lack token_version entirely.
				if stored > 0 && (!hasVersion || tokenVersion != stored) {
					return nil, false
				}
				break
			}
		}
		if !foundSubUser {
			return nil, false
		}
	}

	return claims, ok
}

func claimsFromRequest(r *http.Request) (jwt.MapClaims, bool) {
	return claimsFromToken(tokenFromRequest(r))
}

func isSubUserRequest(r *http.Request) bool {
	if ctx, ok := authContextFromRequest(r); ok {
		return ctx.Type == authTypeSubUser
	}
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
		if claims, ok := claimsFromToken(tokenString); ok {
			if subUser, _ := claims["sub_user"].(string); subUser != "" {
				auth := AuthContext{Type: authTypeSubUser, Username: subUser, Actor: "user:" + subUser}
				if values, ok := claims["container_uuids"].([]interface{}); ok {
					for _, value := range values {
						if uuid, ok := value.(string); ok {
							auth.ContainerUUIDs = append(auth.ContainerUUIDs, uuid)
						}
					}
				}
				next(w, withAuthContext(r, auth))
				return
			}
			username, _ := claims["username"].(string)
			if username == "" {
				username = config.AppConfig.AdminUser
			}
			next(w, withAuthContext(r, AuthContext{Type: authTypeAdmin, Username: username, Actor: username}))
			return
		}

		if key, ok := validateApiKeyRequest(r); ok {
			next(w, withAuthContext(r, authContextFromAPIKey(key)))
			return
		}

		jsonResponse(w, http.StatusUnauthorized, APIResponse{Success: false, Message: "Authentication required"})
	}
}

// AdminMiddleware requires a valid administrator token and rejects sub-user tokens.
func AdminMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		ctx, _ := authContextFromRequest(r)
		if ctx.Type == authTypeSubUser {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Administrator permission required"})
			return
		}
		if ctx.Type == authTypeAPIKey && !scopeAllowed(ctx.Scopes, "admin:access") {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Administrator permission required"})
			return
		}
		next(w, r)
	})
}
