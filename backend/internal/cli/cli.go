package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"clicd/internal/config"
	"clicd/internal/lxc"
	"clicd/internal/version"
)

var manager = lxc.NewManager()

const (
	clicdBackupDir     = "/root/clicd-backups"
	clicdNewBinaryPath = "/usr/local/bin/clicd.new"
)

// Run starts the CLI interface.
func Run() {
	reader := bufio.NewReader(os.Stdin)

	for {
		if _, err := config.InitConfig(); err != nil {
			fmt.Printf("重新加载配置失败: %v\n", err)
			waitEnter(reader)
		}
		clearScreen()
		printMenu()
		fmt.Print("\n请选择操作 [1-12,0/q]: ")
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
			cliUpgradeSystem(reader)
			waitEnter(reader)
		case "12":
			clearScreen()
			cliUninstall(reader)
			return
		case "0":
			clearScreen()
			cliShowInfo()
			waitEnter(reader)
		case "q", "exit", "quit":
			fmt.Println("再见")
			return
		default:
			fmt.Println("无效选择")
		}
	}
}

func printMenu() {
	webStatus := "启动"
	if isWebPanelRunning() {
		webStatus = "停止"
	}
	fmt.Println()
	fmt.Println("  ==========================================")
	fmt.Println("       CLICD - LXC 容器管理器")
	fmt.Println("  ==========================================")
	fmt.Println()
	fmt.Printf("  Web 面板: %s (端口 %d)\n", func() string {
		if isWebPanelRunning() {
			return "运行中"
		}
		return "已停止"
	}(), config.AppConfig.Port)
	fmt.Printf("  当前版本: %s\n", version.Current())
	fmt.Println()
	fmt.Println("  1. 查看容器列表")
	fmt.Println("  2. 创建容器")
	fmt.Println("  3. 开机容器")
	fmt.Println("  4. 关机容器")
	fmt.Println("  5. 重启容器")
	fmt.Println("  6. 删除容器")
	fmt.Println("  7. 重装容器系统")
	fmt.Println("  8. 重置 Web 管理员密码")
	fmt.Printf("  9. %s Web 面板\n", webStatus)
	fmt.Println("  10. 导入现有 LXC 容器")
	fmt.Println("  11. 检查并升级 CLICD")
	fmt.Println("  12. 卸载 CLICD")
	fmt.Println("  0. 系统信息")
	fmt.Println("  q. 退出")
}

func cliListContainers() {
	containers, err := manager.ListContainers()
	if err != nil {
		fmt.Printf("获取容器列表失败: %v\n", err)
		return
	}

	if len(containers) == 0 {
		fmt.Println("\n暂无容器")
		return
	}

	fmt.Println()
	fmt.Printf("%-18s %-10s %-18s %-6s %-10s %-10s %-16s\n", "名称", "状态", "镜像", "vCPU", "内存(MB)", "磁盘(GB)", "SSH")
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
	fmt.Println("\n--- 创建容器 ---")

	name := promptString(reader, "容器名称", "")
	if name == "" {
		fmt.Println("容器名称不能为空")
		return
	}

	templates := lxc.GetTemplates()
	fmt.Println("\n可用镜像:")
	for i, template := range templates {
		fmt.Printf("  %d. %s\n", i+1, template.Name)
	}

	tmplIdx := promptInt(reader, fmt.Sprintf("镜像 [1-%d]", len(templates)), 1)
	if tmplIdx < 1 || tmplIdx > len(templates) {
		fmt.Println("镜像选择无效")
		return
	}

	cfg := lxc.ContainerConfig{
		Name:             name,
		TemplateID:       templates[tmplIdx-1].ID,
		VCPU:             promptFloat(reader, "vCPU", 1),
		RAMMB:            promptInt(reader, "内存 (MB)", 512),
		DiskGB:           promptInt(reader, "磁盘 (GB)", 10),
		NetworkBWMbps:    promptInt(reader, "网络带宽 (Mbps)", 100),
		MonthlyTrafficGB: promptInt(reader, "月流量 (GB)", 1000),
		IOSpeedMBps:      promptInt(reader, "IO 速度 (MB/s)", 500),
		ExtraPorts:       promptPortList(reader, "额外 NAT 端口，多个用逗号分隔"),
	}

	fmt.Printf("\n正在创建容器 %s ...\n", name)
	if err := manager.CreateContainer(cfg); err != nil {
		fmt.Printf("创建失败: %v\n", err)
		return
	}

	container := config.FindContainerByName(name)
	fmt.Printf("容器 %s 创建成功\n", name)
	if container != nil {
		fmt.Print(formatSSHAccess(container.SSHPort))
	}
	restartWebPanelForConfigChange()
}

