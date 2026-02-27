'use client'

import { useState, useEffect } from 'react'
import { motion } from 'framer-motion'
import { RefreshCw, Plus, ExternalLink, AlertCircle, CheckCircle2, Loader2, Key, Play, Pause, RotateCcw, Trash2, Settings, Edit3 } from 'lucide-react'
import Link from 'next/link'
import SectionBadge from '@/components/ui/SectionBadge'
import StatusPill from '@/components/ui/StatusPill'
import { tunnelApi } from '@/lib/api'
import { apiKeyStorage } from '@/utils/apiKey'

interface TunnelWithDomains {
  id: string
  name: string
  status: string
  created_at: string
  domains: string[]
  account_id: string
  is_managed: boolean
}

interface Account {
  id: string
  name: string
}

interface Zone {
  id: string
  name: string
}

export default function TunnelDashboardPage() {
  const [tunnels, setTunnels] = useState<TunnelWithDomains[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [apiKey, setApiKey] = useState<string | null>(null)
  const [accounts, setAccounts] = useState<Account[]>([])
  const [zones, setZones] = useState<Zone[]>([])
  const [showApiKeyForm, setShowApiKeyForm] = useState(false)
  const [inputApiKey, setInputApiKey] = useState('')
  const [validating, setValidating] = useState(false)
  const [selectedTunnel, setSelectedTunnel] = useState<TunnelWithDomains | null>(null)
  const [showManageForm, setShowManageForm] = useState(false)
  const [selectedAccount, setSelectedAccount] = useState('')
  const [selectedZone, setSelectedZone] = useState('')
  const [newSubdomain, setNewSubdomain] = useState('')
  const [currentDomain, setCurrentDomain] = useState('')
  const [addingDomain, setAddingDomain] = useState(false)

  // Load API key from storage
  useEffect(() => {
    const storedKey = apiKeyStorage.get()
    setApiKey(storedKey)
    if (storedKey) {
      loadTunnels(storedKey)
    }
  }, [])

  const loadTunnels = async (key: string) => {
    try {
      setLoading(true)
      setError('')
      const data = await tunnelApi.listAll(key)
      setTunnels(data)
    } catch (err) {
      setError(String(err))
    } finally {
      setLoading(false)
    }
  }

  const handleValidateApiKey = async () => {
    if (!inputApiKey.trim()) {
      setError('Please enter an API token')
      return
    }

    setValidating(true)
    setError('')

    try {
      // Validate token and get accounts/zones
      const tokenResult = await tunnelApi.validateToken(inputApiKey)
      if (!tokenResult.valid || tokenResult.status !== 'active') {
        setError(`Token is not active: ${tokenResult.status}`)
        setValidating(false)
        return
      }

      const accountsResult = await tunnelApi.getAccounts(inputApiKey)
      if (accountsResult.length === 0) {
        setError('No Cloudflare accounts found for this token')
        setValidating(false)
        return
      }
      setAccounts(accountsResult)
      setSelectedAccount(accountsResult[0].id)

      const zonesResult = await tunnelApi.getZones(inputApiKey)
      if (zonesResult.length === 0) {
        setError('No domains found on this Cloudflare account. Add a domain to Cloudflare first.')
        setValidating(false)
        return
      }
      setZones(zonesResult)
      setSelectedZone(zonesResult[0].id)

      // Save key to storage and load tunnels
      apiKeyStorage.set(inputApiKey)
      setApiKey(inputApiKey)
      loadTunnels(inputApiKey)
      setShowApiKeyForm(false)
    } catch (err) {
      setError(String(err))
    } finally {
      setValidating(false)
    }
  }

  const handleRestartTunnel = async (tunnel: TunnelWithDomains) => {
    // For now, just refresh the tunnel list
    if (apiKey) {
      loadTunnels(apiKey)
    }
  }

  const handleStartTunnel = async (tunnel: TunnelWithDomains) => {
    if (tunnel.is_managed) {
      // If already managed, restart the tunnel
      handleRestartTunnel(tunnel);
      return;
    }

    // For unmanaged tunnels, we need to show a form to get the tunnel token
    // Since we can't get the token from the Cloudflare API, we need to ask the user
    const token = prompt(`To adopt tunnel "${tunnel.name}", please enter the tunnel token. This is required to configure cloudflared on this server:`);

    if (!token) {
      return; // User cancelled
    }

    try {
      // We need to get the zones and routes for this tunnel from Cloudflare
      // For now, we'll use the first domain as the zone, but ideally we'd fetch the tunnel config
      const zone = zones.length > 0 ? zones[0] : null;

      if (!zone) {
        alert('No zones available to associate with this tunnel. Please add a domain to your Cloudflare account first.');
        return;
      }

      // For adoption, we'll use the domains found as routes
      const routes = tunnel.domains.map((domain, index) => ({
        hostname: domain,
        zone_id: zone.id, // This is a simplification - in reality, domains could be from different zones
        local_scheme: 'http',
        local_port: 80,
        sort_order: index
      }));

      await tunnelApi.adoptTunnel({
        tunnel_id: tunnel.id,
        tunnel_token: token,
        account_id: tunnel.account_id,
        zone_id: zone.id,
        tunnel_name: tunnel.name,
        routes
      });

      // Refresh the tunnel list
      if (apiKey) {
        loadTunnels(apiKey);
      }
    } catch (err) {
      setError(String(err));
    }
  }

  const handleStopTunnel = async (tunnel: TunnelWithDomains) => {
    if (!confirm(`Are you sure you want to ${tunnel.is_managed ? 'stop' : 'remove'} tunnel "${tunnel.name}"?`)) {
      return
    }

    try {
      if (tunnel.is_managed) {
        // For managed tunnels, stop local service only (don't delete from Cloudflare)
        await tunnelApi.stopLocal() // This stops the local service only
      } else {
        // For unmanaged tunnels, we just show a message since they're not running
        alert(`Tunnel "${tunnel.name}" is not currently managed by OpenDeploy, so no local action is needed.`);
      }

      if (apiKey) {
        loadTunnels(apiKey)
      }
    } catch (err) {
      setError(String(err))
    }
  }

  const handleDeleteTunnel = async (tunnel: TunnelWithDomains) => {
    if (!confirm(`Are you sure you want to delete tunnel "${tunnel.name}"? This will remove it from Cloudflare.`)) {
      return
    }

    try {
      await tunnelApi.stopRemote(apiKey!, tunnel.account_id, tunnel.id)
      if (apiKey) {
        loadTunnels(apiKey)
      }
    } catch (err) {
      setError(String(err))
    }
  }

  const handleAddDomain = async () => {
    if (!selectedTunnel || !selectedZone || !newSubdomain.trim()) {
      setError('Please select a tunnel, zone, and subdomain')
      return
    }

    setAddingDomain(true)
    setError('')

    try {
      const zone = zones.find(z => z.id === selectedZone)
      if (!zone) throw new Error('Selected zone not found')

      const hostname = `${newSubdomain}.${zone.name}`

      await tunnelApi.createRoute(apiKey!, {
        hostname,
        zone_id: selectedZone,
        local_scheme: 'http',
        local_port: 80,
        path_prefix: undefined
      })

      // Refresh tunnels
      if (apiKey) {
        loadTunnels(apiKey)
      }

      setNewSubdomain('')
      setShowManageForm(false)
    } catch (err) {
      setError(String(err))
    } finally {
      setAddingDomain(false)
    }
  }

  if (!apiKey && !showApiKeyForm) {
    return (
      <motion.div
        initial={{ opacity: 0, x: 20 }}
        animate={{ opacity: 1, x: 0 }}
        transition={{ duration: 0.3 }}
      >
        <div className="mb-8">
          <SectionBadge label="TUNNEL MANAGEMENT" />
        </div>

        <div className="bg-bg-secondary rounded-card border border-border-dark p-12 text-center">
          <div className="max-w-lg mx-auto">
            <Key className="mx-auto mb-6 text-accent-lime" size={48} />
            <h2 className="font-serif text-h2 mb-4">Access Cloudflare Tunnels</h2>
            <p className="text-body text-text-secondary mb-6">
              Enter your Cloudflare API token to view and manage your tunnels
            </p>

            <div className="mb-8 p-6 bg-bg-primary rounded-lg border border-border-dark text-left">
              <p className="font-mono text-small text-text-secondary mb-4">
                Don't have a token? <a
                  href="https://dash.cloudflare.com/profile/api-tokens"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-accent-lime hover:text-accent-lime-muted underline inline-flex items-center gap-1"
                >
                  Create one in Cloudflare <ExternalLink size={12} />
                </a>
              </p>

              <div className="space-y-2">
                <p className="font-mono text-label uppercase tracking-wider text-text-primary mb-3">Required Permissions:</p>
                <div className="flex items-center gap-2">
                  <CheckCircle2 size={14} className="text-accent-lime flex-shrink-0" />
                  <span className="font-mono text-small text-text-secondary">Account → Cloudflare Tunnel → Edit</span>
                </div>
                <div className="flex items-center gap-2">
                  <CheckCircle2 size={14} className="text-accent-lime flex-shrink-0" />
                  <span className="font-mono text-small text-text-secondary">Zone → DNS → Edit</span>
                </div>
                <div className="flex items-center gap-2">
                  <CheckCircle2 size={14} className="text-accent-lime flex-shrink-0" />
                  <span className="font-mono text-small text-text-secondary">Account → Account Settings → Read</span>
                </div>
              </div>
            </div>

            <button
              onClick={() => setShowApiKeyForm(true)}
              className="px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all"
            >
              Enter API Token
            </button>
          </div>
        </div>
      </motion.div>
    )
  }

  if (showApiKeyForm) {
    return (
      <motion.div
        initial={{ opacity: 0, x: 20 }}
        animate={{ opacity: 1, x: 0 }}
        transition={{ duration: 0.3 }}
      >
        <div className="mb-8">
          <SectionBadge label="TUNNEL API ACCESS" />
        </div>

        <div className="bg-bg-secondary rounded-card border border-border-dark p-8 max-w-2xl">
          <h2 className="font-serif text-h2 mb-6">Enter Cloudflare API Token</h2>

          <div className="mb-6 p-5 bg-bg-primary rounded-lg border border-border-dark">
            <div className="flex items-start gap-4 mb-4">
              <Key size={24} className="text-accent-lime flex-shrink-0 mt-1" />
              <div className="flex-1">
                <h3 className="font-mono text-small font-bold text-text-primary mb-2">Create Your API Token</h3>
                <p className="font-mono text-small text-text-secondary mb-3">
                  Visit your Cloudflare dashboard to generate a new API token with the required permissions.
                </p>
                <a
                  href="https://dash.cloudflare.com/profile/api-tokens"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-2 px-4 py-2 bg-bg-secondary border border-border-dark rounded-lg font-mono text-small text-accent-lime hover:bg-border-dark transition-all"
                >
                  Open Cloudflare Dashboard <ExternalLink size={14} />
                </a>
              </div>
            </div>

            <div className="pt-4 border-t border-border-dark">
              <p className="font-mono text-label uppercase tracking-wider text-text-primary mb-3">Required Permissions:</p>
              <div className="grid grid-cols-1 gap-2">
                <div className="flex items-center gap-2">
                  <CheckCircle2 size={14} className="text-accent-lime flex-shrink-0" />
                  <span className="font-mono text-small text-text-secondary">Account → Cloudflare Tunnel → Edit</span>
                </div>
                <div className="flex items-center gap-2">
                  <CheckCircle2 size={14} className="text-accent-lime flex-shrink-0" />
                  <span className="font-mono text-small text-text-secondary">Zone → DNS → Edit</span>
                </div>
                <div className="flex items-center gap-2">
                  <CheckCircle2 size={14} className="text-accent-lime flex-shrink-0" />
                  <span className="font-mono text-small text-text-secondary">Account → Account Settings → Read</span>
                </div>
              </div>
            </div>
          </div>

          <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
            Cloudflare API Token
          </label>
          <input
            type="password"
            value={inputApiKey}
            onChange={e => setInputApiKey(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && handleValidateApiKey()}
            className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary placeholder:text-text-secondary mb-4"
            placeholder="Paste your API token here"
          />

          {error && (
            <div className="mb-4 p-3 bg-red-900/20 border border-red-700 rounded-lg flex items-start gap-2">
              <AlertCircle size={16} className="text-red-500 mt-0.5 flex-shrink-0" />
              <span className="font-mono text-small text-red-400">{error}</span>
            </div>
          )}

          <div className="flex gap-3">
            <button
              onClick={handleValidateApiKey}
              disabled={validating || !inputApiKey.trim()}
              className="px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all disabled:opacity-50 flex items-center gap-2"
            >
              {validating ? (
                <>
                  <Loader2 size={16} className="animate-spin" />
                  Validating...
                </>
              ) : (
                'Validate & Load Tunnels'
              )}
            </button>
            <button
              onClick={() => setShowApiKeyForm(false)}
              className="px-6 py-3 bg-bg-secondary border border-border-dark rounded-lg font-mono text-small text-text-primary hover:bg-border-dark transition-all"
            >
              Cancel
            </button>
          </div>
        </div>
      </motion.div>
    )
  }

  if (loading) {
    return (
      <motion.div
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        className="flex items-center justify-center h-64"
      >
        <Loader2 size={48} className="animate-spin text-accent-lime" />
      </motion.div>
    )
  }

  return (
    <motion.div
      initial={{ opacity: 0, x: 20 }}
      animate={{ opacity: 1, x: 0 }}
      transition={{ duration: 0.3 }}
    >
      <div className="mb-8 flex items-center justify-between">
        <SectionBadge label="TUNNEL MANAGEMENT" />
        <div className="flex gap-3">
          <button
            onClick={() => {
              if (apiKey) {
                loadTunnels(apiKey)
              }
            }}
            className="inline-flex items-center gap-2 px-4 py-2 bg-bg-secondary border border-border-dark rounded-lg font-mono text-small text-text-primary hover:bg-border-dark transition-all"
          >
            <RefreshCw size={16} /> Refresh
          </button>
          <Link
            href="/tunnel/manage"
            className="inline-flex items-center gap-2 px-4 py-2 bg-bg-secondary border border-border-dark rounded-lg font-mono text-small text-text-primary hover:bg-border-dark transition-all"
          >
            <Settings size={16} /> Configure Tunnel
          </Link>
          <button
            onClick={() => setShowApiKeyForm(true)}
            className="inline-flex items-center gap-2 px-4 py-2 bg-bg-secondary border border-border-dark rounded-lg font-mono text-small text-text-primary hover:bg-border-dark transition-all"
          >
            <Key size={16} /> API Key
          </button>
        </div>
      </div>

      {error && (
        <div className="mb-6 p-4 bg-red-900/20 border border-red-700 rounded-lg flex items-start gap-3">
          <AlertCircle size={20} className="text-red-500 flex-shrink-0 mt-0.5" />
          <div>
            <p className="font-mono text-small text-red-400">{error}</p>
          </div>
        </div>
      )}

      {tunnels.length === 0 && (
        <div className="bg-bg-secondary rounded-card border border-border-dark p-12 text-center">
          <p className="font-mono text-text-secondary mb-6">No tunnels found in your Cloudflare account.</p>
          <div className="flex flex-col sm:flex-row gap-3 justify-center">
            <Link
              href="/tunnel/manage"
              className="inline-flex items-center gap-2 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all"
            >
              <Plus size={16} /> Create New Tunnel
            </Link>
            <button
              onClick={() => setShowApiKeyForm(true)}
              className="inline-flex items-center gap-2 px-6 py-3 bg-bg-secondary border border-border-dark rounded-lg font-mono text-small text-text-primary hover:bg-border-dark transition-all"
            >
              <Key size={14} /> Change API Token
            </button>
          </div>
        </div>
      )}

      {tunnels.length > 0 && (
        <div className="space-y-4">
          {tunnels.map((tunnel) => (
            <motion.div
              key={tunnel.id}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              className="bg-bg-secondary rounded-card border border-border-dark p-6 hover:border-accent-lime/30 transition-colors"
            >
              <div className="flex items-start justify-between mb-4">
                <div className="flex-1">
                  <div className="flex items-center gap-3 mb-2">
                    <h3 className="font-serif text-h3">{tunnel.name}</h3>
                    {tunnel.is_managed && (
                      <span className="px-2 py-1 bg-accent-lime/20 border border-accent-lime/50 rounded text-[10px] font-mono text-accent-lime uppercase">
                        Managed by OpenDeploy
                      </span>
                    )}
                    <StatusPill
                      status={tunnel.status === 'active' ? 'healthy' : 'inactive'}
                      label={tunnel.status}
                      size="sm"
                    />
                  </div>
                  <p className="font-mono text-small text-text-secondary">
                    ID: <span className="text-text-primary">{tunnel.id}</span>
                  </p>
                  <p className="font-mono text-small text-text-secondary">
                    Created: <span className="text-text-primary">{tunnel.created_at}</span>
                  </p>
                </div>

                <div className="flex items-center gap-2">
                  {/* For managed tunnels, use existing controls */}
                  {tunnel.is_managed ? (
                    <>
                      <button
                        onClick={() => handleRestartTunnel(tunnel)}
                        className="p-2 text-text-secondary hover:text-accent-lime transition-colors rounded hover:bg-bg-primary"
                        title="Restart tunnel"
                      >
                        <RotateCcw size={18} />
                      </button>
                      <Link
                        href="/tunnel/manage"
                        className="p-2 text-text-secondary hover:text-accent-lime transition-colors rounded hover:bg-bg-primary"
                        title="Manage tunnel routes"
                      >
                        <Settings size={18} />
                      </Link>
                      <button
                        onClick={() => handleStopTunnel(tunnel)}
                        className="p-2 text-text-secondary hover:text-status-warning transition-colors rounded hover:bg-bg-primary"
                        title="Stop tunnel"
                      >
                        <Pause size={18} />
                      </button>
                    </>
                  ) : (
                    // For unmanaged tunnels, offer start/stop/delete options
                    <>
                      <button
                        onClick={() => handleStartTunnel(tunnel)}
                        className="p-2 text-text-secondary hover:text-accent-lime transition-colors rounded hover:bg-bg-primary"
                        title="Start tunnel on this server"
                      >
                        <Play size={18} />
                      </button>
                      <button
                        onClick={() => handleStopTunnel(tunnel)}
                        className="p-2 text-text-secondary hover:text-status-warning transition-colors rounded hover:bg-bg-primary"
                        title="Remove tunnel from this server"
                      >
                        <Pause size={18} />
                      </button>
                    </>
                  )}
                  <button
                    onClick={() => handleDeleteTunnel(tunnel)}
                    className="p-2 text-text-secondary hover:text-status-error transition-colors rounded hover:bg-bg-primary"
                    title="Delete tunnel from Cloudflare"
                  >
                    <Trash2 size={18} />
                  </button>
                </div>
              </div>

              {tunnel.domains.length > 0 ? (
                <div className="mt-4 pt-4 border-t border-border-dark">
                  <p className="font-mono text-label uppercase tracking-wider text-text-secondary mb-3">
                    Connected Applications ({tunnel.domains.length})
                  </p>
                  <div className="space-y-2">
                    {tunnel.domains.map((domain) => (
                      <div
                        key={domain}
                        className="flex items-center gap-3 font-mono text-small text-text-primary"
                      >
                        <CheckCircle2 size={14} className="text-accent-lime" />
                        <a
                          href={`https://${domain}`}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="text-accent-lime hover:underline"
                        >
                          {domain}
                        </a>
                        <ExternalLink size={12} className="text-text-secondary" />
                        {!tunnel.is_managed && (
                          <button
                            onClick={() => {}}
                            className="text-text-secondary hover:text-status-error"
                            title="Remove domain"
                          >
                            <Trash2 size={12} />
                          </button>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              ) : (
                <div className="mt-4 pt-4 border-t border-border-dark">
                  <p className="font-mono text-small text-text-secondary italic">
                    No applications connected to this tunnel
                  </p>
                </div>
              )}
            </motion.div>
          ))}
        </div>
      )}

      {/* Manage Tunnel Form */}
      {showManageForm && selectedTunnel && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          className="fixed inset-0 bg-black/50 flex items-center justify-center p-4 z-50"
          onClick={() => setShowManageForm(false)}
        >
          <div
            className="bg-bg-secondary rounded-card border border-border-dark p-6 w-full max-w-md"
            onClick={e => e.stopPropagation()}
          >
            <h3 className="font-serif text-h3 mb-4">Manage Tunnel: {selectedTunnel.name}</h3>

            <div className="space-y-4">
              <div>
                <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                  Domain
                </label>
                <select
                  value={selectedZone}
                  onChange={e => setSelectedZone(e.target.value)}
                  className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary"
                >
                  {zones.map(z => (
                    <option key={z.id} value={z.id}>{z.name}</option>
                  ))}
                </select>
              </div>

              <div>
                <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                  New Subdomain
                </label>
                <input
                  value={newSubdomain}
                  onChange={e => setNewSubdomain(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))}
                  className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary"
                  placeholder="api"
                />
                {newSubdomain && selectedZone && (
                  <p className="mt-2 font-mono text-small text-text-secondary">
                    New domain: <span className="text-accent-lime font-bold">{newSubdomain}.{zones.find(z => z.id === selectedZone)?.name}</span>
                  </p>
                )}
              </div>
            </div>

            {error && (
              <div className="mt-4 p-3 bg-red-900/20 border border-red-700 rounded-lg flex items-start gap-2">
                <AlertCircle size={16} className="text-red-500 mt-0.5 flex-shrink-0" />
                <span className="font-mono text-small text-red-400">{error}</span>
              </div>
            )}

            <div className="flex gap-3 mt-6">
              <button
                onClick={handleAddDomain}
                disabled={addingDomain || !selectedZone || !newSubdomain.trim()}
                className="flex-1 px-4 py-2 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all disabled:opacity-50 flex items-center justify-center gap-2"
              >
                {addingDomain ? (
                  <>
                    <Loader2 size={14} className="animate-spin" />
                    Adding...
                  </>
                ) : (
                  'Add Domain'
                )}
              </button>
              <button
                onClick={() => setShowManageForm(false)}
                className="px-4 py-2 bg-bg-secondary border border-border-dark rounded-lg font-mono text-small text-text-primary hover:bg-border-dark transition-all"
              >
                Cancel
              </button>
            </div>
          </div>
        </motion.div>
      )}

      <div className="mt-8 flex gap-3">
        <Link
          href="/tunnel/manage"
          className="inline-flex items-center gap-2 px-4 py-2 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all"
        >
          <Plus size={14} /> Create New Tunnel
        </Link>
      </div>
    </motion.div>
  )
}