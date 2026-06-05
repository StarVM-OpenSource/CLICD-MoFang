<p align="center">
  <img src="frontend/public/favicon.svg" width="96" alt="CLICD">
</p>

<h1 align="center">CLICD</h1>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.22-00ADD8?style=flat-square&logo=go&logoColor=white">
  <img alt="React" src="https://img.shields.io/badge/React-18-61DAFB?style=flat-square&logo=react&logoColor=111111">
  <img alt="TypeScript" src="https://img.shields.io/badge/TypeScript-5-3178C6?style=flat-square&logo=typescript&logoColor=white">
  <img alt="Vite" src="https://img.shields.io/badge/Vite-5-646CFF?style=flat-square&logo=vite&logoColor=white">
  <img alt="Tailwind CSS" src="https://img.shields.io/badge/Tailwind_CSS-3-06B6D4?style=flat-square&logo=tailwindcss&logoColor=white">
  <img alt="LXC" src="https://img.shields.io/badge/LXC-container-111111?style=flat-square">
</p>

CLICD 是一个面向 LXC 的轻量容器管理面板，提供 Web 控制台、CLI、批量任务、镜像管理、NAT 端口、IPv6 分配、WebSSH、资源限制、流量限制和安全告警能力。它适合用来管理小型 VPS 上的 LXC 容器，也适合需要批量创建和分发子用户管理链接的场景。

## 功能介绍

1. 支持 Ubuntu、Debian、Alpine、CentOS、Arch Linux、Fedora、Rocky Linux 等系统镜像。镜像可以在镜像管理中按需下载；如果宿主机资源比较小，建议优先选择 Alpine 这类轻量镜像。
2. 支持 WebSSH 管理，可以在浏览器里一键进入容器终端，不需要手动复制 SSH 密码。
3. 支持子用户管理链接，管理员可以把指定容器分发给拼车用户，子用户只能管理自己被授权的容器。
4. 支持设置 NAT4 端口数量、NAT 端口映射和协议限制，并支持分配公网 IPv6。IPv6 分配要求宿主机本身拥有可路由的 IPv6 地址段。
5. 支持超售容量估算。宿主机控制页提供 KSM 合并、Swap 倾向和 cgroup v2 `memory.reclaim` 一次性回收能力；不会展示 LXC 下无实际通用效果的内存气球回收开关。
6. 支持 API 接入，可以通过 API 完成容器、任务、镜像、端口、流量、安全告警等功能的自动化控制。
7. 支持仅使用 CLI 管理。需要关闭 Web 控制台时，可以停止并禁用 systemd 服务，然后使用 `clicd cli --no-web` 进入命令行模式。
8. 支持设置容器有效期。到期后容器会自动关机，子用户无法继续操作，只有管理员重新设置延期日期后才能恢复使用。
9. 支持单向和双向网络流量限制。达到限制后容器会自动关机，避免流量超额。
10. 内置基于 conntrack 的轻量安全告警。系统不会保存完整正常连接日志，但会对端口扫描、横向扫描、爆破倾向、SMTP 滥用、UDP 反射、挖矿端口、代理/VPN/Tor 等可疑行为生成告警并写入审计日志。

## 技术栈

- Backend: Go, net/http, LXC, cgroup v2, iptables, conntrack
- Frontend: React, TypeScript, Vite, Tailwind CSS, lucide-react, xterm.js
- Runtime: Linux, systemd, LXC
- Build: GitHub Actions, Node.js 20, Go 1.22

## 安装

推荐使用最新 Release 一键安装。在目标服务器上执行：

```bash
curl -fsSL https://raw.githubusercontent.com/MengMengCode/CLICD/main/install.sh | sudo sh
```

也可以下载 GitHub Actions 构建出的 Release 产物 `clicd-linux-amd64.tar.gz` 后手动安装：

```bash
tar -xzf clicd-linux-amd64.tar.gz
cd clicd-linux-amd64
sudo ./install.sh
```

安装完成后访问：

```text
http://YOUR_SERVER_IP:8999
```

首次启动时会自动初始化管理员账号：

```text
Username: admin
Password: 随机 16 位密码
```

安装脚本会尝试从 systemd 日志中输出初始账号密码。如果机器上已经存在 `/root/.clicd/config.json`，则不会重新生成密码。

查看初始密码日志：

```bash
journalctl -u clicd --no-pager -n 80 | grep -E "Username:|Password:"
```

![alt text](/img/image.png)
![alt text](/img/image-1.png)
![alt text](/img/image-2.png)