func formatSSHAccess(sshPort int) string {
	if sshPort <= 0 {
		return "SSH: root, 端口未分配。密码已保存，请在 Web 面板中查看或重置。\n"
	}
	return fmt.Sprintf("SSH: root, port %d -> 22。密码已保存，请在 Web 面板中查看或重置。\n", sshPort)
}

func cliStartContainer(reader *bufio.Reader) {
	id, name := selectContainer(reader, "开机")
	if id == 0 {
		return
	}
	if err := manager.StartContainer(id); err != nil {
		fmt.Printf("开机失败: %v\n", err)
		return
	}
	fmt.Printf("容器 %s 已开机\n", name)
}

func cliStopContainer(reader *bufio.Reader) {
	id, name := selectContainer(reader, "关机")
	if id == 0 {
		return
	}
	if err := manager.StopContainer(id); err != nil {
		fmt.Printf("关机失败: %v\n", err)
		return
	}
	fmt.Printf("容器 %s 已关机\n", name)
}

func cliRestartContainer(reader *bufio.Reader) {
	id, name := selectContainer(reader, "重启")
	if id == 0 {
		return
	}
	if err := manager.RestartContainer(id); err != nil {
		fmt.Printf("重启失败: %v\n", err)
		return
	}
	fmt.Printf("容器 %s 已重启\n", name)
}

func cliDeleteContainer(reader *bufio.Reader) {
	id, name := selectContainer(reader, "删除")
	if id == 0 {
		return
	}
	confirm := promptString(reader, fmt.Sprintf("确认删除容器 %s？输入 yes 继续", name), "no")
	if strings.ToLower(confirm) != "yes" {
		fmt.Println("已取消")
		return
	}
	if err := manager.DestroyContainer(id); err != nil {
		fmt.Printf("删除失败: %v\n", err)
		return
	}
	fmt.Printf("容器 %s 已删除\n", name)
	restartWebPanelForConfigChange()
}

func cliReinstallContainer(reader *bufio.Reader) {
	id, name := selectContainer(reader, "重装")
	if id == 0 {
		return
	}

	templates := lxc.GetTemplates()
	fmt.Println("\n可用镜像:")
	for i, template := range templates {
		fmt.Printf("  %d. %s\n", i+1, template.Name)
	}

	tmplIdx := promptInt(reader, fmt.Sprintf("镜像 [1-%d]", len(templates)), 1)
	if tmplIdx < 1 || tmplIdx > len(templates) {
		fmt.Println("镜像选择无效")
		return
	}

	confirm := promptString(reader, fmt.Sprintf("确认重装容器 %s？输入 yes 继续", name), "no")
	if strings.ToLower(confirm) != "yes" {
		fmt.Println("已取消")
		return
	}

	if err := manager.ReinstallContainer(id, templates[tmplIdx-1].ID); err != nil {
		fmt.Printf("重装失败: %v\n", err)
		return
	}
	fmt.Printf("容器 %s 已重装\n", name)
	restartWebPanelForConfigChange()
}

func cliResetPassword(reader *bufio.Reader) {
	newPass := promptString(reader, "新的管理员密码（至少 6 位）", "")
	if len(newPass) < 6 {
		fmt.Println("密码至少需要 6 位")
		return
	}
	confirm := promptString(reader, "确认密码", "")
	if newPass != confirm {
		fmt.Println("两次输入的密码不一致")
		return
	}

	if err := config.ResetAdminPassword(newPass); err != nil {
		fmt.Printf("重置失败: %v\n", err)
		return
	}
	fmt.Println("管理员密码已重置。")
	restartWebPanelForConfigChange()
}

