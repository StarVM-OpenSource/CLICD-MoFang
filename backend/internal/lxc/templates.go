package lxc

// Template represents an LXC image template
type Template struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Distro      string `json:"distro"`
	Release     string `json:"release"`
	Arch        string `json:"arch"`
	Variant     string `json:"variant"`
	Description string `json:"description"`
}

// GetTemplates returns available LXC image templates (only verified working ones)
func GetTemplates() []Template {
	return []Template{
		{
			ID: "ubuntu-noble", Name: "Ubuntu 24.04",
			Distro: "ubuntu", Release: "noble", Arch: "amd64",
			Description: "Ubuntu 24.04 LTS",
		},
		{
			ID: "ubuntu-jammy", Name: "Ubuntu 22.04",
			Distro: "ubuntu", Release: "jammy", Arch: "amd64",
			Description: "Ubuntu 22.04 LTS",
		},
		{
			ID: "debian-bookworm", Name: "Debian 12",
			Distro: "debian", Release: "bookworm", Arch: "amd64",
			Description: "Debian 12 (Bookworm)",
		},
		{
			ID: "debian-bullseye", Name: "Debian 11",
			Distro: "debian", Release: "bullseye", Arch: "amd64",
			Description: "Debian 11 (Bullseye)",
		},
		{
			ID: "alpine-3.21", Name: "Alpine 3.21",
			Distro: "alpine", Release: "3.21", Arch: "amd64",
			Description: "Alpine Linux 3.21",
		},
		{
			ID: "centos-9-stream", Name: "CentOS 9 Stream",
			Distro: "centos", Release: "9-Stream", Arch: "amd64",
			Description: "CentOS 9 Stream",
		},
		{
			ID: "archlinux-current", Name: "Arch Linux",
			Distro: "archlinux", Release: "current", Arch: "amd64",
			Description: "Arch Linux (Rolling)",
		},
		{
			ID: "fedora-44", Name: "Fedora 44",
			Distro: "fedora", Release: "44", Arch: "amd64",
			Description: "Fedora 44",
		},
		{
			ID: "rockylinux-10", Name: "Rocky Linux 10",
			Distro: "rockylinux", Release: "10", Arch: "amd64",
			Description: "Rocky Linux 10",
		},
	}
}

// FindTemplate finds a template by ID
func FindTemplate(id string) *Template {
	templates := GetTemplates()
	for _, t := range templates {
		if t.ID == id {
			return &t
		}
	}
	return nil
}
