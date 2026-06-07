package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"clicd/internal/config"

	"golang.org/x/crypto/argon2"
)

type ApiKey struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Key         string `json:"key,omitempty"`
	Prefix      string `json:"prefix"`
	IPWhitelist string `json:"ip_whitelist"`
	CreatedAt   string `json:"created_at"`
	LastUsed    string `json:"last_used"`
}

// HandleApiKeys handles GET (list) and POST (create) for API keys
func HandleApiKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listApiKeys(w, r)
	case http.MethodPost:
		createApiKey(w, r)
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}

// HandleApiKeyDelete handles DELETE for a specific API key
func HandleApiKeyDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	keyID := strings.TrimPrefix(r.URL.Path, "/api/api-keys/")
	if keyID == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Key ID required"})
		return
	}
	config.DeleteApiKey(keyID)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "API key deleted"})
}

func listApiKeys(w http.ResponseWriter, r *http.Request) {
	keys := make([]ApiKey, 0)
	for _, k := range config.AppConfig.ApiKeys {
		keys = append(keys, ApiKey{
			ID:          k.ID,
			Name:        k.Name,
			Prefix:      k.Prefix,
			IPWhitelist: k.IPWhitelist,
			CreatedAt:   k.CreatedAt,
			LastUsed:    k.LastUsed,
		})
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: keys})
}

func createApiKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		IPWhitelist string `json:"ip_whitelist"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Name is required"})
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
	key := config.ApiKeyConfig{
		ID:          generateShortID(),
		Name:        req.Name,
		KeyHash:     keyHash,
		Prefix:      rawKey[:13] + "...",
		IPWhitelist: strings.TrimSpace(req.IPWhitelist),
		CreatedAt:   now,
	}
	config.AppConfig.ApiKeys = append(config.AppConfig.ApiKeys, key)
	config.SaveConfig()

	jsonResponse(w, http.StatusCreated, APIResponse{
		Success: true,
		Message: "API key created. Save this key now - it won't be shown again.",
		Data: ApiKey{
			ID:          key.ID,
			Name:        key.Name,
			Key:         rawKey,
			Prefix:      key.Prefix,
			IPWhitelist: key.IPWhitelist,
			CreatedAt:   key.CreatedAt,
		},
	})
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
	idx, needsRehash := matchApiKey(rawKey)
	if idx < 0 {
		return false
	}
	k := config.AppConfig.ApiKeys[idx]
	if k.IPWhitelist != "" && !isIPAllowed(clientIP, k.IPWhitelist) {
		return false
	}
	if needsRehash {
		if newHash, err := hashAPIKey(rawKey); err == nil {
			config.AppConfig.ApiKeys[idx].KeyHash = newHash
			config.SaveConfig()
		}
	}
	return true
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
	apiKey := apiKeyFromRequest(r)
	if apiKey == "" {
		return false
	}
	if !validateApiKey(apiKey, clientIP(r)) {
		return false
	}
	updateApiKeyLastUsed(apiKey)
	return true
}

// isIPAllowed checks if clientIP matches any entry in the whitelist
func isIPAllowed(clientIP, whitelist string) bool {
	clientIP = strings.TrimSpace(clientIP)
	// Strip port if present
	if idx := strings.LastIndex(clientIP, ":"); idx > strings.LastIndex(clientIP, "]") {
		clientIP = clientIP[:idx]
	}
	for _, entry := range strings.Split(whitelist, "\n") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			// CIDR match
			if ipInCIDR(clientIP, entry) {
				return true
			}
		} else if entry == clientIP {
			return true
		}
	}
	return false
}

func ipInCIDR(ipStr, cidr string) bool {
	parts := strings.Split(cidr, "/")
	if len(parts) != 2 {
		return false
	}
	// Simple prefix match for IPv4
	ip := netParseIP(ipStr)
	cidrIP := netParseIP(parts[0])
	if ip == nil || cidrIP == nil {
		return false
	}
	bits, err := strconv.Atoi(parts[1])
	if err != nil || bits < 0 || bits > 32 {
		return false
	}
	mask := uint32(0xFFFFFFFF) << (32 - bits)
	ipVal := ip4ToUint32(ip)
	cidrVal := ip4ToUint32(cidrIP)
	return (ipVal & mask) == (cidrVal & mask)
}

func netParseIP(s string) net.IP {
	s = strings.TrimSpace(s)
	if idx := strings.LastIndex(s, ":"); idx > strings.LastIndex(s, "]") {
		s = s[:idx]
	}
	return net.ParseIP(s)
}

func ip4ToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	if ip == nil {
		return 0
	}
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

// updateApiKeyLastUsed marks the key as recently used.
func updateApiKeyLastUsed(rawKey string) {
	idx, _ := matchApiKey(rawKey)
	if idx < 0 {
		return
	}
	config.AppConfig.ApiKeys[idx].LastUsed = time.Now().Format("2006-01-02 15:04:05")
	config.SaveConfig()
}

// ApiKeyMiddleware authenticates requests via X-API-Key header or Authorization bearer.
func ApiKeyMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiKey := apiKeyFromRequest(r)
		if apiKey == "" || !validateApiKey(apiKey, clientIP(r)) {
			jsonResponse(w, http.StatusUnauthorized, APIResponse{Success: false, Message: "Invalid API key or IP not in whitelist"})
			return
		}

		updateApiKeyLastUsed(apiKey)
		next(w, r)
	}
}
