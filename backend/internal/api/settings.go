package api

import (
	"encoding/json"
	"net/http"
	"time"

	"clicd/internal/config"

	"golang.org/x/crypto/bcrypt"
)

type LoginLog struct {
	Time      string `json:"time"`
	Username  string `json:"username"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	Success   bool   `json:"success"`
}

var loginLogs = make([]LoginLog, 0)

// RecordLoginLog adds a login attempt to the log (persisted to config)
func RecordLoginLog(username, ip, userAgent string, success bool) {
	config.AddLoginLog(username, ip, userAgent, success)

	log := LoginLog{
		Time:      time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Username:  username,
		IP:        ip,
		UserAgent: userAgent,
		Success:   success,
	}
	loginLogs = append(loginLogs, log)
	if len(loginLogs) > 200 {
		loginLogs = loginLogs[len(loginLogs)-200:]
	}
}

// RestoreLoginLogs restores login logs from config
func RestoreLoginLogs() {
	for _, l := range config.AppConfig.LoginLogs {
		loginLogs = append(loginLogs, LoginLog{
			Time:      l.Time,
			Username:  l.Username,
			IP:        l.IP,
			UserAgent: l.UserAgent,
			Success:   l.Success,
		})
	}
}

// HandleLoginLogs returns login history
func HandleLoginLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "loginlog:read") {
		return
	}

	// Return in reverse (newest first)
	reversed := make([]LoginLog, len(loginLogs))
	for i, l := range loginLogs {
		reversed[len(loginLogs)-1-i] = l
	}
	if reversed == nil {
		reversed = []LoginLog{}
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: reversed})
}

// HandleAdminPasswordChange changes admin password
func HandleAdminPasswordChange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}

	if len(req.NewPassword) < 6 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "新密码至少 6 位"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(config.AppConfig.AdminPassHash), []byte(req.OldPassword)); err != nil {
		jsonResponse(w, http.StatusUnauthorized, APIResponse{Success: false, Message: "当前密码不正确"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "密码加密失败"})
		return
	}

	config.AppConfig.AdminPassHash = string(hash)
	if err := config.SaveConfig(); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "保存配置失败"})
		return
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "密码修改成功"})
}

// HandleAdminUsernameChange changes admin username
func HandleAdminUsernameChange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	var req struct {
		NewUsername string `json:"new_username"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}

	if len(req.NewUsername) < 3 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "用户名至少 3 位"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(config.AppConfig.AdminPassHash), []byte(req.Password)); err != nil {
		jsonResponse(w, http.StatusUnauthorized, APIResponse{Success: false, Message: "密码不正确"})
		return
	}

	config.AppConfig.AdminUser = req.NewUsername
	if err := config.SaveConfig(); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "保存配置失败"})
		return
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "用户名修改成功"})
}
