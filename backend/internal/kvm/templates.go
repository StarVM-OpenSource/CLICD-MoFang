package kvm

import (
	"path/filepath"
)

type Image struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Distro      string `json:"distro"`
	Release     string `json:"release"`
	Arch        string `json:"arch"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Desktop     string `json:"desktop,omitempty"`
}

func GetImages() []Image {
	return []Image{
		{
			ID: "kvm-ubuntu-noble", Name: "Ubuntu 24.04 KVM",
			Distro: "ubuntu", Release: "noble", Arch: "amd64",
			Description: "Ubuntu 24.04 LTS cloud image for KVM",
			URL:         "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img",
		},
		{
			ID: "kvm-ubuntu-noble-xfce", Name: "Ubuntu 24.04 XFCE KVM",
			Distro: "ubuntu", Release: "noble", Arch: "amd64",
			Description: "Ubuntu 24.04 LTS cloud image with XFCE desktop provisioned via cloud-init",
			URL:         "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img",
			Desktop:     "xfce",
		},
		{
			ID: "kvm-ubuntu-jammy", Name: "Ubuntu 22.04 KVM",
			Distro: "ubuntu", Release: "jammy", Arch: "amd64",
			Description: "Ubuntu 22.04 LTS cloud image for KVM",
			URL:         "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img",
		},
		{
			ID: "kvm-debian-bookworm", Name: "Debian 12 KVM",
			Distro: "debian", Release: "bookworm", Arch: "amd64",
			Description: "Debian 12 generic cloud image for KVM",
			URL:         "https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2",
		},
		{
			ID: "kvm-debian-bookworm-xfce", Name: "Debian 12 XFCE KVM",
			Distro: "debian", Release: "bookworm", Arch: "amd64",
			Description: "Debian 12 generic cloud image with XFCE desktop provisioned via cloud-init",
			URL:         "https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2",
			Desktop:     "xfce",
		},
		{
			ID: "kvm-debian-bullseye", Name: "Debian 11 KVM",
			Distro: "debian", Release: "bullseye", Arch: "amd64",
			Description: "Debian 11 generic cloud image for KVM",
			URL:         "https://cloud.debian.org/images/cloud/bullseye/latest/debian-11-genericcloud-amd64.qcow2",
		},
		{
			ID: "kvm-alpine-3.23", Name: "Alpine 3.23 KVM",
			Distro: "alpine", Release: "3.23", Arch: "amd64",
			Description: "Alpine Linux 3.23 NoCloud cloud-init image for KVM",
			URL:         "https://dev.alpinelinux.org/~tomalok/alpine-cloud-images/v3.23/nocloud/x86_64/nocloud_alpine-3.23.4-x86_64-bios-cloudinit-r0.qcow2",
		},
		{
			ID: "kvm-centos-9-stream", Name: "CentOS Stream 9 KVM",
			Distro: "centos", Release: "9-stream", Arch: "amd64",
			Description: "CentOS Stream 9 GenericCloud image for KVM",
			URL:         "https://cloud.centos.org/centos/9-stream/x86_64/images/CentOS-Stream-GenericCloud-9-latest.x86_64.qcow2",
		},
		{
			ID: "kvm-archlinux-current", Name: "Arch Linux KVM",
			Distro: "archlinux", Release: "current", Arch: "amd64",
			Description: "Arch Linux (Rolling) cloud image for KVM",
			URL:         "https://geo.mirror.pkgbuild.com/images/latest/Arch-Linux-x86_64-cloudimg.qcow2",
		},
		{
			ID: "kvm-fedora-44", Name: "Fedora 44 KVM",
			Distro: "fedora", Release: "44", Arch: "amd64",
			Description: "Fedora 44 GenericCloud image for KVM",
			URL:         "https://download.fedoraproject.org/pub/fedora/linux/releases/44/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2",
		},
		{
			ID: "kvm-rockylinux-9", Name: "Rocky Linux 9 KVM",
			Distro: "rockylinux", Release: "9", Arch: "amd64",
			Description: "Rocky Linux 9 GenericCloud image for KVM",
			URL:         "https://dl.rockylinux.org/pub/rocky/9/images/x86_64/Rocky-9-GenericCloud-Base.latest.x86_64.qcow2",
		},
		{
			ID: "kvm-windows-10", Name: "Windows 10 KVM",
			Distro: "windows", Release: "10", Arch: "amd64",
			Description: "Windows 10 Enterprise LTSC Evaluation",
			URL:         "https://go.microsoft.com/fwlink/?LinkID=2195404",
		},
	}
}

func FindImage(id string) *Image {
	for _, image := range GetImages() {
		if image.ID == id {
			return &image
		}
	}
	return nil
}

func CacheDir() string {
	return filepath.Join(BaseDir(), "images")
}

func ImagePath(id string) string {
	img := FindImage(id)
	ext := ".qcow2"
	if img != nil && img.Distro == "windows" {
		ext = ".iso"
	}
	return filepath.Join(CacheDir(), id+ext)
}

// IsWindowsImage returns true if the image distro is "windows".
func IsWindowsImage(id string) bool {
	img := FindImage(id)
	return img != nil && img.Distro == "windows"
}

func virtioWinISOPath() string {
	return filepath.Join(CacheDir(), "virtio-win.iso")
}
