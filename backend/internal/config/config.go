package config

import (
	"crypto/rand"
	"encoding/hex"
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
	HostIP        string `json:"host_ip,omitempty"`
	Protocol      string `json:"protocol"`
	Description   string `json:"description"`
}

type PublicIPv4Assignment struct {
	Address   string `json:"address"`
	Interface string `json:"interface,omitempty"`
	PrefixLen int    `json:"prefix_len,omitempty"`
	Gateway   string `json:"gateway,omitempty"`
}

type IPv6Assignment struct {
	Address   string `json:"address"`
	PrefixLen int    `json:"prefix_len"`
	Interface string `json:"interface,omitempty"`
}

type PublicIPv6Prefix struct {
	Address   string `json:"address"`
	Prefix    string `json:"prefix,omitempty"`
	PrefixLen int    `json:"prefix_len"`
	Interface string `json:"interface,omitempty"`
	Gateway   string `json:"gateway,omitempty"`
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
	IP            string `json:"ip,omitempty"`
	UserAgent     string `json:"user_agent,omitempty"`
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
	Time      string `json:"time"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	Detail    string `json:"detail"`
	User      string `json:"user"`
	IP        string `json:"ip,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
	Success   *bool  `json:"success,omitempty"`
	Error     string `json:"error,omitempty"`
}

type VMReadinessCheck struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Container represents an LXC container configuration
type Container struct {
	ID                            int                    `json:"id"`
	UUID                          string                 `json:"uuid"`
	Name                          string                 `json:"name"`
	Virtualization                string                 `json:"virtualization,omitempty"`
	LXCName                       string                 `json:"lxc_name,omitempty"`
	KVMName                       string                 `json:"kvm_name,omitempty"`
	DiskImage                     string                 `json:"disk_image,omitempty"`
	MACAddress                    string                 `json:"mac_address,omitempty"`
	Template                      string                 `json:"template"`
	VCPU                          float64                `json:"vcpu"`
	RAMMB                         int                    `json:"ram_mb"`
	DiskGB                        int                    `json:"disk_gb"`
	NetworkBWMbps                 int                    `json:"network_bw_mbps"`
	MonthlyTrafficGB              int                    `json:"monthly_traffic_gb"`
	TrafficMode                   string                 `json:"traffic_mode"`   // "total" or "in_out"
	TrafficInGB                   int                    `json:"traffic_in_gb"`  // 0 = unlimited
	TrafficOutGB                  int                    `json:"traffic_out_gb"` // 0 = unlimited
	TrafficUsedRX                 int64                  `json:"traffic_used_rx"`
	TrafficUsedTX                 int64                  `json:"traffic_used_tx"`
	TrafficResetDate              string                 `json:"traffic_reset_date"`
	IOSpeedMBps                   int                    `json:"io_speed_mbps"`
	Status                        string                 `json:"status"`
	IP                            string                 `json:"ip"`
	PublicIPv4s                   []PublicIPv4Assignment `json:"public_ipv4s,omitempty"`
	IPv6                          string                 `json:"ipv6"`
	IPv6PrefixLen                 int                    `json:"ipv6_prefix_len"`
	IPv6Interface                 string                 `json:"ipv6_interface"`
	IPv6Addresses                 []IPv6Assignment       `json:"ipv6_addresses,omitempty"`
	VNCPort                       int                    `json:"vnc_port"`
	SSHPort                       int                    `json:"ssh_port"`
	SSHPassword                   string                 `json:"ssh_password"`
	SSHHostKey                    string                 `json:"ssh_host_key,omitempty"`
	PortMappings                  []PortMapping          `json:"port_mappings"`
	PortMappingLimit              int                    `json:"port_mapping_limit"`
	SnapshotLimit                 int                    `json:"snapshot_limit"`
	CreatedAt                     string                 `json:"created_at"`
	ExpiresAt                     string                 `json:"expires_at"`
	SnapshotScheduleEnabled       bool                   `json:"snapshot_schedule_enabled"`
	SnapshotScheduleIntervalHours int                    `json:"snapshot_schedule_interval_hours"`
	SnapshotScheduleTime          string                 `json:"snapshot_schedule_time"`
	SnapshotScheduleLastRun       string                 `json:"snapshot_schedule_last_run"`
	SnapshotScheduleNextRun       string                 `json:"snapshot_schedule_next_run"`
	SnapshotScheduleCreatedBy     string                 `json:"snapshot_schedule_created_by"`
	PolicyBlocked                 bool                   `json:"policy_blocked"`
	PolicyBlockedReason           string                 `json:"policy_blocked_reason,omitempty"`
	PolicyBlockedAt               string                 `json:"policy_blocked_at,omitempty"`
}

