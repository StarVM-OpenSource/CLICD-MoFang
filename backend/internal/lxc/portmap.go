package lxc

import (
	"fmt"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"

	"clicd/internal/config"
)

// ApplyPortMappings applies iptables DNAT rules for a container's port mappings
func (m *Manager) ApplyPortMappings(id int) error {
	c := config.FindContainer(id)
	if c == nil {
		return fmt.Errorf("container not found: %d", id)
	}
	if c.IP == "" {
		return fmt.Errorf("container has no IP")
	}
	EnsureAssignedPublicIPv4s(c.PublicIPv4s)
	tag := clicdTag(id)
	bridge := "lxcbr0"
	subnet := "10.0.3.0/24"
	if c.IsKVM() {
		bridge = "virbr0"
		subnet = "192.168.122.0/24"
	}

	EnsureForwardRules(bridge)
	m.CleanPortMappings(id)
	deleteBridgeMasquerade(subnet)

	for _, pm := range c.PortMappings {
		for _, hostIP := range expandPortMappingHostIPs(c, pm) {
			args := []string{
				"-t", "nat",
				"-I", "PREROUTING", "1",
				"-p", pm.Protocol,
			}
			if hostIP != "" {
				args = append(args, "-d", hostIP)
			}
			args = append(args,
				"--dport", fmt.Sprintf("%d", pm.HostPort),
				"-j", "DNAT",
				"--to-destination", fmt.Sprintf("%s:%d", c.IP, pm.ContainerPort),
				"-m", "comment", "--comment", fmt.Sprintf("clicd-%s-%s-%d", tag, natRuleIPTag(hostIP), pm.HostPort),
			)
			cmd := exec.Command("iptables", args...)
			output, err := cmd.CombinedOutput()
			if err != nil {
				fmt.Printf("Warning: failed to apply port mapping %s:%d->%s:%d: %v, output: %s\n",
					displayHostIP(hostIP), pm.HostPort, c.IP, pm.ContainerPort, err, string(output))
				continue
			}
			fmt.Printf("Port mapping: %s:%d -> %s:%d\n", displayHostIP(hostIP), pm.HostPort, c.IP, pm.ContainerPort)
		}
	}

	// When container has public IPv4, apply full port passthrough DNAT so the
	// container owns all ports on its public IP (no NAT management needed).
	if len(c.PublicIPv4s) > 0 {
		ensureIndependentIPv4Ingress(c, tag)
	}

	applyIPv4EgressPolicy(c, bridge, subnet, tag)

	return nil
}

func ensureIndependentIPv4Ingress(c *config.Container, tag string) {
	if c == nil || c.IP == "" || len(c.PublicIPv4s) == 0 {
		return
	}

	for _, assignment := range c.PublicIPv4s {
		hostIP := strings.TrimSpace(assignment.Address)
		if hostIP == "" {
			continue
		}
		// Full port passthrough: DNAT all TCP+UDP traffic on this public IP to the container.
		for _, proto := range []string{"tcp", "udp"} {
			args := []string{
				"-t", "nat",
				"-I", "PREROUTING", "1",
				"-d", hostIP,
				"-p", proto,
				"-j", "DNAT",
				"--to-destination", c.IP,
				"-m", "comment", "--comment", fmt.Sprintf("clicd-%s-%s-all-%s", tag, natRuleIPTag(hostIP), proto),
			}
			cmd := exec.Command("iptables", args...)
			output, err := cmd.CombinedOutput()
			if err != nil {
				fmt.Printf("Warning: failed to apply %s passthrough %s->%s: %v, output: %s\n",
					proto, hostIP, c.IP, err, string(output))
				continue
			}
			fmt.Printf("IPv4 passthrough (%s): %s -> %s (all ports)\n", proto, hostIP, c.IP)
		}
	}
}

