const API_PREFIX = '/api/v1'

// Helper for standard JSON fetch requests
async function fetchApi<T>(endpoint: string, options: RequestInit = {}): Promise<T> {
  const url = `${API_PREFIX}${endpoint}`
  const headers = {
    'Content-Type': 'application/json',
    ...options.headers,
  }

  const response = await fetch(url, { ...options, headers })

  if (!response.ok) {
    let errorMsg = `API Error: ${response.status}`
    try {
      const errorData = await response.json()
      errorMsg = errorData.error || errorMsg
    } catch (e) {
      // Ignore if not JSON
    }
    throw new Error(errorMsg)
  }

  return response.json()
}

// Helper for tunnel API calls that require Cloudflare API key
async function fetchTunnelApi<T>(endpoint: string, apiKey: string, options: RequestInit = {}): Promise<T> {
  const url = `${API_PREFIX}${endpoint}`
  const headers = {
    'Content-Type': 'application/json',
    'X-CF-API-Key': apiKey,
    ...options.headers,
  }

  const response = await fetch(url, { ...options, headers })

  if (!response.ok) {
    let errorMsg = `API Error: ${response.status}`
    try {
      const errorData = await response.json()
      errorMsg = errorData.error || errorMsg
    } catch (e) {
      // Ignore if not JSON
    }
    throw new Error(errorMsg)
  }

  return response.json()
}

export const authApi = {
  changePassword: (data: any) => fetchApi<{ status: string }>('/auth/change-password', {
    method: 'POST',
    body: JSON.stringify(data),
  }),
}

// System API
export const systemApi = {
  getStats: () => fetchApi<{ cpu: number; ram: number; temp: number; uptime: string }>('/system/stats'),
  getInfo: () => fetchApi<{ hostname: string; ip: string; model: string; os: string }>('/system/info'),
  getSetupState: () => fetchApi<Record<string, string>>('/system/setup-state'),
}

// WiFi API
export const wifiApi = {
  getNetworks: () => fetchApi<{ ssid: string; signal: number; security: string; connected: boolean; saved: boolean }[]>('/wifi/networks'),
  getStatus: () => fetchApi<{ connected: boolean; ssid?: string; ip?: string; signal?: number; state: string }>('/wifi/status'),
  connect: (ssid: string, password?: string) => fetchApi<{ success: boolean; job_id: string }>('/wifi/connect', {
    method: 'POST',
    body: JSON.stringify({ ssid, password }),
  }),
  disconnect: () => fetchApi<{ status: string }>('/wifi/disconnect', { method: 'POST' }),
  updatePassword: (ssid: string, password: string) => fetchApi<{ status: string }>('/wifi/password', {
    method: 'PUT',
    body: JSON.stringify({ ssid, password }),
  }),
  deleteSaved: (ssid: string) => fetchApi<{ status: string }>('/wifi/saved', {
    method: 'DELETE',
    body: JSON.stringify({ ssid }),
  }),
  getSavedNetworks: () => fetchApi<{ ssid: string; security: string; last_connected_at?: string }[]>('/wifi/saved'),
}

