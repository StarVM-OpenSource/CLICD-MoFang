package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"clicd/internal/config"
	"clicd/internal/lxc"
)

var manager = lxc.NewManager()

// Run starts the CLI interface.
func Run() {
	reader := bufio.NewReader(os.Stdin)

	for {
		if _, err := config.InitConfig(); err != nil {
			fmt.Printf("Failed to reload config: %v\n", err)
			waitEnter(reader)
		}
		clearScreen()
		printMenu()
		fmt.Print("\nSelect action [1-11,0/q]: ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		switch strings.ToLower(input) {
		case "1":
			clearScreen()
			cliListContainers()
			waitEnter(reader)
		case "2":
			clearScreen()
			cliCreateContainer(reader)
			waitEnter(reader)
		case "3":
			clearScreen()
			cliStartContainer(reader)
			waitEnter(reader)
		case "4":
			clearScreen()
			cliStopContainer(reader)
			waitEnter(reader)
		case "5":
			clearScreen()
			cliRestartContainer(reader)
			waitEnter(reader)
		case "6":
			clearScreen()
			cliDeleteContainer(reader)
			waitEnter(reader)
		case "7":
			clearScreen()
			cliReinstallContainer(reader)
			waitEnter(reader)
		case "8":
			clearScreen()
			cliResetPassword(reader)
			waitEnter(reader)
		case "9":
			clearScreen()
			cliToggleWebPanel()
			waitEnter(reader)
		case "10":
			clearScreen()
			cliImportExistingContainers()
			waitEnter(reader)
		case "11":
			clearScreen()
			cliUninstall(reader)
			return
		case "0":
			clearScreen()
			cliShowInfo()
			waitEnter(reader)
		case "q", "exit", "quit":
			fmt.Println("Bye")
			return
		default:
			fmt.Println("Invalid selection")
		}
	}
}

func printMenu() {
	webStatus := "start"
	if isWebPanelRunning() {
		webStatus = "stop"
	}
	fmt.Println()
	fmt.Println("  ==========================================")
	fmt.Println("       CLICD - LXC Container Manager")
	fmt.Println("  ==========================================")
	fmt.Println()
	fmt.Printf("  Web panel: %s (port %d)\n", func() string {
		if isWebPanelRunning() {
			return "running"
		}
		return "stopped"
	}(), config.AppConfig.Port)
	fmt.Println()
	fmt.Println("  1. List containers")
	fmt.Println("  2. Create container")
	fmt.Println("  3. Start container")
	fmt.Println("  4. Stop container")
	fmt.Println("  5. Restart container")
	fmt.Println("  6. Delete container")
	fmt.Println("  7. Reinstall container")
	fmt.Println("  8. Reset web admin password")
	fmt.Printf("  9. %s web panel\n", webStatus)
	fmt.Println("  10. Import existing LXC containers")
	fmt.Println("  11. Uninstall CLICD")
	fmt.Println("  0. System info")
	fmt.Println("  q. Quit")
}

func cliListContainers() {
	containers, err := manager.ListContainers()
	if err != nil {
		fmt.Printf("Failed to list containers: %v\n", err)
		return
	}

	if len(containers) == 0 {
		fmt.Println("\nNo containers")
		return
	}

	fmt.Println()
	fmt.Printf("%-18s %-10s %-18s %-6s %-10s %-10s %-16s\n", "Name", "Status", "Template", "vCPU", "RAM(MB)", "Disk(GB)", "SSH")
	fmt.Println(strings.Repeat("-", 94))
	for _, c := range containers {
		ssh := "-"
		if c.SSHPort > 0 {
			ssh = fmt.Sprintf("%d->22", c.SSHPort)
		}
		fmt.Printf("%-18s %-10s %-18s %-6.2f %-10d %-10d %-16s\n",
			c.Name, c.Status, c.Template, c.VCPU, c.RAMMB, c.DiskGB, ssh)
	}
}