func applyIPv4EgressPolicy(c *config.Container, bridge, subnet, tag string) {
	if c == nil || strings.TrimSpace(c.IP) == "" {
		return
	}
	if containerAllowsPublicIPv4Egress(c) {
		if _, ok := primaryPublicIPv4Assignment(c); ok {
			applyPublicIPv4SNAT(c, tag)
			return
		}
		ensureContainerMasquerade(c, tag)
		return
	}
	ensureIPv4EgressBlocked(c, bridge, subnet, tag)
}

func containerAllowsPublicIPv4Egress(c *config.Container) bool {
	if c == nil {
		return false
	}
	if len(c.PublicIPv4s) > 0 {
		return true
	}
	return c.PortMappingLimit > 0 || len(c.PortMappings) > 0
}

func ensureContainerMasquerade(c *config.Container, tag string) {
	args := []string{
		"-s", c.IP + "/32",
		"-m", "comment", "--comment", fmt.Sprintf("clicd-%s-masq", tag),
		"-j", "MASQUERADE",
	}
	if host := DetectPublicIPv4(); strings.TrimSpace(host.Interface) != "" {
		args = append([]string{"-o", strings.TrimSpace(host.Interface)}, args...)
	} else {
		args = append([]string{"-o", "eth+"}, args...)
	}
	ensureNATRule("POSTROUTING", args)
}

func ensureIPv4EgressBlocked(c *config.Container, bridge, subnet, tag string) {
	args := []string{
		"-i", bridge,
		"-s", c.IP + "/32",
		"!", "-d", subnet,
		"-m", "comment", "--comment", fmt.Sprintf("clicd-%s-v4-egress-block", tag),
		"-j", "REJECT",
	}
	ensureFilterRule("FORWARD", args)
}

func ensureNATRule(chain string, args []string) {
	check := append([]string{"-t", "nat", "-C", chain}, args...)
	if exec.Command("iptables", check...).Run() == nil {
		return
	}
	add := append([]string{"-t", "nat", "-I", chain, "1"}, args...)
	exec.Command("iptables", add...).Run()
}

func ensureFilterRule(chain string, args []string) {
	check := append([]string{"-C", chain}, args...)
	if exec.Command("iptables", check...).Run() == nil {
		return
	}
	add := append([]string{"-I", chain, "1"}, args...)
	exec.Command("iptables", add...).Run()
}

func deleteBridgeMasquerade(subnet string) {
	for exec.Command("iptables", "-t", "nat", "-D", "POSTROUTING", "-s", subnet, "-o", "eth+", "-j", "MASQUERADE").Run() == nil {
	}
}

func applyPublicIPv4SNAT(c *config.Container, tag string) {
	if c == nil || strings.TrimSpace(c.IP) == "" {
		return
	}
	assignment, ok := primaryPublicIPv4Assignment(c)
	if !ok {
		return
	}
	hostIP := strings.TrimSpace(assignment.Address)
	if hostIP == "" {
		return
	}
	iface := strings.TrimSpace(assignment.Interface)
	if iface == "" {
		if info, ok := publicIPv4InfoByAddress(hostIP); ok {
			iface = strings.TrimSpace(info.Interface)
		}
	}
	if iface == "" {
		if host := DetectPublicIPv4(); host.Interface != "" {
			iface = host.Interface
		}
	}
	args := []string{
		"-t", "nat",
		"-I", "POSTROUTING", "1",
		"-s", c.IP + "/32",
	}
	if iface != "" {
		args = append(args, "-o", iface)
	}
	args = append(args,
		"-m", "comment", "--comment", fmt.Sprintf("clicd-%s-snat-%s", tag, natRuleIPTag(hostIP)),
		"-j", "SNAT", "--to-source", hostIP,
	)
	if output, err := exec.Command("iptables", args...).CombinedOutput(); err != nil {
		fmt.Printf("Warning: failed to apply public IPv4 SNAT %s -> %s: %v, output: %s\n", c.IP, hostIP, err, string(output))
	}
}

