package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"clicd/internal/config"
	"clicd/internal/lxc"
	"clicd/internal/version"
)

var lxcManager = lxc.NewManager()

// HandleContainers handles container list and creation
func HandleContainers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !requireScope(w, r, "container:read") {
			return
		}
		listContainers(w, r)
	case http.MethodPost:
		if !requireScope(w, r, "container:create") {
			return
		}
		if isAccessRestrictedRequest(r) {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Container-bound API keys cannot create containers"})
			return
		}
		createContainer(w, r)
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}

// HandleContainerListAlias supports legacy integrations that call
// /api/containers/list or /api/v1/containers/list.
func HandleContainerListAlias(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "container:read") {
		return
	}
	listContainers(w, r)
}

// HandleSingleContainer handles individual container operations by ID or name: /api/containers/{id-or-name}/...
func HandleSingleContainer(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/containers/")
	path = strings.TrimPrefix(path, "/api/containers/")
	parts := strings.SplitN(path, "/", 2)
	c := containerByIdentifier(parts[0])
	id := 0
	if c != nil {
		id = c.ID
	}
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	// Snapshot delete/restore operations: allow even if the container was deleted
	isSnapshotDelete := strings.HasPrefix(action, "snapshots/") && r.Method == http.MethodDelete
	isSnapshotRestore := strings.HasPrefix(action, "snapshots/") && strings.HasSuffix(action, "/restore") && r.Method == http.MethodPost
	isSnapshotAction := isSnapshotDelete || isSnapshotRestore
	if !isSnapshotAction && c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	if !isSnapshotAction && !isContainerAllowedForRequest(r, parts[0]) {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Access denied to this container"})
		return
	}
	if isSnapshotAction && id == 0 {
		// For orphaned snapshots, resolve containerID from the snapshot itself
		snapshotID := strings.TrimPrefix(action, "snapshots/")
		snapshotID = strings.TrimSuffix(snapshotID, "/restore")
		snapshot := config.FindSnapshot(snapshotID)
		if snapshot == nil {
			jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Snapshot not found"})
			return
		}
		id = snapshot.ContainerID
	}
	if isSnapshotAction {
		if c := config.FindContainer(id); c != nil && !isContainerAllowedForRequest(r, c.UUID) {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Access denied to this container"})
			return
		}
	}

	switch {
	case action == "start" && r.Method == http.MethodPost:
		if !requireScope(w, r, "container:power") {
			return
		}
		HandleSingleTaskAction(w, r, id, "start")
	case action == "stop" && r.Method == http.MethodPost:
		if !requireScope(w, r, "container:power") {
			return
		}
		HandleSingleTaskAction(w, r, id, "stop")
	case action == "restart" && r.Method == http.MethodPost:
		if !requireScope(w, r, "container:power") {
			return
		}
		HandleSingleTaskAction(w, r, id, "restart")
	case action == "reinstall" && r.Method == http.MethodPost:
		if !requireScope(w, r, "container:reinstall") {
			return
		}
		HandleSingleTaskAction(w, r, id, "reinstall")
	case action == "delete" && r.Method == http.MethodDelete:
		if !requireScope(w, r, "container:delete") {
			return
		}
		HandleSingleTaskAction(w, r, id, "delete")
	case action == "reset-password" && r.Method == http.MethodPost:
		if !requireScope(w, r, "container:password") {
			return
		}
		resetSSHPassword(w, r, id)
	case action == "usage" && r.Method == http.MethodGet:
		if !requireScope(w, r, "container:read") {
			return
		}
		getUsage(w, r, id)
	case action == "traffic" && r.Method == http.MethodGet:
		if !requireScope(w, r, "container:read") {
			return
		}
		getTraffic(w, r, id)
	case action == "traffic-reset" && r.Method == http.MethodPost:
		if !requireScope(w, r, "container:traffic") {
			return
		}
		resetTraffic(w, r, id)
	case action == "traffic-limit" && r.Method == http.MethodPut:
		if !requireScope(w, r, "container:traffic") {
			return
		}
		updateTrafficLimit(w, r, id)
	case action == "resource-limit" && r.Method == http.MethodPut:
		if !requireScope(w, r, "container:resize") {
			return
		}
		updateResourceLimit(w, r, id)
	case action == "random-port" && r.Method == http.MethodGet:
		if !requireScope(w, r, "container:network") {
			return
		}
		getRandomPort(w, r, id)
	case action == "expiry" && r.Method == http.MethodPut:
		if !requireScope(w, r, "container:resize") {
			return
		}
		updateExpiry(w, r, id)
	case action == "ipv6" && r.Method == http.MethodPost:
		if !requireScope(w, r, "ipv6:assign") {
			return
		}
		assignIPv6(w, r, id)
	case action == "snapshots" || strings.HasPrefix(action, "snapshots/"):
		handleContainerSnapshots(w, r, id, action)
	case action == "port-mappings" && r.Method == http.MethodPost:
		if !requireScope(w, r, "container:network") {
			return
		}
		addPortMapping(w, r, id)
	case strings.HasPrefix(action, "port-mappings/") && r.Method == http.MethodPut:
		if !requireScope(w, r, "container:network") {
			return
		}
		updatePortMapping(w, r, id, strings.TrimPrefix(action, "port-mappings/"))
	case strings.HasPrefix(action, "port-mappings/") && r.Method == http.MethodDelete:
		if !requireScope(w, r, "container:network") {
			return
		}
		deletePortMapping(w, r, id, strings.TrimPrefix(action, "port-mappings/"))
	case r.Method == http.MethodGet:
		if !requireScope(w, r, "container:read") {
			return
		}
		getContainer(w, r, id)
	default:
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Action not found"})
	}
}

