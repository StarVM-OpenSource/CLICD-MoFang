#!/bin/sh
set -eu

REPO="${CLICD_REPO:-MengMengCode/CLICD}"
CLICD_INSTALL_VERSION="${CLICD_VERSION:-latest}"
ASSET="clicd-linux-amd64.tar.gz"
ACTION="${1:-install}"
ACTION_CONFIRM="${2:-}"
ISSUE_URL="https://github.com/${REPO}/issues"
LOG_FILE="${CLICD_LOG_FILE:-/var/log/clicd-install.log}"

echo "====================================="
echo "  CLICD 中文安装/卸载脚本"
echo "====================================="

write_log_file() {
    if [ "$(id -u 2>/dev/null || echo 1)" = "0" ]; then
        printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S' 2>/dev/null || true)" "$*" >> "$LOG_FILE" 2>/dev/null || true
    fi
}

log() {
    echo "[clicd] $*"
    write_log_file "[clicd] $*"
}

warn() {
    echo "[clicd][警告] $*" >&2
    write_log_file "[警告] $*"
}

die() {
    echo "[clicd][错误] $*" >&2
    write_log_file "[错误] $*"
    echo "" >&2
    echo "安装/卸载未完成。请查看日志：$LOG_FILE" >&2
    echo "如果你确认这是程序问题，请提交 issue：$ISSUE_URL" >&2
    exit 1
}

has_cmd() {
    command -v "$1" >/dev/null 2>&1
}

is_systemd() {
    has_cmd systemctl && [ -d /run/systemd/system ]
}

is_openrc() {
    has_cmd rc-service && has_cmd rc-update
}

run_step() {
    step_name="$1"
    shift
    log "开始：$step_name"
    if "$@" >> "$LOG_FILE" 2>&1; then
        log "完成：$step_name"
        return 0
    fi
    rc="$?"
    echo "" >&2
    echo "[clicd][错误] 步骤失败：$step_name，退出码：$rc" >&2
    echo "[clicd][错误] 最近 80 行日志：$LOG_FILE" >&2
    tail -n 80 "$LOG_FILE" >&2 2>/dev/null || true
    echo "" >&2
    echo "请将上述日志和系统信息提交到：$ISSUE_URL" >&2
    exit "$rc"
}

check_os_compatibility() {
    log "系统检测：ID=${OS_ID} ID_LIKE=${OS_LIKE} ARCH=$(uname -m 2>/dev/null || echo unknown)"
    case "$(uname -m 2>/dev/null || echo unknown)" in
        x86_64|amd64)
            ;;
        *)
            die "当前安装包仅支持 x86_64/amd64，当前架构：$(uname -m 2>/dev/null || echo unknown)。"
            ;;
    esac
    if ! is_systemd && ! is_openrc; then
        die "未检测到 systemd 或 OpenRC，无法安装服务。"
    fi
    case "$OS_ID" in
        ubuntu|debian|alpine|centos|rhel|rocky|almalinux|fedora)
            ;;
        *)
            if ! has_cmd apt-get && ! has_cmd apk && ! has_cmd dnf && ! has_cmd yum; then
                die "暂不支持当前 Linux 发行版：${OS_ID} ${OS_LIKE}。请提交 issue 并附上 /etc/os-release。"
            fi
            warn "发行版 ${OS_ID} 不在主要支持列表，将按检测到的软件包管理器尝试安装。"
            ;;
    esac
}

check_storage_compatibility() {
    root_fs="$(findmnt -no FSTYPE / 2>/dev/null || echo unknown)"
    avail_kb="$(df -Pk / 2>/dev/null | awk 'NR==2 {print $4}' || echo 0)"
    log "存储检测：根文件系统=${root_fs} 可用空间=${avail_kb}KB"
    if [ "${avail_kb:-0}" -lt 5242880 ]; then
        warn "根分区可用空间低于 5GB，下载镜像或创建 KVM/LXC 时可能失败。"
    fi
}

if [ "$(id -u)" -ne 0 ]; then
    echo "请使用 root 权限运行：sudo ./install.sh"
    echo "或执行：curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo sh"
    echo "卸载：curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo sh -s -- uninstall"
    echo "问题反馈：$ISSUE_URL"
    exit 1
fi