const (
	VirtualizationLXC = "lxc"
	VirtualizationKVM = "kvm"
)

func NormalizeVirtualization(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case VirtualizationKVM:
		return VirtualizationKVM
	default:
		return VirtualizationLXC
	}
}

func (c *Container) Runtime() string {
	return NormalizeVirtualization(c.Virtualization)
}

func (c *Container) IsKVM() bool {
	return c.Runtime() == VirtualizationKVM
}

func (c *Container) NormalizeNetworkAssignments() bool {
	changed := false
	seenIPv4 := map[string]bool{}
	filteredIPv4 := make([]PublicIPv4Assignment, 0, len(c.PublicIPv4s))
	for _, item := range c.PublicIPv4s {
		item.Address = strings.TrimSpace(item.Address)
		item.Interface = strings.TrimSpace(item.Interface)
		item.Gateway = strings.TrimSpace(item.Gateway)
		if item.Address == "" || seenIPv4[item.Address] {
			if item.Address != "" {
				changed = true
			}
			continue
		}
		seenIPv4[item.Address] = true
		filteredIPv4 = append(filteredIPv4, item)
	}
	if len(filteredIPv4) != len(c.PublicIPv4s) {
		changed = true
	}
	c.PublicIPv4s = filteredIPv4

	seenIPv6 := map[string]bool{}
	filteredIPv6 := make([]IPv6Assignment, 0, len(c.IPv6Addresses)+1)
	for _, item := range c.IPv6Addresses {
		item.Address = strings.TrimSpace(item.Address)
		item.Interface = strings.TrimSpace(item.Interface)
		if item.Address == "" || seenIPv6[item.Address] {
			if item.Address != "" {
				changed = true
			}
			continue
		}
		seenIPv6[item.Address] = true
		filteredIPv6 = append(filteredIPv6, item)
	}
	if strings.TrimSpace(c.IPv6) != "" && !seenIPv6[c.IPv6] {
		filteredIPv6 = append([]IPv6Assignment{{
			Address:   c.IPv6,
			PrefixLen: c.IPv6PrefixLen,
			Interface: c.IPv6Interface,
		}}, filteredIPv6...)
		changed = true
	}
	if len(filteredIPv6) != len(c.IPv6Addresses) {
		changed = true
	}
	c.IPv6Addresses = filteredIPv6
	if len(c.IPv6Addresses) > 0 {
		first := c.IPv6Addresses[0]
		if c.IPv6 != first.Address || c.IPv6PrefixLen != first.PrefixLen || c.IPv6Interface != first.Interface {
			c.IPv6 = first.Address
			c.IPv6PrefixLen = first.PrefixLen
			c.IPv6Interface = first.Interface
			changed = true
		}
	} else if c.IPv6 != "" || c.IPv6PrefixLen != 0 || c.IPv6Interface != "" {
		c.IPv6 = ""
		c.IPv6PrefixLen = 0
		c.IPv6Interface = ""
		changed = true
	}
	return changed
}

func (c *Container) PublicIPv4Addresses() []string {
	values := make([]string, 0, len(c.PublicIPv4s))
	for _, item := range c.PublicIPv4s {
		if item.Address != "" {
			values = append(values, item.Address)
		}
	}
	return values
}

