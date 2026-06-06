package lxc

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"clicd/internal/config"
)

const ipv6GatewayLinkLocal = "fe80::1"

type IPv6PrefixInfo struct {
	Interface string `json:"interface"`
	Address   string `json:"address"`
	Prefix    string `json:"prefix"`
	PrefixLen int    `json:"prefix_len"`
	Gateway   string `json:"gateway"`
	IsTunnel  bool   `json:"is_tunnel"`
	Source    string `json:"source"`
}

type PublicIPInfo struct {
	Address   string `json:"address"`
	Interface string `json:"interface"`
	Prefix    string `json:"prefix"`
	IsTunnel  bool   `json:"is_tunnel"`
	Source    string `json:"source"`
}

type IPv6Status struct {
	Available bool             `json:"available"`
	Reachable bool             `json:"reachable"`
	Reason    string           `json:"reason"`
	Prefixes  []IPv6PrefixInfo `json:"prefixes"`
}

func (m *Manager) DetectIPv6Status() IPv6Status {
	status := IPv6Status{}
	prefixes := DetectPublicIPv6Prefixes()
	status.Prefixes = prefixes
	if len(prefixes) == 0 {
		status.Reason = "no usable public IPv6 prefix found; /128 single-address IPv6 is not assignable"
		return status
	}
	status.Reachable = ipv6ConnectivityOK()
	if !status.Reachable {
		status.Reason = "host has an IPv6 prefix, but outbound IPv6 connectivity test failed"
		return status
	}
	status.Available = true
	status.Reason = "usable public IPv6 prefix detected"
	return status
}

func DetectPublicIPv6Prefixes() []IPv6PrefixInfo {
	return detectPublicIPv6Prefixes(detectIPv6DefaultRoutes())
}

func DetectPublicIPv4() PublicIPInfo {
	candidates := DetectPublicIPv4Candidates()
	if len(candidates) == 0 {
		return PublicIPInfo{}
	}
	return candidates[0]
}

