package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"clicd/internal/config"
	"clicd/internal/kvm"
	"clicd/internal/lxc"
)

// ImageInfo represents a template image with its download/enable status.
type ImageInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Distro      string `json:"distro"`
	Release     string `json:"release"`
	Arch        string `json:"arch"`
	Description string `json:"description"`
	Downloaded  bool   `json:"downloaded"`
	Enabled     bool   `json:"enabled"`
	Downloading bool   `json:"downloading"`
	SizeBytes   int64  `json:"size_bytes"`
	ManualPath  string `json:"manual_path,omitempty"`
	Desktop     string `json:"desktop,omitempty"`
}

var imageDownloadsMu sync.Mutex
var imageDownloads = map[string]bool{}

// isImageDownloaded checks if the LXC download cache exists for a template.
func isImageDownloaded(distro, release, arch string) bool {
	downloaded, _ := imageDownloadedInfo(distro, release, arch)
	return downloaded
}

// imageDownloadedInfo returns whether the image is downloaded and its total size in bytes.
func imageDownloadedInfo(distro, release, arch string) (bool, int64) {
	cachePath := filepath.Join("/var/cache/lxc/download", distro, release, arch)
	info, err := os.Stat(cachePath)
	if err != nil || !info.IsDir() {
		return false, 0
	}
	// Check directly for rootfs.tar.xz (some LXC versions store it here)
	if fi, err := os.Stat(filepath.Join(cachePath, "rootfs.tar.xz")); err == nil {
		return true, fi.Size()
	}
	if fi, err := os.Stat(filepath.Join(cachePath, "meta.tar.xz")); err == nil {
		return true, fi.Size()
	}
	// Check one level deeper (LXC uses variant subdirectories like "default")
	entries, err := os.ReadDir(cachePath)
	if err != nil {
		return false, 0
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		subPath := filepath.Join(cachePath, entry.Name())
		if fi, err := os.Stat(filepath.Join(subPath, "rootfs.tar.xz")); err == nil {
			return true, fi.Size()
		}
		if fi, err := os.Stat(filepath.Join(subPath, "meta.tar.xz")); err == nil {
			return true, fi.Size()
		}
	}
	return false, 0
}

// getEnabledImageSet returns the set of enabled image IDs.
// If none have been explicitly set, all templates are enabled by default.
func getEnabledImageSet() map[string]bool {
	set := make(map[string]bool)
	if len(config.AppConfig.EnabledImages) == 0 {
		for _, t := range lxc.GetTemplates() {
			set[t.ID] = true
		}
		for _, t := range kvm.GetImages() {
			set[t.ID] = true
		}
	} else {
		for _, id := range config.AppConfig.EnabledImages {
			set[id] = true
		}
	}
	return set
}

// HandleImages returns the list of templates with download/enable status.
func HandleImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	enabledSet := getEnabledImageSet()

	templates := lxc.GetTemplates()
	images := make([]ImageInfo, 0, len(templates)+len(kvm.GetImages()))
	for _, t := range templates {
		_, downloading := imageDownloads[t.ID]
		downloaded, size := imageDownloadedInfo(t.Distro, t.Release, t.Arch)
		images = append(images, ImageInfo{
			ID:          t.ID,
			Name:        t.Name,
			Type:        config.VirtualizationLXC,
			Distro:      t.Distro,
			Release:     t.Release,
			Arch:        t.Arch,
			Description: t.Description,
			Downloaded:  downloaded,
			Enabled:     enabledSet[t.ID],
			Downloading: downloading,
			SizeBytes:   size,
		})
	}
	for _, t := range kvm.GetImages() {
		_, downloading := imageDownloads[t.ID]
		downloaded, size := kvm.ImageDownloadedInfo(t.ID)
		manualPath := ""
		if t.Distro == "windows" {
			manualPath = kvm.ImagePath(t.ID)
		}
		images = append(images, ImageInfo{
			ID:          t.ID,
			Name:        t.Name,
			Type:        config.VirtualizationKVM,
			Distro:      t.Distro,
			Release:     t.Release,
			Arch:        t.Arch,
			Description: t.Description,
			Downloaded:  downloaded,
			Enabled:     enabledSet[t.ID],
			Downloading: downloading,
			SizeBytes:   size,
			ManualPath:  manualPath,
			Desktop:     t.Desktop,
		})
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: images})
}