func (c *Container) PrimaryPublicIPv4() string {
	if len(c.PublicIPv4s) == 0 {
		return ""
	}
	return c.PublicIPv4s[0].Address
}

func (c *Container) IPv6AddressStrings() []string {
	values := make([]string, 0, len(c.IPv6Addresses))
	for _, item := range c.IPv6Addresses {
		if item.Address != "" {
			values = append(values, item.Address)
		}
	}
	if len(values) == 0 && c.IPv6 != "" {
		values = append(values, c.IPv6)
	}
	return values
}

// LxcName returns the internal LXC container name (ct-{id})
func (c *Container) LxcName() string {
	if c.LXCName != "" {
		return c.LXCName
	}
	return fmt.Sprintf("ct-%d", c.ID)
}

// VirshName returns the internal libvirt domain name for KVM instances.
func (c *Container) VirshName() string {
	if c.KVMName != "" {
		return c.KVMName
	}
	return fmt.Sprintf("vm-%d", c.ID)
}

// SubUser represents a sub-user with access to specific containers
type ApiKeyConfig struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	KeyHash        string   `json:"key_hash"`
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
	Password       string   `json:"password,omitempty"`
	PassHash       string   `json:"pass_hash"`
	ContainerNames []string `json:"container_names"`
	ContainerUUIDs []string `json:"container_uuids,omitempty"`
	Token          string   `json:"-"`
	AccessCode     string   `json:"access_code"`
	CreatedAt      string   `json:"created_at"`
	TokenVersion   int      `json:"token_version"`
}

type Snapshot struct {
	ID            string `json:"id"`
	ContainerID   int    `json:"container_id"`
	ContainerName string `json:"container_name"`
	LXCName       string `json:"lxc_name"`
	CreatedAt     string `json:"created_at"`
	CreatedBy     string `json:"created_by"`
	Scheduled     bool   `json:"scheduled"`
	Path          string `json:"path"`
	SizeBytes     int64  `json:"size_bytes"`
}

const (
	SSLModeDisabled    = "disabled"
	SSLModeLetsEncrypt = "letsencrypt"
	SSLModeSelfSigned  = "self_signed"
	SSLModeUploaded    = "uploaded"
)

type SSLConfig struct {
	Enabled      bool   `json:"enabled"`
	Mode         string `json:"mode"`
	Target       string `json:"target"`
	Email        string `json:"email,omitempty"`
	CertPath     string `json:"cert_path,omitempty"`
	KeyPath      string `json:"key_path,omitempty"`
	LastIssuedAt string `json:"last_issued_at,omitempty"`
	LastError    string `json:"last_error,omitempty"`
}

// ClicdConfig is the main configuration structure
type ClicdConfig struct {
	AdminUser            string                 `json:"admin_user"`
	AdminPassHash        string                 `json:"admin_pass_hash"`
	JWTSecret            string                 `json:"jwt_secret"`
	Port                 int                    `json:"port"`
	DataDir              string                 `json:"data_dir"`
	Containers           []Container            `json:"containers"`
	NextContainerID      int                    `json:"next_container_id"`
	NextVNCPort          int                    `json:"next_vnc_port"`
	NextSSHPort          int                    `json:"next_ssh_port"`
	SetupComplete        bool                   `json:"setup_complete"`
	SubUsers             []SubUser              `json:"sub_users"`
	ApiKeys              []ApiKeyConfig         `json:"api_keys"`
	AuditLogs            []AuditLog             `json:"audit_logs"`
	Tasks                []SavedTask            `json:"tasks"`
	LoginLogs            []SavedLoginLog        `json:"login_logs"`
	EnabledImages        []string               `json:"enabled_images"`
	Snapshots            []Snapshot             `json:"snapshots"`
	PublicIPv4Pool       []PublicIPv4Assignment `json:"public_ipv4_pool"`
	PublicIPv6Prefixes   []PublicIPv6Prefix     `json:"public_ipv6_prefixes"`
	SecurityAutoShutdown bool                   `json:"security_auto_shutdown"`
	Language             string                 `json:"language"`
	SSL                  SSLConfig              `json:"ssl"`
	SSLCertificates      map[string]SSLConfig   `json:"ssl_certificates"`
}