func primaryPublicIPv4Assignment(c *config.Container) (config.PublicIPv4Assignment, bool) {
	if c == nil {
		return config.PublicIPv4Assignment{}, false
	}
	for _, item := range c.PublicIPv4s {
		if strings.TrimSpace(item.Address) != "" {
			return item, true
		}
	}
	return config.PublicIPv4Assignment{}, false
}

func clicdTag(id int) string { return "c" + strconv.Itoa(id) }

func EnsureAllRunningPortMappings() {
	m := NewManager()
	for i := range config.AppConfig.Containers {
		c := &config.AppConfig.Containers[i]
		if c.Status != "running" || strings.TrimSpace(c.IP) == "" {
			continue
		}
		if err := m.ApplyPortMappings(c.ID); err != nil {
			fmt.Printf("Warning: failed to restore port mappings for %s: %v\n", c.Name, err)
		}
	}
}

// EnsureForwardRules makes sure iptables FORWARD chain allows bridge traffic.
func EnsureForwardRules(bridge string) {
	if bridge == "" {
		bridge = "lxcbr0"
	}
	rules := [][]string{
		{"-i", bridge, "-j", "ACCEPT"},
		{"-o", bridge, "-j", "ACCEPT"},
		{"-i", bridge, "-o", bridge, "-j", "ACCEPT"},
	}
	for _, args := range rules {
		for {
			deleteArgs := append([]string{"-D", "FORWARD"}, args...)
			if exec.Command("iptables", deleteArgs...).Run() != nil {
				break
			}
		}
		insertArgs := append([]string{"-I", "FORWARD", "1"}, args...)
		exec.Command("iptables", insertArgs...).Run()
	}
}

// CleanPortMappings removes all iptables rules for a container
func (m *Manager) CleanPortMappings(id int) error {
	tag := clicdTag(id)
	for _, chain := range []string{"PREROUTING", "POSTROUTING"} {
		cmd := exec.Command("sh", "-c",
			fmt.Sprintf("iptables -t nat -L %s -n --line-numbers 2>/dev/null | grep 'clicd-%s-' | awk '{print $1}' | sort -rn | while read num; do iptables -t nat -D %s $num; done", chain, tag, chain))
		cmd.Run()
	}
	cmd := exec.Command("sh", "-c",
		fmt.Sprintf("iptables -S FORWARD 2>/dev/null | grep 'clicd-%s-' | sed 's/^-A /-D /' | while read rule; do iptables $rule; done", tag))
	cmd.Run()
	return nil
}

// SetupDefaultPortMappings creates default port mappings
func SetupDefaultPortMappings(sshPort int) []config.PortMapping {
	return []config.PortMapping{
		{ContainerPort: 22, HostPort: sshPort, Protocol: "tcp", Description: "SSH"},
	}
}

func DefaultPortMappingHostIP(assignments []config.PublicIPv4Assignment) string {
	if len(assignments) == 1 {
		return strings.TrimSpace(assignments[0].Address)
	}
	return ""
}

func defaultPortMappingHostIP(assignments []config.PublicIPv4Assignment) string {
	return DefaultPortMappingHostIP(assignments)
}

// AddPortMapping adds a NAT rule to a container
func (m *Manager) AddPortMapping(id int, pm config.PortMapping) ([]config.PortMapping, error) {
	c := config.FindContainer(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %d", id)
	}
	if c.PortMappingLimit <= 0 {
		return nil, fmt.Errorf("container has no IPv4 NAT port quota")
	}
	if c.PortMappingLimit > 0 && len(c.PortMappings) >= c.PortMappingLimit {
		return nil, fmt.Errorf("port mapping quota exceeded: %d/%d", len(c.PortMappings), c.PortMappingLimit)
	}
	normalized, err := normalizePortMapping(c, -1, pm)
	if err != nil {
		return nil, err
	}
	c.PortMappings = append(c.PortMappings, normalized)
	if err := persistAndReloadMappings(m, c); err != nil {
		return nil, err
	}
	return c.PortMappings, nil
}