: > "$LOG_FILE" 2>/dev/null || true
log "日志文件：$LOG_FILE"
log "仓库地址：https://github.com/${REPO}"
log "问题反馈：$ISSUE_URL"

OS_ID="unknown"
OS_LIKE=""
if [ -r /etc/os-release ]; then
    . /etc/os-release
    OS_ID="${ID:-unknown}"
    OS_LIKE="${ID_LIKE:-}"
fi

usage() {
    cat << EOF
用法：
  ./install.sh              安装或升级 CLICD
  ./install.sh uninstall    卸载 CLICD（会删除容器、虚拟机、镜像缓存和配置数据）

环境变量：
  CLICD_REPO=owner/repo          默认：${REPO}
  CLICD_VERSION=latest|v1.0.0    默认：latest
  CLICD_LOG_FILE=/path/file.log  默认：${LOG_FILE}

示例：
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo sh
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo sh -s -- uninstall

日志：${LOG_FILE}
问题反馈：${ISSUE_URL}
EOF
}

remove_path() {
    path="$1"
    if [ ! -e "$path" ] && [ ! -L "$path" ]; then
        return
    fi
    rm -rf "$path"
    log "已删除 $path"
}

unmount_path_tree() {
    path="$1"
    if [ ! -e "$path" ]; then
        return
    fi

    if has_cmd findmnt; then
        findmnt -R -n -o TARGET "$path" 2>/dev/null | sort -r | while IFS= read -r mountpoint; do
            [ -n "$mountpoint" ] || continue
            umount -R -l "$mountpoint" >/dev/null 2>&1 || umount -l "$mountpoint" >/dev/null 2>&1 || true
        done
    fi

    umount -R -l "$path/rootfs" >/dev/null 2>&1 || umount -l "$path/rootfs" >/dev/null 2>&1 || true
    umount -R -l "$path" >/dev/null 2>&1 || umount -l "$path" >/dev/null 2>&1 || true
}

detach_container_loop_devices() {
    path="$1"
    if ! has_cmd losetup; then
        return
    fi

    for image in "$path"/rootfs.img "$path"/*.img; do
        [ -e "$image" ] || continue
        losetup -j "$image" 2>/dev/null | sed 's/:.*//' | while IFS= read -r loopdev; do
            [ -n "$loopdev" ] || continue
            losetup -d "$loopdev" >/dev/null 2>&1 || true
        done
    done
}

kill_path_users() {
    path="$1"
    if has_cmd fuser && [ -e "$path" ]; then
        fuser -km "$path" >/dev/null 2>&1 || true
    fi
}

remove_lxc_container_dir() {
    container_dir="$1"
    container_name="$(basename "$container_dir")"

    if has_cmd lxc-stop; then
        lxc-stop -n "$container_name" -k >/dev/null 2>&1 || true
    fi
    if has_cmd lxc-destroy; then
        lxc-destroy -n "$container_name" -f >/dev/null 2>&1 || true
    fi

    unmount_path_tree "$container_dir"
    detach_container_loop_devices "$container_dir"

    if rm -rf "$container_dir" >/dev/null 2>&1; then
        log "已删除 $container_dir"
        return
    fi

    log "检测到 $container_dir 被占用，终止占用进程后重试删除..."
    kill_path_users "$container_dir/rootfs"
    kill_path_users "$container_dir"
    unmount_path_tree "$container_dir"
    detach_container_loop_devices "$container_dir"
    rm -rf "$container_dir"
    log "已删除 $container_dir"
}

remove_kvm_domain() {
    domain="$1"
    case "$domain" in
        vm-[0-9]*)
            ;;
        *)
            return
            ;;
    esac
    suffix="${domain#vm-}"
    case "$suffix" in
        ""|*[!0-9]*)
            return
            ;;
    esac
    if [ ! -d "/var/lib/clicd/kvm/instances/$domain" ] &&
        ! virsh dumpxml "$domain" 2>/dev/null | grep -q '/var/lib/clicd/kvm/'; then
        return
    fi

    log "正在删除 KVM 虚拟机域 $domain..."
    virsh destroy "$domain" >/dev/null 2>&1 || true
    virsh undefine "$domain" --remove-all-storage --nvram >/dev/null 2>&1 ||
        virsh undefine "$domain" --nvram >/dev/null 2>&1 ||
        virsh undefine "$domain" >/dev/null 2>&1 ||
        true
}

