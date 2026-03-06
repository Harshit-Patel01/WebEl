package api

import (
	"context"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/opendeploy/opendeploy/internal/auth"
	"github.com/opendeploy/opendeploy/internal/config"
	"github.com/opendeploy/opendeploy/internal/exec"
	"github.com/opendeploy/opendeploy/internal/services"
	"github.com/opendeploy/opendeploy/internal/state"
	"github.com/opendeploy/opendeploy/internal/ws"
	pstatic "github.com/opendeploy/opendeploy/static"
	"go.uber.org/zap"
)

func NewRouter(cfg *config.Config, db *state.DB, hub *ws.Hub, runner *exec.Runner, logger *zap.Logger, wifiMonitor *services.WifiMonitor) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.RequestID)
	r.Use(loggingMiddleware(logger))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Initialize auth
	a := auth.New(db, cfg.Security.SessionDuration, cfg.Security.BcryptCost, cfg.Security.LANOnly, logger)

	// Initialize services
	wifiSvc := services.NewWifiService(runner, logger, db)
	tunnelSvc := services.NewTunnelService(runner, cfg.Cloudflared, db, logger)
	deploySvc := services.NewDeployService(runner, db, cfg.Deploy, logger)
	deploySvc.SetBroadcaster(hub)
	nginxSvc := services.NewNginxService(runner, cfg.Nginx, logger)
	systemSvc := services.NewSystemService(runner, logger)
	internetSvc := services.NewInternetService(runner, logger)
	containerSvc := services.NewContainerService(runner, db, cfg.Deploy, logger)
	cleanupSvc := services.NewCleanupService(runner, db, cfg.Deploy, logger)

	// Connect services to deploy service
	deploySvc.SetNginxService(nginxSvc)
	deploySvc.SetContainerService(containerSvc)
	deploySvc.SetWifiMonitor(wifiMonitor)

	// Run startup cleanup: fix stale deployments and orphan containers
	go func() {
		startupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		report := cleanupSvc.RunFullCleanup(startupCtx)
		if report.StaleDeploysFixed > 0 || report.OrphanContainersRemoved > 0 {
			logger.Info("startup cleanup completed",
				zap.Int("staleDeploysFixed", report.StaleDeploysFixed),
				zap.Int("orphanContainersRemoved", report.OrphanContainersRemoved),
			)
		}
	}()

	// Initialize handlers
	authH := &authHandlers{auth: a}
	wifiH := &wifiHandlers{service: wifiSvc}
	tunnelH := &tunnelHandlers{service: tunnelSvc}
	deployH := &deployHandlers{service: deploySvc, db: db}
	nginxH := &nginxHandlers{service: nginxSvc, db: db}
	systemH := &systemHandlers{service: systemSvc, db: db}
	servicesH := &servicesHandlers{system: systemSvc, runner: runner}
	jobH := &jobHandlers{db: db, runner: runner}
	envH := &envHandlers{db: db}
	internetH := &internetHandlers{service: internetSvc}
	containerH := &containerHandlers{service: containerSvc, db: db}
	deployLogH := &deployLogHandlers{db: db}
	sseH := &sseHandlers{db: db, logger: logger}
	cleanupH := &cleanupHandlers{service: cleanupSvc}

	// Public routes (no auth required)
	r.Route("/api/v1/auth", func(r chi.Router) {
		// Auth routes are public
		r.Post("/login", authH.login)
		r.Post("/setup", authH.setupPassword)
		r.Get("/status", authH.status)
	})

	// WebSocket endpoint (auth checked on upgrade)
	r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWS(hub, w, r)
	})

	// Protected routes
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(a.Middleware)

		// Auth
		r.Post("/auth/logout", authH.logout)
		r.Post("/auth/change-password", authH.changePassword)

		// WiFi
		r.Get("/wifi/networks", wifiH.scanNetworks)
		r.Get("/wifi/status", wifiH.getStatus)
		r.Post("/wifi/connect", wifiH.connect)
		r.Post("/wifi/disconnect", wifiH.disconnect)
		r.Put("/wifi/password", wifiH.updatePassword)
		r.Get("/wifi/saved", wifiH.getSavedNetworks)
		r.Delete("/wifi/saved", wifiH.deleteSavedNetwork)

		// Tunnel
		r.Post("/tunnel/validate-token", tunnelH.validateToken)
		r.Get("/tunnel/accounts", tunnelH.listAccounts)
		r.Get("/tunnel/zones", tunnelH.listZones)
		r.Get("/tunnel/zones/stored", tunnelH.getStoredZones)
		r.Post("/tunnel/create", tunnelH.createTunnel)
		r.Get("/tunnel/status", tunnelH.getStatus)
		r.Post("/tunnel/verify", tunnelH.verifyAndCleanup)
		r.Post("/tunnel/restart", tunnelH.restart)
		r.Post("/tunnel/stop", tunnelH.stopLocalTunnel)
		r.Delete("/tunnel", tunnelH.deleteTunnel)
		r.Get("/tunnel/all", tunnelH.listAllTunnels)
		r.Delete("/tunnel/remote/{accountId}/{tunnelId}", tunnelH.stopRemoteTunnel)

		// Tunnel Routes
		r.Get("/tunnel/routes", tunnelH.listRoutes)
		r.Post("/tunnel/routes", tunnelH.createRoute)
		r.Put("/tunnel/routes/{id}", tunnelH.updateRoute)
		r.Delete("/tunnel/routes/{id}", tunnelH.deleteRoute)
		r.Post("/tunnel/routes/reorder", tunnelH.reorderRoutes)
		r.Get("/tunnel/check-port/{port}", tunnelH.checkPort)
		r.Get("/tunnel/routes/{id}/verify-dns", tunnelH.verifyDNS)
		r.Get("/tunnel/detect-drift", tunnelH.detectDrift)
		r.Post("/tunnel/adopt", tunnelH.adoptTunnel)

		// Projects & Deploy
		r.Get("/projects", deployH.listProjects)
		r.Post("/projects", deployH.createProject)
		r.Get("/projects/{id}", deployH.getProject)
		r.Put("/projects/{id}", deployH.updateProject)
		r.Delete("/projects/{id}", deployH.deleteProject)
		r.Post("/projects/{id}/deploy", deployH.triggerDeploy)
		r.Post("/projects/{id}/rebuild", deployH.rebuildProject)
		r.Get("/projects/{id}/deploys", deployH.listDeploys)
		r.Get("/deploys/{deployId}", deployH.getDeploy)

		// Nginx
		r.Get("/nginx/sites", nginxH.listSites)
		r.Post("/nginx/sites", nginxH.createSite)
		r.Put("/nginx/sites/{id}", nginxH.updateSite)
		r.Delete("/nginx/sites/{id}", nginxH.deleteSite)
		r.Post("/nginx/test", nginxH.testConfig)
		r.Post("/nginx/reload", nginxH.reload)
		r.Get("/nginx/logs", nginxH.getLogs)

		// Nginx file management
		r.Get("/nginx/files", nginxH.listConfigFiles)
		r.Get("/nginx/files/{name}", nginxH.readConfigFile)
		r.Put("/nginx/files/{name}", nginxH.writeConfigFile)
		r.Delete("/nginx/files/{name}", nginxH.deleteConfigFile)
		r.Post("/nginx/files/{name}/enable", nginxH.enableSite)
		r.Post("/nginx/files/{name}/disable", nginxH.disableSite)

		// Services
		r.Get("/services", servicesH.listServices)
		r.Get("/services/{name}", servicesH.getService)
		r.Post("/services/{name}/start", servicesH.startService)
		r.Post("/services/{name}/restart", servicesH.restartService)
		r.Post("/services/{name}/stop", servicesH.stopService)
		r.Get("/services/{name}/logs", servicesH.getServiceLogs)

		// System
		r.Get("/system/stats", systemH.getStats)
		r.Get("/system/info", systemH.getInfo)
		r.Get("/system/setup-state", systemH.getSetupState)

		// Jobs
		r.Get("/jobs/{id}", jobH.getJob)
		r.Post("/jobs/{id}/cancel", jobH.cancelJob)
		r.Get("/jobs", jobH.listJobs)

		// Environment Variables
		r.Get("/projects/{id}/env", envH.listEnvVars)
		r.Post("/projects/{id}/env", envH.createEnvVar)
		r.Post("/projects/{id}/env/bulk", envH.bulkImport)
		r.Put("/env/{envId}", envH.updateEnvVar)
		r.Delete("/env/{envId}", envH.deleteEnvVar)

		// Containers
		r.Get("/projects/{id}/containers", containerH.listContainers)
		r.Post("/projects/{id}/containers/{containerId}/start", containerH.startContainer)
		r.Post("/projects/{id}/containers/stop", containerH.stopContainer)
		r.Post("/projects/{id}/containers/restart", containerH.restartContainer)
		r.Delete("/projects/{id}/containers", containerH.removeContainer)
		r.Get("/containers/{containerId}/logs", containerH.getContainerLogs)

		// Deploy Logs
		r.Get("/deploys/{deployId}/logs", deployLogH.getDeployLogs)
		r.Get("/deploys/{deployId}/logs/stream", sseH.streamDeployLogs)
		r.Get("/deploys/{deployId}/logs/poll", sseH.longPollDeployLogs)

		// System Cleanup
		r.Post("/system/cleanup", cleanupH.runCleanup)
		r.Get("/system/cleanup/status", cleanupH.getCleanupStatus)

		// Internet Checks
		r.Get("/internet/check", internetH.runChecks)
	})

	// Health check (always public)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		respondOK(w, map[string]string{"status": "ok"})
	})

	// Serve embedded frontend files (catch-all)
	frontendFS, _ := fs.Sub(pstatic.Frontend, "frontend")
	fileServer := http.FileServer(http.FS(frontendFS))

	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		path := strings.TrimPrefix(req.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// Check if the exact file exists in the embedded FS
		if _, err := fs.Stat(frontendFS, path); err == nil {
			// Set proper content type for static assets
			if strings.HasSuffix(path, ".js") {
				w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			} else if strings.HasSuffix(path, ".css") {
				w.Header().Set("Content-Type", "text/css; charset=utf-8")
			} else if strings.HasSuffix(path, ".json") {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
			} else if strings.HasSuffix(path, ".html") {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
			}

			// Add cache control for static assets
			if strings.Contains(path, "/_next/static/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}

			fileServer.ServeHTTP(w, req)
			return
		}

		// Check for .html suffix (Next.js static routes: /dashboard -> dashboard.html)
		if _, err := fs.Stat(frontendFS, path+".html"); err == nil {
			req.URL.Path = "/" + path + ".html"
			fileServer.ServeHTTP(w, req)
			return
		}

		// For paths that look like static assets but don't exist, return 404
		if strings.Contains(path, ".") && (strings.HasSuffix(path, ".js") ||
			strings.HasSuffix(path, ".css") ||
			strings.HasSuffix(path, ".json") ||
			strings.HasSuffix(path, ".png") ||
			strings.HasSuffix(path, ".jpg") ||
			strings.HasSuffix(path, ".svg") ||
			strings.HasSuffix(path, ".ico")) {
			http.NotFound(w, req)
			return
		}

		// SPA fallback — serve index.html for unknown paths
		indexData, _ := fs.ReadFile(frontendFS, "index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexData)
	})

	return r
}
