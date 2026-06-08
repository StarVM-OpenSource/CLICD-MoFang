package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"clicd/internal/config"

	"golang.org/x/crypto/argon2"
)

type ApiKey struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Key            string   `json:"key,omitempty"`
	Prefix         string   `json:"prefix"`
	IPWhitelist    string   `json:"ip_whitelist"`
	CreatedAt      string   `json:"created_at"`
	LastUsed       string   `json:"last_used"`
	Scopes         []string `json:"scopes,omitempty"`
	ExpiresAt      string   `json:"expires_at,omitempty"`
	Disabled       bool     `json:"disabled,omitempty"`
	ContainerUUIDs []string `json:"container_uuids,omitempty"`
	LastUsedIP     string   `json:"last_used_ip,omitempty"`
}

type apiKeyRequest struct {
	Name           string   `json:"name"`
	IPWhitelist    string   `json:"ip_whitelist"`
	Scopes         []string `json:"scopes"`
	ExpiresAt      string   `json:"expires_at"`
	Disabled       bool     `json:"disabled"`
	ContainerUUIDs []string `json:"container_uuids"`
}

var defaultApiKeyScopes = []string{
	"dashboard:read",
	"container:read",
	"task:read",
	"image:read",
	"snapshot:read",
	"routing:read",
	"ipv6:read",
	"host:read",
}

// HandleApiKeys handles GET (list) and POST (create) for API keys
func HandleApiKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !requireScope(w, r, "apikey:read") {
			return
		}
		listApiKeys(w, r)
	case http.MethodPost:
		if !requireScope(w, r, "apikey:create") {
			return
		}
		createApiKey(w, r)
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}

// HandleApiKeyDelete handles PATCH and DELETE for a specific API key
func HandleApiKeyDelete(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPatch:
		if !requireScope(w, r, "apikey:update") {
			return
		}
		updateApiKey(w, r)
	case http.MethodDelete:
		if !requireScope(w, r, "apikey:delete") {
			return
		}
		deleteApiKey(w, r)
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}

func apiKeyIDFromPath(path string) string {
	path = strings.TrimPrefix(path, "/api/api-keys/")
	path = strings.TrimPrefix(path, "/api/v1/api-keys/")
	return strings.Trim(path, "/")
}

func listApiKeys(w http.ResponseWriter, r *http.Request) {
	keys := make([]ApiKey, 0)
	for _, k := range config.AppConfig.ApiKeys {
		keys = append(keys, apiKeyResponse(k))
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: keys})
}

func createApiKey(w http.ResponseWriter, r *http.Request) {
	var req apiKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Name is required"})
		return
	}
	if req.ExpiresAt != "" && !validApiKeyTime(req.ExpiresAt) {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid expiration date"})
		return
	}

	// Generate key: clicd_sk_ + 32 hex chars
	rawBytes := make([]byte, 16)
	if _, err := rand.Read(rawBytes); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to generate API key"})
		return
	}
	rawKey := "clicd_sk_" + hex.EncodeToString(rawBytes)

	keyHash, err := hashAPIKey(rawKey)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to store API key"})
		return
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	scopes := normalizeRequestedScopes(req.Scopes, defaultApiKeyScopes)
	key := config.ApiKeyConfig{
		ID:             generateShortID(),
		Name:           strings.TrimSpace(req.Name),
		KeyHash:        keyHash,
		Prefix:         rawKey[:13] + "...",
		IPWhitelist:    strings.TrimSpace(req.IPWhitelist),
		CreatedAt:      now,
		Scopes:         scopes,
		ExpiresAt:      strings.TrimSpace(req.ExpiresAt),
		Disabled:       req.Disabled,
		ContainerUUIDs: normalizeStringSlice(req.ContainerUUIDs),
	}
	config.AppConfig.ApiKeys = append(config.AppConfig.ApiKeys, key)
	if err := config.SaveConfig(); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to save API key"})
		return
	}
	auditRequest(r, "apikey.create", key.Name, "scopes="+strings.Join(key.Scopes, ","), true, "")

	resp := apiKeyResponse(key)
	resp.Key = rawKey
	jsonResponse(w, http.StatusCreated, APIResponse{
		Success: true,
		Message: "API key created. Save this key now - it won't be shown again.",
		Data:    resp,
	})
}