destroy_clicd_kvm_domains() {
    if ! has_cmd virsh; then
        return
    fi

    log "正在销毁 CLICD 创建的 KVM 虚拟机..."
    virsh list --all --name 2>/dev/null | while IFS= read -r domain; do
        [ -n "$domain" ] || continue
        remove_kvm_domain "$domain"
    done
}

delete_iptables_lines() {
    table="$1"
    chain="$2"
    pattern="$3"
    if ! has_cmd iptables; then
        return
    fi

    while :; do
        line="$(iptables -t "$table" -L "$chain" -n --line-numbers 2>/dev/null | awk -v pat="$pattern" '$0 ~ pat {print $1; exit}')"
        [ -n "$line" ] || break
        iptables -t "$table" -D "$chain" "$line" >/dev/null 2>&1 || break
    done
}

delete_iptables_rule() {
    table="$1"
    shift
    if ! has_cmd iptables; then
        return
    fi

    while iptables -t "$table" -D "$@" >/dev/null 2>&1; do
        :
    done
}

delete_filter_rule() {
    if ! has_cmd iptables; then
        return
    fi

    while iptables -D "$@" >/dev/null 2>&1; do
        :
    done
}

delete_ip6tables_bridge_rules() {
    if ! has_cmd ip6tables; then
        return
    fi

    for bridge in lxcbr0 virbr0; do
        while :; do
            rule="$(ip6tables -S FORWARD 2>/dev/null | grep -- "$bridge" | sed 's/^-A /-D /' | head -n 1)"
            [ -n "$rule" ] || break
            # shellcheck disable=SC2086
            ip6tables $rule >/dev/null 2>&1 || break
        done
    done
}

cleanup_clicd_networking() {
    log "正在清理 CLICD 防火墙和网桥规则..."
    delete_iptables_lines nat PREROUTING 'clicd-'
    delete_iptables_rule nat POSTROUTING -s 10.0.3.0/24 -o eth+ -j MASQUERADE
    delete_iptables_rule nat POSTROUTING -s 192.168.122.0/24 -o eth+ -j MASQUERADE

    for bridge in lxcbr0 virbr0; do
        delete_filter_rule FORWARD -i "$bridge" -j ACCEPT
        delete_filter_rule FORWARD -o "$bridge" -j ACCEPT
        delete_filter_rule FORWARD -i "$bridge" -o "$bridge" -j ACCEPT
    done
    delete_ip6tables_bridge_rules
}

remove_clicd_host_hooks() {
    if has_cmd systemctl; then
        systemctl stop clicd-kvm-ipv6.service >/dev/null 2>&1 || true
        systemctl disable clicd-kvm-ipv6.service >/dev/null 2>&1 || true
    fi
    if has_cmd rc-service; then
        rc-service clicd-kvm-ipv6 stop >/dev/null 2>&1 || true
    fi
    if has_cmd rc-update; then
        rc-update del clicd-kvm-ipv6 default >/dev/null 2>&1 || true
    fi

    remove_path /usr/local/sbin/clicd-kvm-ipv6-init
    remove_path /etc/systemd/system/clicd-kvm-ipv6.service
    remove_path /etc/local.d/clicd-kvm-ipv6.start
    remove_path /etc/network/if-up.d/clicd-kvm-ipv6
}

remove_clicd_quota_records() {
    for file in /etc/projects /etc/projid; do
        [ -f "$file" ] || continue
        tmp="${file}.clicd-clean.$$"
        grep -v 'clicd-' "$file" > "$tmp" || true
        cat "$tmp" > "$file"
        rm -f "$tmp"
        log "已清理 $file 中的 CLICD 配额记录"
    done
}

remove_clicd_tmp_files() {
    current_dir="$(pwd -P 2>/dev/null || pwd)"
    for path in /tmp/clicd-* /tmp/clicd.*; do
        [ -e "$path" ] || [ -L "$path" ] || continue
        abs_path="$(cd "$(dirname "$path")" 2>/dev/null && pwd -P)/$(basename "$path")"
        if [ "$abs_path" = "$current_dir" ]; then
            log "跳过当前安装目录 $path，避免中断后续安装步骤。"
            continue
        fi
        rm -rf "$path"
        log "已删除 $path"
    done
}

