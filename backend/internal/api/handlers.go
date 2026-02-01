package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/opendeploy/opendeploy/internal/auth"
	"github.com/opendeploy/opendeploy/internal/exec"
	"github.com/opendeploy/opendeploy/internal/services"
	"github.com/opendeploy/opendeploy/internal/state"
)

// --- Auth handlers ---

type authHandlers struct {
	auth *auth.Auth
}

func (h *authHandlers) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !h.auth.ValidatePassword(body.Password) {
		respondError(w, http.StatusUnauthorized, "invalid password")
		return
	}

	token, err := h.auth.GenerateToken()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	h.auth.SetSessionCookie(w, token)
	respondOK(w, map[string]string{"status": "authenticated"})
}

func (h *authHandlers) setupPassword(w http.ResponseWriter, r *http.Request) {
	if h.auth.IsPasswordSet() {
		respondError(w, http.StatusConflict, "password already set, use change-password instead")
		return
	}

	var body struct {
		Password string `json:"password"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.auth.SetPassword(body.Password); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	token, err := h.auth.GenerateToken()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	h.auth.SetSessionCookie(w, token)
	respondOK(w, map[string]string{"status": "password_set"})
}

func (h *authHandlers) changePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !h.auth.ValidatePassword(body.CurrentPassword) {
		respondError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}
	if err := h.auth.SetPassword(body.NewPassword); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "password_changed"})
}

func (h *authHandlers) logout(w http.ResponseWriter, r *http.Request) {
	h.auth.ClearSessionCookie(w)
	respondOK(w, map[string]string{"status": "logged_out"})
}

func (h *authHandlers) status(w http.ResponseWriter, r *http.Request) {
	respondOK(w, map[string]interface{}{
		"password_set": h.auth.IsPasswordSet(),
	})
}

// --- WiFi handlers ---

type wifiHandlers struct {
	service *services.WifiService
}

func (h *wifiHandlers) scanNetworks(w http.ResponseWriter, r *http.Request) {
	networks, err := h.service.ScanNetworks(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if networks == nil {
		networks = []services.WifiNetwork{}
	}
	respondOK(w, networks)
}

func (h *wifiHandlers) getStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.service.GetStatus(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, status)
}

func (h *wifiHandlers) connect(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SSID     string `json:"ssid"`
		Password string `json:"password"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.SSID == "" {
		respondError(w, http.StatusBadRequest, "SSID is required")
		return
	}

	result, err := h.service.Connect(r.Context(), body.SSID, body.Password, "")
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(w, map[string]interface{}{
		"success": result.Success,
		"job_id":  result.JobID,
	})
}

func (h *wifiHandlers) disconnect(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Disconnect(r.Context()); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "disconnected"})
}

func (h *wifiHandlers) updatePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SSID     string `json:"ssid"`
		Password string `json:"password"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.SSID == "" || body.Password == "" {
		respondError(w, http.StatusBadRequest, "ssid and password are required")
		return
	}

	if err := h.service.UpdatePassword(body.SSID, body.Password); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "updated"})
}

func (h *wifiHandlers) deleteSavedNetwork(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SSID string `json:"ssid"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.SSID == "" {
		respondError(w, http.StatusBadRequest, "ssid is required")
		return
	}

	if err := h.service.DeleteSavedNetwork(body.SSID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "deleted"})
}

func (h *wifiHandlers) getSavedNetworks(w http.ResponseWriter, r *http.Request) {
	networks, err := h.service.GetSavedNetworks()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if networks == nil {
		networks = []state.SavedWifiNetwork{}
	}
	respondOK(w, networks)
}

// --- Tunnel handlers ---

type tunnelHandlers struct {
	service *services.TunnelService
}

func (h *tunnelHandlers) validateToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cfAPI := services.NewCloudflareAPI(body.Token)
	tokenResult, err := cfAPI.VerifyToken(r.Context())
	if err != nil {
		respondError(w, http.StatusUnauthorized, err.Error())
		return
	}

	respondOK(w, map[string]interface{}{
		"valid":  tokenResult.Status == "active",
		"status": tokenResult.Status,
	})
}