// UpdatePortMapping updates an existing NAT rule
func (m *Manager) UpdatePortMapping(id int, index int, pm config.PortMapping) ([]config.PortMapping, error) {
	c := config.FindContainer(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %d", id)
	}
	if index < 0 || index >= len(c.PortMappings) {
		return nil, fmt.Errorf("invalid port mapping index: %d", index)
	}
	normalized, err := normalizePortMapping(c, index, pm)
	if err != nil {
		return nil, err
	}
	c.PortMappings[index] = normalized
	if err := persistAndReloadMappings(m, c); err != nil {
		return nil, err
	}
	return c.PortMappings, nil
}

// DeletePortMapping removes a NAT rule
func (m *Manager) DeletePortMapping(id int, index int) ([]config.PortMapping, error) {
	c := config.FindContainer(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %d", id)
	}
	if index < 0 || index >= len(c.PortMappings) {
		return nil, fmt.Errorf("invalid port mapping index: %d", index)
	}
	if c.PortMappings[index].Description == "SSH" {
		return nil, fmt.Errorf("SSH default mapping cannot be deleted")
	}
	c.PortMappings = append(c.PortMappings[:index], c.PortMappings[index+1:]...)
	if err := persistAndReloadMappings(m, c); err != nil {
		return nil, err
	}
	return c.PortMappings, nil
}

func persistAndReloadMappings(m *Manager, c *config.Container) error {
	config.SaveConfig()
	if c.Status == "running" && c.IP != "" {
		return m.ApplyPortMappings(c.ID)
	}
	return nil
}

func normalizePortMapping(c *config.Container, skipIndex int, pm config.PortMapping) (config.PortMapping, error) {
	if pm.ContainerPort < 1 || pm.ContainerPort > 65535 {
		return pm, fmt.Errorf("container port must be 1-65535")
	}
	if pm.Protocol == "" {
		pm.Protocol = "tcp"
	}
	pm.Protocol = strings.ToLower(strings.TrimSpace(pm.Protocol))
	pm.HostIP = strings.TrimSpace(pm.HostIP)
	if pm.HostIP != "" {
		addr, err := netip.ParseAddr(pm.HostIP)
		if err != nil || !addr.Is4() {
			return pm, fmt.Errorf("host_ip must be a valid IPv4 address")
		}
		if !containerHasPublicIPv4(c, pm.HostIP) {
			return pm, fmt.Errorf("host_ip %s is not assigned to this container", pm.HostIP)
		}
	}
	if pm.Description == "" {
		pm.Description = fmt.Sprintf("Port-%d", pm.ContainerPort)
	}
	if pm.HostPort <= 0 {
		pm.HostPort = pm.ContainerPort
	}
	// Check current container's own mappings
	for i, existing := range c.PortMappings {
		if i == skipIndex {
			continue
		}
		if portMappingsConflict(c, pm, c, existing) {
			return pm, fmt.Errorf("host port %d/%s already mapped on the same IPv4 in this container", pm.HostPort, pm.Protocol)
		}
	}
	// Check all other containers (LXC + KVM) for port conflicts
	for _, oc := range config.AppConfig.Containers {
		if oc.ID == c.ID {
			continue
		}
		for _, existing := range oc.PortMappings {
			oc := oc
			if portMappingsConflict(c, pm, &oc, existing) {
				return pm, fmt.Errorf("host port %d/%s already used on the same IPv4 by container %s (ID: %d)", pm.HostPort, pm.Protocol, oc.Name, oc.ID)
			}
		}
	}
	return pm, nil
}