// HandleImageDownload downloads a template image from the LXC image server.
func HandleImageDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	var req struct {
		TemplateID string `json:"template_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TemplateID == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "template_id required"})
		return
	}

	tmpl := lxc.FindTemplate(req.TemplateID)
	if tmpl == nil {
		image := kvm.FindImage(req.TemplateID)
		if image == nil {
			jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Template not found"})
			return
		}
		if ok, _ := kvm.ImageDownloadedInfo(image.ID); ok {
			ensureImageEnabled(image.ID)
			jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Already downloaded"})
			return
		}
		imageDownloadsMu.Lock()
		if imageDownloads[req.TemplateID] {
			imageDownloadsMu.Unlock()
			jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "Already downloading"})
			return
		}
		imageDownloads[req.TemplateID] = true
		imageDownloadsMu.Unlock()
		defer func() {
			imageDownloadsMu.Lock()
			delete(imageDownloads, req.TemplateID)
			imageDownloadsMu.Unlock()
		}()
		ensureImageEnabled(image.ID)
		if err := kvm.DownloadImage(*image); err != nil {
			message := "Download failed: " + err.Error()

			jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: message})
			return
		}
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Downloaded successfully"})
		return
	}

	// Already downloaded? Just enable if needed.
	if isImageDownloaded(tmpl.Distro, tmpl.Release, tmpl.Arch) {
		ensureImageEnabled(tmpl.ID)
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Already downloaded"})
		return
	}

	// Already downloading?
	imageDownloadsMu.Lock()
	if imageDownloads[req.TemplateID] {
		imageDownloadsMu.Unlock()
		jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "Already downloading"})
		return
	}
	imageDownloads[req.TemplateID] = true
	imageDownloadsMu.Unlock()

	defer func() {
		imageDownloadsMu.Lock()
		delete(imageDownloads, req.TemplateID)
		imageDownloadsMu.Unlock()
	}()

	// Auto-enable on download
	ensureImageEnabled(tmpl.ID)

	// Download via lxc-create with a temp container, then destroy it.
	tmpName := fmt.Sprintf("clicd-img-dl-%s", tmpl.ID)
	args := []string{"-n", tmpName, "-t", "download", "--",
		"-d", tmpl.Distro, "-r", tmpl.Release, "-a", tmpl.Arch}
	if tmpl.Variant != "" {
		args = append(args, "--variant", tmpl.Variant)
	}
	cmd := exec.Command("lxc-create", args...)
	output, err := cmd.CombinedOutput()

	// Clean up the temp container unconditionally.
	exec.Command("lxc-destroy", "-n", tmpName, "-f").Run()
	os.RemoveAll(filepath.Join("/var/lib/lxc", tmpName))

	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: fmt.Sprintf("Download failed: %v, output: %s", err, string(output)),
		})
		return
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Downloaded successfully"})
}

// HandleImageDelete deletes a cached template image from disk.
func HandleImageDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	var req struct {
		TemplateID string `json:"template_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TemplateID == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "template_id required"})
		return
	}

	tmpl := lxc.FindTemplate(req.TemplateID)
	if tmpl == nil {
		if image := kvm.FindImage(req.TemplateID); image != nil {
			if err := kvm.DeleteImage(image.ID); err != nil {
				jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to delete image cache: " + err.Error()})
				return
			}
			removeImageEnabled(image.ID)
			jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Deleted"})
			return
		}
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Template not found"})
		return
	}

	// Remove cache directory
	cachePath := filepath.Join("/var/cache/lxc/download", tmpl.Distro, tmpl.Release, tmpl.Arch)
	if err := os.RemoveAll(cachePath); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to delete image cache: %v", err),
		})
		return
	}

	// Remove from enabled list
	removeImageEnabled(tmpl.ID)

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Deleted"})
}

// HandleImageToggle enables or disables a template image.
func HandleImageToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	var req struct {
		TemplateID string `json:"template_id"`
		Enabled    bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TemplateID == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "template_id required"})
		return
	}

	if req.Enabled {
		ensureImageEnabled(req.TemplateID)
	} else {
		removeImageEnabled(req.TemplateID)
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "OK"})
}