func (h *tunnelHandlers) listAccounts(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		respondError(w, http.StatusBadRequest, "token query parameter required")
		return
	}

	cfAPI := services.NewCloudflareAPI(token)
	accounts, err := cfAPI.ListAccounts(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, accounts)
}

func (h *tunnelHandlers) listZones(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		respondError(w, http.StatusBadRequest, "token query parameter required")
		return
	}

	cfAPI := services.NewCloudflareAPI(token)
	zones, err := cfAPI.ListZones(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, zones)
}

func (h *tunnelHandlers) createTunnel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIToken   string `json:"api_token"`
		AccountID  string `json:"account_id"`
		ZoneID     string `json:"zone_id"`
		Subdomain  string `json:"subdomain"`
		Domain     string `json:"domain"`
		TunnelName string `json:"tunnel_name"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.APIToken == "" || body.AccountID == "" || body.ZoneID == "" || body.Subdomain == "" || body.Domain == "" {
		respondError(w, http.StatusBadRequest, "api_token, account_id, zone_id, subdomain, and domain are required")
		return
	}

	if body.TunnelName == "" {
		body.TunnelName = "opendeploy-tunnel"
	}

	req := services.TunnelSetupRequest{
		APIToken:   body.APIToken,
		AccountID:  body.AccountID,
		ZoneID:     body.ZoneID,
		Subdomain:  body.Subdomain,
		Domain:     body.Domain,
		TunnelName: body.TunnelName,
	}

	info, err := h.service.SetupTunnel(r.Context(), req)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(w, info)
}

func (h *tunnelHandlers) getStatus(w http.ResponseWriter, r *http.Request) {
	info, err := h.service.GetStatus(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, info)
}

func (h *tunnelHandlers) verifyAndCleanup(w http.ResponseWriter, r *http.Request) {
	apiKey := r.Header.Get("X-CF-API-Key")

	if err := h.service.VerifyAndCleanupTunnel(r.Context(), apiKey); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "verified"})
}

func (h *tunnelHandlers) restart(w http.ResponseWriter, r *http.Request) {
	if err := h.service.RestartTunnel(r.Context()); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "restarted"})
}

func (h *tunnelHandlers) stopLocalTunnel(w http.ResponseWriter, r *http.Request) {
	if err := h.service.StopLocalTunnel(r.Context()); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "local tunnel stopped"})
}

func (h *tunnelHandlers) deleteTunnel(w http.ResponseWriter, r *http.Request) {
	apiKey := r.Header.Get("X-CF-API-Key")
	if apiKey == "" {
		respondError(w, http.StatusBadRequest, "X-CF-API-Key header is required")
		return
	}

	if err := h.service.DeleteTunnel(r.Context(), apiKey); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "deleted"})
}

func (h *tunnelHandlers) listRoutes(w http.ResponseWriter, r *http.Request) {
	routes, err := h.service.ListRoutes(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if routes == nil {
		routes = []state.TunnelRoute{}
	}
	respondOK(w, routes)
}

func (h *tunnelHandlers) createRoute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Hostname    string `json:"hostname"`
		ZoneID      string `json:"zone_id"`
		LocalScheme string `json:"local_scheme"`
		LocalPort   int    `json:"local_port"`
		PathPrefix  string `json:"path_prefix"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.Hostname == "" || body.ZoneID == "" || body.LocalPort == 0 {
		respondError(w, http.StatusBadRequest, "hostname, zone_id, and local_port are required")
		return
	}

	if body.LocalScheme == "" {
		body.LocalScheme = "http"
	}

	apiKey := r.Header.Get("X-CF-API-Key")
	if apiKey == "" {
		respondError(w, http.StatusBadRequest, "X-CF-API-Key header is required")
		return
	}

	route := &state.TunnelRoute{
		Hostname:    body.Hostname,
		ZoneID:      body.ZoneID,
		LocalScheme: body.LocalScheme,
		LocalPort:   body.LocalPort,
		PathPrefix:  body.PathPrefix,
	}

	if err := h.service.AddRoute(r.Context(), apiKey, route); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(w, route)
}

