'use client'

import { useState, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { Plus, Trash2, ExternalLink, AlertCircle, CheckCircle2, Loader2, RefreshCw, Key, Settings } from 'lucide-react'
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

interface Zone {
  id: string
  name: string
}

interface Account {
  id: string
  name: string
}

export default function TunnelDashboardPage() {
  const [tunnels, setTunnels] = useState<TunnelWithDomains[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [apiKey, setApiKey] = useState<string | null>(null)
  const [inputApiKey, setInputApiKey] = useState('')
  const [validating, setValidating] = useState(false)
  const [showCreateModal, setShowCreateModal] = useState(false)

  // Create tunnel form state
  const [accounts, setAccounts] = useState<Account[]>([])
  const [zones, setZones] = useState<Zone[]>([])
  const [selectedAccount, setSelectedAccount] = useState('')
  const [selectedZone, setSelectedZone] = useState('')
  const [tunnelName, setTunnelName] = useState('')
  const [subdomain, setSubdomain] = useState('')
  const [creating, setCreating] = useState(false)

  useEffect(() => {
    const storedKey = apiKeyStorage.get()
    setApiKey(storedKey)
    if (storedKey) {
      loadTunnels(storedKey)
    } else {
      setLoading(false)
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

      const zonesResult = await tunnelApi.getZones(inputApiKey)
      if (zonesResult.length === 0) {
        setError('No domains found on this Cloudflare account. Add a domain to Cloudflare first.')
        setValidating(false)
        return
      }

      apiKeyStorage.set(inputApiKey)
      setApiKey(inputApiKey)
      loadTunnels(inputApiKey)
    } catch (err) {
      setError(String(err))
    } finally {
      setValidating(false)
    }
  }

  const handleDeleteTunnel = async (tunnel: TunnelWithDomains) => {
    if (!confirm(`Are you sure you want to delete tunnel "${tunnel.name}"? This will remove it from Cloudflare.`)) {
      return
    }

    const key = apiKeyStorage.get()
    if (!key) {
      alert('API key not found')
      return
    }

    try {
      await tunnelApi.stopRemote(key, tunnel.account_id, tunnel.id)
      if (apiKey) {
        loadTunnels(apiKey)
      }
    } catch (err) {
      setError(String(err))
    }
  }

  const handleOpenCreateModal = async () => {
    const key = apiKeyStorage.get()
    if (!key) {
      setError('API key not found')
      return
    }

    try {
      const [accountsRes, zonesRes] = await Promise.all([
        tunnelApi.getAccounts(key),
        tunnelApi.getZones(key),
      ])
      setAccounts(accountsRes)
      setZones(zonesRes)
      if (accountsRes.length > 0) setSelectedAccount(accountsRes[0].id)
      if (zonesRes.length > 0) setSelectedZone(zonesRes[0].id)
      setShowCreateModal(true)
    } catch (err) {
      setError(String(err))
    }
  }

  const handleCreateTunnel = async () => {
    if (!tunnelName || !subdomain || !selectedAccount || !selectedZone) {
      setError('Please fill in all fields')
      return
    }

    const key = apiKeyStorage.get()
    if (!key) {
      setError('API key not found')
      return
    }

    setCreating(true)
    setError('')

    try {
      const zone = zones.find(z => z.id === selectedZone)
      if (!zone) throw new Error('Zone not found')

      await tunnelApi.create(key, {
        api_token: key,
        account_id: selectedAccount,
        zone_id: selectedZone,
        subdomain,
        domain: zone.name,
        tunnel_name: tunnelName,
      })

      setShowCreateModal(false)
      setTunnelName('')
      setSubdomain('')
      loadTunnels(key)
    } catch (err) {
      setError(String(err))
    } finally {
      setCreating(false)
    }
  }

  // API Key Entry Form
  if (!apiKey) {
    return (
      <motion.div
        initial={{ opacity: 0, x: 20 }}
        animate={{ opacity: 1, x: 0 }}
        transition={{ duration: 0.3 }}
      >
        <div className="mb-8">
          <SectionBadge label="TUNNEL API ACCESS" />
        </div>

        <div className="bg-bg-secondary rounded-card border border-border-dark p-8 md:p-12 text-center">
          <div className="max-w-4xl mx-auto">
            <div>
              <Key className="mx-auto mb-6 text-accent-lime" size={48} />
              <h2 className="font-serif text-h2 mb-4">Enter Cloudflare API Token</h2>
              <p className="text-body text-text-secondary mb-8">
                Securely connect to your Cloudflare account to manage tunnels
              </p>
            </div>

            <div className="mb-8 p-6 bg-bg-primary rounded-lg border border-border-dark text-left">
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
                <div className="space-y-2">
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

            <div className="text-left mb-6">
              <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                Cloudflare API Token
              </label>
              <input
                type="password"
                value={inputApiKey}
                onChange={e => setInputApiKey(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && handleValidateApiKey()}
                className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary placeholder:text-text-secondary focus:border-accent-lime focus:outline-none transition-colors"
                placeholder="Paste your API token here"
              />
            </div>

            {error && (
              <motion.div
                className="mb-6 p-4 bg-red-900/20 border border-red-700 rounded-lg flex items-start gap-3"
                initial={{ opacity: 0, scale: 0.95 }}
                animate={{ opacity: 1, scale: 1 }}
                transition={{ duration: 0.3 }}
              >
                <AlertCircle size={20} className="text-red-500 flex-shrink-0 mt-0.5" />
                <span className="font-mono text-small text-red-400">{error}</span>
              </motion.div>
            )}

            <div>
              <button
                onClick={handleValidateApiKey}
                disabled={validating || !inputApiKey.trim()}
                className="w-full px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all disabled:opacity-50 flex items-center justify-center gap-2"
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
            </div>
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
          <button
            onClick={() => {
              if (confirm('Are you sure you want to remove the API key? You will need to re-enter it to access tunnels.')) {
                apiKeyStorage.clear()
                setApiKey(null)
              }
            }}
            className="inline-flex items-center gap-2 px-4 py-2 bg-bg-secondary border border-border-dark rounded-lg font-mono text-small text-text-primary hover:bg-border-dark transition-all"
          >
            <Key size={16} /> Change API Key
          </button>
        </div>
      </div>

      {/* Create Tunnel Button */}
      <div className="mb-6">
        <button
          onClick={handleOpenCreateModal}
          className="inline-flex items-center gap-2 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all"
        >
          <Plus size={16} /> Create New Tunnel
        </button>
      </div>

      {error && (
        <div className="mb-6 p-4 bg-red-900/20 border border-red-700 rounded-lg flex items-start gap-3">
          <AlertCircle size={20} className="text-red-500 flex-shrink-0 mt-0.5" />
          <div>
            <p className="font-mono text-small text-red-400">{error}</p>
          </div>
        </div>
      )}

      {tunnels.length === 0 ? (
        <div className="bg-bg-secondary rounded-card border border-border-dark p-12 text-center">
          <p className="font-mono text-text-secondary mb-6">No tunnels found in your Cloudflare account.</p>
          <button
            onClick={handleOpenCreateModal}
            className="inline-flex items-center gap-2 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all"
          >
            <Plus size={16} /> Create Your First Tunnel
          </button>
        </div>
      ) : (
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
                        Managed
                      </span>
                    )}
                    <StatusPill
                      status={tunnel.status === 'active' ? 'healthy' : 'inactive'}
                      label={tunnel.status}
                      size="sm"
                    />
                  </div>
                  <p className="font-mono text-small text-text-secondary mb-1">
                    ID: <span className="text-text-primary">{tunnel.id}</span>
                  </p>
                  <p className="font-mono text-small text-text-secondary">
                    Created: <span className="text-text-primary">{new Date(tunnel.created_at).toLocaleDateString()}</span>
                  </p>
                </div>

                <div className="flex items-center gap-2">
                  <Link
                    href={`/tunnel/config?id=${tunnel.id}`}
                    className="px-4 py-2 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all flex items-center gap-2"
                  >
                    <Settings size={14} />
                    Manage
                  </Link>
                  <button
                    onClick={() => handleDeleteTunnel(tunnel)}
                    className="p-2 text-text-secondary hover:text-status-error transition-colors rounded hover:bg-bg-primary"
                    title="Delete tunnel"
                  >
                    <Trash2 size={18} />
                  </button>
                </div>
              </div>

              {tunnel.domains.length > 0 ? (
                <div className="mt-4 pt-4 border-t border-border-dark">
                  <p className="font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                    Connected Domains ({tunnel.domains.length})
                  </p>
                  <div className="space-y-2">
                    {tunnel.domains.map((domain) => (
                      <div
                        key={domain}
                        className="flex items-center gap-2 font-mono text-small text-text-primary"
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
                      </div>
                    ))}
                  </div>
                </div>
              ) : (
                <div className="mt-4 pt-4 border-t border-border-dark">
                  <p className="font-mono text-small text-text-secondary italic">
                    No domains connected to this tunnel
                  </p>
                </div>
              )}
            </motion.div>
          ))}
        </div>
      )}

      {/* Create Tunnel Modal */}
      <AnimatePresence>
        {showCreateModal && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="fixed inset-0 bg-black/50 flex items-center justify-center p-4 z-50"
            onClick={() => setShowCreateModal(false)}
          >
            <motion.div
              initial={{ scale: 0.95, opacity: 0 }}
              animate={{ scale: 1, opacity: 1 }}
              exit={{ scale: 0.95, opacity: 0 }}
              className="bg-bg-secondary rounded-card border border-border-dark p-6 w-full max-w-2xl"
              onClick={e => e.stopPropagation()}
            >
              <h3 className="font-serif text-h3 mb-6">Create New Tunnel</h3>

              <div className="space-y-4 mb-6">
                <div>
                  <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                    Tunnel Name
                  </label>
                  <input
                    value={tunnelName}
                    onChange={e => setTunnelName(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))}
                    className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary"
                    placeholder="my-app-tunnel"
                  />
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                      Account
                    </label>
                    <select
                      value={selectedAccount}
                      onChange={e => setSelectedAccount(e.target.value)}
                      className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary"
                    >
                      {accounts.map(acc => (
                        <option key={acc.id} value={acc.id}>{acc.name}</option>
                      ))}
                    </select>
                  </div>
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
                </div>

                <div>
                  <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                    Subdomain
                  </label>
                  <input
                    value={subdomain}
                    onChange={e => setSubdomain(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))}
                    className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary"
                    placeholder="app"
                  />
                  {subdomain && selectedZone && (
                    <p className="mt-2 font-mono text-small text-text-secondary">
                      Will create: <span className="text-accent-lime font-bold">{subdomain}.{zones.find(z => z.id === selectedZone)?.name}</span>
                    </p>
                  )}
                </div>
              </div>

              {error && (
                <div className="mb-4 p-3 bg-red-900/20 border border-red-700 rounded-lg flex items-start gap-2">
                  <AlertCircle size={16} className="text-red-500 mt-0.5 flex-shrink-0" />
                  <span className="font-mono text-small text-red-400">{error}</span>
                </div>
              )}

              <div className="flex gap-3">
                <button
                  onClick={handleCreateTunnel}
                  disabled={creating || !tunnelName || !subdomain}
                  className="flex-1 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all disabled:opacity-50 flex items-center justify-center gap-2"
                >
                  {creating ? (
                    <>
                      <Loader2 size={16} className="animate-spin" />
                      Creating...
                    </>
                  ) : (
                    'Create Tunnel'
                  )}
                </button>
                <button
                  onClick={() => setShowCreateModal(false)}
                  className="px-6 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary hover:bg-border-dark transition-all"
                >
                  Cancel
                </button>
              </div>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>
    </motion.div>
  )
}