// Tunnel API
export const tunnelApi = {
  validateToken: (token: string) => fetchTunnelApi<{ valid: boolean; status: string }>('/tunnel/validate-token', token, {
    method: 'POST',
    body: JSON.stringify({ token }),
  }),
  getAccounts: (token: string) => fetchTunnelApi<{ id: string; name: string }[]>(`/tunnel/accounts?token=${encodeURIComponent(token)}`, token),
  getZones: (token: string) => fetchTunnelApi<{ id: string; name: string }[]>(`/tunnel/zones?token=${encodeURIComponent(token)}`, token),
  getStoredZones: (apiKey: string) => fetchTunnelApi<{ id: string; name: string }[]>('/tunnel/zones/stored', apiKey),
  create: (apiKey: string, data: {
    api_token: string;
    account_id: string;
    zone_id: string;
    subdomain: string;
    domain: string;
    tunnel_name: string;
  }) => fetchTunnelApi<{ tunnel_id: string; tunnel_name: string; domain: string; status: string }>('/tunnel/create', apiKey, {
    method: 'POST',
    body: JSON.stringify(data),
  }),
  getStatus: () => fetchApi<{ tunnel_id?: string; tunnel_name?: string; domain?: string; status: string }>('/tunnel/status'),
  verifyAndCleanup: (apiKey: string) => fetchTunnelApi<{ status: string }>('/tunnel/verify', apiKey, {
    method: 'POST',
  }),
  restart: () => fetchApi<{ status: string }>('/tunnel/restart', { method: 'POST' }),
  stopLocal: () => fetchApi<{ status: string }>('/tunnel/stop', { method: 'POST' }),
  delete: (apiKey: string) => fetchTunnelApi<{ status: string }>('/tunnel', apiKey, {
    method: 'DELETE',
  }),
  listAll: (apiKey: string) => fetchTunnelApi<{
    id: string;
    name: string;
    status: string;
    created_at: string;
    domains: string[];
    account_id: string;
    is_managed: boolean;
  }[]>('/tunnel/all', apiKey),
  stopRemote: (apiKey: string, accountId: string, tunnelId: string) => fetchTunnelApi<{ status: string }>(`/tunnel/remote/${accountId}/${tunnelId}`, apiKey, {
    method: 'DELETE',
  }),
  startRemote: (apiKey: string, accountId: string, tunnelId: string) => fetchTunnelApi<{ status: string; tunnel?: any }>(`/tunnel/remote/${accountId}/${tunnelId}/start`, apiKey, {
    method: 'POST',
  }),
  adoptTunnel: (data: {
    tunnel_id: string;
    tunnel_token: string;
    account_id: string;
    zone_id: string;
    tunnel_name: string;
    routes: any[];
  }) => fetchApi<{ status: string }>('/tunnel/adopt', {
    method: 'POST',
    body: JSON.stringify(data),
  }),

  // Route management
  listRoutes: () => fetchApi<{
    id: string;
    tunnel_id: string;
    hostname: string;
    zone_id: string;
    dns_record_id?: string;
    local_scheme: string;
    local_port: number;
    path_prefix?: string;
    sort_order: number;
    created_at: string;
    updated_at: string;
  }[]>('/tunnel/routes'),
  createRoute: (apiKey: string, data: {
    hostname: string;
    zone_id: string;
    local_scheme: string;
    local_port: number;
    path_prefix?: string;
  }) => fetchTunnelApi<any>('/tunnel/routes', apiKey, {
    method: 'POST',
    body: JSON.stringify(data),
  }),
  updateRoute: (routeId: string, data: {
    local_scheme?: string;
    local_port?: number;
    path_prefix?: string;
  }) => fetchApi<{ status: string }>(`/tunnel/routes/${routeId}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  }),
  deleteRoute: (apiKey: string, routeId: string) => fetchTunnelApi<{ status: string }>(`/tunnel/routes/${routeId}`, apiKey, {
    method: 'DELETE',
  }),
  reorderRoutes: (orderedIds: string[]) => fetchApi<{ status: string }>('/tunnel/routes/reorder', {
    method: 'POST',
    body: JSON.stringify({ ordered_ids: orderedIds }),
  }),
  checkPort: (port: number) => fetchApi<{ port: number; listening: boolean }>(`/tunnel/check-port/${port}`),
  verifyDNS: (apiKey: string, routeId: string) => fetchTunnelApi<{ route_id: string; verified: boolean }>(`/tunnel/routes/${routeId}/verify-dns`, apiKey),
  detectDrift: (apiKey: string) => fetchTunnelApi<{
    has_drift: boolean;
    local_routes: number;
    cloudflare_routes: number;
    missing_in_cloudflare: string[];
    extra_in_cloudflare: string[];
  }>('/tunnel/detect-drift', apiKey),
}

// Deploy API
export const deployApi = {
  listProjects: () => fetchApi<any[]>('/projects'),
  getProject: (id: string) => fetchApi<any>(`/projects/${id}`),
  createProject: (data: any) => fetchApi<any>('/projects', {
    method: 'POST',
    body: JSON.stringify(data),
  }),
  updateProject: (id: string, data: any) => fetchApi<any>(`/projects/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  }),
  deleteProject: (id: string) => fetchApi<{ status: string }>(`/projects/${id}`, {
    method: 'DELETE',
  }),
  triggerDeploy: (projectId: string, options?: {
    domain?: string;
    zone_id?: string;
    manual_domain?: boolean;
    enable_nginx?: boolean;
  }) => fetchApi<{ deploy_id: string; status: string }>(`/projects/${projectId}/deploy`, {
    method: 'POST',
    body: options ? JSON.stringify(options) : undefined,
  }),
  rebuildProject: (projectId: string) => fetchApi<{ deploy_id: string; status: string }>(`/projects/${projectId}/rebuild`, {
    method: 'POST',
  }),
  listDeploys: (projectId: string) => fetchApi<any[]>(`/projects/${projectId}/deploys`),
  getDeploy: (deployId: string) => fetchApi<any>(`/deploys/${deployId}`),
  getDeployLogs: (deployId: string) => fetchApi<any[]>(`/deploys/${deployId}/logs`),
  // SSE stream URL (not a fetch, used with EventSource)
  getDeployLogStreamUrl: (deployId: string) => `${API_PREFIX}/deploys/${deployId}/logs/stream`,
  // Long-poll endpoint
  pollDeployLogs: (deployId: string, after?: string) => {
    const params = after ? `?after=${encodeURIComponent(after)}` : ''
    return fetchApi<any[]>(`/deploys/${deployId}/logs/poll${params}`)
  },
}