remove_clicd_swapfile() {
    if [ ! -e /swapfile ]; then
        return
    fi
    swapoff /swapfile >/dev/null 2>&1 || true
    remove_path /swapfile
}


confirm_uninstall() {
    if [ "${CLICD_UNINSTALL_CONFIRM:-}" = "1" ] || [ "${CLICD_UNINSTALL_CONFIRM:-}" = "yes" ] || [ "$ACTION_CONFIRM" = "--yes" ] || [ "$ACTION_CONFIRM" = "-y" ]; then
        return
    fi
    echo ""
    echo "[clicd][警告] 卸载会停止并删除 CLICD 服务、配置数据库、CLICD 创建的 LXC/KVM 实例和缓存数据。" >&2
    echo "[clicd][警告] 为避免误删生产数据，脚本只会删除名称形如 ct-数字 的 LXC 容器和 vm-数字 的 KVM 域。" >&2
    echo "如需确认卸载，请输入：YES" >&2
    if [ -t 0 ]; then
        read answer
    else
        answer=""
    fi
    if [ "$answer" != "YES" ]; then
        die "已取消卸载。如需非交互卸载，请设置 CLICD_UNINSTALL_CONFIRM=1。"
    fi
}

uninstall_clicd() {
    confirm_uninstall
    log "正在卸载 CLICD..."

    if has_cmd systemctl; then
        systemctl stop clicd >/dev/null 2>&1 || true
        systemctl disable clicd >/dev/null 2>&1 || true
    fi

    if has_cmd rc-service; then
        rc-service clicd stop >/dev/null 2>&1 || true
    fi
    if has_cmd rc-update; then
        rc-update del clicd default >/dev/null 2>&1 || true
    fi

    log "正在删除 CLICD 创建的 LXC 容器（/var/lib/lxc/ct-数字）..."
    for container_dir in /var/lib/lxc/ct-[0-9]*; do
        [ -d "$container_dir" ] || continue
        remove_lxc_container_dir "$container_dir"
    done
    destroy_clicd_kvm_domains
    cleanup_clicd_networking
    remove_clicd_host_hooks
    remove_clicd_quota_records

    remove_path /etc/systemd/system/clicd.service
    remove_path /etc/init.d/clicd
    remove_path /usr/local/bin/clicd
    remove_path /etc/sysctl.d/99-clicd.conf
    remove_path /var/log/clicd.log
    remove_path /var/log/clicd.err
    remove_path /root/.clicd
    # /var/lib/lxc 可能包含非 CLICD 容器，生产环境不整体删除。
    unmount_path_tree /var/lib/clicd
    remove_path /var/lib/clicd
    # /var/cache/lxc 是 LXC 全局镜像缓存，可能被其他工具复用，生产环境不整体删除。
    remove_path /var/cache/clicd
    warn "保留 /root/clicd-backups，避免误删部署/回滚备份。确认不需要后可手动删除。"
    remove_clicd_tmp_files
    remove_clicd_swapfile

    if has_cmd systemctl; then
        systemctl daemon-reload >/dev/null 2>&1 || true
        systemctl reset-failed clicd >/dev/null 2>&1 || true
    fi
    if has_cmd sysctl; then
        sysctl --system >/dev/null 2>&1 || true
    fi

    echo ""
    echo "====================================="
    echo "  CLICD 卸载完成"
    echo "====================================="
    echo "  已删除服务、二进制、SQLite/配置数据、CLICD LXC/KVM 实例、"
    echo "  CLICD 镜像缓存、防火墙规则、主机钩子、配额记录和临时文件。"
    echo "  已保留 /root/clicd-backups 和 LXC 全局缓存，避免误删生产备份/共享镜像。"
    echo "  日志：$LOG_FILE"
    echo "  问题反馈：$ISSUE_URL"
    echo "====================================="
}

case "$ACTION" in
    install|"")
        ;;
    uninstall|remove)
        uninstall_clicd
        exit 0
        ;;
    -h|--help|help)
        usage
        exit 0
        ;;
    *)
        die "未知操作：$ACTION"
        ;;
esac