func DetectPublicIPv4Candidates() []PublicIPInfo {
	out, err := exec.Command("ip", "-4", "-o", "addr", "show", "scope", "global").Output()
	if err != nil {
		return nil
	}
	defaultRoutes := detectIPv4DefaultRoutes()
	defaultIfaces := map[string]bool{}
	for _, route := range defaultRoutes {
		defaultIfaces[route.Interface] = true
	}
	type candidate struct {
		info  PublicIPInfo
		score int
	}
	var candidates []candidate
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[2] != "inet" {
			continue
		}
		iface := normalizeIface(fields[1])
		if isContainerLikeInterface(iface) {
			continue
		}
		prefix, err := netip.ParsePrefix(fields[3])
		if err != nil || !prefix.Addr().Is4() || !isPublicIPv4(prefix.Addr()) {
			continue
		}
		key := iface + "|" + prefix.Addr().String()
		if seen[key] {
			continue
		}
		seen[key] = true
		score := publicInterfaceScore(iface, defaultIfaces)
		candidates = append(candidates, candidate{
			info: PublicIPInfo{
				Address:   prefix.Addr().String(),
				Interface: iface,
				Prefix:    prefix.Masked().String(),
				IsTunnel:  isTunnelLikeInterface(iface),
				Source:    "local",
			},
			score: score,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	result := make([]PublicIPInfo, 0, len(candidates))
	for _, c := range candidates {
		result = append(result, c.info)
	}
	return result
}

func detectPublicIPv6Prefixes(defaultRoutes []routeInfo) []IPv6PrefixInfo {
	out, err := exec.Command("ip", "-6", "-o", "addr", "show", "scope", "global").Output()
	if err != nil {
		return nil
	}
	defaultIfaces := map[string]bool{}
	gateways := map[string]string{}
	for _, route := range defaultRoutes {
		defaultIfaces[route.Interface] = true
		if route.Gateway != "" && gateways[route.Interface] == "" {
			gateways[route.Interface] = route.Gateway
		}
	}
	type candidate struct {
		info  IPv6PrefixInfo
		score int
	}
	var candidates []candidate
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[2] != "inet6" {
			continue
		}
		iface := normalizeIface(fields[1])
		if isContainerLikeInterface(iface) {
			continue
		}
		prefix, err := netip.ParsePrefix(fields[3])
		if err != nil || !prefix.Addr().Is6() {
			continue
		}
		addr := prefix.Addr()
		if !isPublicIPv6(addr) {
			continue
		}
		// Require at least 8 host bits. /128 is a single address, not a usable segment.
		if prefix.Bits() > 120 {
			continue
		}
		masked := prefix.Masked()
		key := iface + "|" + masked.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		score := publicInterfaceScore(iface, defaultIfaces)
		if masked.Bits() <= 64 {
			score += 20
		}
		info := IPv6PrefixInfo{
			Interface: iface,
			Address:   addr.String(),
			Prefix:    masked.String(),
			PrefixLen: masked.Bits(),
			Gateway:   gateways[iface],
			IsTunnel:  isTunnelLikeInterface(iface),
			Source:    "local",
		}
		candidates = append(candidates, candidate{info: info, score: score})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	result := make([]IPv6PrefixInfo, 0, len(candidates))
	for _, c := range candidates {
		result = append(result, c.info)
	}
	return result
}

type routeInfo struct {
	Interface string
	Gateway   string
	Metric    int
}

func detectIPv4DefaultRoutes() []routeInfo {
	out, err := exec.Command("ip", "-4", "route", "show", "default").Output()
	if err != nil {
		return nil
	}
	return parseDefaultRoutes(string(out))
}

func detectIPv6DefaultRoutes() []routeInfo {
	out, err := exec.Command("ip", "-6", "route", "show", "default").Output()
	if err != nil {
		return nil
	}
	return parseDefaultRoutes(string(out))
}

func parseDefaultRoutes(output string) []routeInfo {
	var routes []routeInfo
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "default" {
			continue
		}
		route := routeInfo{Metric: 1024}
		for i := 1; i < len(fields)-1; i++ {
			switch fields[i] {
			case "dev":
				route.Interface = normalizeIface(fields[i+1])
			case "via":
				route.Gateway = fields[i+1]
			case "metric":
				metric, err := strconv.Atoi(fields[i+1])
				if err == nil {
					route.Metric = metric
				}
			}
		}
		if route.Interface != "" {
			routes = append(routes, route)
		}
	}
	sort.SliceStable(routes, func(i, j int) bool {
		return routes[i].Metric < routes[j].Metric
	})
	return routes
}

func ipv6ConnectivityOK() bool {
	targets := [][]string{
		{"ping", "-6", "-c", "1", "-W", "2", "2606:4700:4700::1111"},
		{"ping", "-6", "-c", "1", "-W", "2", "2001:4860:4860::8888"},
		{"ping6", "-c", "1", "-W", "2", "2606:4700:4700::1111"},
	}
	for _, args := range targets {
		if exec.Command(args[0], args[1:]...).Run() == nil {
			return true
		}
	}
	return false
}

func isContainerLikeInterface(iface string) bool {
	prefixes := []string{
		"lo", "lxc", "docker", "br-", "veth", "virbr", "cni", "flannel", "cali",
		"kube", "dummy", "ifb", "zt", "zerotier",
	}
	for _, prefix := range prefixes {
		if iface == prefix || strings.HasPrefix(iface, prefix) {
			return true
		}
	}
	return false
}

func normalizeIface(iface string) string {
	iface = strings.TrimSuffix(iface, ":")
	if at := strings.Index(iface, "@"); at >= 0 {
		iface = iface[:at]
	}
	return iface
}

func publicInterfaceScore(iface string, defaultIfaces map[string]bool) int {
	score := 0
	if defaultIfaces[iface] {
		score += 100
	}
	if isTunnelLikeInterface(iface) {
		score -= 120
	} else {
		score += 80
	}
	if isLikelyPhysicalInterface(iface) {
		score += 40
	}
	if operState(iface) == "up" {
		score += 10
	}
	return score
}

func isLikelyPhysicalInterface(iface string) bool {
	prefixes := []string{"eth", "ens", "eno", "enp", "em", "bond", "team"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(iface, prefix) {
			return true
		}
	}
	return false
}

func isTunnelLikeInterface(iface string) bool {
	lower := strings.ToLower(iface)
	prefixes := []string{
		"wg", "wgcf", "warp", "cloudflare", "tun", "tap", "tailscale", "ts",
		"vpn", "ppp", "ipsec", "gre", "gretap", "sit", "he-", "nebula", "zt",
	}
	for _, prefix := range prefixes {
		if lower == prefix || strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return strings.Contains(lower, "warp") || strings.Contains(lower, "cloudflare")
}

func operState(iface string) string {
	data, err := os.ReadFile("/sys/class/net/" + iface + "/operstate")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func isPublicIPv4(addr netip.Addr) bool {
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	raw := addr.As4()
	if raw[0] == 100 && raw[1] >= 64 && raw[1] <= 127 {
		return false
	}
	if raw[0] == 192 && raw[1] == 0 && raw[2] == 0 {
		return false
	}
	return true
}

func isPublicIPv6(addr netip.Addr) bool {
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	return !strings.HasPrefix(addr.String(), "2001:db8:")
}

func (m *Manager) allocateIPv6ForContainer(id int) (string, int, string, error) {
	status := m.DetectIPv6Status()
	if !status.Available {
		return "", 0, "", fmt.Errorf("public IPv6 allocation is unavailable: %s", status.Reason)
	}
	prefixInfo := status.Prefixes[0]
	prefix, err := netip.ParsePrefix(prefixInfo.Prefix)
	if err != nil {
		return "", 0, "", err
	}

	used := map[string]bool{}
	hostAddrs := map[string]bool{}
	for _, p := range status.Prefixes {
		hostAddrs[p.Address] = true
	}
	for _, c := range config.AppConfig.Containers {
		if c.IPv6 != "" {
			used[c.IPv6] = true
		}
	}
	for offset := uint64(0x1000 + id); offset < 0x100000; offset++ {
		addr, err := ipv6Add(prefix.Masked().Addr(), offset)
		if err != nil || !prefix.Contains(addr) {
			break
		}
		candidate := addr.String()
		if !used[candidate] && !hostAddrs[candidate] {
			return candidate, prefix.Bits(), prefixInfo.Interface, nil
		}
	}
	return "", 0, "", fmt.Errorf("no free IPv6 address in %s", prefix.String())
}

func ipv6Add(base netip.Addr, offset uint64) (netip.Addr, error) {
	raw := base.As16()
	value := big.NewInt(0).SetBytes(raw[:])
	add := make([]byte, 8)
	binary.BigEndian.PutUint64(add, offset)
	value.Add(value, big.NewInt(0).SetBytes(add))
	bytes := value.Bytes()
	if len(bytes) > 16 {
		return netip.Addr{}, fmt.Errorf("IPv6 address overflow")
	}
	padded := make([]byte, 16)
	copy(padded[16-len(bytes):], bytes)
	var out [16]byte
	copy(out[:], padded)
	return netip.AddrFrom16(out), nil
}

func (m *Manager) AssignIPv6(id int) (*config.Container, error) {
	c := config.FindContainer(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %d", id)
	}
	if c.IPv6 == "" {
		addr, prefixLen, iface, err := m.allocateIPv6ForContainer(id)
		if err != nil {
			return nil, err
		}
		c.IPv6 = addr
		c.IPv6PrefixLen = prefixLen
		c.IPv6Interface = iface
		config.SaveConfig()
	}
	if err := m.applyIPv6Config(c.LxcName(), c.IPv6); err != nil {
		return nil, err
	}
	rootfsPath := filepath.Join(m.LxcPath, c.LxcName(), "rootfs")
	if _, err := os.Stat(rootfsPath); err == nil {
		if err := installContainerIPv6Init(rootfsPath, c.IPv6); err != nil {
			fmt.Printf("Warning: failed to install IPv6 init in %s: %v\n", c.LxcName(), err)
		}
	}
	if err := m.ApplyIPv6(id); err != nil {
		return nil, err
	}
	return c, nil
}

func (m *Manager) applyIPv6Config(lxcName, ipv6 string) error {
	configFile := filepath.Join(m.LxcPath, lxcName, "config")
	data, err := os.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("failed to read container config: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	next := make([]string, 0, len(lines)+4)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "# clicd managed: public IPv6") ||
			strings.HasPrefix(trimmed, "lxc.net.0.ipv6.address") ||
			strings.HasPrefix(trimmed, "lxc.net.0.ipv6.gateway") {
			continue
		}
		next = append(next, line)
	}
	if ipv6 != "" {
		next = append(next, "", "# clicd managed: public IPv6 routed /128")
		next = append(next, fmt.Sprintf("lxc.net.0.ipv6.address = %s/128", ipv6))
		next = append(next, "lxc.net.0.ipv6.gateway = auto")
	}
	return os.WriteFile(configFile, []byte(strings.Join(next, "\n")), 0644)
}

func (m *Manager) ApplyIPv6(id int) error {
	c := config.FindContainer(id)
	if c == nil {
		return fmt.Errorf("container not found: %d", id)
	}
	if c.IPv6 == "" {
		return nil
	}
	if c.IPv6Interface == "" {
		status := m.DetectIPv6Status()
		if len(status.Prefixes) == 0 {
			return fmt.Errorf("failed to detect IPv6 uplink for %s", c.IPv6)
		}
		c.IPv6Interface = status.Prefixes[0].Interface
		c.IPv6PrefixLen = status.Prefixes[0].PrefixLen
		config.SaveConfig()
	}

	rootfsPath := filepath.Join(m.LxcPath, c.LxcName(), "rootfs")
	if _, err := os.Stat(rootfsPath); err == nil {
		if err := installContainerIPv6Init(rootfsPath, c.IPv6); err != nil {
			fmt.Printf("Warning: failed to install IPv6 init in %s: %v\n", c.LxcName(), err)
		}
	}
	if err := ensureHostIPv6Routing(c.IPv6, c.IPv6Interface); err != nil {
		return err
	}
	status, _ := m.GetContainerStatus(c.LxcName())
	if status != "running" {
		return nil
	}
	cmd := exec.Command("lxc-attach", "-n", c.LxcName(), "--", "sh", "-c",
		fmt.Sprintf("ip -6 addr replace %s/128 dev eth0 && ip -6 route replace default via %s dev eth0 metric 100",
			shellQuote(c.IPv6), shellQuote(ipv6GatewayLinkLocal)))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to apply IPv6 inside container: %v, output: %s", err, string(output))
	}
	removeIPv6NAT66(c.IPv6, c.IPv6Interface)
	if !containerIPv6ConnectivityOK(c.LxcName()) {
		ensureIPv6NAT66(c.IPv6, c.IPv6Interface)
	}
	return nil
}

func ensureHostIPv6Routing(ipv6, uplink string) error {
	if uplink == "" {
		return fmt.Errorf("missing IPv6 uplink interface")
	}
	runQuiet("sysctl", "-w", "net.ipv6.conf.all.forwarding=1")
	runQuiet("sysctl", "-w", "net.ipv6.conf."+uplink+".accept_ra=2")
	runQuiet("sysctl", "-w", "net.ipv6.conf."+uplink+".proxy_ndp=1")
	runQuiet("ip", "link", "set", "lxcbr0", "up")
	runQuiet("ip", "-6", "addr", "add", ipv6GatewayLinkLocal+"/64", "dev", "lxcbr0")
	if out, err := exec.Command("ip", "-6", "route", "replace", ipv6+"/128", "dev", "lxcbr0").CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add IPv6 host route: %v, output: %s", err, string(out))
	}
	if out, err := exec.Command("ip", "-6", "neigh", "replace", "proxy", ipv6, "dev", uplink).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add IPv6 proxy NDP: %v, output: %s", err, string(out))
	}
	ensureIPv6ForwardRules(ipv6)
	return nil
}

