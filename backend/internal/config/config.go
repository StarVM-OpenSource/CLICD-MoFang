package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// PortMapping represents a port mapping rule
type PortMapping struct {
	ContainerPort int    `json:"container_port"`
	HostPort      int    `json:"host_port"`
	Protocol      string `json:"protocol"`
	Description   string `json:"description"`
}

// SavedTask for persisting task queue across restarts
type SavedTask struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	ContainerID   int    `json:"container_id"`
	ContainerName string `json:"container_name"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
	CreatedAt     string `json:"created_at"`
	TemplateID    string `json:"template_id,omitempty"`
	Config        string `json:"config,omitempty"`
	User          string `json:"user,omitempty"`
}

// SavedLoginLog for persisting login logs
type SavedLoginLog struct {
	Time      string `json:"time"`
	Username  string `json:"username"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	Success   bool   `json:"success"`
}

// AuditLog represents an operation log entry
type AuditLog struct {
	Time   string `json:"time"`
	Action string `json:"action"`
	Target string `json:"target"`
	Detail string `json:"detail"`
	User   string `json:"user"`
}

// OversellConfig controls host-level overselling behavior
type OversellConfig struct {
	CPUOvercommit  int  `json:"cpu_overcommit"`  // multiplier, e.g. 4 means 4x oversell
	RAMOvercommit  int  `json:"ram_overcommit"`  // multiplier
	DiskOvercommit int  `json:"disk_overcommit"` // multiplier
	KSMEnabled     bool `json:"ksm_enabled"`     // kernel same-page merging
	Swappiness     int  `json:"swappiness"`      // 0-100, lower = less swap
}

// Container represents an LXC container configuration
type Container struct {
	ID               int           `json:"id"`
	UUID             string        `json:"uuid"`
	Name             string        `json:"name"`
	LXCName          string        `json:"lxc_name,omitempty"`
	Template         string        `json:"template"`
	VCPU             float64       `json:"vcpu"`
	RAMMB            int           `json:"ram_mb"`
	DiskGB           int           `json:"disk_gb"`
	NetworkBWMbps    int           `json:"network_bw_mbps"`
	MonthlyTrafficGB int           `json:"monthly_traffic_gb"`
	TrafficMode      string        `json:"traffic_mode"`   // "total" or "in_out"
	TrafficInGB      int           `json:"traffic_in_gb"`  // 0 = unlimited
	TrafficOutGB     int           `json:"traffic_out_gb"` // 0 = unlimited
	TrafficUsedRX    int64         `json:"traffic_used_rx"`
	TrafficUsedTX    int64         `json:"traffic_used_tx"`
	TrafficResetDate string        `json:"traffic_reset_date"`
	IOSpeedMBps      int           `json:"io_speed_mbps"`
	Status           string        `json:"status"`
	IP               string        `json:"ip"`
	IPv6             string        `json:"ipv6"`
	IPv6PrefixLen    int           `json:"ipv6_prefix_len"`
	IPv6Interface    string        `json:"ipv6_interface"`
	VNCPort          int           `json:"vnc_port"`
	SSHPort          int           `json:"ssh_port"`
	SSHPassword      string        `json:"ssh_password"`
	PortMappings     []PortMapping `json:"port_mappings"`
	PortMappingLimit int           `json:"port_mapping_limit"`
	CreatedAt        string        `json:"created_at"`
	ExpiresAt        string        `json:"expires_at"`
}

// LxcName returns the internal LXC container name (ct-{id})
func (c *Container) LxcName() string {
	if c.LXCName != "" {
		return c.LXCName
	}
	return fmt.Sprintf("ct-%d", c.ID)
}

// SubUser represents a sub-user with access to specific containers
type ApiKeyConfig struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	KeyHash     string `json:"key_hash"`
	Prefix      string `json:"prefix"`
	IPWhitelist string `json:"ip_whitelist"`
	CreatedAt   string `json:"created_at"`
	LastUsed    string `json:"last_used"`
}

// DeleteApiKey removes an API key by ID
func DeleteApiKey(id string) {
	filtered := make([]ApiKeyConfig, 0, len(AppConfig.ApiKeys))
	for _, k := range AppConfig.ApiKeys {
		if k.ID != id {
			filtered = append(filtered, k)
		}
	}
	AppConfig.ApiKeys = filtered
	SaveConfig()
}