var configPath string
var AppConfig *ClicdConfig

const DefaultSnapshotLimit = 3

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
		return nil, fmt.Errorf("failed to create data directory: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %v", err)
	}
	if err := openConfigDB(); err != nil {
		return nil, err
	}

	cfg, ok, err := loadConfigFromDB()
	if err != nil {
		return nil, err
	}
	if ok {
		AppConfig = cfg
		changed := normalizeConfigDefaults(dataDir)
		if migrateLoadedConfig() {
			changed = true
		}
		if changed {
			if err := SaveConfig(); err != nil {
				return nil, err
			}
		}
		return AppConfig, nil
	}

	legacy, ok, err := loadLegacyJSONConfig(cfgPath)
	if err != nil {
		return nil, err
	}
	if ok {
		AppConfig = legacy
		normalizeConfigDefaults(dataDir)
		migrateLoadedConfig()
		// Always save legacy JSON data into SQLite.
		if err := SaveConfig(); err != nil {
			return nil, err
		}
		return AppConfig, nil
	}

	adminUser := "admin"
	adminPass := generateRandomString(16)
	jwtSecret := generateRandomString(32)
	hash, err := bcrypt.GenerateFromPassword([]byte(adminPass), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %v", err)
	}

	AppConfig = &ClicdConfig{
		AdminUser:          adminUser,
		AdminPassHash:      string(hash),
		JWTSecret:          jwtSecret,
		Port:               8999,
		DataDir:            dataDir,
		Containers:         []Container{},
		NextContainerID:    1,
		NextVNCPort:        5900,
		NextSSHPort:        22000,
		SetupComplete:      false,
		SubUsers:           []SubUser{},
		AuditLogs:          []AuditLog{},
		Tasks:              []SavedTask{},
		LoginLogs:          []SavedLoginLog{},
		Snapshots:          []Snapshot{},
		PublicIPv4Pool:     []PublicIPv4Assignment{},
		PublicIPv6Prefixes: []PublicIPv6Prefix{},
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

func normalizeConfigDefaults(dataDir string) bool {
	changed := false
	if AppConfig.Port == 0 {
		AppConfig.Port = 8999
		changed = true
	}
	if AppConfig.NextVNCPort == 0 {
		AppConfig.NextVNCPort = 5900
		changed = true
	}
	if AppConfig.NextSSHPort == 0 {
		AppConfig.NextSSHPort = 22000
		changed = true
	}
	if AppConfig.NextContainerID == 0 {
		AppConfig.NextContainerID = 1
		changed = true
	}
	if AppConfig.DataDir == "" {
		AppConfig.DataDir = dataDir
		changed = true
	}
	if AppConfig.Containers == nil {
		AppConfig.Containers = make([]Container, 0)
		changed = true
	}
	if AppConfig.Snapshots == nil {
		AppConfig.Snapshots = make([]Snapshot, 0)
		changed = true
	}
	if AppConfig.PublicIPv4Pool == nil {
		AppConfig.PublicIPv4Pool = make([]PublicIPv4Assignment, 0)
		changed = true
	}
	if AppConfig.PublicIPv6Prefixes == nil {
		AppConfig.PublicIPv6Prefixes = make([]PublicIPv6Prefix, 0)
		changed = true
	}
	if AppConfig.SubUsers == nil {
		AppConfig.SubUsers = make([]SubUser, 0)
		changed = true
	}
	if AppConfig.ApiKeys == nil {
		AppConfig.ApiKeys = make([]ApiKeyConfig, 0)
		changed = true
	} else {
		for i := range AppConfig.ApiKeys {
			if len(AppConfig.ApiKeys[i].Scopes) == 0 {
				AppConfig.ApiKeys[i].Scopes = []string{"*"}
				changed = true
			}
		}
	}
	if AppConfig.AuditLogs == nil {
		AppConfig.AuditLogs = make([]AuditLog, 0)
		changed = true
	}
	if AppConfig.Tasks == nil {
		AppConfig.Tasks = make([]SavedTask, 0)
		changed = true
	}
	if AppConfig.LoginLogs == nil {
		AppConfig.LoginLogs = make([]SavedLoginLog, 0)
		changed = true
	}
	if AppConfig.EnabledImages == nil {
		AppConfig.EnabledImages = make([]string, 0)
		changed = true
	}
	if AppConfig.Language == "" {
		AppConfig.Language = "zh"
		changed = true
	}
	if AppConfig.Language != "zh" && AppConfig.Language != "en" {
		AppConfig.Language = "zh"
		changed = true
	}
	if normalizeSSLDefaults() {
		changed = true
	}
	return changed
}

func NormalizeLanguage(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "en", "en-us", "en_us", "english":
		return "en"
	default:
		return "zh"
	}
}