func cliCreateContainer(reader *bufio.Reader) {
	fmt.Println("\n--- Create container ---")

	name := promptString(reader, "Container name", "")
	if name == "" {
		fmt.Println("Container name is required")
		return
	}

	templates := lxc.GetTemplates()
	fmt.Println("\nAvailable templates:")
	for i, template := range templates {
		fmt.Printf("  %d. %s\n", i+1, template.Name)
	}

	tmplIdx := promptInt(reader, fmt.Sprintf("Template [1-%d]", len(templates)), 1)
	if tmplIdx < 1 || tmplIdx > len(templates) {
		fmt.Println("Invalid template selection")
		return
	}

	cfg := lxc.ContainerConfig{
		Name:             name,
		TemplateID:       templates[tmplIdx-1].ID,
		VCPU:             promptFloat(reader, "vCPU", 1),
		RAMMB:            promptInt(reader, "Memory (MB)", 512),
		DiskGB:           promptInt(reader, "Disk (GB)", 10),
		NetworkBWMbps:    promptInt(reader, "Network bandwidth (Mbps)", 100),
		MonthlyTrafficGB: promptInt(reader, "Monthly traffic (GB)", 1000),
		IOSpeedMBps:      promptInt(reader, "IO speed (MB/s)", 500),
		ExtraPorts:       promptPortList(reader, "Extra NAT ports, comma separated"),
	}

	fmt.Printf("\nCreating container %s ...\n", name)
	if err := manager.CreateContainer(cfg); err != nil {
		fmt.Printf("Create failed: %v\n", err)
		return
	}

	container := config.FindContainerByName(name)
	fmt.Printf("Container %s created successfully\n", name)
	if container != nil {
		fmt.Printf("SSH: root / %s, port %d -> 22\n", container.SSHPassword, container.SSHPort)
	}
	restartWebPanelForConfigChange()
}

func cliStartContainer(reader *bufio.Reader) {
	id, name := selectContainer(reader, "start")
	if id == 0 {
		return
	}
	if err := manager.StartContainer(id); err != nil {
		fmt.Printf("Start failed: %v\n", err)
		return
	}
	fmt.Printf("Container %s started\n", name)
}

func cliStopContainer(reader *bufio.Reader) {
	id, name := selectContainer(reader, "stop")
	if id == 0 {
		return
	}
	if err := manager.StopContainer(id); err != nil {
		fmt.Printf("Stop failed: %v\n", err)
		return
	}
	fmt.Printf("Container %s stopped\n", name)
}

func cliRestartContainer(reader *bufio.Reader) {
	id, name := selectContainer(reader, "restart")
	if id == 0 {
		return
	}
	if err := manager.RestartContainer(id); err != nil {
		fmt.Printf("Restart failed: %v\n", err)
		return
	}
	fmt.Printf("Container %s restarted\n", name)
}

func cliDeleteContainer(reader *bufio.Reader) {
	id, name := selectContainer(reader, "delete")
	if id == 0 {
		return
	}
	confirm := promptString(reader, fmt.Sprintf("Delete container %s? Type yes", name), "no")
	if strings.ToLower(confirm) != "yes" {
		fmt.Println("Canceled")
		return
	}
	if err := manager.DestroyContainer(id); err != nil {
		fmt.Printf("Delete failed: %v\n", err)
		return
	}
	fmt.Printf("Container %s deleted\n", name)
	restartWebPanelForConfigChange()
}

func cliReinstallContainer(reader *bufio.Reader) {
	id, name := selectContainer(reader, "reinstall")
	if id == 0 {
		return
	}

	templates := lxc.GetTemplates()
	fmt.Println("\nAvailable templates:")
	for i, template := range templates {
		fmt.Printf("  %d. %s\n", i+1, template.Name)
	}

	tmplIdx := promptInt(reader, fmt.Sprintf("Template [1-%d]", len(templates)), 1)
	if tmplIdx < 1 || tmplIdx > len(templates) {
		fmt.Println("Invalid template selection")
		return
	}

	confirm := promptString(reader, fmt.Sprintf("Reinstall container %s? Type yes", name), "no")
	if strings.ToLower(confirm) != "yes" {
		fmt.Println("Canceled")
		return
	}

	if err := manager.ReinstallContainer(id, templates[tmplIdx-1].ID); err != nil {
		fmt.Printf("Reinstall failed: %v\n", err)
		return
	}
	fmt.Printf("Container %s reinstalled\n", name)
	restartWebPanelForConfigChange()
}

func cliResetPassword(reader *bufio.Reader) {
	newPass := promptString(reader, "New admin password (at least 6 chars)", "")
	if len(newPass) < 6 {
		fmt.Println("Password must be at least 6 chars")
		return
	}
	confirm := promptString(reader, "Confirm password", "")
	if newPass != confirm {
		fmt.Println("Passwords do not match")
		return
	}

	if err := config.ResetAdminPassword(newPass); err != nil {
		fmt.Printf("Reset failed: %v\n", err)
		return
	}
	fmt.Println("Admin password reset.")
	restartWebPanelForConfigChange()
}

