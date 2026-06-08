package api

import (
	"net/http"
	"sort"
	"strconv"

	"clicd/internal/config"
	"clicd/internal/lxc"
)

type routeCapacity struct {
	Used      int    `json:"used"`
	Remaining string `json:"remaining"`
	Total     string `json:"total"`
}

type nat4Route struct {
	ContainerID   int    `json:"container_id"`
	ContainerName string `json:"container_name"`
	LXCName       string `json:"lxc_name"`
	Status        string `json:"status"`
	IP            string `json:"ip"`
	HostPort      int    `json:"host_port"`
	ContainerPort int    `json:"container_port"`
	Protocol      string `json:"protocol"`
	Description   string `json:"description"`
}

type ipv6Route struct {
	ContainerID   int    `json:"container_id"`
	ContainerName string `json:"container_name"`
	LXCName       string `json:"lxc_name"`
	Status        string `json:"status"`
	Address       string `json:"address"`
	PrefixLen     int    `json:"prefix_len"`
	Interface     string `json:"interface"`
}

type routingResponse struct {
	NAT4            routeCapacity        `json:"nat4"`
	IPv6            routeCapacity        `json:"ipv6"`
	NAT4Mappings    []nat4Route          `json:"nat4_mappings"`
	IPv6Assignments []ipv6Route          `json:"ipv6_assignments"`
	IPv6Prefixes    []lxc.IPv6PrefixInfo `json:"ipv6_prefixes"`
}

func HandleRouting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "routing:read") {
		return
	}

	nat4Mappings := make([]nat4Route, 0)
	usedPorts := map[int]bool{}
	ipv6Assignments := make([]ipv6Route, 0)

	const nat4StartPort = 20000
	const nat4EndPort = 65535

	for _, c := range config.AppConfig.Containers {
		for _, pm := range c.PortMappings {
			if pm.HostPort >= nat4StartPort && pm.HostPort <= nat4EndPort {
				usedPorts[pm.HostPort] = true
			}
			nat4Mappings = append(nat4Mappings, nat4Route{
				ContainerID:   c.ID,
				ContainerName: c.Name,
				LXCName:       c.LxcName(),
				Status:        c.Status,
				IP:            c.IP,
				HostPort:      pm.HostPort,
				ContainerPort: pm.ContainerPort,
				Protocol:      pm.Protocol,
				Description:   pm.Description,
			})
		}
		if c.IPv6 != "" {
			ipv6Assignments = append(ipv6Assignments, ipv6Route{
				ContainerID:   c.ID,
				ContainerName: c.Name,
				LXCName:       c.LxcName(),
				Status:        c.Status,
				Address:       c.IPv6,
				PrefixLen:     c.IPv6PrefixLen,
				Interface:     c.IPv6Interface,
			})
		}
	}
	sort.SliceStable(nat4Mappings, func(i, j int) bool {
		if nat4Mappings[i].HostPort == nat4Mappings[j].HostPort {
			return nat4Mappings[i].ContainerName < nat4Mappings[j].ContainerName
		}
		return nat4Mappings[i].HostPort < nat4Mappings[j].HostPort
	})
	sort.SliceStable(ipv6Assignments, func(i, j int) bool {
		return ipv6Assignments[i].Address < ipv6Assignments[j].Address
	})

	const totalNAT4Ports = nat4EndPort - nat4StartPort + 1
	nat4Used := len(usedPorts)
	nat4Remaining := totalNAT4Ports - nat4Used
	if nat4Remaining < 0 {
		nat4Remaining = 0
	}

	prefixes := lxc.DetectPublicIPv6Prefixes()
	ipv6Total := "0"
	ipv6Remaining := "0"
	if len(prefixes) > 0 {
		ipv6Total = lxc.IPv6PrefixCapacity(prefixes[0].PrefixLen)
		ipv6Remaining = subtractCapacity(ipv6Total, len(ipv6Assignments))
	}

	jsonResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Data: routingResponse{
			NAT4: routeCapacity{
				Used:      nat4Used,
				Remaining: strconv.Itoa(nat4Remaining),
				Total:     strconv.Itoa(totalNAT4Ports),
			},
			IPv6: routeCapacity{
				Used:      len(ipv6Assignments),
				Remaining: ipv6Remaining,
				Total:     ipv6Total,
			},
			NAT4Mappings:    nat4Mappings,
			IPv6Assignments: ipv6Assignments,
			IPv6Prefixes:    prefixes,
		},
	})
}

func subtractCapacity(total string, used int) string {
	if total == "" || total == "0" {
		return "0"
	}
	if total == "large" {
		return "large"
	}
	parsed, err := strconv.ParseInt(total, 10, 64)
	if err != nil {
		return total
	}
	remaining := parsed - int64(used)
	if remaining < 0 {
		remaining = 0
	}
	return strconv.FormatInt(remaining, 10)
}