func normalizeSSLDefaults() bool {
	changed := false
	previousMode := AppConfig.SSL.Mode
	AppConfig.SSL.Mode = NormalizeSSLMode(AppConfig.SSL.Mode)
	if AppConfig.SSL.Mode != previousMode {
		changed = true
	}
	if AppConfig.SSL.Mode == SSLModeDisabled {
		if AppConfig.SSL.Enabled {
			changed = true
		}
		AppConfig.SSL.Enabled = false
	}
	if AppConfig.SSLCertificates == nil {
		AppConfig.SSLCertificates = map[string]SSLConfig{}
		changed = true
	}
	for mode, cert := range AppConfig.SSLCertificates {
		cert.Mode = NormalizeSSLMode(cert.Mode)
		if cert.Mode == SSLModeDisabled {
			delete(AppConfig.SSLCertificates, mode)
			changed = true
			continue
		}
		if AppConfig.SSLCertificates[cert.Mode] != cert {
			changed = true
		}
		AppConfig.SSLCertificates[cert.Mode] = cert
		if mode != cert.Mode {
			delete(AppConfig.SSLCertificates, mode)
			changed = true
		}
	}
	if AppConfig.SSL.Mode != SSLModeDisabled && AppConfig.SSL.CertPath != "" && AppConfig.SSL.KeyPath != "" {
		cert := AppConfig.SSL
		cert.Enabled = false
		if AppConfig.SSLCertificates[cert.Mode] != cert {
			changed = true
		}
		AppConfig.SSLCertificates[cert.Mode] = cert
	}
	return changed
}

func NormalizeSSLMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case SSLModeLetsEncrypt:
		return SSLModeLetsEncrypt
	case SSLModeSelfSigned:
		return SSLModeSelfSigned
	case SSLModeUploaded:
		return SSLModeUploaded
	default:
		return SSLModeDisabled
	}
}

func migrateLoadedConfig() bool {
	changed := ensureContainerUUIDs()
	if ensureContainerVirtualization() {
		changed = true
	}
	if ensureContainerPortMappingLimits() {
		changed = true
	}
	if ensureContainerSnapshotLimits() {
		changed = true
	}
	if ensureContainerNetworkAssignments() {
		changed = true
	}
	if ensureContainerSnapshotScheduleDefaults() {
		changed = true
	}
	if migrateSubUsers() {
		changed = true
	}
	if removeLegacyVNCMappings() {
		changed = true
	}
	return changed
}

func ensureContainerVirtualization() bool {
	changed := false
	for i := range AppConfig.Containers {
		next := NormalizeVirtualization(AppConfig.Containers[i].Virtualization)
		if AppConfig.Containers[i].Virtualization != next {
			AppConfig.Containers[i].Virtualization = next
			changed = true
		}
	}
	return changed
}