func listContainers(w http.ResponseWriter, r *http.Request) {
	containers, _ := listByRuntime()
	containers = filterContainersForRequest(r, containers)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: containers})
}

func createContainer(w http.ResponseWriter, r *http.Request) {
	var cfg lxc.ContainerConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	if cfg.Name == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Container name is required"})
		return
	}
	cfg.Virtualization = runtimeFromRequest(cfg.Virtualization)
	if cfg.TemplateID == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Template is required"})
		return
	}
	if !isImageEnabledAndDownloaded(cfg.TemplateID, cfg.Virtualization) {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Template is not enabled or downloaded"})
		return
	}
	if cfg.VCPU <= 0 {
		cfg.VCPU = 1
	}
	if cfg.RAMMB < 128 {
		cfg.RAMMB = 512
	}
	if cfg.DiskGB < 1 {
		cfg.DiskGB = 5
	}
	if cfg.PortMappingCount < 0 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Port mapping count cannot be negative"})
		return
	}
	if cfg.WantsNAT() && cfg.PortMappingCount < 2 {
		cfg.PortMappingCount = 2
	} else if !cfg.WantsNAT() {
		cfg.PortMappingCount = 0
		cfg.ExtraPorts = nil
	}
	if cfg.PortMappingCount > 64 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Port mapping count cannot exceed 64"})
		return
	}
	if cfg.IPv4Count < 0 || cfg.IPv6Count < 0 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "IP address count cannot be negative"})
		return
	}
	if cfg.IPv4Count > 64 || cfg.IPv6Count > 64 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "IP address count cannot exceed 64"})
		return
	}
	if !cfg.AssignIPv4 && len(cfg.PublicIPv4s) == 0 {
		cfg.IPv4Count = 0
	}
	if !cfg.AssignIPv6 && len(cfg.IPv6Addresses) == 0 {
		cfg.IPv6Count = 0
	}
	if !hasRequestedNetwork(cfg) {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: noNetworkSelectedMessage})
		return
	}
	if cfg.SnapshotLimit <= 0 {
		cfg.SnapshotLimit = config.DefaultSnapshotLimit
	}
	if err := validateRuntimeResourceRequest(cfg.Virtualization, cfg.VCPU, cfg.RAMMB, cfg.DiskGB); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	if cfg.ExpiresAt != "" {
		expiresAt, ok := lxc.ParseExpiration(cfg.ExpiresAt)
		if !ok {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid expiration date"})
			return
		}
		if !time.Now().Before(expiresAt) {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Expiration date must be in the future"})
			return
		}
	}

	if err := createByRuntime(cfg); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusCreated, APIResponse{Success: true, Message: "Container created successfully"})
}

func getContainer(w http.ResponseWriter, r *http.Request, id int) {
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	if c.IsKVM() && c.Status == "running" {
		_, _ = kvmManager.RefreshVNCPort(c.ID)
		_, _ = kvmManager.RefreshNetwork(c.ID)
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: c})
}

func getUsage(w http.ResponseWriter, r *http.Request, id int) {
	usage, err := usageByRuntime(id)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: usage})
}

func getTraffic(w http.ResponseWriter, r *http.Request, id int) {
	info := trafficByRuntime(id)
	if info == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: info})
}

func updateExpiry(w http.ResponseWriter, r *http.Request, id int) {
	var req struct {
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request"})
		return
	}
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	c.ExpiresAt = req.ExpiresAt
	config.SaveConfig()
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Expiry updated"})
}

func resetTraffic(w http.ResponseWriter, r *http.Request, id int) {
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	c.TrafficUsedRX = 0
	c.TrafficUsedTX = 0
	c.TrafficResetDate = time.Now().Format("2006-01")
	config.SaveConfig()
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Traffic reset"})
}