func installContainerIPv6Init(rootfsPath, ipv6 string) error {
	if strings.TrimSpace(ipv6) == "" {
		return nil
	}
	if _, err := netip.ParseAddr(ipv6); err != nil {
		return fmt.Errorf("invalid IPv6 address %q: %w", ipv6, err)
	}

	scriptPath := filepath.Join(rootfsPath, "usr", "local", "sbin", "clicd-ipv6-init")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
		return err
	}
	script := `#!/bin/sh
IPV6_ADDR=` + shellQuote(ipv6) + `
IPV6_GW=` + shellQuote(ipv6GatewayLinkLocal) + `
IFACE="${CLICD_IPV6_IFACE:-eth0}"

command -v ip >/dev/null 2>&1 || exit 0

i=0
while [ "$i" -lt 30 ]; do
	if ip link show dev "$IFACE" >/dev/null 2>&1; then
		break
	fi
	i=$((i + 1))
	sleep 1
done

ip link set dev "$IFACE" up >/dev/null 2>&1 || true
ip -6 addr replace "$IPV6_ADDR/128" dev "$IFACE" >/dev/null 2>&1 || true
ip -6 route replace default via "$IPV6_GW" dev "$IFACE" metric 100 >/dev/null 2>&1 || true
exit 0
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return err
	}

	osRelease := ""
	if data, err := os.ReadFile(filepath.Join(rootfsPath, "etc", "os-release")); err == nil {
		osRelease = strings.ToLower(string(data))
	}
	hasSystemd := dirExists(filepath.Join(rootfsPath, "etc", "systemd", "system"))
	hasOpenRC := fileExists(filepath.Join(rootfsPath, "sbin", "openrc-run")) || strings.Contains(osRelease, "alpine")

	if hasSystemd {
		if err := installContainerIPv6Systemd(rootfsPath); err != nil {
			return err
		}
	}
	if hasOpenRC {
		if err := installContainerIPv6OpenRC(rootfsPath); err != nil {
			return err
		}
	}
	if !hasSystemd && !hasOpenRC {
		if err := installContainerIPv6SysV(rootfsPath); err != nil {
			return err
		}
	}
	return nil
}

func installContainerIPv6Systemd(rootfsPath string) error {
	servicePath := filepath.Join(rootfsPath, "etc", "systemd", "system", "clicd-ipv6.service")
	if err := os.MkdirAll(filepath.Dir(servicePath), 0755); err != nil {
		return err
	}
	service := `[Unit]
