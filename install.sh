#!/bin/sh
set -eu

REPO="${CLICD_REPO:-MengMengCode/CLICD}"
CLICD_INSTALL_VERSION="${CLICD_VERSION:-latest}"
ASSET="clicd-linux-amd64.tar.gz"
ACTION="${1:-install}"

echo "====================================="
echo "  CLICD Installer"
echo "====================================="

log() {
    echo "[clicd] $*"
}

die() {
    echo "ERROR: $*" >&2
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

if [ "$(id -u)" -ne 0 ]; then
    echo "Please run as root: sudo ./install.sh"
    echo "Or: curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo sh"
    echo "Uninstall: curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo sh -s -- uninstall"
    exit 1
fi

OS_ID="unknown"
OS_LIKE=""
if [ -r /etc/os-release ]; then
    . /etc/os-release
    OS_ID="${ID:-unknown}"
    OS_LIKE="${ID_LIKE:-}"
fi

usage() {
    cat << EOF
Usage:
  ./install.sh
  ./install.sh uninstall

Environment:
  CLICD_REPO=owner/repo
  CLICD_VERSION=latest|v1.0.0

Examples:
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo sh
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo sh -s -- uninstall
EOF
}

remove_path() {
    path="$1"
    if [ ! -e "$path" ] && [ ! -L "$path" ]; then
        return
    fi
    rm -rf "$path"
    log "Removed $path"
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
        log "Removed $container_dir"
        return
    fi

    log "Retrying removal after terminating processes using $container_dir..."
    kill_path_users "$container_dir/rootfs"
    kill_path_users "$container_dir"
    unmount_path_tree "$container_dir"
    detach_container_loop_devices "$container_dir"
    rm -rf "$container_dir"
    log "Removed $container_dir"
}

uninstall_clicd() {
    log "Uninstalling CLICD..."

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

    log "Destroying LXC containers under /var/lib/lxc..."
    for container_dir in /var/lib/lxc/*; do
        [ -d "$container_dir" ] || continue
        remove_lxc_container_dir "$container_dir"
    done

    remove_path /etc/systemd/system/clicd.service
    remove_path /etc/init.d/clicd
    remove_path /usr/local/bin/clicd
    remove_path /etc/sysctl.d/99-clicd.conf
    remove_path /var/log/clicd.log
    remove_path /var/log/clicd.err
    remove_path /root/.clicd
    unmount_path_tree /var/lib/lxc
    remove_path /var/lib/lxc
    remove_path /var/cache/lxc

    if has_cmd systemctl; then
        systemctl daemon-reload >/dev/null 2>&1 || true
        systemctl reset-failed clicd >/dev/null 2>&1 || true
    fi
    if has_cmd sysctl; then
        sysctl --system >/dev/null 2>&1 || true
    fi

    echo ""
    echo "====================================="
    echo "  CLICD Uninstalled"
    echo "====================================="
    echo "  Removed service, binary, config, containers, and LXC image cache."
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
        die "Unknown action: $ACTION"
        ;;
esac

install_apk() {
    log "Installing dependencies with apk..."
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
        dnsmasq

    for pkg in lxcfs shadow conntrack-tools quota-tools e2fsprogs xfsprogs; do
        apk add --no-cache "$pkg" >/dev/null 2>&1 || log "Optional package not installed: $pkg"
    done
}

install_apt() {
    log "Installing dependencies with apt..."
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
        dnsmasq-base
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
    log "Installing dependencies with dnf..."
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
        dnsmasq

    dnf install -y lxcfs >/dev/null 2>&1 || log "Optional package not installed: lxcfs"
}

install_yum() {
    log "Installing dependencies with yum..."
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
        dnsmasq

    yum install -y lxcfs >/dev/null 2>&1 || log "Optional package not installed: lxcfs"
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
                die "dnf/yum not found on $OS_ID"
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
                die "Unsupported Linux distribution: ${OS_ID} ${OS_LIKE}"
            fi
            ;;
    esac

    has_cmd lxc-create || die "lxc-create is still missing after dependency installation."
    has_cmd iptables || die "iptables is still missing after dependency installation."
    has_cmd ip || die "iproute2/ip command is still missing after dependency installation."
}

configure_kernel_networking() {
    log "Enabling kernel forwarding settings..."
    cat > /etc/sysctl.d/99-clicd.conf << 'EOF'
net.ipv4.ip_forward = 1
net.ipv6.conf.all.forwarding = 1
net.bridge.bridge-nf-call-iptables = 0
net.bridge.bridge-nf-call-ip6tables = 0
EOF

    modprobe br_netfilter >/dev/null 2>&1 || true
    sysctl --system >/dev/null 2>&1 || true
}

setup_lxc_services() {
    log "Configuring LXC services..."

    if is_systemd; then
        systemctl enable --now lxcfs >/dev/null 2>&1 || true
        systemctl enable --now lxc-net >/dev/null 2>&1 || true
        systemctl enable --now lxc >/dev/null 2>&1 || true
        return
    fi

    if is_openrc; then
        rc-update add cgroups default >/dev/null 2>&1 || true
        rc-service cgroups start >/dev/null 2>&1 || true
        rc-update add lxc default >/dev/null 2>&1 || true
        rc-service lxc start >/dev/null 2>&1 || true
        rc-update add lxcfs default >/dev/null 2>&1 || true
        rc-service lxcfs start >/dev/null 2>&1 || true
        return
    fi

    die "No supported service manager found. CLICD supports systemd or OpenRC."
}

setup_subids() {
    log "Setting up subordinate UID/GID ranges..."
    touch /etc/subuid /etc/subgid
    grep -q '^root:' /etc/subuid 2>/dev/null || echo 'root:100000:65536' >> /etc/subuid
    grep -q '^root:' /etc/subgid 2>/dev/null || echo 'root:100000:65536' >> /etc/subgid
}

try_enable_project_quota() {
    root_src="$(findmnt -no SOURCE / 2>/dev/null || true)"
    root_fs="$(findmnt -no FSTYPE / 2>/dev/null || true)"

    if [ "$root_fs" != "ext4" ] || [ -z "$root_src" ] || [ ! -b "$root_src" ]; then
        log "Project quota auto-enable skipped for root filesystem: ${root_fs:-unknown}"
        return
    fi

    if ! has_cmd tune2fs; then
        log "Project quota auto-enable skipped because tune2fs is unavailable."
        return
    fi

    if tune2fs -l "$root_src" 2>/dev/null | grep -q 'project'; then
        log "Ext4 project quota support already appears to be enabled."
        return
    fi

    log "Ext4 project quota is not enabled. Disk limits will fall back to loopback images."
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

    log "clicd binary not found in current directory."
    log "Downloading release package: ${download_url}"

    tmp_dir="$(mktemp -d)"
    trap 'rm -rf "$tmp_dir"' 0

    if has_cmd curl; then
        curl -fL "$download_url" -o "$tmp_dir/$ASSET"
    elif has_cmd wget; then
        wget -O "$tmp_dir/$ASSET" "$download_url"
    else
        die "curl or wget is required to download the release package."
    fi

    tar -xzf "$tmp_dir/$ASSET" -C "$tmp_dir"
    cd "$tmp_dir/clicd-linux-amd64"
    [ -f "./clicd" ] || die "Downloaded release package did not contain clicd."
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
    log "Installed binary: /usr/local/bin/clicd"
}

install_systemd_service() {
    cat > /etc/systemd/system/clicd.service << 'EOF'
[Unit]
Description=CLICD - LXC Container Manager
After=network.target lxc.service

[Service]
Type=simple
ExecStart=/usr/local/bin/clicd server
Restart=always
RestartSec=5
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
description="CLICD - LXC Container Manager"
command="/usr/local/bin/clicd"
command_args="server"
command_background=true
pidfile="/run/clicd.pid"
output_log="/var/log/clicd.log"
error_log="/var/log/clicd.err"

depend() {
    need net
    after lxc
}
EOF

    chmod +x /etc/init.d/clicd
    rc-update add clicd default
    rc-service clicd restart
}

install_service() {
    log "Installing CLICD service..."

    if is_systemd; then
        install_systemd_service
    elif is_openrc; then
        install_openrc_service
    else
        die "No supported service manager found. CLICD supports systemd or OpenRC."
    fi
}

print_summary() {
    echo ""
    echo "====================================="
    echo "  Installation Complete"
    echo "====================================="
    echo "  Web: http://YOUR_SERVER_IP:8999"
    echo "  Binary: /usr/local/bin/clicd"
    if is_systemd; then
        echo "  Service: systemctl {start|stop|restart|status} clicd"
        echo "  Logs: journalctl -u clicd -f"
    elif is_openrc; then
        echo "  Service: rc-service clicd {start|stop|restart|status}"
        echo "  Logs: tail -f /var/log/clicd.log /var/log/clicd.err"
    fi
    echo "====================================="
    echo ""
    echo "Initial credentials, if this was the first run:"
    if is_systemd; then
        journalctl -u clicd --no-pager -n 80 | grep -E "Username:|Password:" || true
    else
        grep -E "Username:|Password:" /var/log/clicd.log /var/log/clicd.err 2>/dev/null || true
    fi
    echo ""
    echo "If no password is shown, this server already had /root/.clicd/config.json."
    echo "The existing admin password cannot be recovered from the bcrypt hash."
}

install_dependencies
configure_kernel_networking
setup_lxc_services
setup_subids
try_enable_project_quota
download_release_if_needed
install_binary
install_service
sleep 2
print_summary