func cliToggleWebPanel() {
	if isWebPanelRunning() {
		if err := stopService("clicd"); err != nil {
			fmt.Printf("停止 Web 面板失败: %v\n", err)
			return
		}
		fmt.Println("Web 面板已停止，LXC 容器不会受影响。")
		return
	}

	if err := startService("clicd"); err != nil {
		fmt.Printf("启动 Web 面板失败: %v\n", err)
		return
	}
	fmt.Println("Web 面板已启动")
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func cliUpgradeSystem(reader *bufio.Reader) {
	fmt.Println("\n--- 检查并升级 CLICD ---")
	fmt.Println("升级只会替换 /usr/local/bin/clicd，并保留 /root/.clicd 里的配置、容器数据和任务记录。")

	if os.Geteuid() != 0 {
		fmt.Println("升级需要 root 权限。请使用: sudo clicd cli")
		return
	}

	repo := strings.TrimSpace(os.Getenv("CLICD_REPO"))
	if repo == "" {
		repo = version.Repo
	}
	current := version.Current()
	fmt.Printf("当前版本: %s\n", current)
	fmt.Printf("检查仓库: https://github.com/%s\n", repo)

	release, err := fetchLatestRelease(repo)
	if err != nil {
		fmt.Printf("检查 GitHub 最新版本失败: %v\n", err)
		return
	}
	latest := strings.TrimSpace(release.TagName)
	if latest == "" {
		fmt.Println("GitHub Release 没有 tag_name，无法判断最新版本。")
		return
	}
	fmt.Printf("最新版本: %s\n", latest)
	if release.HTMLURL != "" {
		fmt.Printf("发布页面: %s\n", release.HTMLURL)
	}

	assetURL := findReleaseAsset(release, "clicd-linux-amd64.tar.gz")
	if assetURL == "" {
		fmt.Println("最新 Release 没有找到 clicd-linux-amd64.tar.gz，无法自动升级。")
		return
	}

	if sameVersion(current, latest) {
		fmt.Println("当前已经是最新版本。")
		confirm := promptString(reader, "是否仍然重新安装最新版本？输入 reinstall 继续", "no")
		if strings.ToLower(confirm) != "reinstall" {
			fmt.Println("已取消。")
			return
		}
	} else {
		confirm := promptString(reader, "输入 upgrade 开始升级", "no")
		if strings.ToLower(confirm) != "upgrade" {
			fmt.Println("已取消。")
			return
		}
	}

	if err := upgradeFromReleaseAsset(assetURL, latest); err != nil {
		fmt.Printf("升级失败: %v\n", err)
		return
	}
	fmt.Printf("升级完成: %s -> %s\n", current, latest)
	fmt.Println("原有数据已保留，Web 服务已重启。")
}

func fetchLatestRelease(repo string) (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	setGitHubRequestHeaders(req)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if fallback, fallbackErr := fetchLatestReleaseFallback(repo); fallbackErr == nil {
			return fallback, nil
		}
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		apiErr := fmt.Errorf("GitHub API 返回 %s: %s", resp.Status, strings.TrimSpace(string(body)))
		if fallback, fallbackErr := fetchLatestReleaseFallback(repo); fallbackErr == nil {
			if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
				fmt.Println("GitHub API 被限流，已切换到备用检查方式。")
			} else {
				fmt.Println("GitHub API 不可用，已切换到备用检查方式。")
			}
			return fallback, nil
		}
		return nil, apiErr
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

func fetchLatestReleaseFallback(repo string) (*githubRelease, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://github.com/%s/releases/latest", repo), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "clicd-updater/"+version.Current())

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("GitHub releases/latest 返回 %s", resp.Status)
	}

	tag := latestTagFromPath(resp.Request.URL.Path)
	if tag == "" {
		return nil, fmt.Errorf("无法从 GitHub releases/latest 跳转结果解析最新版本")
	}

	const assetName = "clicd-linux-amd64.tar.gz"
	return &githubRelease{
		TagName: tag,
		Name:    tag,
		HTMLURL: fmt.Sprintf("https://github.com/%s/releases/tag/%s", repo, tag),
		Assets: []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		}{
			{
				Name:               assetName,
				BrowserDownloadURL: fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", repo, assetName),
			},
		},
	}, nil
}