install_apk() {
    log "正在使用 apk 安装依赖..."
    apk update
    apk add --no-cache \
        ca-certificates \
        curl \
        wget \
        tar \
        gzip \
        xz \
        lxc \
        lxc-download \
        lxc-openrc \
        lxc-bridge \
        lxc-templates \
        bridge-utils \
        iproute2 \
        iptables \
        dnsmasq \
        dbus \
        qemu-system-x86_64 \
        qemu-img \
        libvirt \
        libvirt-daemon \
        libvirt-client \
        libvirt-qemu

    for pkg in lxcfs shadow conntrack-tools quota-tools e2fsprogs xfsprogs cloud-utils genisoimage xorriso; do
        apk add --no-cache "$pkg" >/dev/null 2>&1 || warn "可选依赖未安装：$pkg"
    done
}

install_apt() {
    log "正在使用 apt 安装依赖..."
    export DEBIAN_FRONTEND=noninteractive
    apt-get update
    apt-get install -y \
        ca-certificates \
        curl \
        wget \
        tar \
        gzip \
        xz-utils \
        lxc \
        lxc-templates \
        lxcfs \
        bridge-utils \
        uidmap \
        iproute2 \
        iptables \
        conntrack \
        quota \
        e2fsprogs \
        xfsprogs \
        dnsmasq-base \
        qemu-kvm \
        qemu-utils \
        libvirt-daemon-system \
        libvirt-clients \
        cloud-image-utils \
        genisoimage \
        xorriso \
        virtinst \
        ovmf
}

enable_el_repos() {
    if has_cmd dnf; then
        dnf install -y 'dnf-command(config-manager)' >/dev/null 2>&1 || true
        dnf install -y epel-release || true
        dnf config-manager --set-enabled crb >/dev/null 2>&1 || true
        dnf config-manager --set-enabled powertools >/dev/null 2>&1 || true
    elif has_cmd yum; then
        yum install -y yum-utils >/dev/null 2>&1 || true
        yum install -y epel-release || true
        yum-config-manager --enable powertools >/dev/null 2>&1 || true
    fi
}

install_dnf() {
    log "正在使用 dnf 安装依赖..."
    enable_el_repos
    dnf install -y \
        ca-certificates \
        curl \
        wget \
        tar \
        gzip \
        xz \
        lxc \
        lxc-templates \
        bridge-utils \
        iproute \
        iptables \
        conntrack-tools \
        shadow-utils \
        quota \
        e2fsprogs \
        xfsprogs \
        dnsmasq \
        qemu-kvm \
        qemu-img \
        libvirt \
        libvirt-daemon-kvm \
        libvirt-client \
        virt-install \
        cloud-utils \
        genisoimage

    for pkg in lxcfs xorriso edk2-ovmf; do
        dnf install -y "$pkg" >/dev/null 2>&1 || warn "可选依赖未安装：$pkg"
    done
}

install_yum() {
    log "正在使用 yum 安装依赖..."
    enable_el_repos
    yum install -y \
        ca-certificates \
        curl \
        wget \
        tar \
        gzip \
        xz \
        lxc \
        lxc-templates \
        bridge-utils \
        iproute \
        iptables \
        conntrack-tools \
        shadow-utils \
        quota \
        e2fsprogs \
        xfsprogs \
        dnsmasq \
        qemu-kvm \
        qemu-img \
        libvirt \
        libvirt-daemon-kvm \
        libvirt-client \
        virt-install \
        cloud-utils \
        genisoimage

    for pkg in lxcfs xorriso edk2-ovmf; do
        yum install -y "$pkg" >/dev/null 2>&1 || warn "可选依赖未安装：$pkg"
    done
}