func (h *tunnelHandlers) updateRoute(w http.ResponseWriter, r *http.Request) {
	routeID := chi.URLParam(r, "id")
	if routeID == "" {
		respondError(w, http.StatusBadRequest, "route id is required")
		return
	}

	var body map[string]interface{}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Check if hostname is being changed (not allowed)
	if _, ok := body["hostname"]; ok {
		respondError(w, http.StatusBadRequest, "hostname cannot be changed, delete and recreate the route instead")
		return
	}

	if err := h.service.UpdateRoute(r.Context(), routeID, body); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(w, map[string]string{"status": "updated"})
}

func (h *tunnelHandlers) deleteRoute(w http.ResponseWriter, r *http.Request) {
	routeID := chi.URLParam(r, "id")
	if routeID == "" {
		respondError(w, http.StatusBadRequest, "route id is required")
		return
	}

	apiKey := r.Header.Get("X-CF-API-Key")
	if apiKey == "" {
		respondError(w, http.StatusBadRequest, "X-CF-API-Key header is required")
		return
	}

	if err := h.service.DeleteRoute(r.Context(), apiKey, routeID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(w, map[string]string{"status": "deleted"})
}

func (h *tunnelHandlers) reorderRoutes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OrderedIDs []string `json:"ordered_ids"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(body.OrderedIDs) == 0 {
		respondError(w, http.StatusBadRequest, "ordered_ids is required")
		return
	}

	if err := h.service.ReorderRoutes(r.Context(), body.OrderedIDs); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(w, map[string]string{"status": "reordered"})
}

func (h *tunnelHandlers) checkPort(w http.ResponseWriter, r *http.Request) {
	portStr := chi.URLParam(r, "port")
	if portStr == "" {
		respondError(w, http.StatusBadRequest, "port is required")
		return
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid port number")
		return
	}

	listening, err := h.service.CheckPortListening(r.Context(), port)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(w, map[string]interface{}{
		"port":      port,
		"listening": listening,
	})
}

func (h *tunnelHandlers) verifyDNS(w http.ResponseWriter, r *http.Request) {
	routeID := chi.URLParam(r, "id")
	if routeID == "" {
		respondError(w, http.StatusBadRequest, "route id is required")
		return
	}

	apiKey := r.Header.Get("X-CF-API-Key")
	if apiKey == "" {
		respondError(w, http.StatusBadRequest, "X-CF-API-Key header is required")
		return
	}

	verified, err := h.service.VerifyDNSRecord(r.Context(), apiKey, routeID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(w, map[string]interface{}{
		"route_id": routeID,
		"verified": verified,
	})
}

func (h *tunnelHandlers) detectDrift(w http.ResponseWriter, r *http.Request) {
	apiKey := r.Header.Get("X-CF-API-Key")
	if apiKey == "" {
		respondError(w, http.StatusBadRequest, "X-CF-API-Key header is required")
		return
	}

	result, err := h.service.DetectConfigDrift(r.Context(), apiKey)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(w, result)
}

func (h *tunnelHandlers) getStoredZones(w http.ResponseWriter, r *http.Request) {
	apiKey := r.Header.Get("X-CF-API-Key")
	if apiKey == "" {
		respondError(w, http.StatusBadRequest, "X-CF-API-Key header is required")
		return
	}

	zones, err := h.service.GetZonesFromStoredToken(r.Context(), apiKey)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, zones)
}

func (h *tunnelHandlers) listAllTunnels(w http.ResponseWriter, r *http.Request) {
	apiKey := r.Header.Get("X-CF-API-Key")
	if apiKey == "" {
		respondError(w, http.StatusBadRequest, "X-CF-API-Key header is required")
		return
	}

	tunnels, err := h.service.ListAllTunnels(r.Context(), apiKey)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, tunnels)
}

func (h *tunnelHandlers) stopRemoteTunnel(w http.ResponseWriter, r *http.Request) {
	accountID := chi.URLParam(r, "accountId")
	tunnelID := chi.URLParam(r, "tunnelId")

	if accountID == "" || tunnelID == "" {
		respondError(w, http.StatusBadRequest, "account_id and tunnel_id are required")
		return
	}

	apiKey := r.Header.Get("X-CF-API-Key")
	if apiKey == "" {
		respondError(w, http.StatusBadRequest, "X-CF-API-Key header is required")
		return
	}

	if err := h.service.StopRemoteTunnel(r.Context(), apiKey, accountID, tunnelID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(w, map[string]string{"status": "tunnel deleted"})
}

// adoptTunnel is a handler to adopt an existing Cloudflare tunnel into OpenDeploy management
func (h *tunnelHandlers) adoptTunnel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TunnelID    string                    `json:"tunnel_id"`
		TunnelToken string                    `json:"tunnel_token"`
		AccountID   string                    `json:"account_id"`
		ZoneID      string                    `json:"zone_id"`
		TunnelName  string                    `json:"tunnel_name"`
		Routes      []state.TunnelRoute       `json:"routes"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.TunnelID == "" || req.TunnelToken == "" || req.AccountID == "" {
		respondError(w, http.StatusBadRequest, "tunnel_id, tunnel_token, and account_id are required")
		return
	}

	if err := h.service.AdoptTunnelWithToken(r.Context(), req.TunnelID, req.TunnelToken, req.AccountID, req.ZoneID, req.TunnelName, req.Routes); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(w, map[string]string{"status": "tunnel adopted successfully"})
}