type SubUser struct {
	ID             string   `json:"id"`
	Username       string   `json:"username"`
	Password       string   `json:"password"` // plaintext for display
	PassHash       string   `json:"pass_hash"`
	ContainerNames []string `json:"container_names"`
	Token          string   `json:"token"`
	AccessCode     string   `json:"access_code"`
	CreatedAt      string   `json:"created_at"`
}

// ClicdConfig is the main configuration structure
type ClicdConfig struct {
	AdminUser       string          `json:"admin_user"`
	AdminPassHash   string          `json:"admin_pass_hash"`
	JWTSecret       string          `json:"jwt_secret"`
	Port            int             `json:"port"`
	DataDir         string          `json:"data_dir"`
	Containers      []Container     `json:"containers"`
	NextContainerID int             `json:"next_container_id"`
	NextVNCPort     int             `json:"next_vnc_port"`
	NextSSHPort     int             `json:"next_ssh_port"`
	SetupComplete   bool            `json:"setup_complete"`
	Oversell        OversellConfig  `json:"oversell"`
	SubUsers        []SubUser       `json:"sub_users"`
	ApiKeys         []ApiKeyConfig  `json:"api_keys"`
	AuditLogs       []AuditLog      `json:"audit_logs"`
	Tasks           []SavedTask     `json:"tasks"`
	LoginLogs       []SavedLoginLog `json:"login_logs"`
	EnabledImages   []string        `json:"enabled_images"`
}

var configPath string
var AppConfig *ClicdConfig

func getConfigPath() string {
	if configPath != "" {
		return configPath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/root"
	}
	return filepath.Join(home, ".clicd", "config.json")
}

func SetConfigPath(path string) {
	configPath = path
}

func getDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/root"
	}
	return filepath.Join(home, ".clicd")
}

func generateRandomString(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return hex.EncodeToString(b)[:length]
}

func generateUUIDString() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return generateRandomString(32)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// NewContainerUUID returns a UUID that is unique within the current config.
func NewContainerUUID() string {
	for {
		uuid := generateUUIDString()
		if FindContainerByUUID(uuid) == nil {
			return uuid
		}
	}
}

// InitConfig initializes or loads the configuration
func InitConfig() (*ClicdConfig, error) {
	cfgPath := getConfigPath()
	dataDir := getDataDir()

	if err := os.MkdirAll(filepath.Dir(cfgPath), 0700); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %v", err)
	}

	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		// First run: generate new config
		adminUser := "admin"
		adminPass := generateRandomString(16)
		jwtSecret := generateRandomString(32)
		hash, err := bcrypt.GenerateFromPassword([]byte(adminPass), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("failed to hash password: %v", err)
		}

		AppConfig = &ClicdConfig{
			AdminUser:       adminUser,
			AdminPassHash:   string(hash),
			JWTSecret:       jwtSecret,
			Port:            8999,
			DataDir:         dataDir,
			Containers:      []Container{},
			NextContainerID: 1,
			NextVNCPort:     5900,
			NextSSHPort:     22000,
			SetupComplete:   false,
			SubUsers:        []SubUser{},
			AuditLogs:       []AuditLog{},
			Tasks:           []SavedTask{},
			LoginLogs:       []SavedLoginLog{},
			Oversell: OversellConfig{
				CPUOvercommit:  4,
				RAMOvercommit:  1,
				DiskOvercommit: 2,
				KSMEnabled:     true,
				Swappiness:     10,
			},
		}

		if err := SaveConfig(); err != nil {
			return nil, err
		}

		fmt.Println("\n========================================")
		fmt.Println("  CLICD - LXC Container Manager")
		fmt.Println("========================================")
		fmt.Printf("  Username: %s\n", adminUser)
		fmt.Printf("  Password: %s\n", adminPass)
		fmt.Println("========================================")
		fmt.Println("  Please save these credentials!")
		fmt.Println("  Web Interface: http://0.0.0.0:8999")
		fmt.Println("========================================")
		fmt.Println()

		return AppConfig, nil
	}

	// Load existing config
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %v", err)
	}

	AppConfig = &ClicdConfig{}
	if err := json.Unmarshal(data, AppConfig); err != nil {
		return nil, fmt.Errorf("failed to parse config: %v", err)
	}

	if AppConfig.Port == 0 {
		AppConfig.Port = 8999
	}
	if AppConfig.NextVNCPort == 0 {
		AppConfig.NextVNCPort = 5900
	}
	if AppConfig.NextSSHPort == 0 {
		AppConfig.NextSSHPort = 22000
	}
	if AppConfig.NextContainerID == 0 {
		AppConfig.NextContainerID = 1
	}
	if AppConfig.DataDir == "" {
		AppConfig.DataDir = dataDir
	}
	if AppConfig.Containers == nil {
		AppConfig.Containers = make([]Container, 0)
	}
	changed := ensureContainerUUIDs()
	if ensureContainerPortMappingLimits() {
		changed = true
	}
	if removeLegacyVNCMappings() {
		changed = true
	}
	if changed {
		if err := SaveConfig(); err != nil {
			return nil, err
		}
	}

	return AppConfig, nil
}