func updateApiKey(w http.ResponseWriter, r *http.Request) {
	keyID := apiKeyIDFromPath(r.URL.Path)
	if keyID == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Key ID required"})
		return
	}
	var req apiKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	if req.ExpiresAt != "" && !validApiKeyTime(req.ExpiresAt) {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid expiration date"})
		return
	}
	for i := range config.AppConfig.ApiKeys {
		if config.AppConfig.ApiKeys[i].ID != keyID {
			continue
		}
		if strings.TrimSpace(req.Name) != "" {
			config.AppConfig.ApiKeys[i].Name = strings.TrimSpace(req.Name)
		}
		config.AppConfig.ApiKeys[i].IPWhitelist = strings.TrimSpace(req.IPWhitelist)
		if len(req.Scopes) > 0 {
			config.AppConfig.ApiKeys[i].Scopes = normalizeStringSlice(req.Scopes)
		}
		config.AppConfig.ApiKeys[i].ExpiresAt = strings.TrimSpace(req.ExpiresAt)
		config.AppConfig.ApiKeys[i].Disabled = req.Disabled
		config.AppConfig.ApiKeys[i].ContainerUUIDs = normalizeStringSlice(req.ContainerUUIDs)
		if err := config.SaveConfig(); err != nil {
			jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to save API key"})
			return
		}
		auditRequest(r, "apikey.update", config.AppConfig.ApiKeys[i].Name, "scopes="+strings.Join(config.AppConfig.ApiKeys[i].Scopes, ","), true, "")
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: apiKeyResponse(config.AppConfig.ApiKeys[i])})
		return
	}
	jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "API key not found"})
}

func deleteApiKey(w http.ResponseWriter, r *http.Request) {
	keyID := apiKeyIDFromPath(r.URL.Path)
	if keyID == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Key ID required"})
		return
	}
	name := keyID
	for _, k := range config.AppConfig.ApiKeys {
		if k.ID == keyID {
			name = k.Name
			break
		}
	}
	config.DeleteApiKey(keyID)
	auditRequest(r, "apikey.delete", name, "", true, "")
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "API key deleted"})
}

func apiKeyResponse(k config.ApiKeyConfig) ApiKey {
	return ApiKey{
		ID:             k.ID,
		Name:           k.Name,
		Prefix:         k.Prefix,
		IPWhitelist:    k.IPWhitelist,
		CreatedAt:      k.CreatedAt,
		LastUsed:       k.LastUsed,
		Scopes:         normalizeApiKeyScopes(k.Scopes),
		ExpiresAt:      k.ExpiresAt,
		Disabled:       k.Disabled,
		ContainerUUIDs: k.ContainerUUIDs,
		LastUsedIP:     k.LastUsedIP,
	}
}

func generateShortID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

const (
	apiKeyHashPrefix     = "argon2id"
	apiKeyHashTime       = uint32(3)
	apiKeyHashMemory     = uint32(64 * 1024)
	apiKeyHashThreads    = uint8(1)
	apiKeyHashSaltLength = 16
	apiKeyHashKeyLength  = uint32(32)
)