install_dependencies() {
    case "$OS_ID" in
        ubuntu|debian)
            install_apt
            ;;
        alpine)
            install_apk
            ;;
        centos|rhel|rocky|almalinux|fedora)
            if has_cmd dnf; then
                install_dnf
            elif has_cmd yum; then
                install_yum
            else
                die "当前系统 $OS_ID 未找到 dnf/yum，无法安装依赖。"
            fi
            ;;
        *)
            if has_cmd apt-get; then
                install_apt
            elif has_cmd apk; then
                install_apk
            elif has_cmd dnf; then
                install_dnf
            elif has_cmd yum; then
                install_yum
            else
                die "暂不支持当前 Linux 发行版：${OS_ID} ${OS_LIKE}。请提交 issue 并附上 /etc/os-release。"
            fi
            ;;
    esac

    has_cmd lxc-create || die "依赖安装后仍未找到 lxc-create，请检查 LXC 软件源/安装日志。"
    has_cmd iptables || die "依赖安装后仍未找到 iptables，请检查系统网络工具包。"
    has_cmd ip || die "依赖安装后仍未找到 ip 命令，请检查 iproute2 安装。"
    has_cmd virsh || die "依赖安装后仍未找到 virsh，请检查 libvirt-client/libvirt-clients 安装。"
    has_cmd qemu-img || die "依赖安装后仍未找到 qemu-img，请检查 qemu-utils/qemu-img 安装。"
    has_cmd cloud-localds || die "依赖安装后仍未找到 cloud-localds，请检查 cloud-image-utils/cloud-utils 安装。"
    if ! has_cmd genisoimage && ! has_cmd mkisofs && ! has_cmd xorriso; then
        die "Windows KVM 初始化需要 genisoimage、mkisofs 或 xorriso 中任意一个。"
    fi
    if [ ! -e /dev/kvm ]; then
        warn "未检测到 /dev/kvm。LXC 可用，但 KVM 虚拟机需要硬件虚拟化或嵌套虚拟化。"
    fi
}

configure_kernel_networking() {
    log "正在启用内核转发配置..."
    cat > /etc/sysctl.d/99-clicd.conf << 'EOF'
net.ipv4.ip_forward = 1
net.ipv6.conf.all.forwarding = 1
net.bridge.bridge-nf-call-iptables = 0
net.bridge.bridge-nf-call-ip6tables = 0
EOF

    modprobe br_netfilter >/dev/null 2>&1 || true
    sysctl --system >/dev/null 2>&1 || true
}

systemd_unit_exists() {
    unit="$1"
    systemctl list-unit-files "$unit" >/dev/null 2>&1 || [ -e "/etc/systemd/system/$unit" ] || [ -e "/usr/lib/systemd/system/$unit" ] || [ -e "/lib/systemd/system/$unit" ]
}

systemd_enable_now_if_exists() {
    unit="$1"
    if systemd_unit_exists "$unit"; then
        systemctl enable --now "$unit" >/dev/null 2>&1 || warn "服务 $unit 启动失败，将继续安装并在运行时降级处理。"
        return
    fi
    log "未检测到 systemd 单元 $unit，跳过。"
}

systemd_existing_units() {
    for unit in "$@"; do
        if systemd_unit_exists "$unit"; then
            printf ' %s' "$unit"
        fi
    done
}

setup_runtime_services() {
    log "正在配置 LXC 和 KVM 服务..."

    if is_systemd; then
        systemd_enable_now_if_exists lxcfs.service
        systemd_enable_now_if_exists lxc-net.service
        systemd_enable_now_if_exists lxc.service
        if systemd_unit_exists libvirtd.service; then
            systemd_enable_now_if_exists libvirtd.service
            log "检测到 libvirt 传统 libvirtd 服务，已使用 libvirtd 模式。"
        else
            systemd_enable_now_if_exists virtqemud.service
            systemd_enable_now_if_exists virtqemud.socket
        fi
        systemd_enable_now_if_exists virtlogd.socket
        return
    fi

    if is_openrc; then
        rc-update add cgroups default >/dev/null 2>&1 || true
        rc-service cgroups start >/dev/null 2>&1 || true
        rc-update add lxc default >/dev/null 2>&1 || true
        rc-service lxc start >/dev/null 2>&1 || true
        rc-update add lxcfs default >/dev/null 2>&1 || true
        rc-service lxcfs start >/dev/null 2>&1 || true
        rc-update add dbus default >/dev/null 2>&1 || true
        rc-service dbus start >/dev/null 2>&1 || true
        rc-update add libvirtd default >/dev/null 2>&1 || true
        rc-service libvirtd start >/dev/null 2>&1 || true
        rc-update add virtlogd default >/dev/null 2>&1 || true
        rc-service virtlogd start >/dev/null 2>&1 || true
        return
    fi

    die "未检测到支持的服务管理器。CLICD 当前支持 systemd 或 OpenRC。"
}