func cliToggleWebPanel() {
	if isWebPanelRunning() {
		if err := stopService("clicd"); err != nil {
			fmt.Printf("Failed to stop web panel: %v\n", err)
			return
		}
		fmt.Println("Web panel stopped. LXC containers are not affected.")
		return
	}

	if err := startService("clicd"); err != nil {
		fmt.Printf("Failed to start web panel: %v\n", err)
		return
	}
	fmt.Println("Web panel started")
}

func isWebPanelRunning() bool {
	if commandExists("systemctl") {
		cmd := exec.Command("systemctl", "is-active", "clicd")
		output, err := cmd.Output()
		if err == nil && strings.TrimSpace(string(output)) == "active" {
			return true
		}
	}
	if commandExists("rc-service") {
		cmd := exec.Command("rc-service", "clicd", "status")
		return cmd.Run() == nil
	}
	return false
}

func cliImportExistingContainers() {
	fmt.Println("\n--- Import existing LXC containers ---")
	fmt.Println("This imports containers from /var/lib/lxc into CLICD config.")
	fmt.Println("Imported containers keep their real LXC name so Web and CLI manage the same container.")

	imported, err := manager.ImportExistingClicdContainers()
	if err != nil {
		fmt.Printf("Import failed: %v\n", err)
		return
	}
	if len(imported) == 0 {
		fmt.Println("No new ct-* containers found to import.")
		return
	}

	fmt.Printf("Imported %d container(s):\n", len(imported))
	for _, c := range imported {
		fmt.Printf("  [%d] %s [%s]\n", c.ID, c.Name, c.Status)
	}
	restartWebPanelForConfigChange()
}

func cliUninstall(reader *bufio.Reader) {
	fmt.Println("\n--- Uninstall CLICD ---")
	fmt.Println("This removes the CLICD service and /usr/local/bin/clicd.")
	fmt.Println("LXC containers and /root/.clicd are kept unless you explicitly choose to delete them.")

	if os.Geteuid() != 0 {
		fmt.Println("Uninstall must be run as root.")
		fmt.Println("Run: sudo clicd cli --no-web")
		return
	}

	confirm := promptString(reader, "Type uninstall to continue", "no")
	if strings.ToLower(confirm) != "uninstall" {
		fmt.Println("Canceled")
		return
	}

	removeData := strings.ToLower(promptString(reader, "Delete /root/.clicd config/data? Type delete-data", "no")) == "delete-data"
	removeContainers := strings.ToLower(promptString(reader, "Destroy CLICD-managed LXC containers? Type delete-containers", "no")) == "delete-containers"

	if removeContainers {
		destroyManagedContainers()
	}

	stopAndRemoveService()
	removePath("/usr/local/bin/clicd")
	removePath("/etc/sysctl.d/99-clicd.conf")
	removePath("/var/log/clicd.log")
	removePath("/var/log/clicd.err")

	if removeData {
		removePath("/root/.clicd")
	}

	reloadSysctl()

	fmt.Println()
	fmt.Println("CLICD has been uninstalled.")
	if !removeData {
		fmt.Println("Kept data: /root/.clicd")
	}
	if !removeContainers {
		fmt.Println("Kept LXC containers under /var/lib/lxc")
	}
}

func destroyManagedContainers() {
	containers := append([]config.Container(nil), config.AppConfig.Containers...)
	if len(containers) == 0 {
		fmt.Println("No CLICD-managed containers found in config.")
		return
	}

	for _, c := range containers {
		fmt.Printf("Destroying container %s...\n", c.Name)
		if err := manager.DestroyContainer(c.ID); err != nil {
			fmt.Printf("Failed to destroy %s: %v\n", c.Name, err)
		}
	}
}

func stopAndRemoveService() {
	if commandExists("systemctl") {
		runQuiet("systemctl", "stop", "clicd")
		runQuiet("systemctl", "disable", "clicd")
		removePath("/etc/systemd/system/clicd.service")
		runQuiet("systemctl", "daemon-reload")
		runQuiet("systemctl", "reset-failed", "clicd")
	}

	if commandExists("rc-service") {
		runQuiet("rc-service", "clicd", "stop")
	}
	if commandExists("rc-update") {
		runQuiet("rc-update", "del", "clicd", "default")
	}
	removePath("/etc/init.d/clicd")
}