// hashAPIKey stores API keys using a salted slow password-hash style function.
func hashAPIKey(key string) (string, error) {
	salt := make([]byte, apiKeyHashSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	return hashAPIKeyWithSalt(key, salt), nil
}

func hashAPIKeyWithSalt(key string, salt []byte) string {
	digest := argon2.IDKey([]byte(key), salt, apiKeyHashTime, apiKeyHashMemory, apiKeyHashThreads, apiKeyHashKeyLength)
	return fmt.Sprintf("%s$v=19$m=%d,t=%d,p=%d$%s$%s",
		apiKeyHashPrefix,
		apiKeyHashMemory,
		apiKeyHashTime,
		apiKeyHashThreads,
		hex.EncodeToString(salt),
		hex.EncodeToString(digest),
	)
}

func verifyAPIKeyHash(rawKey, storedHash string) bool {
	parts := strings.Split(storedHash, "$")
	if len(parts) != 5 || parts[0] != apiKeyHashPrefix || parts[1] != "v=19" {
		return false
	}
	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false
	}
	if memory != apiKeyHashMemory || iterations != apiKeyHashTime || threads != apiKeyHashThreads {
		return false
	}
	salt, err := hex.DecodeString(parts[3])
	if err != nil || len(salt) == 0 {
		return false
	}
	expected, err := hex.DecodeString(parts[4])
	if err != nil || len(expected) == 0 {
		return false
	}
	digest := argon2.IDKey([]byte(rawKey), salt, iterations, memory, threads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(digest, expected) == 1
}

func legacyHashKey(key string) string {
	b := make([]byte, 32)
	for i := range key {
		b[i%32] ^= key[i]
	}
	return hex.EncodeToString(b)
}

func matchApiKey(rawKey string) (idx int, needsRehash bool) {
	legacyHashed := legacyHashKey(rawKey)
	for i, k := range config.AppConfig.ApiKeys {
		if verifyAPIKeyHash(rawKey, k.KeyHash) {
			return i, false
		}
		if subtle.ConstantTimeCompare([]byte(k.KeyHash), []byte(legacyHashed)) == 1 {
			return i, true
		}
	}
	return -1, false
}

// validateApiKey checks if the given key is valid and IP is allowed.
func validateApiKey(rawKey, clientIP string) bool {
	_, ok := validateApiKeyDetails(rawKey, clientIP)
	return ok
}

func validateApiKeyDetails(rawKey, clientIP string) (*config.ApiKeyConfig, bool) {
	idx, needsRehash := matchApiKey(rawKey)
	if idx < 0 {
		return nil, false
	}
	k := &config.AppConfig.ApiKeys[idx]
	if k.Disabled || apiKeyExpired(k.ExpiresAt) {
		return nil, false
	}
	if clientIP != "" && k.IPWhitelist != "" && !isIPAllowed(clientIP, k.IPWhitelist) {
		return nil, false
	}
	if needsRehash {
		if newHash, err := hashAPIKey(rawKey); err == nil {
			config.AppConfig.ApiKeys[idx].KeyHash = newHash
			config.SaveConfig()
		}
	}
	if len(k.Scopes) == 0 {
		k.Scopes = []string{"*"}
	}
	return k, true
}

func validateApiKeyRequest(r *http.Request) (*config.ApiKeyConfig, bool) {
	apiKey := apiKeyFromRequest(r)
	if apiKey == "" {
		return nil, false
	}
	key, ok := validateApiKeyDetails(apiKey, clientIP(r))
	if !ok {
		return nil, false
	}
	updateApiKeyLastUsedForKey(key, clientIP(r))
	return key, true
}

func authContextFromAPIKey(key *config.ApiKeyConfig) AuthContext {
	actor := "api:" + key.ID
	if key.Name != "" {
		actor = "api:" + key.Name
	}
	return AuthContext{
		Type:           authTypeAPIKey,
		ApiKeyID:       key.ID,
		ApiKeyName:     key.Name,
		Actor:          actor,
		Scopes:         normalizeApiKeyScopes(key.Scopes),
		ContainerUUIDs: key.ContainerUUIDs,
	}
}

func apiKeyFromRequest(r *http.Request) string {
	if apiKey := strings.TrimSpace(r.Header.Get("X-API-Key")); apiKey != "" {
		return apiKey
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer clicd_sk_") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func isValidApiKeyRequest(r *http.Request) bool {
	_, ok := validateApiKeyRequest(r)
	return ok
}

// isIPAllowed checks if clientIP matches any entry in the whitelist
func isIPAllowed(clientIP, whitelist string) bool {
	clientIP = normalizeIPString(clientIP)
	client := net.ParseIP(clientIP)
	if client == nil {
		return false
	}
	for _, entry := range strings.Split(whitelist, "\n") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			_, network, err := net.ParseCIDR(entry)
			if err == nil && network.Contains(client) {
				return true
			}
			continue
		}
		if allowed := net.ParseIP(normalizeIPString(entry)); allowed != nil && allowed.Equal(client) {
			return true
		}
	}
	return false
}

func normalizeIPString(s string) string {
	s = strings.TrimSpace(s)
	if host, _, err := net.SplitHostPort(s); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(s, "[]")
}

func ipInCIDR(ipStr, cidr string) bool {
	ip := net.ParseIP(normalizeIPString(ipStr))
	_, network, err := net.ParseCIDR(cidr)
	return err == nil && ip != nil && network.Contains(ip)
}

// updateApiKeyLastUsed marks the key as recently used.
func updateApiKeyLastUsed(rawKey string) {
	key, ok := validateApiKeyDetails(rawKey, "")
	if !ok {
		return
	}
	updateApiKeyLastUsedForKey(key, "")
}

func updateApiKeyLastUsedForKey(key *config.ApiKeyConfig, ip string) {
	key.LastUsed = time.Now().Format("2006-01-02 15:04:05")
	if ip != "" {
		key.LastUsedIP = ip
	}
	config.SaveConfig()
}

// ApiKeyMiddleware authenticates requests via X-API-Key header or Authorization bearer.
func ApiKeyMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key, ok := validateApiKeyRequest(r)
		if !ok {
			jsonResponse(w, http.StatusUnauthorized, APIResponse{Success: false, Message: "Invalid API key or IP not in whitelist"})
			return
		}
		next(w, withAuthContext(r, authContextFromAPIKey(key)))
	}
}

func normalizeApiKeyScopes(scopes []string) []string {
	return normalizeRequestedScopes(scopes, []string{"*"})
}

func normalizeRequestedScopes(scopes []string, fallback []string) []string {
	result := normalizeStringSlice(scopes)
	if len(result) == 0 {
		return append([]string(nil), fallback...)
	}
	return result
}

func normalizeStringSlice(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func validApiKeyTime(value string) bool {
	_, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.Local)
	return err == nil
}

func apiKeyExpired(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	expiresAt, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.Local)
	return err == nil && !time.Now().Before(expiresAt)
}