func ensureContainerUUIDs() bool {
	changed := false
	used := make(map[string]bool)
	for i := range AppConfig.Containers {
		uuid := AppConfig.Containers[i].UUID
		if uuid == "" || used[uuid] {
			for {
				uuid = generateUUIDString()
				if !used[uuid] {
					break
				}
			}
			AppConfig.Containers[i].UUID = uuid
			changed = true
		}
		used[uuid] = true
	}
	return changed
}

func ensureContainerPortMappingLimits() bool {
	changed := false
	for i := range AppConfig.Containers {
		if AppConfig.Containers[i].PortMappingLimit <= 0 {
			limit := len(AppConfig.Containers[i].PortMappings)
			if limit < 2 {
				limit = 2
			}
			AppConfig.Containers[i].PortMappingLimit = limit
			changed = true
		}
	}
	return changed
}

func removeLegacyVNCMappings() bool {
	changed := false
	for i := range AppConfig.Containers {
		mappings := AppConfig.Containers[i].PortMappings
		if len(mappings) == 0 {
			continue
		}

		filtered := mappings[:0]
		for _, pm := range mappings {
			isLegacyVNC := strings.EqualFold(pm.Description, "VNC") || pm.ContainerPort == 5901
			if isLegacyVNC {
				changed = true
				continue
			}
			filtered = append(filtered, pm)
		}
		AppConfig.Containers[i].PortMappings = filtered
	}
	return changed
}

// SaveConfig saves configuration to disk
func SaveConfig() error {
	data, err := json.MarshalIndent(AppConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}
	return os.WriteFile(getConfigPath(), data, 0600)
}

// AddContainer adds a container to the config
func AddContainer(c Container) {
	if c.UUID == "" {
		c.UUID = NewContainerUUID()
	}
	AppConfig.Containers = append(AppConfig.Containers, c)
	SaveConfig()
}

// AllocateContainerID allocates a new container ID
func AllocateContainerID() int {
	id := AppConfig.NextContainerID
	AppConfig.NextContainerID++
	SaveConfig()
	return id
}

// RemoveContainer removes a container from config by ID
func RemoveContainer(id int) bool {
	for i, c := range AppConfig.Containers {
		if c.ID == id {
			removeSubUserContainerAccess(c.Name)
			AppConfig.Containers = append(AppConfig.Containers[:i], AppConfig.Containers[i+1:]...)
			SaveConfig()
			return true
		}
	}
	return false
}

func removeSubUserContainerAccess(containerName string) {
	if containerName == "" || len(AppConfig.SubUsers) == 0 {
		return
	}
	filteredUsers := make([]SubUser, 0, len(AppConfig.SubUsers))
	for _, su := range AppConfig.SubUsers {
		filteredNames := make([]string, 0, len(su.ContainerNames))
		for _, name := range su.ContainerNames {
			if name != containerName {
				filteredNames = append(filteredNames, name)
			}
		}
		if len(filteredNames) == 0 {
			continue
		}
		su.ContainerNames = filteredNames
		filteredUsers = append(filteredUsers, su)
	}
	AppConfig.SubUsers = filteredUsers
}