Description=CLICD IPv6 setup
After=network-online.target network.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/clicd-ipv6-init
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`
	if err := os.WriteFile(servicePath, []byte(service), 0644); err != nil {
		return err
	}
	wantsDir := filepath.Join(rootfsPath, "etc", "systemd", "system", "multi-user.target.wants")
	if err := os.MkdirAll(wantsDir, 0755); err != nil {
		return err
	}
	return replaceSymlink("../clicd-ipv6.service", filepath.Join(wantsDir, "clicd-ipv6.service"))
}

func installContainerIPv6OpenRC(rootfsPath string) error {
	initPath := filepath.Join(rootfsPath, "etc", "init.d", "clicd-ipv6")
	if err := os.MkdirAll(filepath.Dir(initPath), 0755); err != nil {
		return err
	}
	initScript := `#!/sbin/openrc-run
name="CLICD IPv6 setup"
description="Apply CLICD IPv6 settings"

depend() {
	after net networking
	need net
}

start() {
	ebegin "Applying CLICD IPv6"
	/usr/local/sbin/clicd-ipv6-init
	eend $?
}
`
	if err := os.WriteFile(initPath, []byte(initScript), 0755); err != nil {
		return err
	}
	runlevelDir := filepath.Join(rootfsPath, "etc", "runlevels", "default")
	if err := os.MkdirAll(runlevelDir, 0755); err != nil {
		return err
	}
	return replaceSymlink(filepath.Join("..", "..", "init.d", "clicd-ipv6"), filepath.Join(runlevelDir, "clicd-ipv6"))
}

func installContainerIPv6SysV(rootfsPath string) error {
	initDir := filepath.Join(rootfsPath, "etc", "init.d")
	if err := os.MkdirAll(initDir, 0755); err != nil {
		return err
	}
	initPath := filepath.Join(initDir, "clicd-ipv6")
	initScript := `#!/bin/sh