// HandleEnabledImages returns only the enabled AND downloaded templates.
// Used by container create / reinstall to filter available templates.
func HandleEnabledImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	runtime := runtimeFromRequest(r.URL.Query().Get("type"))
	enabledSet := getEnabledImageSet()

	result := make([]map[string]string, 0)
	if runtime == config.VirtualizationKVM {
		for _, t := range kvm.GetImages() {
			if downloaded, _ := kvm.ImageDownloadedInfo(t.ID); enabledSet[t.ID] && downloaded {
				result = append(result, map[string]string{
					"id": t.ID, "name": t.Name, "distro": t.Distro, "release": t.Release, "arch": t.Arch,
					"description": t.Description, "type": config.VirtualizationKVM, "desktop": t.Desktop,
				})
			}
		}
	} else {
		for _, t := range lxc.GetTemplates() {
			if enabledSet[t.ID] && isImageDownloaded(t.Distro, t.Release, t.Arch) {
				result = append(result, map[string]string{
					"id": t.ID, "name": t.Name, "distro": t.Distro, "release": t.Release, "arch": t.Arch,
					"variant": t.Variant, "description": t.Description, "type": config.VirtualizationLXC,
				})
			}
		}
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: result})
}

func isTemplateEnabledAndDownloaded(templateID string) bool {
	return isImageEnabledAndDownloaded(templateID, runtimeFromTemplateID(templateID))
}

func isImageEnabledAndDownloaded(templateID string, runtime string) bool {
	runtime = runtimeFromRequest(runtime)
	if runtime == config.VirtualizationKVM {
		image := kvm.FindImage(templateID)
		if image == nil {
			return false
		}
		enabledSet := getEnabledImageSet()
		downloaded, _ := kvm.ImageDownloadedInfo(image.ID)
		return enabledSet[image.ID] && downloaded
	}
	tmpl := lxc.FindTemplate(templateID)
	if tmpl == nil {
		return false
	}
	enabledSet := getEnabledImageSet()
	return enabledSet[tmpl.ID] && isImageDownloaded(tmpl.Distro, tmpl.Release, tmpl.Arch)
}

func ensureImageEnabled(id string) {
	// If the enabled list is empty, all templates are currently enabled by default.
	// We must populate the list with all template IDs first so that explicit toggles stick.
	if len(config.AppConfig.EnabledImages) == 0 {
		for _, t := range lxc.GetTemplates() {
			config.AppConfig.EnabledImages = append(config.AppConfig.EnabledImages, t.ID)
		}
		for _, t := range kvm.GetImages() {
			config.AppConfig.EnabledImages = append(config.AppConfig.EnabledImages, t.ID)
		}
		config.SaveConfig()
		return // Already contains all IDs including this one
	}
	found := false
	for _, eid := range config.AppConfig.EnabledImages {
		if eid == id {
			found = true
			break
		}
	}
	if !found {
		config.AppConfig.EnabledImages = append(config.AppConfig.EnabledImages, id)
		config.SaveConfig()
	}
}

func removeImageEnabled(id string) {
	// If the enabled list is empty, populate it first with all templates,
	// then remove the one being disabled.
	if len(config.AppConfig.EnabledImages) == 0 {
		for _, t := range lxc.GetTemplates() {
			if t.ID != id {
				config.AppConfig.EnabledImages = append(config.AppConfig.EnabledImages, t.ID)
			}
		}
		for _, t := range kvm.GetImages() {
			if t.ID != id {
				config.AppConfig.EnabledImages = append(config.AppConfig.EnabledImages, t.ID)
			}
		}
		config.SaveConfig()
		return
	}
	filtered := make([]string, 0, len(config.AppConfig.EnabledImages))
	for _, eid := range config.AppConfig.EnabledImages {
		if eid != id {
			filtered = append(filtered, eid)
		}
	}
	if len(filtered) != len(config.AppConfig.EnabledImages) {
		config.AppConfig.EnabledImages = filtered
		config.SaveConfig()
	}
}