func allocateDefaultEqualPorts(c *config.Container, count int) []int {
	if count <= 0 {
		return nil
	}
	used := map[int]bool{}
	// Mark current container's ports
	for _, pm := range c.PortMappings {
		for _, hostIP := range expandPortMappingHostIPs(c, pm) {
			used[hostPortKey(hostIP, pm.HostPort)] = true
		}
		used[pm.ContainerPort] = true
	}
	// Also mark all other containers' host ports (LXC + KVM)
	for _, oc := range config.AppConfig.Containers {
		if oc.ID == c.ID {
			continue
		}
		for _, pm := range oc.PortMappings {
			oc := oc
			for _, hostIP := range expandPortMappingHostIPs(&oc, pm) {
				used[hostPortKey(hostIP, pm.HostPort)] = true
			}
		}
	}
	ports := make([]int, 0, count)
	next := 20000
	for len(ports) < count {
		hostIP := c.PrimaryPublicIPv4()
		if !used[hostPortKey(hostIP, next)] && !used[next] {
			ports = append(ports, next)
		}
		next++
		if next > 65535 || len(ports) >= count {
			break
		}
	}
	return ports
}

func HostPortAvailable(c *config.Container, hostIP string, hostPort int, protocol string) bool {
	if c == nil || hostPort <= 0 {
		return false
	}
	pm := config.PortMapping{HostIP: strings.TrimSpace(hostIP), HostPort: hostPort, Protocol: protocol}
	for _, existing := range c.PortMappings {
		if portMappingsConflict(c, pm, c, existing) {
			return false
		}
	}
	for _, oc := range config.AppConfig.Containers {
		if oc.ID == c.ID {
			continue
		}
		oc := oc
		for _, existing := range oc.PortMappings {
			if portMappingsConflict(c, pm, &oc, existing) {
				return false
			}
		}
	}
	return true
}

func expandPortMappingHostIPs(c *config.Container, pm config.PortMapping) []string {
	if strings.TrimSpace(pm.HostIP) != "" {
		return []string{strings.TrimSpace(pm.HostIP)}
	}
	if c != nil && len(c.PublicIPv4s) > 0 {
		values := make([]string, 0, len(c.PublicIPv4s))
		for _, item := range c.PublicIPv4s {
			if strings.TrimSpace(item.Address) != "" {
				values = append(values, strings.TrimSpace(item.Address))
			}
		}
		if len(values) > 0 {
			return values
		}
	}
	return []string{""}
}

func containerHasPublicIPv4(c *config.Container, hostIP string) bool {
	if c == nil {
		return false
	}
	for _, item := range c.PublicIPv4s {
		if item.Address == hostIP {
			return true
		}
	}
	return false
}

func portMappingsConflict(aContainer *config.Container, a config.PortMapping, bContainer *config.Container, b config.PortMapping) bool {
	if a.HostPort != b.HostPort || !protocolsOverlap(a.Protocol, b.Protocol) {
		return false
	}
	aIPs := expandPortMappingHostIPs(aContainer, a)
	bIPs := expandPortMappingHostIPs(bContainer, b)
	for _, aIP := range aIPs {
		for _, bIP := range bIPs {
			if aIP == "" || bIP == "" || aIP == bIP {
				return true
			}
		}
	}
	return false
}

func protocolsOverlap(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == "" {
		a = "tcp"
	}
	if b == "" {
		b = "tcp"
	}
	if a == b || a == "all" || b == "all" {
		return true
	}
	return (a == "tcp+udp" && (b == "tcp" || b == "udp")) ||
		(b == "tcp+udp" && (a == "tcp" || a == "udp"))
}

func natRuleIPTag(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "any"
	}
	return strings.ReplaceAll(ip, ".", "_")
}

func displayHostIP(ip string) string {
	if strings.TrimSpace(ip) == "" {
		return "host"
	}
	return ip
}

func hostPortKey(hostIP string, port int) int {
	if hostIP == "" {
		return port
	}
	sum := 0
	for _, r := range hostIP {
		sum = sum*31 + int(r)
	}
	if sum < 0 {
		sum = -sum
	}
	return port + (sum % 1000000 * 100000)
}