libvirt_network_active() {
    virsh net-info default 2>/dev/null | awk -F: 'tolower($1) ~ /^[[:space:]]*active[[:space:]]*$/ {gsub(/^[ \t]+|[ \t]+$/, "", $2); print tolower($2)}' | grep -qx yes
}

setup_default_libvirt_network() {
    if ! has_cmd virsh; then
        warn "未找到 virsh，跳过 libvirt default NAT 网络检查。"
        return
    fi
    log "正在检查 libvirt default NAT 网络..."
    if ! virsh net-info default >/dev/null 2>&1; then
        net_xml="$(mktemp /tmp/clicd-default-net.XXXXXX.xml)"
        cat > "$net_xml" << 'EOF'
<network>
  <name>default</name>
  <bridge name='virbr0'/>
  <forward mode='nat'/>
  <ip address='192.168.122.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='192.168.122.2' end='192.168.122.254'/>
    </dhcp>
  </ip>
</network>
EOF
        virsh net-define "$net_xml"
        rm -f "$net_xml"
    fi
    if ! libvirt_network_active; then
        virsh net-start default
    fi
    virsh net-autostart default >/dev/null
    if ! libvirt_network_active; then
        die "libvirt default 网络仍未启动。请执行 virsh net-info default 查看详情。"
    fi
    log "libvirt default NAT 网络已启用。"
}

setup_subids() {
    log "正在配置 subordinate UID/GID 范围..."
    touch /etc/subuid /etc/subgid
    grep -q '^root:' /etc/subuid 2>/dev/null || echo 'root:100000:65536' >> /etc/subuid
    grep -q '^root:' /etc/subgid 2>/dev/null || echo 'root:100000:65536' >> /etc/subgid
}

configure_lxc_storage_access() {
    log "Configuring LXC storage directory permissions..."
    mkdir -p /var/lib/lxc
    chmod 755 /var/lib/lxc
}

try_enable_project_quota() {
    root_src="$(findmnt -no SOURCE / 2>/dev/null || true)"
    root_fs="$(findmnt -no FSTYPE / 2>/dev/null || true)"

    case "$root_fs" in
        ext4)
            ;;
        xfs|btrfs|zfs|overlay|unknown|"")
            log "根文件系统 ${root_fs:-unknown} 不需要/不适合自动启用 ext4 project quota，CLICD 将使用兼容磁盘限制模式。"
            return
            ;;
        *)
            log "根文件系统 ${root_fs:-unknown} 不在自动 project quota 支持范围，CLICD 将使用兼容磁盘限制模式。"
            return
            ;;
    esac

    if [ -z "$root_src" ] || [ ! -b "$root_src" ]; then
        log "根分区来源 ${root_src:-unknown} 不是块设备，跳过 project quota 自动检查，CLICD 将使用兼容磁盘限制模式。"
        return
    fi

    if ! has_cmd tune2fs; then
        log "未找到 tune2fs，跳过 project quota 检查，CLICD 将使用兼容磁盘限制模式。"
        return
    fi

    if tune2fs -l "$root_src" 2>/dev/null | grep -q 'project'; then
        log "检测到 ext4 project quota 已可用。"
        return
    fi

    log "ext4 project quota 未启用，CLICD 将自动回退到 loopback 镜像磁盘限制模式。"
}

download_release_if_needed() {
    if [ -f "./clicd" ]; then
        return
    fi

    if [ "$CLICD_INSTALL_VERSION" = "latest" ]; then
        download_url="https://github.com/${REPO}/releases/latest/download/${ASSET}"
    else
        download_url="https://github.com/${REPO}/releases/download/${CLICD_INSTALL_VERSION}/${ASSET}"
    fi

    log "当前目录未找到 clicd 二进制，将下载发行版包。"
    log "正在下载发行版包：${download_url}"

    tmp_dir="$(mktemp -d)"
    trap 'rm -rf "$tmp_dir"' 0

    if has_cmd curl; then
        curl -fL "$download_url" -o "$tmp_dir/$ASSET"
    elif has_cmd wget; then
        wget -O "$tmp_dir/$ASSET" "$download_url"
    else
        die "下载发行版包需要 curl 或 wget。"
    fi

    tar -xzf "$tmp_dir/$ASSET" -C "$tmp_dir"
    cd "$tmp_dir/clicd-linux-amd64"
    [ -f "./clicd" ] || die "下载的发行版包中未找到 clicd 二进制。"
}

