package server

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"clicd/internal/api"
	"clicd/internal/config"
	"clicd/internal/lxc"
)

// webFS holds embedded frontend files
var webFS http.FileSystem

// corsMiddleware adds CORS headers
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && isAllowedOrigin(origin, r.Host) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")

		if r.Method == http.MethodOptions {
			if origin := r.Header.Get("Origin"); origin != "" && !isAllowedOrigin(origin, r.Host) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

func isAllowedOrigin(origin string, requestHost string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	originHost := normalizeHost(u.Host)
	host := normalizeHost(requestHost)
	if originHost == host {
		return true
	}
	return isLoopbackHost(originHost) && isLoopbackHost(host)
}

func normalizeHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return strings.ToLower(h)
	}
	return strings.ToLower(host)
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// setupRoutes configures API and static routes
func setupRoutes(mux *http.ServeMux) {
	// API routes
	mux.HandleFunc("/api/login", corsMiddleware(api.HandleLogin))
	mux.HandleFunc("/api/check-auth", corsMiddleware(api.AuthMiddleware(api.HandleCheckAuth)))
	mux.HandleFunc("/api/change-password", corsMiddleware(api.AdminMiddleware(api.HandleAdminPasswordChange)))
	mux.HandleFunc("/api/change-username", corsMiddleware(api.AdminMiddleware(api.HandleAdminUsernameChange)))
	mux.HandleFunc("/api/login-logs", corsMiddleware(api.AdminMiddleware(api.HandleLoginLogs)))
	mux.HandleFunc("/api/containers", corsMiddleware(api.AuthMiddleware(api.SubUserMiddleware(api.HandleContainers))))
	mux.HandleFunc("/api/containers/", corsMiddleware(api.AuthMiddleware(api.SubUserMiddleware(api.HandleSingleContainer))))
	mux.HandleFunc("/api/templates", corsMiddleware(api.AuthMiddleware(api.HandleTemplates)))
	mux.HandleFunc("/api/images", corsMiddleware(api.AdminMiddleware(api.HandleImages)))
	mux.HandleFunc("/api/images/download", corsMiddleware(api.AdminMiddleware(api.HandleImageDownload)))
	mux.HandleFunc("/api/images/delete", corsMiddleware(api.AdminMiddleware(api.HandleImageDelete)))
	mux.HandleFunc("/api/images/toggle", corsMiddleware(api.AdminMiddleware(api.HandleImageToggle)))
	mux.HandleFunc("/api/images/enabled", corsMiddleware(api.AuthMiddleware(api.SubUserMiddleware(api.HandleEnabledImages))))
	mux.HandleFunc("/api/dashboard", corsMiddleware(api.AdminMiddleware(api.HandleDashboard)))
	mux.HandleFunc("/api/host-info", corsMiddleware(api.AdminMiddleware(api.HandleHostInfo)))
	mux.HandleFunc("/api/snapshots", corsMiddleware(api.AdminMiddleware(api.HandleSnapshots)))
	mux.HandleFunc("/api/ipv6/status", corsMiddleware(api.AdminMiddleware(api.HandleIPv6Status)))
	mux.HandleFunc("/api/oversell", corsMiddleware(api.AdminMiddleware(api.HandleOversell)))
	mux.HandleFunc("/api/oversell/status", corsMiddleware(api.AdminMiddleware(api.HandleOversellStatus)))
	mux.HandleFunc("/api/oversell/reclaim", corsMiddleware(api.AdminMiddleware(api.HandleOversellReclaim)))
	mux.HandleFunc("/api/tasks", corsMiddleware(api.AuthMiddleware(api.SubUserMiddleware(api.HandleTasks))))
	mux.HandleFunc("/api/tasks/", corsMiddleware(api.AuthMiddleware(api.AdminMiddleware(api.HandleTaskDelete))))
	mux.HandleFunc("/api/batch-create", corsMiddleware(api.AdminMiddleware(api.HandleBatchCreate)))
	mux.HandleFunc("/api/batch-action", corsMiddleware(api.AdminMiddleware(api.HandleBatchAction)))
	mux.HandleFunc("/api/sub-user/create", corsMiddleware(api.AdminMiddleware(api.HandleSubUserCreate)))
	mux.HandleFunc("/api/sub-user/login", corsMiddleware(api.HandleSubUserLogin))
	mux.HandleFunc("/api/sub-user/access", corsMiddleware(api.HandleSubUserAccessCode))
	mux.HandleFunc("/api/audit-logs", corsMiddleware(api.AdminMiddleware(api.HandleAuditLogs)))
	mux.HandleFunc("/api/security/alerts", corsMiddleware(api.AdminMiddleware(api.HandleSecurityAlerts)))
	mux.HandleFunc("/api/security/check", corsMiddleware(api.AdminMiddleware(api.HandleSecurityCheck)))
	mux.HandleFunc("/api/security/logs", corsMiddleware(api.AdminMiddleware(api.HandleSecurityLogs)))
	mux.HandleFunc("/api/security/summary", corsMiddleware(api.AdminMiddleware(api.HandleContainerSecuritySummary)))
	mux.HandleFunc("/api/ssh-ticket", corsMiddleware(api.AuthMiddleware(api.HandleWebSSHTicket)))
	mux.HandleFunc("/api/ssh", api.HandleWebSSH) // WebSocket

	// API Key management
	mux.HandleFunc("/api/api-keys", corsMiddleware(api.AdminMiddleware(api.HandleApiKeys)))
	mux.HandleFunc("/api/api-keys/", corsMiddleware(api.AdminMiddleware(api.HandleApiKeyDelete)))

	// Static files
	if webFS != nil {
		fs := http.FileServer(webFS)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// API routes already handled above
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			// Try to serve file
			path := r.URL.Path
			f, err := webFS.Open(path)
			if err != nil {
				// SPA fallback: serve index.html
				indexFile, err := webFS.Open("index.html")
				if err != nil {
					http.Error(w, "Not found", http.StatusNotFound)
					return
				}
				defer indexFile.Close()
				stat, _ := indexFile.Stat()
				http.ServeContent(w, r, "index.html", stat.ModTime(), indexFile)
				return
			}
			defer f.Close()
			fs.ServeHTTP(w, r)
		})
	}
}

// Run starts the HTTP server
func Run() error {
	// Use embedded frontend files
	webFS = GetEmbeddedFS()
	startExpiryMonitor()

	mux := http.NewServeMux()
	setupRoutes(mux)

	addr := fmt.Sprintf("0.0.0.0:%d", config.AppConfig.Port)
	log.Printf("CLICD Web Server starting on http://0.0.0.0:%d", config.AppConfig.Port)
	log.Printf("Admin user: %s", config.AppConfig.AdminUser)

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return server.ListenAndServe()
}

func startExpiryMonitor() {
	manager := lxc.NewManager()
	go func() {
		manager.StopExpiredContainers(time.Now())

		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for now := range ticker.C {
			manager.StopExpiredContainers(now)
		}
	}()
}