func latestTagFromPath(path string) string {
	const marker = "/releases/tag/"
	idx := strings.Index(path, marker)
	if idx < 0 {
		return ""
	}
	tag := strings.TrimSpace(path[idx+len(marker):])
	if slash := strings.Index(tag, "/"); slash >= 0 {
		tag = tag[:slash]
	}
	return tag
}

func setGitHubRequestHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "clicd-updater/"+version.Current())
	token := strings.TrimSpace(os.Getenv("CLICD_GITHUB_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func findReleaseAsset(release *githubRelease, name string) string {
	for _, asset := range release.Assets {
		if asset.Name == name && asset.BrowserDownloadURL != "" {
			return asset.BrowserDownloadURL
		}
	}
	return ""
}

func upgradeFromReleaseAsset(assetURL, latest string) error {
	tmpDir, err := os.MkdirTemp("", "clicd-upgrade-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, "clicd-linux-amd64.tar.gz")
	fmt.Println("正在下载升级包...")
	if err := downloadFile(assetURL, archivePath); err != nil {
		return err
	}

	fmt.Println("正在解压升级包...")
	if out, err := exec.Command("tar", "-xzf", archivePath, "-C", tmpDir).CombinedOutput(); err != nil {
		return fmt.Errorf("解压失败: %v, output: %s", err, string(out))
	}

	newBinary, err := findFile(tmpDir, "clicd")
	if err != nil {
		return err
	}

	backupDir := clicdBackupDir
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return err
	}
	backupName := fmt.Sprintf("clicd.%s.%s", safeReleaseBackupComponent(latest), time.Now().Format("20060102-150405"))
	if _, err := os.Stat("/usr/local/bin/clicd"); err == nil {
		backupPath, err := copyFileToBackup("/usr/local/bin/clicd", backupName, 0755)
		if err != nil {
			return fmt.Errorf("备份旧二进制失败: %w", err)
		}
		fmt.Printf("旧版本已备份: %s\n", backupPath)
	}

	fmt.Println("正在替换二进制...")
	if err := stopService("clicd"); err != nil {
		fmt.Printf("停止 Web 服务失败，继续尝试替换: %v\n", err)
	}
	tmpBin := clicdNewBinaryPath
	if err := copyFileToUpgradeTemp(newBinary, 0755); err != nil {
		return err
	}
	if err := os.Rename(tmpBin, "/usr/local/bin/clicd"); err != nil {
		return err
	}
	if err := os.Chmod("/usr/local/bin/clicd", 0755); err != nil {
		return err
	}

	if err := restartService("clicd"); err != nil {
		return fmt.Errorf("二进制已替换，但重启 Web 服务失败: %w", err)
	}
	return nil
}

func downloadFile(url, dest string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	setGitHubRequestHeaders(req)
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败，HTTP %s", resp.Status)
	}

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

func findFile(root, name string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == name {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("升级包内未找到 clicd 二进制")
	}
	return found, nil
}

func copyFileToBackup(src, fileName string, mode os.FileMode) (string, error) {
	if fileName == "" || strings.Contains(fileName, "/") || strings.Contains(fileName, "\\") || strings.Contains(fileName, "..") {
		return "", fmt.Errorf("unsafe backup file name: %s", fileName)
	}
	dst := filepath.Join(clicdBackupDir, fileName)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return "", err
	}
	if err := copyIntoOpenFile(src, out, mode); err != nil {
		return "", err
	}
	return dst, nil
}

func copyFileToUpgradeTemp(src string, mode os.FileMode) error {
	out, err := os.OpenFile(clicdNewBinaryPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	return copyIntoOpenFile(src, out, mode)
}

func copyIntoOpenFile(src string, out *os.File, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		out.Close()
		return err
	}
	defer in.Close()

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Chmod(mode); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return nil
}