func ensureContainerSnapshotScheduleDefaults() bool {
	changed := false
	for i := range AppConfig.Containers {
		if AppConfig.Containers[i].SnapshotScheduleEnabled && AppConfig.Containers[i].SnapshotScheduleIntervalHours < 24 {
			AppConfig.Containers[i].SnapshotScheduleIntervalHours = 24
			changed = true
		}
		if AppConfig.Containers[i].SnapshotScheduleEnabled && AppConfig.Containers[i].SnapshotScheduleTime == "" {
			AppConfig.Containers[i].SnapshotScheduleTime = "03:00"
			changed = true
		}
	}
	return changed
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
		if AppConfig.Containers[i].PortMappingLimit < 0 {
			limit := len(AppConfig.Containers[i].PortMappings)
			if limit < 2 {
				limit = 2
			}
			AppConfig.Containers[i].PortMappingLimit = limit
			changed = true
		} else if AppConfig.Containers[i].PortMappingLimit == 0 && len(AppConfig.Containers[i].PortMappings) > 0 {
			AppConfig.Containers[i].PortMappingLimit = len(AppConfig.Containers[i].PortMappings)
			changed = true
		}
	}
	return changed
}

func ensureContainerSnapshotLimits() bool {
	changed := false
	for i := range AppConfig.Containers {
		if AppConfig.Containers[i].SnapshotLimit <= 0 {
			AppConfig.Containers[i].SnapshotLimit = DefaultSnapshotLimit
			changed = true
		}
	}
	return changed
}

func ensureContainerNetworkAssignments() bool {
	changed := false
	for i := range AppConfig.Containers {
		if AppConfig.Containers[i].NormalizeNetworkAssignments() {
			changed = true
		}
	}
	return changed
}

func migrateSubUsers() bool {
	changed := false
	for i := range AppConfig.SubUsers {
		su := &AppConfig.SubUsers[i]
		if su.PassHash == "" && su.Password != "" {
			if hash, err := bcrypt.GenerateFromPassword([]byte(su.Password), bcrypt.DefaultCost); err == nil {
				su.PassHash = string(hash)
				changed = true
			}
		}
		if su.Token != "" {
			su.Token = ""
			changed = true
		}
		if len(su.ContainerUUIDs) == 0 && len(su.ContainerNames) > 0 {
			for _, name := range su.ContainerNames {
				if c := FindContainerByName(name); c != nil && c.UUID != "" {
					su.ContainerUUIDs = appendUniqueString(su.ContainerUUIDs, c.UUID)
				}
			}
			if len(su.ContainerUUIDs) > 0 {
				changed = true
			}
		}
	}
	return changed
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func NormalizeSnapshotLimit(limit int) int {
	if limit <= 0 {
		return DefaultSnapshotLimit
	}
	return limit
}

func ContainerSnapshotLimit(c *Container) int {
	if c == nil {
		return DefaultSnapshotLimit
	}
	return NormalizeSnapshotLimit(c.SnapshotLimit)
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
	return saveConfigToDB()
}

// AddContainer adds a container to the config
func AddContainer(c Container) {
	if c.UUID == "" {
		c.UUID = NewContainerUUID()
	}
	c.Virtualization = NormalizeVirtualization(c.Virtualization)
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
			removeSubUserContainerAccess(c.Name, c.UUID)
			removeContainerSnapshotMetadata(id)
			// Clear snapshot schedule for this container
			clearContainerSnapshotSchedule(&AppConfig.Containers[i])
			AppConfig.Containers = append(AppConfig.Containers[:i], AppConfig.Containers[i+1:]...)
			SaveConfig()
			return true
		}
	}
	return false
}

func clearContainerSnapshotSchedule(c *Container) {
	c.SnapshotScheduleEnabled = false
	c.SnapshotScheduleIntervalHours = 0
	c.SnapshotScheduleTime = ""
	c.SnapshotScheduleLastRun = ""
	c.SnapshotScheduleNextRun = ""
	c.SnapshotScheduleCreatedBy = ""
}

func AddSnapshot(snapshot Snapshot) {
	AppConfig.Snapshots = append(AppConfig.Snapshots, snapshot)
	SaveConfig()
}