func removePath(path string) {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return
	}
	if err := os.RemoveAll(path); err != nil {
		fmt.Printf("Failed to remove %s: %v\n", path, err)
		return
	}
	fmt.Printf("Removed %s\n", path)
}

func reloadSysctl() {
	if commandExists("sysctl") {
		runQuiet("sysctl", "--system")
	}
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func runQuiet(name string, args ...string) {
	_ = exec.Command(name, args...).Run()
}

func restartWebPanelForConfigChange() {
	if err := restartService("clicd"); err != nil {
		fmt.Printf("Web panel reload skipped: %v\n", err)
		return
	}
	fmt.Println("Web panel reloaded to pick up config changes.")
}

func stopService(name string) error {
	if commandExists("systemctl") {
		return exec.Command("systemctl", "stop", name).Run()
	}
	if commandExists("rc-service") {
		return exec.Command("rc-service", name, "stop").Run()
	}
	return fmt.Errorf("no supported service manager found")
}

func startService(name string) error {
	if commandExists("systemctl") {
		return exec.Command("systemctl", "start", name).Run()
	}
	if commandExists("rc-service") {
		return exec.Command("rc-service", name, "start").Run()
	}
	return fmt.Errorf("no supported service manager found")
}

func restartService(name string) error {
	if commandExists("systemctl") {
		return exec.Command("systemctl", "restart", name).Run()
	}
	if commandExists("rc-service") {
		return exec.Command("rc-service", name, "restart").Run()
	}
	return fmt.Errorf("no supported service manager found")
}

func cliShowInfo() {
	containers, err := manager.ListContainers()
	if err != nil {
		fmt.Printf("Failed to read container status: %v\n", err)
	}

	total := len(containers)
	running := 0
	for _, container := range containers {
		if container.Status == "running" {
			running++
		}
	}

	fmt.Println("\n--- System info ---")
	fmt.Printf("Web port: %d\n", config.AppConfig.Port)
	fmt.Printf("Admin user: %s\n", config.AppConfig.AdminUser)
	fmt.Printf("Containers: %d\n", total)
	fmt.Printf("Running: %d\n", running)
	fmt.Printf("Stopped: %d\n", total-running)

	if hostname, err := os.Hostname(); err == nil {
		fmt.Printf("Hostname: %s\n", hostname)
	}

	cmd := exec.Command("lxc-info", "--version")
	output, err := cmd.Output()
	if err == nil {
		fmt.Printf("LXC version: %s", string(output))
	}
}

func selectContainer(reader *bufio.Reader, action string) (int, string) {
	containers, err := manager.ListContainers()
	if err != nil {
		fmt.Printf("Failed to list containers: %v\n", err)
		return 0, ""
	}
	if len(containers) == 0 {
		fmt.Println("No containers available")
		return 0, ""
	}

	fmt.Printf("\n--- Select container to %s ---\n", action)
	for i, container := range containers {
		fmt.Printf("  %d. [%d] %s [%s]\n", i+1, container.ID, container.Name, container.Status)
	}

	idx := promptInt(reader, "Container", 0)
	if idx < 1 || idx > len(containers) {
		fmt.Println("Invalid selection")
		return 0, ""
	}

	c := containers[idx-1]
	return c.ID, c.Name
}

func promptString(reader *bufio.Reader, label string, fallback string) string {
	if fallback == "" {
		fmt.Printf("%s: ", label)
	} else {
		fmt.Printf("%s [%s]: ", label, fallback)
	}

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return fallback
	}
	return input
}

func promptInt(reader *bufio.Reader, label string, fallback int) int {
	input := promptString(reader, label, strconv.Itoa(fallback))
	value, err := strconv.Atoi(input)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func promptFloat(reader *bufio.Reader, label string, fallback float64) float64 {
	input := promptString(reader, label, strconv.FormatFloat(fallback, 'f', -1, 64))
	value, err := strconv.ParseFloat(input, 64)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}

func waitEnter(reader *bufio.Reader) {
	fmt.Print("\nPress Enter to return to menu...")
	reader.ReadString('\n')
}

func promptPortList(reader *bufio.Reader, label string) []int {
	input := promptString(reader, label, "")
	if input == "" {
		return nil
	}

	var ports []int
	for _, part := range strings.Split(input, ",") {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || value <= 0 || value > 65535 {
			fmt.Printf("Ignoring invalid port: %s\n", strings.TrimSpace(part))
			continue
		}
		ports = append(ports, value)
	}
	return ports
}