func updateTrafficLimit(w http.ResponseWriter, r *http.Request, id int) {
	var req struct {
		Mode         string `json:"traffic_mode"`
		MonthlyGB    int    `json:"monthly_traffic_gb"`
		TrafficInGB  int    `json:"traffic_in_gb"`
		TrafficOutGB int    `json:"traffic_out_gb"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request"})
		return
	}
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	c.TrafficMode = req.Mode
	c.MonthlyTrafficGB = req.MonthlyGB
	c.TrafficInGB = req.TrafficInGB
	c.TrafficOutGB = req.TrafficOutGB
	config.SaveConfig()
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Traffic limit updated"})
}

func updateResourceLimit(w http.ResponseWriter, r *http.Request, id int) {
	var req struct {
		VCPU   float64 `json:"vcpu"`
		RAMMB  int     `json:"ram_mb"`
		IOMBps int     `json:"io_speed_mbps"`
		BWMbps int     `json:"network_bw_mbps"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request"})
		return
	}
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}

	// Update config
	nextVCPU := c.VCPU
	nextRAMMB := c.RAMMB
	if req.VCPU > 0 {
		nextVCPU = req.VCPU
	}
	if req.RAMMB > 0 {
		nextRAMMB = req.RAMMB
	}
	if err := validateRuntimeResourceRequest(c.Runtime(), nextVCPU, nextRAMMB, c.DiskGB); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}

	c.VCPU = nextVCPU
	c.RAMMB = nextRAMMB
	c.IOSpeedMBps = req.IOMBps
	c.NetworkBWMbps = req.BWMbps
	config.SaveConfig()

	// Re-apply resource limits to running container
	if c.Status == "running" {
		if err := applyLimitsByRuntime(c); err != nil {
			jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
			return
		}
	}

	msg := "Resource limits updated"
	if c.IsKVM() && c.Status == "running" {
		msg = "资源已保存，请关机重启虚拟机后生效"
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: msg})
}

func getRandomPort(w http.ResponseWriter, r *http.Request, id int) {
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	hostIP := strings.TrimSpace(r.URL.Query().Get("host_ip"))
	// Try random ports
	for tries := 0; tries < 100; tries++ {
		port := 10000 + (int(time.Now().UnixNano()) % 55535)
		if lxc.HostPortAvailable(c, hostIP, port, "tcp") {
			jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]int{"port": port}})
			return
		}
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]int{"port": 0}})
}

// HandleTemplates returns available LXC templates
func HandleTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "image:read") {
		return
	}
	if isSubUserRequest(r) {
		HandleEnabledImages(w, r)
		return
	}
	templates := lxc.GetTemplates()
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: templates})
}

// HandleDashboard returns dashboard stats
func HandleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "dashboard:read") {
		return
	}
	containers, _ := listByRuntime()
	containers = filterContainersForRequest(r, containers)
	running := 0
	stopped := 0
	for _, c := range containers {
		if c.Status == "running" {
			running++
		} else {
			stopped++
		}
	}
	stats := map[string]interface{}{
		"total_containers": len(containers),
		"running":          running,
		"stopped":          stopped,
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: stats})
}

// HandleHostInfo returns host machine resource info
func HandleHostInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "host:read") {
		return
	}
	info := getHostInfo()
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: info})
}

func resetSSHPassword(w http.ResponseWriter, r *http.Request, id int) {
	c := config.FindContainer(id)
	if c != nil && lxc.IsExpired(*c) {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "容器已到期，不允许此操作"})
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && err.Error() != "EOF" {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
			return
		}
	}
	password := strings.TrimSpace(req.Password)
	if password != "" {
		if err := validateSSHPassword(password); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
			return
		}
	}
	newPassword, err := resetPasswordByRuntime(id, password)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "SSH password reset successfully",
		Data:    map[string]string{"password": newPassword},
	})
}

func validateSSHPassword(password string) error {
	if len(password) < 8 || len(password) > 64 {
		return fmt.Errorf("密码长度必须为 8-64 位")
	}
	hasLetter := false
	hasDigit := false
	for _, r := range password {
		if unicode.IsSpace(r) {
			return fmt.Errorf("密码不能包含空白字符")
		}
		if unicode.IsLetter(r) {
			hasLetter = true
		}
		if unicode.IsDigit(r) {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return fmt.Errorf("密码至少需要包含字母和数字")
	}
	return nil
}

func addPortMapping(w http.ResponseWriter, r *http.Request, id int) {
	var pm config.PortMapping
	if err := json.NewDecoder(r.Body).Decode(&pm); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	mappings, err := lxcManager.AddPortMapping(id, pm)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: mappings})
}

func updatePortMapping(w http.ResponseWriter, r *http.Request, id int, indexStr string) {
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid port mapping index"})
		return
	}
	var pm config.PortMapping
	if err := json.NewDecoder(r.Body).Decode(&pm); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	if isSubUserRequest(r) {
		c := config.FindContainer(id)
		if c == nil {
			jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
			return
		}
		if index < 0 || index >= len(c.PortMappings) {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid port mapping index"})
			return
		}
		if pm.ContainerPort < 1 || pm.ContainerPort > 65535 {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "container port must be 1-65535"})
			return
		}
		existing := c.PortMappings[index]
		pm = config.PortMapping{
			ContainerPort: pm.ContainerPort,
			HostPort:      existing.HostPort,
			Protocol:      existing.Protocol,
			Description:   existing.Description,
		}
	}
	mappings, err := lxcManager.UpdatePortMapping(id, index, pm)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: mappings})
}

func deletePortMapping(w http.ResponseWriter, r *http.Request, id int, indexStr string) {
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid port mapping index"})
		return
	}
	mappings, err := lxcManager.DeletePortMapping(id, index)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: mappings})
}

// HandleVersion returns the current CLICD version.
func HandleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]string{
		"version": version.Current(),
	}})
}