func safeReleaseBackupComponent(tag string) string {
	tag = strings.TrimPrefix(strings.TrimSpace(tag), "v")
	var b strings.Builder
	for _, r := range tag {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	component := strings.Trim(b.String(), "._-")
	if component == "" {
		return "unknown"
	}
	if len(component) > 64 {
		return component[:64]
	}
	return component
}

func sameVersion(current, latest string) bool {
	c := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(current)), "v")
	l := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(latest)), "v")
	return c != "" && c == l
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
	fmt.Println("\n--- 导入现有 LXC 容器 ---")
	fmt.Println("将 /var/lib/lxc 里的容器导入 CLICD 配置。")
	fmt.Println("导入后会保留真实 LXC 名称，Web 和 CLI 都能管理同一个容器。")

	imported, err := manager.ImportExistingClicdContainers()
	if err != nil {
		fmt.Printf("导入失败: %v\n", err)
		return
	}
	if len(imported) == 0 {
		fmt.Println("没有发现新的 ct-* 容器。")
		return
	}

	fmt.Printf("已导入 %d 个容器:\n", len(imported))
	for _, c := range imported {
		fmt.Printf("  [%d] %s [%s]\n", c.ID, c.Name, c.Status)
	}
	restartWebPanelForConfigChange()
}

func cliUninstall(reader *bufio.Reader) {
	fmt.Println("\n--- 卸载 CLICD ---")
	fmt.Println("将删除 CLICD 服务和 /usr/local/bin/clicd。")
	fmt.Println("同时会删除 /root/.clicd、/var/lib/lxc、/var/lib/clicd、镜像缓存、备份、临时文件、/swapfile 和 CLICD 网络规则。")

	if os.Geteuid() != 0 {
		fmt.Println("卸载需要 root 权限。")
		fmt.Println("请运行: sudo clicd cli --no-web")
		return
	}

	confirm := promptString(reader, "输入 uninstall 继续卸载", "no")
	if strings.ToLower(confirm) != "uninstall" {
		fmt.Println("已取消")
		return
	}

	destroyAllLXCContainers()
	destroyAllKVMDomains()
	cleanupCLICDNetworking()
	removeCLICDHostHooks()
	removeCLICDQuotaRecords()
	stopAndRemoveService()
	removePath("/usr/local/bin/clicd")
	removePath("/etc/sysctl.d/99-clicd.conf")
	removePath("/var/log/clicd.log")
	removePath("/var/log/clicd.err")
	removePath("/root/.clicd")
	removePath("/var/lib/lxc")
	removePath("/var/lib/clicd")
	removePath("/var/cache/lxc")
	removePath("/var/cache/clicd")
	removePath("/root/clicd-backups")
	removeCLICDTmpFiles()
	removeCLICDSwapfile()

	reloadSysctl()

	fmt.Println()
	fmt.Println("CLICD 已卸载。")
	fmt.Println("服务、二进制、配置、容器/虚拟机、本地镜像、缓存、备份、临时文件和 CLICD 网络规则均已删除。")
}

func destroyAllLXCContainers() {
	entries, err := os.ReadDir("/var/lib/lxc")
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		fmt.Printf("Destroying LXC container %s...\n", name)
		runQuiet("lxc-stop", "-n", name, "-k")
		runQuiet("lxc-destroy", "-n", name, "-f")
		removeLXCContainerPath("/var/lib/lxc/" + name)
	}
}

func destroyAllKVMDomains() {
	if !commandExists("virsh") {
		return
	}
	out, err := exec.Command("virsh", "list", "--all", "--name").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(line)
		if isCLICDKVMDomain(name) {
			removeKVMDomain(name)
		}
	}
}

func isCLICDKVMDomain(name string) bool {
	if !strings.HasPrefix(name, "vm-") || len(name) <= len("vm-") {
		return false
	}
	for _, r := range strings.TrimPrefix(name, "vm-") {
		if r < '0' || r > '9' {
			return false
		}
	}
	if dirExists("/var/lib/clicd/kvm/instances/" + name) {
		return true
	}
	out, err := exec.Command("virsh", "dumpxml", name).Output()
	return err == nil && strings.Contains(string(out), "/var/lib/clicd/kvm/")
}