// System Cleanup API
export const cleanupApi = {
  runCleanup: () => fetchApi<{
    orphan_containers_removed: number
    dangling_images_removed: number
    stale_deploys_fixed: number
    errors: string[]
  }>('/system/cleanup', { method: 'POST' }),
  getStatus: () => fetchApi<{
    orphan_containers_removed: number
    dangling_images_removed: number
    stale_deploys_fixed: number
    errors: string[]
  }>('/system/cleanup/status'),
}

// Nginx API
export const nginxApi = {
  testConfig: () => fetchApi<{ success: boolean; output: string }>('/nginx/test', { method: 'POST' }),
  reload: () => fetchApi<{ status: string }>('/nginx/reload', { method: 'POST' }),
  applyConfig: (data: any) => fetchApi<any>('/nginx/sites', {
    method: 'POST',
    body: JSON.stringify(data),
  }),
  getAccessLogs: (lines?: number) => fetchApi<{ timestamp: string; level: string; message: string }[]>(`/nginx/logs${lines ? `?lines=${lines}` : ''}`),

  // File management
  listFiles: () => fetchApi<{ name: string; enabled: boolean; size: number }[]>('/nginx/files'),
  readFile: (name: string) => fetchApi<{ name: string; content: string }>(`/nginx/files/${encodeURIComponent(name)}`),
  writeFile: (name: string, content: string) => fetchApi<{ status: string }>(`/nginx/files/${encodeURIComponent(name)}`, {
    method: 'PUT',
    body: JSON.stringify({ content }),
  }),
  deleteFile: (name: string) => fetchApi<{ status: string }>(`/nginx/files/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  }),
  enableSite: (name: string) => fetchApi<{ status: string }>(`/nginx/files/${encodeURIComponent(name)}/enable`, {
    method: 'POST',
  }),
  disableSite: (name: string) => fetchApi<{ status: string }>(`/nginx/files/${encodeURIComponent(name)}/disable`, {
    method: 'POST',
  }),
}

// Services API
export const servicesApi = {
  list: () => fetchApi<{ name: string; status: string; uptime?: string }[]>('/services'),
  get: (name: string) => fetchApi<{ name: string; status: string; uptime?: string }>(`/services/${name}`),
  start: (name: string) => fetchApi<{ status: string }>(`/services/${name}/start`, { method: 'POST' }),
  stop: (name: string) => fetchApi<{ status: string }>(`/services/${name}/stop`, { method: 'POST' }),
  restart: (name: string) => fetchApi<{ status: string }>(`/services/${name}/restart`, { method: 'POST' }),
  getLogs: (name: string, lines?: number) => fetchApi<{ timestamp: string; level: string; message: string }[]>(
    `/services/${name}/logs${lines ? `?lines=${lines}` : ''}`
  ),
}

// Internet Check API
export const internetApi = {
  runChecks: () => fetchApi<{
    dns_resolution: { success: boolean; value: string; error?: string };
    cloudflare_ping: { success: boolean; value: string; error?: string };
    download_speed: { success: boolean; value: string; error?: string };
  }>('/internet/check'),
}

// Environment Variables API
export interface EnvVariable {
  id: string
  project_id: string
  key: string
  value: string
  is_secret: boolean
  created_at: string
  updated_at: string
}

export const envApi = {
  list: (projectId: string) => fetchApi<EnvVariable[]>(`/projects/${projectId}/env`),
  create: (projectId: string, data: { key: string; value: string; is_secret: boolean }) =>
    fetchApi<EnvVariable>(`/projects/${projectId}/env`, {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  update: (envId: string, data: { key?: string; value?: string; is_secret?: boolean }) =>
    fetchApi<EnvVariable>(`/env/${envId}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),
  delete: (envId: string) => fetchApi<{ status: string }>(`/env/${envId}`, {
    method: 'DELETE',
  }),
  bulkImport: (projectId: string, content: string, isSecret: boolean) =>
    fetchApi<{ status: string; imported: number }>(`/projects/${projectId}/env/bulk`, {
      method: 'POST',
      body: JSON.stringify({ content, is_secret: isSecret }),
    }),
}