// FindContainer finds a container by ID
func FindContainer(id int) *Container {
	for i, c := range AppConfig.Containers {
		if c.ID == id {
			return &AppConfig.Containers[i]
		}
	}
	return nil
}

// FindContainerByUUID finds a container by UUID.
func FindContainerByUUID(uuid string) *Container {
	for i, c := range AppConfig.Containers {
		if c.UUID == uuid {
			return &AppConfig.Containers[i]
		}
	}
	return nil
}

// FindContainerByName finds a container by name
func FindContainerByName(name string) *Container {
	for i, c := range AppConfig.Containers {
		if c.Name == name {
			return &AppConfig.Containers[i]
		}
	}
	return nil
}

// FindContainerByIdentifier finds a container by ID, UUID, or name.
func FindContainerByIdentifier(identifier string) *Container {
	if id, err := strconv.Atoi(identifier); err == nil {
		if c := FindContainer(id); c != nil {
			return c
		}
	}
	if c := FindContainerByUUID(identifier); c != nil {
		return c
	}
	return FindContainerByName(identifier)
}

// UpdateContainerStatus updates container status by ID
func UpdateContainerStatus(id int, status string) {
	c := FindContainer(id)
	if c != nil {
		c.Status = status
		SaveConfig()
	}
}

// UpdateVNC refreshes all container statuses
func UpdateVNC(containers []Container) {
	AppConfig.Containers = containers
	SaveConfig()
}

// AllocateSSHPort allocates a new SSH port
func AllocateSSHPort() int {
	port := AppConfig.NextSSHPort
	AppConfig.NextSSHPort++
	SaveConfig()
	return port
}

// IsValidContainerName checks if container name is valid (no duplicate check needed, ID is primary key)
func IsValidContainerName(name string) bool {
	return IsValidContainerNameSyntax(name)
}

// IsValidContainerNameSyntax checks only the container name format.
func IsValidContainerNameSyntax(name string) bool {
	if len(name) == 0 || len(name) > 63 {
		return false
	}
	// Only allow alphanumeric, hyphens, underscores
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// AddAuditLog adds an audit log entry
func AddAuditLog(action, target, detail, user string) {
	log := AuditLog{
		Time:   time.Now().Format("2006-01-02 15:04:05"),
		Action: action,
		Target: target,
		Detail: detail,
		User:   user,
	}
	AppConfig.AuditLogs = append(AppConfig.AuditLogs, log)
	if len(AppConfig.AuditLogs) > 500 {
		AppConfig.AuditLogs = AppConfig.AuditLogs[len(AppConfig.AuditLogs)-500:]
	}
	SaveConfig()
}

// SaveTasks persists the task queue to config
func SaveTasks(tasks []SavedTask) {
	AppConfig.Tasks = tasks
	SaveConfig()
}

// AddLoginLog persists a login log entry
func AddLoginLog(username, ip, userAgent string, success bool) {
	log := SavedLoginLog{
		Time:      time.Now().Format("2006-01-02 15:04:05 MST"),
		Username:  username,
		IP:        ip,
		UserAgent: userAgent,
		Success:   success,
	}
	AppConfig.LoginLogs = append(AppConfig.LoginLogs, log)
	if len(AppConfig.LoginLogs) > 200 {
		AppConfig.LoginLogs = AppConfig.LoginLogs[len(AppConfig.LoginLogs)-200:]
	}
	SaveConfig()
}

// ResetAdminPassword resets the admin password from CLI
func ResetAdminPassword(newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	AppConfig.AdminPassHash = string(hash)
	return SaveConfig()
}

// CleanStaleContainers removes containers from config if their LXC directory doesn't exist
func CleanStaleContainers() {
	valid := make([]Container, 0)
	changed := false
	for _, c := range AppConfig.Containers {
		lxcDir := "/var/lib/lxc/" + c.LxcName()
		if _, err := os.Stat(lxcDir); os.IsNotExist(err) {
			fmt.Printf("Cleaning stale container config: %s (LXC dir not found)\n", c.LxcName())
			changed = true
			continue
		}
		valid = append(valid, c)
	}
	if changed {
		AppConfig.Containers = valid
		SaveConfig()
	}
}