func removeKVMDomain(name string) {
	fmt.Printf("Removing KVM domain %s...\n", name)
	runQuiet("virsh", "destroy", name)
	if runCommandOK("virsh", "undefine", name, "--remove-all-storage", "--nvram") {
		return
	}
	if runCommandOK("virsh", "undefine", name, "--nvram") {
		return
	}
	runQuiet("virsh", "undefine", name)
}

func cleanupCLICDNetworking() {
	removeCLICDNATRules()
	for _, bridge := range []string{"lxcbr0", "virbr0"} {
		deleteFilterRule("FORWARD", "-i", bridge, "-j", "ACCEPT")
		deleteFilterRule("FORWARD", "-o", bridge, "-j", "ACCEPT")
		deleteFilterRule("FORWARD", "-i", bridge, "-o", bridge, "-j", "ACCEPT")
		deleteIP6TablesBridgeRules(bridge)
	}
}

func removeCLICDNATRules() {
	if commandExists("iptables") {
		for {
			out, err := exec.Command("sh", "-c", "iptables -t nat -L PREROUTING -n --line-numbers 2>/dev/null | grep 'clicd-' | awk '{print $1}' | head -n 1").Output()
			line := strings.TrimSpace(string(out))
			if err != nil || line == "" {
				break
			}
			if !runCommandOK("iptables", "-t", "nat", "-D", "PREROUTING", line) {
				break
			}
		}
		deleteNATRule("POSTROUTING", "-s", "10.0.3.0/24", "-o", "eth+", "-j", "MASQUERADE")
		deleteNATRule("POSTROUTING", "-s", "192.168.122.0/24", "-o", "eth+", "-j", "MASQUERADE")
	}
}

func deleteNATRule(args ...string) {
	fullArgs := append([]string{"-t", "nat", "-D"}, args...)
	for runCommandOK("iptables", fullArgs...) {
	}
}

func deleteFilterRule(args ...string) {
	fullArgs := append([]string{"-D"}, args...)
	for runCommandOK("iptables", fullArgs...) {
	}
}

func deleteIP6TablesBridgeRules(bridge string) {
	if !commandExists("ip6tables") {
		return
	}
	for {
		cmd := fmt.Sprintf("ip6tables -S FORWARD 2>/dev/null | grep -- %s | sed 's/^-A /-D /' | head -n 1", shellQuote(bridge))
		out, err := exec.Command("sh", "-c", cmd).Output()
		rule := strings.TrimSpace(string(out))
		if err != nil || rule == "" {
			return
		}
		if !runCommandOK("sh", "-c", "ip6tables "+rule) {
			return
		}
	}
}

func removeCLICDHostHooks() {
	runQuiet("systemctl", "stop", "clicd-kvm-ipv6.service")
	runQuiet("systemctl", "disable", "clicd-kvm-ipv6.service")
	runQuiet("rc-service", "clicd-kvm-ipv6", "stop")
	runQuiet("rc-update", "del", "clicd-kvm-ipv6", "default")
	removePath("/usr/local/sbin/clicd-kvm-ipv6-init")
	removePath("/etc/systemd/system/clicd-kvm-ipv6.service")
	removePath("/etc/local.d/clicd-kvm-ipv6.start")
	removePath("/etc/network/if-up.d/clicd-kvm-ipv6")
}

func removeCLICDQuotaRecords() {
	for _, path := range []string{"/etc/projects", "/etc/projid"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var kept []string
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == "" || strings.Contains(line, "clicd-") {
				continue
			}
			kept = append(kept, line)
		}
		_ = os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0644)
	}
}

func removeCLICDTmpFiles() {
	for _, pattern := range []string{"/tmp/clicd-*", "/tmp/clicd.*"} {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			removePath(path)
		}
	}
}

func removeCLICDSwapfile() {
	if !fileExists("/swapfile") {
		return
	}
	runQuiet("swapoff", "/swapfile")
	removePath("/swapfile")
}

