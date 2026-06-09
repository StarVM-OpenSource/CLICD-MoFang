package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"clicd/internal/api"
	"clicd/internal/cli"
	"clicd/internal/config"
	"clicd/internal/kvm"
	"clicd/internal/lxc"
	"clicd/internal/server"

	"golang.org/x/term"
)

func main() {
	isTerminal := term.IsTerminal(int(os.Stdin.Fd()))

	isServerMode := false
	isCliMode := false
	noWebAutostart := false
	for _, arg := range os.Args[1:] {
		if arg == "server" || arg == "-s" || arg == "--server" {
			isServerMode = true
		}
		if arg == "cli" || arg == "-c" || arg == "--cli" {
			isCliMode = true
		}
		if arg == "--no-web" || arg == "--cli-only" {
			noWebAutostart = true
			isCliMode = true
		}
	}

	// Initialize config
	cfg, err := config.InitConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize config: %v\n", err)
		os.Exit(1)
	}
	_ = cfg

	if isServerMode || (!isTerminal && !isCliMode) {
		// Restore persisted state
		api.RestoreTasks()
		api.RestoreLoginLogs()

		// Start security scanner
		api.InitScanner()
		api.StartSSLRenewalMonitor()

		// Ensure iptables FORWARD rules allow managed bridge traffic.
		lxc.EnsureForwardRules("lxcbr0")
		lxc.EnsureForwardRules("virbr0")
		lxc.EnsureAllAssignedPublicIPv4s()

		// Start expiry scanners (stops expired/over-traffic workloads every 30s)
		manager := lxc.NewManager()
		kvmManager := kvm.NewManager()
		manager.StartExpiryScanner()
		kvmManager.StartExpiryScanner()

		// Start usage monitors (computes CPU/network/disk rates every 5s)
		manager.StartUsageMonitor()
		kvmManager.StartUsageMonitor()
		kvmManager.StartNetworkSyncMonitor()
		kvmManager.StartIPv6Guard()

		// Start scheduled snapshot scanners.
		manager.StartSnapshotScheduler()
		kvmManager.StartSnapshotScheduler()

		// Clean up stale container configs (LXC dir was deleted but config remains)
		config.CleanStaleContainers()
		lxc.EnsureAllRunningPortMappings()

		// Pre-warm SSH for containers already running after host boot or service restart.
		manager.StartSSHWarmupScanner()

		// Run in server mode (frontend embedded in binary)
		if err := server.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
	} else {
		// CLI mode normally keeps the web panel available. Use --no-web to avoid
		// starting the systemd web service on locked-down hosts.
		if !noWebAutostart && !isWebPanelSystemdRunning() {
			startWebPanelSystemd()
		}

		// Run CLI interface
		cli.Run()
	}
}

func isWebPanelSystemdRunning() bool {
	cmd := exec.Command("systemctl", "is-active", "clicd")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "active"
}

func startWebPanelSystemd() {
	cmd := exec.Command("systemctl", "start", "clicd")
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", mainT("警告: 自动启动 Web 面板失败"), err)
	} else {
		fmt.Println(mainT("Web 面板已自动启动"))
	}
}

func mainT(text string) string {
	if !mainEnglish() {
		return text
	}
	switch text {
	case "警告: 自动启动 Web 面板失败":
		return "Warning: failed to auto-start web panel"
	case "Web 面板已自动启动":
		return "Web panel auto-started"
	default:
		return text
	}
}

func mainEnglish() bool {
	lang := strings.ToLower(strings.TrimSpace(os.Getenv("CLICD_LANG")))
	if lang == "en" || strings.HasPrefix(lang, "en_") || strings.HasPrefix(lang, "en-") {
		return true
	}
	if lang == "zh" || strings.HasPrefix(lang, "zh_") || strings.HasPrefix(lang, "zh-") {
		return false
	}
	return config.AppConfig != nil && config.NormalizeLanguage(config.AppConfig.Language) == "en"
}