// --- Project/Deploy handlers ---

type deployHandlers struct {
	service *services.DeployService
	db      *state.DB
}

func (h *deployHandlers) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.db.ListProjects()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if projects == nil {
		projects = []state.Project{}
	}
	respondOK(w, projects)
}

func (h *deployHandlers) createProject(w http.ResponseWriter, r *http.Request) {
	var p state.Project
	if err := parseBody(r, &p); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if p.Name == "" || p.RepoURL == "" {
		respondError(w, http.StatusBadRequest, "name and repo_url are required")
		return
	}
	if p.Branch == "" {
		p.Branch = "main"
	}
	if p.EnvVars == "" {
		p.EnvVars = "{}"
	}

	if err := h.db.CreateProject(&p); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, p)
}

func (h *deployHandlers) getProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := h.db.GetProject(id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p == nil {
		respondError(w, http.StatusNotFound, "project not found")
		return
	}
	respondOK(w, p)
}

func (h *deployHandlers) updateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := h.db.GetProject(id)
	if err != nil || existing == nil {
		respondError(w, http.StatusNotFound, "project not found")
		return
	}

	var update state.Project
	if err := parseBody(r, &update); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	update.ID = id
	if update.Name == "" {
		update.Name = existing.Name
	}
	if update.RepoURL == "" {
		update.RepoURL = existing.RepoURL
	}
	if update.Branch == "" {
		update.Branch = existing.Branch
	}

	if err := h.db.UpdateProject(&update); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, update)
}

func (h *deployHandlers) deleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.db.DeleteProject(id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "deleted"})
}

func (h *deployHandlers) triggerDeploy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := h.db.GetProject(id)
	if err != nil || p == nil {
		respondError(w, http.StatusNotFound, "project not found")
		return
	}

	deployID, err := h.service.Deploy(r.Context(), p)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusAccepted, map[string]string{
		"deploy_id": deployID,
		"status":    "running",
	})
}

func (h *deployHandlers) listDeploys(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	deploys, err := h.db.ListDeploysByProject(id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if deploys == nil {
		deploys = []state.Deploy{}
	}
	respondOK(w, deploys)
}

func (h *deployHandlers) getDeploy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "deployId")
	d, err := h.db.GetDeploy(id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if d == nil {
		respondError(w, http.StatusNotFound, "deploy not found")
		return
	}
	respondOK(w, d)
}

// --- Nginx handlers ---

type nginxHandlers struct {
	service *services.NginxService
	db      *state.DB
}

func (h *nginxHandlers) listSites(w http.ResponseWriter, r *http.Request) {
	sites, err := h.db.ListNginxSites()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sites == nil {
		sites = []state.NginxSite{}
	}
	respondOK(w, sites)
}

