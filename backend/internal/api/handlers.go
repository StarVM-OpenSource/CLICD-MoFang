package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"clicd/internal/config"
	"clicd/internal/lxc"
)

var lxcManager = lxc.NewManager()

// HandleContainers handles container list and creation
func HandleContainers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listContainers(w, r)
	case http.MethodPost:
		createContainer(w, r)
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}

// HandleSingleContainer handles individual container operations by ID or name: /api/containers/{id-or-name}/...
func HandleSingleContainer(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/containers/")
	parts := strings.SplitN(path, "/", 2)
	c := containerByIdentifier(parts[0])
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	id := c.ID
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "start" && r.Method == http.MethodPost:
		HandleSingleTaskAction(w, r, id, "start")
	case action == "stop" && r.Method == http.MethodPost:
		HandleSingleTaskAction(w, r, id, "stop")
	case action == "restart" && r.Method == http.MethodPost:
		HandleSingleTaskAction(w, r, id, "restart")
	case action == "reinstall" && r.Method == http.MethodPost:
		HandleSingleTaskAction(w, r, id, "reinstall")
	case action == "delete" && r.Method == http.MethodDelete:
		HandleSingleTaskAction(w, r, id, "delete")
	case action == "reset-password" && r.Method == http.MethodPost:
		resetSSHPassword(w, r, id)
	case action == "usage" && r.Method == http.MethodGet:
		getUsage(w, r, id)
	case action == "traffic" && r.Method == http.MethodGet:
		getTraffic(w, r, id)
	case action == "traffic-reset" && r.Method == http.MethodPost:
		resetTraffic(w, r, id)
	case action == "traffic-limit" && r.Method == http.MethodPut:
		updateTrafficLimit(w, r, id)
	case action == "resource-limit" && r.Method == http.MethodPut:
		updateResourceLimit(w, r, id)
	case action == "random-port" && r.Method == http.MethodGet:
		getRandomPort(w, r, id)
	case action == "expiry" && r.Method == http.MethodPut:
		updateExpiry(w, r, id)
	case action == "ipv6" && r.Method == http.MethodPost:
		assignIPv6(w, r, id)
	case action == "snapshots" || strings.HasPrefix(action, "snapshots/"):
		handleContainerSnapshots(w, r, id, action)
	case action == "port-mappings" && r.Method == http.MethodPost:
		addPortMapping(w, r, id)
	case strings.HasPrefix(action, "port-mappings/") && r.Method == http.MethodPut:
		updatePortMapping(w, r, id, strings.TrimPrefix(action, "port-mappings/"))
	case strings.HasPrefix(action, "port-mappings/") && r.Method == http.MethodDelete:
		deletePortMapping(w, r, id, strings.TrimPrefix(action, "port-mappings/"))
	case r.Method == http.MethodGet:
		getContainer(w, r, id)
	default:
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Action not found"})
	}
}

func listContainers(w http.ResponseWriter, r *http.Request) {
	containers, err := lxcManager.ListContainers()
	if err != nil {
		containers = config.AppConfig.Containers
	}
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
	if cfg.TemplateID == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Template is required"})
		return
	}
	if !isTemplateEnabledAndDownloaded(cfg.TemplateID) {
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
	if cfg.PortMappingCount < 2 {
		cfg.PortMappingCount = 2
	}
	if cfg.PortMappingCount > 64 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Port mapping count cannot exceed 64"})
		return
	}
	if cfg.SnapshotLimit <= 0 {
		cfg.SnapshotLimit = config.DefaultSnapshotLimit
	}
	if err := validateContainerResourceRequest(cfg.VCPU, cfg.RAMMB, cfg.DiskGB); err != nil {
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

	if err := lxcManager.CreateContainer(cfg); err != nil {
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
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: c})
}

func getUsage(w http.ResponseWriter, r *http.Request, id int) {
	usage, err := lxcManager.GetResourceUsage(id)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: usage})
}

func getTraffic(w http.ResponseWriter, r *http.Request, id int) {
	info := lxcManager.GetTrafficInfo(id)
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
	if err := validateContainerResourceRequest(nextVCPU, nextRAMMB, c.DiskGB); err != nil {
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
		if err := lxcManager.ApplyContainerLimits(c); err != nil {
			jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
			return
		}
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Resource limits updated"})
}

func getRandomPort(w http.ResponseWriter, r *http.Request, id int) {
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	// Find a random unused port between 10000-65535
	used := map[int]bool{}
	for _, pm := range c.PortMappings {
		used[pm.HostPort] = true
	}
	// Also check all containers
	for _, oc := range config.AppConfig.Containers {
		if oc.ID == id {
			continue
		}
		for _, pm := range oc.PortMappings {
			used[pm.HostPort] = true
		}
	}
	// Try random ports
	for tries := 0; tries < 100; tries++ {
		port := 10000 + (int(time.Now().UnixNano()) % 55535)
		if !used[port] {
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
	containers, err := lxcManager.ListContainers()
	if err != nil {
		containers = config.AppConfig.Containers
	}
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
	info := getHostInfo()
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: info})
}

func resetSSHPassword(w http.ResponseWriter, r *http.Request, id int) {
	c := config.FindContainer(id)
	if c != nil && lxc.IsExpired(*c) {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "容器已到期，不允许此操作"})
		return
	}
	newPassword, err := lxcManager.ResetSSHPassword(id)
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