func removeLXCContainerPath(path string) {
	unmountPathTree(path)
	detachLoopDevices(path)
	if err := os.RemoveAll(path); err == nil {
		fmt.Printf("Removed %s\n", path)
		return
	}

	runQuiet("fuser", "-km", path+"/rootfs")
	runQuiet("fuser", "-km", path)
	unmountPathTree(path)
	detachLoopDevices(path)
	removePath(path)
}

func unmountPathTree(path string) {
	if commandExists("findmnt") {
		out, err := exec.Command("findmnt", "-R", "-n", "-o", "TARGET", path).Output()
		if err == nil {
			mounts := strings.Split(strings.TrimSpace(string(out)), "\n")
			for i := len(mounts) - 1; i >= 0; i-- {
				mountpoint := strings.TrimSpace(mounts[i])
				if mountpoint != "" {
					runQuiet("umount", "-R", "-l", mountpoint)
					runQuiet("umount", "-l", mountpoint)
				}
			}
		}
	}
	runQuiet("umount", "-R", "-l", path+"/rootfs")
	runQuiet("umount", "-l", path+"/rootfs")
	runQuiet("umount", "-R", "-l", path)
	runQuiet("umount", "-l", path)
}

func detachLoopDevices(path string) {
	if !commandExists("losetup") {
		return
	}
	images := []string{path + "/rootfs.img"}
	if entries, err := os.ReadDir(path); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".img") {
				images = append(images, path+"/"+entry.Name())
			}
		}
	}
	for _, image := range images {
		out, err := exec.Command("losetup", "-j", image).Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if idx := strings.Index(line, ":"); idx > 0 {
				runQuiet("losetup", "-d", line[:idx])
			}
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

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
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

func runCommandOK(name string, args ...string) bool {
	return exec.Command(name, args...).Run() == nil
}

func runQuiet(name string, args ...string) {
	_ = exec.Command(name, args...).Run()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func restartWebPanelForConfigChange() {
	if err := restartService("clicd"); err != nil {
		fmt.Printf("Web 面板重载跳过: %v\n", err)
		return
	}
	fmt.Println("Web 面板已重载并应用配置变更。")
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
		fmt.Printf("读取容器状态失败: %v\n", err)
	}

	total := len(containers)
	running := 0
	for _, container := range containers {
		if container.Status == "running" {
			running++
		}
	}

	fmt.Println("\n--- 系统信息 ---")
	fmt.Printf("CLICD 版本: %s\n", version.Current())
	fmt.Printf("Web 端口: %d\n", config.AppConfig.Port)
	fmt.Printf("管理员用户: %s\n", config.AppConfig.AdminUser)
	fmt.Printf("容器总数: %d\n", total)
	fmt.Printf("运行中: %d\n", running)
	fmt.Printf("已停止: %d\n", total-running)

	if hostname, err := os.Hostname(); err == nil {
		fmt.Printf("主机名: %s\n", hostname)
	}

	cmd := exec.Command("lxc-info", "--version")
	output, err := cmd.Output()
	if err == nil {
		fmt.Printf("LXC 版本: %s", string(output))
	}
}

func selectContainer(reader *bufio.Reader, action string) (int, string) {
	containers, err := manager.ListContainers()
	if err != nil {
		fmt.Printf("获取容器列表失败: %v\n", err)
		return 0, ""
	}
	if len(containers) == 0 {
		fmt.Println("暂无可用容器")
		return 0, ""
	}

	fmt.Printf("\n--- 选择要%s的容器 ---\n", action)
	for i, container := range containers {
		fmt.Printf("  %d. [%d] %s [%s]\n", i+1, container.ID, container.Name, container.Status)
	}

	idx := promptInt(reader, "容器", 0)
	if idx < 1 || idx > len(containers) {
		fmt.Println("选择无效")
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
	fmt.Print("\n按 Enter 返回菜单...")
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
			fmt.Printf("忽略无效端口: %s\n", strings.TrimSpace(part))
			continue
		}
		ports = append(ports, value)
	}
	return ports
}