func (h *nginxHandlers) createSite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Domain       string `json:"domain"`
		ProjectID    string `json:"project_id"`
		FrontendPath string `json:"frontend_path"`
		ProxyEnabled bool   `json:"proxy_enabled"`
		ProxyPort    int    `json:"proxy_port"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Generate nginx config
	configContent := h.service.GenerateConfig(services.NginxSiteConfig{
		Domain:       body.Domain,
		FrontendPath: body.FrontendPath,
		ProxyEnabled: body.ProxyEnabled,
		ProxyPort:    body.ProxyPort,
	})

	// Write config
	if err := h.service.WriteConfig(body.Domain, configContent); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Save to DB
	site := &state.NginxSite{
		ProjectID:  body.ProjectID,
		Domain:     body.Domain,
		ConfigPath: body.Domain,
		IsActive:   true,
	}
	if err := h.db.CreateNginxSite(site); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, map[string]interface{}{
		"site":   site,
		"config": configContent,
	})
}

func (h *nginxHandlers) updateSite(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := h.db.GetNginxSite(id)
	if err != nil || existing == nil {
		respondError(w, http.StatusNotFound, "site not found")
		return
	}

	var body struct {
		Domain       string `json:"domain"`
		FrontendPath string `json:"frontend_path"`
		ProxyEnabled bool   `json:"proxy_enabled"`
		ProxyPort    int    `json:"proxy_port"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	configContent := h.service.GenerateConfig(services.NginxSiteConfig{
		Domain:       body.Domain,
		FrontendPath: body.FrontendPath,
		ProxyEnabled: body.ProxyEnabled,
		ProxyPort:    body.ProxyPort,
	})

	if err := h.service.WriteConfig(body.Domain, configContent); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	existing.Domain = body.Domain
	existing.ConfigPath = body.Domain
	if err := h.db.UpdateNginxSite(existing); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(w, map[string]interface{}{
		"site":   existing,
		"config": configContent,
	})
}

func (h *nginxHandlers) deleteSite(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.db.DeleteNginxSite(id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "deleted"})
}

func (h *nginxHandlers) testConfig(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.TestConfig(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, result)
}

func (h *nginxHandlers) reload(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Reload(r.Context()); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "reloaded"})
}

func (h *nginxHandlers) getLogs(w http.ResponseWriter, r *http.Request) {
	lines := 100
	if q := r.URL.Query().Get("lines"); q != "" {
		if n, err := strconv.Atoi(q); err == nil {
			lines = n
		}
	}

	entries, err := h.service.GetAccessLog(lines)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, entries)
}

func (h *nginxHandlers) listConfigFiles(w http.ResponseWriter, r *http.Request) {
	files, err := h.service.ListConfigFiles()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if files == nil {
		files = []services.NginxFileInfo{}
	}
	respondOK(w, files)
}

func (h *nginxHandlers) readConfigFile(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	content, err := h.service.ReadConfigFile(name)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(w, map[string]string{"name": name, "content": content})
}