### BEGIN INIT INFO
# Provides:          clicd-ipv6
# Required-Start:    $network
# Required-Stop:
# Default-Start:     2 3 4 5
# Default-Stop:
# Short-Description: CLICD IPv6 setup
### END INIT INFO

case "$1" in
	start|restart|force-reload)
		/usr/local/sbin/clicd-ipv6-init
		;;
	stop|status)
		exit 0
		;;
	*)
		echo "Usage: $0 {start|stop|restart|force-reload|status}"
		exit 1
		;;
esac
exit 0
`
	if err := os.WriteFile(initPath, []byte(initScript), 0755); err != nil {
		return err
	}
	for _, level := range []string{"2", "3", "4", "5"} {
		rcDir := filepath.Join(rootfsPath, "etc", "rc"+level+".d")
		if !dirExists(rcDir) {
			continue
		}
		if err := replaceSymlink(filepath.Join("..", "init.d", "clicd-ipv6"), filepath.Join(rcDir, "S99clicd-ipv6")); err != nil {
			return err
		}
	}
	return nil
}

func replaceSymlink(target, linkPath string) error {
	if current, err := os.Readlink(linkPath); err == nil && current == target {
		return nil
	}
	if err := os.Remove(linkPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(target, linkPath)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func ensureIPv6ForwardRules(ipv6 string) {
	rules := [][]string{
		{"FORWARD", "-i", "lxcbr0", "-s", ipv6 + "/128", "-j", "ACCEPT"},
		{"FORWARD", "-o", "lxcbr0", "-d", ipv6 + "/128", "-j", "ACCEPT"},
	}
	for _, rule := range rules {
		check := append([]string{"-C"}, rule...)
		add := append([]string{"-A"}, rule...)
		if exec.Command("ip6tables", check...).Run() != nil {
			exec.Command("ip6tables", add...).Run()
		}
	}
}

func containerIPv6ConnectivityOK(lxcName string) bool {
	targets := []string{"2606:4700:4700::1111", "2001:4860:4860::8888"}
	for _, target := range targets {
		if exec.Command("lxc-attach", "-n", lxcName, "--", "ping", "-6", "-c", "1", "-W", "2", target).Run() == nil {
			return true
		}
	}
	return false
}

func ensureIPv6NAT66(ipv6, uplink string) {
	if ipv6 == "" || uplink == "" {
		return
	}
	rule := []string{"POSTROUTING", "-s", ipv6 + "/128", "-o", uplink, "-j", "MASQUERADE"}
	check := append([]string{"-t", "nat", "-C"}, rule...)
	add := append([]string{"-t", "nat", "-A"}, rule...)
	if exec.Command("ip6tables", check...).Run() != nil {
		exec.Command("ip6tables", add...).Run()
	}
}

func removeIPv6NAT66(ipv6, uplink string) {
	if ipv6 == "" || uplink == "" {
		return
	}
	rule := []string{"POSTROUTING", "-s", ipv6 + "/128", "-o", uplink, "-j", "MASQUERADE"}
	del := append([]string{"-t", "nat", "-D"}, rule...)
	for exec.Command("ip6tables", del...).Run() == nil {
	}
}

func runQuiet(name string, args ...string) {
	_ = exec.Command(name, args...).Run()
}

func (m *Manager) AssignedIPv6Count() int {
	count := 0
	for _, c := range config.AppConfig.Containers {
		if strings.TrimSpace(c.IPv6) != "" {
			count++
		}
	}
	return count
}

func IPv6PrefixCapacity(prefixLen int) string {
	if prefixLen <= 0 || prefixLen > 128 {
		return "0"
	}
	hostBits := 128 - prefixLen
	if hostBits > 32 {
		return "large"
	}
	return strconv.FormatUint(uint64(1)<<uint(hostBits), 10)
}