func FindSnapshot(id string) *Snapshot {
	for i := range AppConfig.Snapshots {
		if AppConfig.Snapshots[i].ID == id {
			return &AppConfig.Snapshots[i]
		}
	}
	return nil
}

func RemoveSnapshot(id string) bool {
	for i := range AppConfig.Snapshots {
		if AppConfig.Snapshots[i].ID == id {
			AppConfig.Snapshots = append(AppConfig.Snapshots[:i], AppConfig.Snapshots[i+1:]...)
			SaveConfig()
			return true
		}
	}
	return false
}

func ContainerSnapshots(containerID int) []Snapshot {
	result := make([]Snapshot, 0)
	for _, snapshot := range AppConfig.Snapshots {
		if snapshot.ContainerID == containerID {
			result = append(result, snapshot)
		}
	}
	return result
}

func removeContainerSnapshotMetadata(containerID int) {
	filtered := make([]Snapshot, 0, len(AppConfig.Snapshots))
	for _, snapshot := range AppConfig.Snapshots {
		if snapshot.ContainerID != containerID {
			filtered = append(filtered, snapshot)
		}
	}
	AppConfig.Snapshots = filtered
}

func RemoveSubUserContainerAccess(containerName string, containerUUID string) {
	removeSubUserContainerAccess(containerName, containerUUID)
	SaveConfig()
}

func removeSubUserContainerAccess(containerName string, containerUUID string) {
	if containerName == "" && containerUUID == "" || len(AppConfig.SubUsers) == 0 {
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
		filteredUUIDs := make([]string, 0, len(su.ContainerUUIDs))
		for _, uuid := range su.ContainerUUIDs {
			if uuid != containerUUID {
				filteredUUIDs = append(filteredUUIDs, uuid)
			}
		}
		if len(filteredNames) == 0 && len(filteredUUIDs) == 0 {
			continue
		}
		su.ContainerNames = filteredNames
		su.ContainerUUIDs = filteredUUIDs
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

func SetContainerPolicyBlock(id int, blocked bool, reason string) {
	c := FindContainer(id)
	if c == nil {
		return
	}
	c.PolicyBlocked = blocked
	if blocked {
		c.PolicyBlockedReason = reason
		c.PolicyBlockedAt = time.Now().Format("2006-01-02 15:04:05")
	} else {
		c.PolicyBlockedReason = ""
		c.PolicyBlockedAt = ""
	}
	SaveConfig()
}

// UpdateVNC refreshes all container statuses
func UpdateVNC(containers []Container) {
	AppConfig.Containers = containers
	SaveConfig()
}

// AllocateSSHPort allocates a new SSH port, skipping ports already used by any container
func AllocateSSHPort() int {
	used := collectAllHostPorts()
	port := AppConfig.NextSSHPort
	for used[port] {
		port++
	}
	AppConfig.NextSSHPort = port + 1
	SaveConfig()
	return port
}

// collectAllHostPorts collects all host ports used by any container (LXC + KVM)
func collectAllHostPorts() map[int]bool {
	used := map[int]bool{}
	for _, c := range AppConfig.Containers {
		for _, pm := range c.PortMappings {
			used[pm.HostPort] = true
		}
	}
	return used
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

func AddAuditLogFull(action, target, detail, user, ip, userAgent string, success bool, errMsg string) {
	s := success
	log := AuditLog{
		Time:      time.Now().Format("2006-01-02 15:04:05"),
		Action:    action,
		Target:    target,
		Detail:    detail,
		User:      user,
		IP:        ip,
		UserAgent: userAgent,
		Success:   &s,
		Error:     errMsg,
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
		if c.IsKVM() {
			if c.DiskImage == "" {
				valid = append(valid, c)
				continue
			}
			if _, err := os.Stat(c.DiskImage); os.IsNotExist(err) {
				fmt.Printf("Cleaning stale KVM config: %s (disk image not found)\n", c.VirshName())
				changed = true
				continue
			}
			valid = append(valid, c)
			continue
		}
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