func (h *nginxHandlers) writeConfigFile(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		Content string `json:"content"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Content == "" {
		respondError(w, http.StatusBadRequest, "content is required")
		return
	}
	if err := h.service.WriteConfigFile(name, body.Content); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "saved"})
}

func (h *nginxHandlers) deleteConfigFile(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.service.DeleteConfigFile(name); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "deleted"})
}

func (h *nginxHandlers) enableSite(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.service.EnableSite(name); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "enabled"})
}

func (h *nginxHandlers) disableSite(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.service.DisableSite(name); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "disabled"})
}

// --- System handlers ---

type systemHandlers struct {
	service *services.SystemService
	db      *state.DB
}

func (h *systemHandlers) getStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.service.GetStats()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, stats)
}

func (h *systemHandlers) getInfo(w http.ResponseWriter, r *http.Request) {
	info, err := h.service.GetInfo()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, info)
}

func (h *systemHandlers) getSetupState(w http.ResponseWriter, r *http.Request) {
	states, err := h.db.GetAllSetupStates()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, states)
}

// --- Services handlers ---

type servicesHandlers struct {
	system *services.SystemService
	runner *exec.Runner
}

func (h *servicesHandlers) listServices(w http.ResponseWriter, r *http.Request) {
	statuses, err := h.system.GetAllServiceStatuses()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, statuses)
}

func (h *servicesHandlers) getService(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	status, err := h.system.GetServiceStatus(name)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, status)
}

func (h *servicesHandlers) startService(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.system.StartService(r.Context(), name); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "started"})
}

func (h *servicesHandlers) restartService(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	_, err := h.system.RestartService(r.Context(), name, "")
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "restarted"})
}

func (h *servicesHandlers) stopService(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.system.StopService(r.Context(), name); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "stopped"})
}

func (h *servicesHandlers) getServiceLogs(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	lines := 100
	if q := r.URL.Query().Get("lines"); q != "" {
		if n, err := strconv.Atoi(q); err == nil {
			lines = n
		}
	}

	entries, err := h.system.GetJournalLogs(name, lines)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, entries)
}

// --- Jobs handlers ---

type jobHandlers struct {
	db     *state.DB
	runner *exec.Runner
}

func (h *jobHandlers) getJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	j, err := h.db.GetJob(id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if j == nil {
		respondError(w, http.StatusNotFound, "job not found")
		return
	}
	respondOK(w, j)
}

func (h *jobHandlers) cancelJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.runner.Cancel(id); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "cancelled"})
}

func (h *jobHandlers) listJobs(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil {
			limit = n
		}
	}

	jobs, err := h.db.ListJobs(limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if jobs == nil {
		jobs = []state.Job{}
	}
	respondOK(w, jobs)
}

// suppress unused import warning
var _ = json.Marshal

// --- Environment Variables handlers ---

type envHandlers struct {
	db *state.DB
}

// --- Internet Check handlers ---

type internetHandlers struct {
	service *services.InternetService
}

func (h *internetHandlers) runChecks(w http.ResponseWriter, r *http.Request) {
	checks, err := h.service.RunChecks(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, checks)
}

// --- Environment Variables handlers ---

func (h *envHandlers) listEnvVars(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	vars, err := h.db.ListEnvVariables(projectID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if vars == nil {
		vars = []state.EnvVariable{}
	}
	// Mask secret values in response
	for i := range vars {
		if vars[i].IsSecret {
			vars[i].Value = "••••••••"
		}
	}
	respondOK(w, vars)
}

func (h *envHandlers) createEnvVar(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	var body struct {
		Key      string `json:"key"`
		Value    string `json:"value"`
		IsSecret bool   `json:"is_secret"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Key == "" || body.Value == "" {
		respondError(w, http.StatusBadRequest, "key and value are required")
		return
	}

	envVar := &state.EnvVariable{
		ProjectID: projectID,
		Key:       body.Key,
		Value:     body.Value,
		IsSecret:  body.IsSecret,
	}
	if err := h.db.CreateEnvVariable(envVar); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if envVar.IsSecret {
		envVar.Value = "••••••••"
	}
	respondJSON(w, http.StatusCreated, envVar)
}

func (h *envHandlers) updateEnvVar(w http.ResponseWriter, r *http.Request) {
	envID := chi.URLParam(r, "envId")
	existing, err := h.db.GetEnvVariable(envID)
	if err != nil || existing == nil {
		respondError(w, http.StatusNotFound, "env variable not found")
		return
	}

	var body struct {
		Key      *string `json:"key"`
		Value    *string `json:"value"`
		IsSecret *bool   `json:"is_secret"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.Key != nil {
		existing.Key = *body.Key
	}
	if body.Value != nil {
		existing.Value = *body.Value
	}
	if body.IsSecret != nil {
		existing.IsSecret = *body.IsSecret
	}

	if err := h.db.CreateEnvVariable(existing); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing.IsSecret {
		existing.Value = "••••••••"
	}
	respondOK(w, existing)
}

func (h *envHandlers) deleteEnvVar(w http.ResponseWriter, r *http.Request) {
	envID := chi.URLParam(r, "envId")
	if err := h.db.DeleteEnvVariable(envID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondOK(w, map[string]string{"status": "deleted"})
}

func (h *envHandlers) bulkImport(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	var body struct {
		Content  string `json:"content"`
		IsSecret bool   `json:"is_secret"`
	}
	if err := parseBody(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Parse .env format: KEY=VALUE lines, skip comments and empty lines
	lines := strings.Split(body.Content, "\n")
	count := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		// Remove surrounding quotes
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}

		envVar := &state.EnvVariable{
			ProjectID: projectID,
			Key:       key,
			Value:     value,
			IsSecret:  body.IsSecret,
		}
		if err := h.db.CreateEnvVariable(envVar); err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		count++
	}

	respondOK(w, map[string]interface{}{
		"status":   "imported",
		"imported": count,
	})
}
