package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"clicd/internal/config"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

func generateRandomStr(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return hex.EncodeToString(b)[:length]
}

type subUserResponse struct {
	ID             string   `json:"id"`
	Username       string   `json:"username"`
	Password       string   `json:"password,omitempty"`
	ContainerNames []string `json:"container_names"`
	ContainerUUIDs []string `json:"container_uuids,omitempty"`
	AccessCode     string   `json:"access_code"`
	CreatedAt      string   `json:"created_at"`
}

func newSubUserResponse(su config.SubUser, password string) subUserResponse {
	return subUserResponse{
		ID:             su.ID,
		Username:       su.Username,
		Password:       password,
		ContainerNames: su.ContainerNames,
		ContainerUUIDs: su.ContainerUUIDs,
		AccessCode:     su.AccessCode,
		CreatedAt:      su.CreatedAt,
	}
}

// HandleSubUserCreate creates a sub-user for a specific container
func HandleSubUserCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	var req struct {
		ContainerName string `json:"container_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}

	c := containerByIdentifier(req.ContainerName)

	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	containerName := c.Name

	// Check if sub-user already exists for this container
	for i := range config.AppConfig.SubUsers {
		su := &config.AppConfig.SubUsers[i]
		for _, uuid := range su.ContainerUUIDs {
			if uuid == c.UUID {
				if su.AccessCode == "" {
					su.AccessCode = generateRandomStr(8)
				}
				password := generateRandomStr(16)
				if hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost); err == nil {
					su.PassHash = string(hash)
				}
				su.Password = ""
				su.Token = ""
				su.ContainerNames = appendUniqueString(su.ContainerNames, containerName)
				su.ContainerUUIDs = appendUniqueString(su.ContainerUUIDs, c.UUID)
				config.SaveConfig()
				jsonResponse(w, http.StatusOK, APIResponse{
					Success: true,
					Message: "Sub-user password rotated",
					Data:    newSubUserResponse(*su, password),
				})
				return
			}
		}
	}

	// Create new sub-user
	username := "user-" + generateRandomStr(8)
	password := generateRandomStr(16)
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)

	// Generate short access code (8 chars, for URL sharing)
	accessCode := generateRandomStr(8)

	subUser := config.SubUser{
		ID:             "sub-" + generateRandomStr(8),
		Username:       username,
		PassHash:       string(hash),
		ContainerNames: []string{containerName},
		ContainerUUIDs: []string{c.UUID},
		AccessCode:     accessCode,
		CreatedAt:      time.Now().Format("2006-01-02 15:04:05"),
	}

	config.AppConfig.SubUsers = append(config.AppConfig.SubUsers, subUser)
	config.SaveConfig()
	config.AddAuditLog("创建子用户", containerName, fmt.Sprintf("用户: %s", username), "admin")

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Sub-user created", Data: newSubUserResponse(subUser, password)})
}

// HandleSubUserLogin handles sub-user login
func HandleSubUserLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}

	// Find sub-user
	for _, su := range config.AppConfig.SubUsers {
		if su.Username == req.Username {
			if err := bcrypt.CompareHashAndPassword([]byte(su.PassHash), []byte(req.Password)); err == nil {
				// Generate fresh token
				containerUUIDs := activeSubUserContainerUUIDs(&su)
				if len(containerUUIDs) == 0 {
					jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "No active container is assigned to this user"})
					return
				}
				tokenStr := newSubUserToken(su.Username, containerUUIDs, time.Now().Add(24*time.Hour))

				jsonResponse(w, http.StatusOK, APIResponse{
					Success: true,
					Data: map[string]interface{}{
						"token":           tokenStr,
						"username":        su.Username,
						"container_uuids": containerUUIDs,
					},
				})
				return
			}
		}
	}

	jsonResponse(w, http.StatusUnauthorized, APIResponse{Success: false, Message: "Invalid credentials"})
}

// HandleSubUserAccessCode handles access via short code + password (no token in URL)
func HandleSubUserAccessCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	var req struct {
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}

	// Find sub-user by access code
	for _, su := range config.AppConfig.SubUsers {
		if su.AccessCode == req.Code {
			if err := bcrypt.CompareHashAndPassword([]byte(su.PassHash), []byte(req.Password)); err != nil {
				jsonResponse(w, http.StatusUnauthorized, APIResponse{Success: false, Message: "Invalid password"})
				return
			}

			containerUUIDs := activeSubUserContainerUUIDs(&su)
			if len(containerUUIDs) == 0 {
				jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "No active container is assigned to this link"})
				return
			}
			tokenStr := newSubUserToken(su.Username, containerUUIDs, time.Now().Add(24*time.Hour))

			jsonResponse(w, http.StatusOK, APIResponse{
				Success: true,
				Data: map[string]interface{}{
					"token":           tokenStr,
					"username":        su.Username,
					"container_uuids": containerUUIDs,
				},
			})
			return
		}
	}

	jsonResponse(w, http.StatusUnauthorized, APIResponse{Success: false, Message: "Invalid access code"})
}

func newSubUserToken(username string, containerUUIDs []string, expiresAt time.Time) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub_user":        username,
		"container_uuids": containerUUIDs,
		"exp":             expiresAt.Unix(),
		"iat":             time.Now().Unix(),
	})
	tokenStr, _ := token.SignedString([]byte(config.AppConfig.JWTSecret))
	return tokenStr
}

type subUserAccess struct {
	names map[string]bool
	uuids map[string]bool
}

func subUserAllowedContainers(r *http.Request) (subUserAccess, bool) {
	claims, ok := claimsFromRequest(r)
	if !ok {
		return subUserAccess{}, false
	}
	if _, isSubUser := claims["sub_user"]; !isSubUser {
		return subUserAccess{}, false
	}

	allowed := subUserAccess{
		names: make(map[string]bool),
		uuids: make(map[string]bool),
	}
	if containerUUIDs, ok := claims["container_uuids"].([]interface{}); ok {
		for _, item := range containerUUIDs {
			if uuid, ok := item.(string); ok {
				allowed.uuids[uuid] = true
			}
		}
	}
	if containerUUIDs, ok := claims["container_uuids"].([]string); ok {
		for _, uuid := range containerUUIDs {
			allowed.uuids[uuid] = true
		}
	}
	return allowed, true
}

func containerByIdentifier(identifier string) *config.Container {
	return config.FindContainerByIdentifier(identifier)
}

func isContainerAllowedForRequest(r *http.Request, identifier string) bool {
	allowed, isSubUser := subUserAllowedContainers(r)
	if !isSubUser {
		return true
	}
	c := containerByIdentifier(identifier)
	if c == nil {
		return false
	}
	return isContainerAllowed(allowed, c)
}

// HandleAuditLogs returns audit logs
func HandleAuditLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	logs := config.AppConfig.AuditLogs
	if logs == nil {
		logs = []config.AuditLog{}
	}
	// Return in reverse order (newest first)
	reversed := make([]config.AuditLog, len(logs))
	for i, l := range logs {
		reversed[len(logs)-1-i] = l
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: reversed})
}

// SubUserMiddleware checks if a request is from a sub-user and restricts container access
func SubUserMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		allowed, isSubUser := subUserAllowedContainers(r)
		if !isSubUser {
			next(w, r)
			return
		}

		path := r.URL.Path
		if path == "/api/tasks" && r.Method == http.MethodGet {
			next(w, r)
			return
		}

		if path == "/api/containers" {
			if r.Method != http.MethodGet {
				jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Sub-users cannot create containers"})
				return
			}
			next(w, r)
			return
		}

		if len(path) > len("/api/containers/") {
			rest := path[len("/api/containers/"):]
			parts := splitPath(rest)
			if len(parts) > 0 && parts[0] != "" {
				c := containerByIdentifier(parts[0])
				if c == nil || !isContainerAllowed(allowed, c) {
					jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Access denied to this container"})
					return
				}
				action := ""
				if len(parts) > 1 {
					action = parts[1]
				}
				if !isSubUserContainerActionAllowed(action, r.Method) {
					jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Action is not allowed for this link"})
					return
				}
			}
			next(w, r)
			return
		}

		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Access denied"})
		return
	}
}

func filterContainersForRequest(r *http.Request, containers []config.Container) []config.Container {
	allowed, isSubUser := subUserAllowedContainers(r)
	if !isSubUser {
		return containers
	}
	filtered := make([]config.Container, 0, len(containers))
	for _, c := range containers {
		if isContainerAllowed(allowed, &c) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

func filterTasksForRequest(r *http.Request, tasks []*Task) []*Task {
	allowed, isSubUser := subUserAllowedContainers(r)
	if !isSubUser {
		return tasks
	}
	filtered := make([]*Task, 0, len(tasks))
	for _, task := range tasks {
		if c := config.FindContainer(task.ContainerID); c != nil && isContainerAllowed(allowed, c) {
			filtered = append(filtered, task)
			continue
		}
		if task.ContainerName != "" {
			if c := config.FindContainerByName(task.ContainerName); c != nil && isContainerAllowed(allowed, c) {
				filtered = append(filtered, task)
				continue
			}
		}
		if task.Config.Name != "" {
			if c := config.FindContainerByName(task.Config.Name); c != nil && isContainerAllowed(allowed, c) {
				filtered = append(filtered, task)
			}
		}
	}
	return filtered
}

func isContainerAllowed(allowed subUserAccess, c *config.Container) bool {
	return c != nil && c.UUID != "" && allowed.uuids[c.UUID]
}

func isSubUserContainerActionAllowed(action string, method string) bool {
	if action == "" {
		return method == http.MethodGet
	}
	switch {
	case action == "usage" || action == "traffic" || action == "random-port":
		return method == http.MethodGet
	case action == "snapshots":
		return method == http.MethodGet || method == http.MethodPost
	case action == "snapshots/schedule":
		return method == http.MethodPost
	case strings.HasPrefix(action, "snapshots/"):
		return method == http.MethodDelete || method == http.MethodPost
	case action == "start" || action == "stop" || action == "restart" || action == "reinstall":
		return method == http.MethodPost
	case strings.HasPrefix(action, "port-mappings/"):
		return method == http.MethodPut
	default:
		return false
	}
}

func activeSubUserContainerUUIDs(su *config.SubUser) []string {
	uuids := make([]string, 0, len(su.ContainerUUIDs))
	for _, uuid := range su.ContainerUUIDs {
		if c := config.FindContainerByUUID(uuid); c != nil {
			uuids = appendUniqueString(uuids, c.UUID)
		}
	}
	if len(uuids) > 0 {
		return uuids
	}
	return subUserContainerUUIDs(su.ContainerNames)
}

func subUserContainerUUIDs(containerNames []string) []string {
	uuids := make([]string, 0, len(containerNames))
	for _, name := range containerNames {
		if c := config.FindContainerByName(name); c != nil && c.UUID != "" {
			uuids = appendUniqueString(uuids, c.UUID)
		}
	}
	return uuids
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func splitPath(path string) []string {
	parts := make([]string, 0)
	for _, p := range splitBy(path, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func splitBy(s, sep string) []string {
	result := make([]string, 0)
	current := ""
	for _, c := range s {
		if string(c) == sep {
			result = append(result, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	result = append(result, current)
	return result
}