install_binary() {
    if has_cmd systemctl; then
        systemctl stop clicd >/dev/null 2>&1 || true
    fi
    if has_cmd rc-service; then
        rc-service clicd stop >/dev/null 2>&1 || true
    fi

    tmp_bin="/usr/local/bin/clicd.new.$$"
    cp ./clicd "$tmp_bin"
    chmod +x "$tmp_bin"
    mv -f "$tmp_bin" /usr/local/bin/clicd
    chmod +x /usr/local/bin/clicd
    log "已安装二进制：/usr/local/bin/clicd"
}

install_systemd_service() {
    libvirt_after="$(systemd_existing_units libvirtd.service virtqemud.service virtqemud.socket virtlogd.socket)"
    libvirt_wants="$(systemd_existing_units libvirtd.service virtqemud.socket virtlogd.socket)"
    lxc_after="$(systemd_existing_units lxc.service lxcfs.service lxc-net.service)"

    cat > /etc/systemd/system/clicd.service << EOF
[Unit]
Description=CLICD - LXC/KVM Container Manager
After=network-online.target${lxc_after}${libvirt_after}
Wants=network-online.target${libvirt_wants}
StartLimitIntervalSec=60
StartLimitBurst=10

[Service]
Type=simple
ExecStart=/usr/local/bin/clicd server
Restart=always
RestartSec=5
LimitNOFILE=1048576
Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable clicd
    systemctl restart clicd
}

install_openrc_service() {
    cat > /etc/init.d/clicd << 'EOF'
#!/sbin/openrc-run

name="CLICD"
description="CLICD - LXC/KVM Container Manager"
command="/usr/local/bin/clicd"
command_args="server"
command_background=true
pidfile="/run/clicd.pid"
output_log="/var/log/clicd.log"
error_log="/var/log/clicd.err"

depend() {
    need net
    after lxc libvirtd
}
EOF

    chmod +x /etc/init.d/clicd
    rc-update add clicd default
    rc-service clicd restart
}

install_service() {
    log "正在安装 CLICD 服务..."

    if is_systemd; then
        install_systemd_service
    elif is_openrc; then
        install_openrc_service
    else
        die "未检测到支持的服务管理器。CLICD 当前支持 systemd 或 OpenRC。"
    fi
}

print_summary() {
    echo ""
    echo "====================================="
    echo "  安装完成"
    echo "====================================="
    echo "  Web 面板：http://YOUR_SERVER_IP:8999"
    echo "  二进制：/usr/local/bin/clicd"
    echo "  安装日志：$LOG_FILE"
    echo "  问题反馈：$ISSUE_URL"
    if is_systemd; then
        echo "  服务：systemctl {start|stop|restart|status} clicd"
        echo "  运行日志：journalctl -u clicd -f"
    elif is_openrc; then
        echo "  服务：rc-service clicd {start|stop|restart|status}"
        echo "  运行日志：tail -f /var/log/clicd.log /var/log/clicd.err"
    fi
    echo "====================================="
    echo ""
    echo "首次安装时的初始账号信息："
    if is_systemd; then
        journalctl -u clicd --no-pager -n 80 | grep -E "Username:|Password:" || true
    else
        grep -E "Username:|Password:" /var/log/clicd.log /var/log/clicd.err 2>/dev/null || true
    fi
    echo ""
    echo "如果没有显示密码，说明服务器已有 /root/.clicd/config.db。"
    echo "已有管理员密码使用 bcrypt 存储，无法反查；请使用面板内修改密码或重置配置。"
}

run_step "兼容性检查" check_os_compatibility
run_step "存储环境检查" check_storage_compatibility
run_step "安装系统依赖" install_dependencies
run_step "配置内核网络参数" configure_kernel_networking
run_step "配置运行时服务" setup_runtime_services
run_step "配置 libvirt default NAT 网络" setup_default_libvirt_network
run_step "配置 UID/GID 映射" setup_subids
run_step "Configure LXC storage permissions" configure_lxc_storage_access
run_step "检查 project quota" try_enable_project_quota
run_step "下载发行版包" download_release_if_needed
run_step "安装 CLICD 二进制" install_binary
run_step "安装并启动 CLICD 服务" install_service
sleep 2
print_summary
