'use client'

import { useState, useEffect, Suspense } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { Plus, Trash2, ArrowUp, ArrowDown, ExternalLink, AlertCircle, CheckCircle2, Loader2, RefreshCw, ArrowLeft } from 'lucide-react'
import Link from 'next/link'
import { useSearchParams } from 'next/navigation'
import SectionBadge from '@/components/ui/SectionBadge'
import StatusPill from '@/components/ui/StatusPill'
import { tunnelApi } from '@/lib/api'
import { apiKeyStorage } from '@/utils/apiKey'

interface TunnelRoute {
  id: string
  tunnel_id: string
  hostname: string
  zone_id: string
  dns_record_id?: string
  local_scheme: string
  local_port: number
  path_prefix?: string
  sort_order: number
  created_at: string
  updated_at: string
}

interface Zone {
  id: string
  name: string
}

function TunnelConfigContent() {
  const searchParams = useSearchParams()
  const tunnelId = searchParams.get('id')

  const [routes, setRoutes] = useState<TunnelRoute[]>([])
  const [zones, setZones] = useState<Zone[]>([])
  const [tunnelStatus, setTunnelStatus] = useState<any>(null)
  const [loading, setLoading] = useState(true)
  const [showAddForm, setShowAddForm] = useState(false)
  const [error, setError] = useState('')
  const [apiKey, setApiKey] = useState<string | null>(null)

  // Form state
  const [selectedZone, setSelectedZone] = useState('')
  const [subdomain, setSubdomain] = useState('')
  const [localPort, setLocalPort] = useState('3000')
  const [localScheme, setLocalScheme] = useState('http')
  const [pathPrefix, setPathPrefix] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [portListening, setPortListening] = useState<boolean | null>(null)
  const [checkingPort, setCheckingPort] = useState(false)
  const [dnsStatus, setDnsStatus] = useState<Record<string, boolean | null>>({})
  const [verifyingDns, setVerifyingDns] = useState<Record<string, boolean>>({})
  const [driftStatus, setDriftStatus] = useState<any>(null)
  const [checkingDrift, setCheckingDrift] = useState(false)

  useEffect(() => {
    const storedKey = apiKeyStorage.get()
    setApiKey(storedKey)
    loadData()
  }, [tunnelId])

  const loadData = async () => {
    try {
      setLoading(true)
      const key = apiKeyStorage.get()
      const [statusRes, routesRes, zonesRes] = await Promise.all([
        tunnelApi.getStatus(),
        tunnelApi.listRoutes(),
        key ? tunnelApi.getStoredZones(key).catch(() => []) : Promise.resolve([]),
      ])
      setTunnelStatus(statusRes)
      setRoutes(routesRes)
      setZones(zonesRes)
      if (zonesRes.length > 0) {
        setSelectedZone(zonesRes[0].id)
      }
    } catch (err) {
      setError(String(err))
    } finally {
      setLoading(false)
    }
  }

  const loadZones = async () => {
    try {
      const key = apiKeyStorage.get()
      if (!key) {
        setError('API key not found. Please set up tunnel first.')
        return
      }
      const zonesRes = await tunnelApi.getStoredZones(key)
      setZones(zonesRes)
      if (zonesRes.length > 0) {
        setSelectedZone(zonesRes[0].id)
      }
    } catch (err) {
      setError('Failed to load zones: ' + String(err))
    }
  }

  const handleAddRoute = async () => {
    if (!selectedZone || !subdomain || !localPort) {
      setError('Please fill in all required fields')
      return
    }

    const key = apiKeyStorage.get()
    if (!key) {
      setError('API key not found. Please set up tunnel first.')
      return
    }

    const port = parseInt(localPort)
    if (isNaN(port) || port < 1 || port > 65535) {
      setError('Invalid port number')
      return
    }

    if (port === 80) {
      setError('Port 80 is reserved for the OpenDeploy dashboard')
      return
    }

    setSubmitting(true)
    setError('')

    try {
      const zone = zones.find(z => z.id === selectedZone)
      if (!zone) {
        throw new Error('Selected zone not found')
      }

      const hostname = `${subdomain}.${zone.name}`

      await tunnelApi.createRoute(key, {
        hostname,
        zone_id: selectedZone,
        local_scheme: localScheme,
        local_port: port,
        path_prefix: pathPrefix || undefined,
      })

      // Reset form
      setSubdomain('')
      setLocalPort('3000')
      setPathPrefix('')
      setShowAddForm(false)

      // Reload routes
      await loadData()
    } catch (err) {
      setError(String(err))
    } finally {
      setSubmitting(false)
    }
  }

  const handleDeleteRoute = async (routeId: string, hostname: string) => {
    if (!confirm(`Delete route for ${hostname}? Traffic to this domain will stop working.`)) {
      return
    }

    const key = apiKeyStorage.get()
    if (!key) {
      setError('API key not found. Please set up tunnel first.')
      return
    }

    try {
      await tunnelApi.deleteRoute(key, routeId)
      await loadData()
    } catch (err) {
      setError(String(err))
    }
  }

  const handleMoveRoute = async (index: number, direction: 'up' | 'down') => {
    const newRoutes = [...routes]
    const targetIndex = direction === 'up' ? index - 1 : index + 1

    if (targetIndex < 0 || targetIndex >= newRoutes.length) return

    // Swap
    [newRoutes[index], newRoutes[targetIndex]] = [newRoutes[targetIndex], newRoutes[index]]

    // Update local state immediately for smooth UX
    setRoutes(newRoutes)

    // Send to backend
    try {
      const orderedIds = newRoutes.map(r => r.id)
      await tunnelApi.reorderRoutes(orderedIds)
    } catch (err) {
      setError(String(err))
      // Reload on error
      await loadData()
    }
  }

  const handleRestart = async () => {
    try {
      await tunnelApi.restart()
      setTimeout(loadData, 2000)
    } catch (err) {
      setError(String(err))
    }
  }

  const handleStop = async () => {
    if (!confirm('Are you sure you want to stop this tunnel? Traffic will be interrupted.')) {
      return
    }
    try {
      await tunnelApi.stopLocal()
      setTimeout(loadData, 1000)
    } catch (err) {
      setError(String(err))
    }
  }

  const getSelectedZoneName = () => {
    const zone = zones.find(z => z.id === selectedZone)
    return zone ? zone.name : ''
  }

  const checkPortStatus = async (port: string) => {
    const portNum = parseInt(port)
    if (isNaN(portNum) || portNum < 1 || portNum > 65535) {
      setPortListening(null)
      return
    }

    setCheckingPort(true)
    try {
      const result = await tunnelApi.checkPort(portNum)
      setPortListening(result.listening)
    } catch (err) {
      setPortListening(null)
    } finally {
      setCheckingPort(false)
    }
  }

  const handlePortChange = (newPort: string) => {
    setLocalPort(newPort)
    setPortListening(null)

    // Debounce port check
    const portNum = parseInt(newPort)
    if (!isNaN(portNum) && portNum >= 1 && portNum <= 65535) {
      setTimeout(() => checkPortStatus(newPort), 500)
    }
  }

  const verifyRouteDNS = async (routeId: string) => {
    const key = apiKeyStorage.get()
    if (!key) return

    setVerifyingDns(prev => ({ ...prev, [routeId]: true }))
    try {
      const result = await tunnelApi.verifyDNS(key, routeId)
      setDnsStatus(prev => ({ ...prev, [routeId]: result.verified }))
    } catch (err) {
      setDnsStatus(prev => ({ ...prev, [routeId]: false }))
    } finally {
      setVerifyingDns(prev => ({ ...prev, [routeId]: false }))
    }
  }

  useEffect(() => {
    // Verify DNS for all routes on load
    if (routes.length > 0 && apiKey) {
      routes.forEach(route => {
        verifyRouteDNS(route.id)
      })
      checkConfigDrift()
    }
  }, [routes.length, apiKey])

  const checkConfigDrift = async () => {
    const key = apiKeyStorage.get()
    if (!key) return

    setCheckingDrift(true)
    try {
      const result = await tunnelApi.detectDrift(key)
      setDriftStatus(result)
    } catch (err) {
      console.error('Failed to check drift:', err)
    } finally {
      setCheckingDrift(false)
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 size={48} className="animate-spin text-accent-lime" />
      </div>
    )
  }

  return (
    <motion.div
      initial={{ opacity: 0, x: 20 }}
      animate={{ opacity: 1, x: 0 }}
      transition={{ duration: 0.3 }}
    >
      {/* Header with Back Button */}
      <div className="mb-8 flex items-center justify-between">
        <Link
          href="/tunnel/dashboard"
          className="inline-flex items-center gap-2 px-4 py-2 bg-bg-secondary border border-border-dark  font-mono text-small text-text-primary hover:bg-border-dark transition-all"
        >
          <ArrowLeft size={16} />
          Back to Dashboard
        </Link>
      </div>

      <div className="mb-8">
        <SectionBadge label="TUNNEL CONFIGURATION" />
      </div>

      {/* Tunnel Status Header */}
      <div className="bg-bg-secondary  border border-border-dark p-6 mb-8">
        <div className="flex items-center justify-between mb-4">
          <div>
            <h2 className="font-serif text-h2 text-text-primary mb-2">
              {tunnelStatus?.tunnel_name || 'Tunnel'}
            </h2>
            {tunnelStatus?.domain && (
              <p className="font-mono text-small text-text-secondary">
                {tunnelStatus.domain}
              </p>
            )}
          </div>
          <div className="flex items-center gap-3">
            <StatusPill
              status={tunnelStatus?.status === 'active' ? 'healthy' : 'inactive'}
              label={tunnelStatus?.status || 'Unknown'}
            />
            <button
              onClick={handleRestart}
              className="px-4 py-2 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary hover:bg-border-dark transition-all flex items-center gap-2"
            >
              <RefreshCw size={14} />
              Restart
            </button>
            <button
              onClick={handleStop}
              className="px-4 py-2 bg-red-900/20 border border-red-700  font-mono text-small text-red-400 hover:bg-red-900/40 transition-all"
            >
              Stop
            </button>
          </div>
        </div>
        <div className="grid grid-cols-2 gap-4 text-small font-mono">
          <div>
            <span className="text-text-secondary">Routes configured: </span>
            <span className="text-text-primary font-bold">{routes.length}</span>
          </div>
          <div>
            <span className="text-text-secondary">Public URL: </span>
            <a
              href={`https://${tunnelStatus?.domain}`}
              target="_blank"
              rel="noopener noreferrer"
              className="text-accent-lime hover:underline"
            >
              {tunnelStatus?.domain}
            </a>
          </div>
        </div>
      </div>

      {/* Config Drift Warning */}
      {driftStatus?.has_drift && (
        <motion.div
          initial={{ opacity: 0, y: -10 }}
          animate={{ opacity: 1, y: 0 }}
          className="mb-6 p-4 bg-yellow-900/20 border border-yellow-700 "
        >
          <div className="flex items-start gap-3">
            <AlertCircle size={20} className="text-yellow-500 flex-shrink-0 mt-0.5" />
            <div className="flex-1">
              <h3 className="font-mono text-small font-bold text-yellow-400 mb-2">Configuration Drift Detected</h3>
              <p className="font-mono text-small text-yellow-300 mb-3">
                Local configuration differs from Cloudflare. This may cause routing issues.
              </p>
              <div className="space-y-1 mb-3">
                {driftStatus.missing_in_cloudflare?.length > 0 && (
                  <p className="font-mono text-label text-yellow-300">
                    Missing in Cloudflare: {driftStatus.missing_in_cloudflare.join(', ')}
                  </p>
                )}
                {driftStatus.extra_in_cloudflare?.length > 0 && (
                  <p className="font-mono text-label text-yellow-300">
                    Extra in Cloudflare: {driftStatus.extra_in_cloudflare.join(', ')}
                  </p>
                )}
              </div>
              <button
                onClick={checkConfigDrift}
                disabled={checkingDrift}
                className="px-4 py-2 bg-yellow-700 text-yellow-100 font-mono text-small  hover:bg-yellow-600 transition-all disabled:opacity-50 flex items-center gap-2"
              >
                {checkingDrift ? (
                  <>
                    <Loader2 size={14} className="animate-spin" />
                    Checking...
                  </>
                ) : (
                  <>
                    <RefreshCw size={14} />
                    Recheck
                  </>
                )}
              </button>
            </div>
          </div>
        </motion.div>
      )}

      {/* Error Display */}
      {error && (
        <motion.div
          initial={{ opacity: 0, y: -10 }}
          animate={{ opacity: 1, y: 0 }}
          className="mb-6 p-4 bg-red-900/20 border border-red-700  flex items-start gap-3"
        >
          <AlertCircle size={20} className="text-red-500 flex-shrink-0 mt-0.5" />
          <div className="flex-1">
            <p className="font-mono text-small text-red-400">{error}</p>
            <button
              onClick={() => setError('')}
              className="mt-2 text-small font-mono text-red-400 hover:text-red-300 underline"
            >
              Dismiss
            </button>
          </div>
        </motion.div>
      )}

      {/* Traffic Routes Section */}
      <div className="bg-bg-secondary  border border-border-dark p-6 mb-8">
        <div className="flex items-center justify-between mb-6">
          <h2 className="font-serif text-h2 text-text-primary">Traffic Routes</h2>
          <button
            onClick={() => {
              setShowAddForm(!showAddForm)
              if (!showAddForm && zones.length === 0) {
                loadZones()
              }
            }}
            className="px-4 py-2 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider  hover:bg-accent-lime-muted transition-all flex items-center gap-2"
          >
            <Plus size={16} />
            Add Route
          </button>
        </div>

        {/* Add Route Form */}
        <AnimatePresence>
          {showAddForm && (
            <motion.div
              initial={{ opacity: 0, height: 0 }}
              animate={{ opacity: 1, height: 'auto' }}
              exit={{ opacity: 0, height: 0 }}
              className="mb-6 p-6 bg-bg-primary  border border-border-dark"
            >
              <h3 className="font-mono text-small font-bold uppercase tracking-wider text-text-primary mb-4">
                Add New Route
              </h3>

              <div className="grid grid-cols-2 gap-4 mb-4">
                <div>
                  <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                    Domain
                  </label>
                  <select
                    value={selectedZone}
                    onChange={e => setSelectedZone(e.target.value)}
                    className="w-full px-4 py-3 bg-bg-secondary border border-border-dark  font-mono text-small text-text-primary"
                  >
                    {zones.length === 0 && <option value="">Loading zones...</option>}
                    {zones.map(z => (
                      <option key={z.id} value={z.id}>{z.name}</option>
                    ))}
                  </select>
                </div>
                <div>
                  <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                    Subdomain
                  </label>
                  <input
                    value={subdomain}
                    onChange={e => setSubdomain(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))}
                    className="w-full px-4 py-3 bg-bg-secondary border border-border-dark  font-mono text-small text-text-primary"
                    placeholder="api"
                  />
                  {subdomain && (
                    <p className="mt-1 font-mono text-label text-text-secondary">
                      → {subdomain}.{getSelectedZoneName()}
                    </p>
                  )}
                </div>
              </div>

              <div className="grid grid-cols-2 gap-4 mb-4">
                <div>
                  <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                    Local Port
                  </label>
                  <input
                    type="number"
                    value={localPort}
                    onChange={e => handlePortChange(e.target.value)}
                    className="w-full px-4 py-3 bg-bg-secondary border border-border-dark  font-mono text-small text-text-primary"
                    placeholder="3000"
                    min="1"
                    max="65535"
                  />
                  {checkingPort && (
                    <p className="mt-1 font-mono text-label text-text-secondary flex items-center gap-1">
                      <Loader2 size={12} className="animate-spin" />
                      Checking port...
                    </p>
                  )}
                  {!checkingPort && portListening === true && (
                    <p className="mt-1 font-mono text-label text-accent-lime flex items-center gap-1">
                      <CheckCircle2 size={12} />
                      Port {localPort} is listening
                    </p>
                  )}
                  {!checkingPort && portListening === false && (
                    <p className="mt-1 font-mono text-label text-yellow-500 flex items-center gap-1">
                      <AlertCircle size={12} />
                      Nothing listening on port {localPort}
                    </p>
                  )}
                </div>
                <div>
                  <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                    Protocol
                  </label>
                  <select
                    value={localScheme}
                    onChange={e => setLocalScheme(e.target.value)}
                    className="w-full px-4 py-3 bg-bg-secondary border border-border-dark  font-mono text-small text-text-primary"
                  >
                    <option value="http">HTTP</option>
                    <option value="https">HTTPS</option>
                  </select>
                </div>
              </div>

              <div className="mb-4">
                <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                  Path Prefix (Optional)
                </label>
                <input
                  value={pathPrefix}
                  onChange={e => setPathPrefix(e.target.value)}
                  className="w-full px-4 py-3 bg-bg-secondary border border-border-dark  font-mono text-small text-text-primary"
                  placeholder="/api"
                />
                <p className="mt-1 font-mono text-label text-text-secondary">
                  Leave empty to match all paths
                </p>
              </div>

              <div className="flex gap-3">
                <button
                  onClick={handleAddRoute}
                  disabled={submitting || !subdomain || !localPort || zones.length === 0}
                  className="flex-1 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider  hover:bg-accent-lime-muted transition-all disabled:opacity-50 flex items-center justify-center gap-2"
                >
                  {submitting ? (
                    <>
                      <Loader2 size={16} className="animate-spin" />
                      Creating...
                    </>
                  ) : (
                    'Create Route'
                  )}
                </button>
                <button
                  onClick={() => setShowAddForm(false)}
                  className="px-6 py-3 bg-bg-secondary border border-border-dark  font-mono text-small text-text-primary hover:bg-border-dark transition-all"
                >
                  Cancel
                </button>
              </div>
            </motion.div>
          )}
        </AnimatePresence>

        {/* Routes List */}
        {routes.length === 0 ? (
          <div className="text-center py-12">
            <p className="font-mono text-small text-text-secondary mb-4">
              No routes configured yet
            </p>
            <button
              onClick={() => setShowAddForm(true)}
              className="px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider  hover:bg-accent-lime-muted transition-all"
            >
              Add Your First Route
            </button>
          </div>
        ) : (
          <div className="space-y-2">
            {routes.map((route, index) => (
              <motion.div
                key={route.id}
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                className="p-4 bg-bg-primary  border border-border-dark hover:border-accent-lime transition-all"
              >
                <div className="flex items-center justify-between">
                  <div className="flex-1">
                    <div className="flex items-center gap-3 mb-2">
                      <a
                        href={`https://${route.hostname}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="font-mono text-small font-bold text-accent-lime hover:underline flex items-center gap-1"
                      >
                        {route.hostname}
                        <ExternalLink size={12} />
                      </a>
                      <StatusPill status="healthy" label="Active" />
                      {verifyingDns[route.id] ? (
                        <span className="font-mono text-label text-text-secondary flex items-center gap-1">
                          <Loader2 size={12} className="animate-spin" />
                          Checking DNS...
                        </span>
                      ) : dnsStatus[route.id] === true ? (
                        <span className="font-mono text-label text-accent-lime flex items-center gap-1">
                          <CheckCircle2 size={12} />
                          DNS OK
                        </span>
                      ) : dnsStatus[route.id] === false ? (
                        <span className="font-mono text-label text-red-400 flex items-center gap-1">
                          <AlertCircle size={12} />
                          DNS Issue
                        </span>
                      ) : null}
                    </div>
                    <div className="font-mono text-label text-text-secondary">
                      → {route.local_scheme}://localhost:{route.local_port}
                      {route.path_prefix && ` (path: ${route.path_prefix})`}
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <button
                      onClick={() => handleMoveRoute(index, 'up')}
                      disabled={index === 0}
                      className="p-2 bg-bg-secondary border border-border-dark  hover:bg-border-dark transition-all disabled:opacity-30"
                      title="Move up"
                    >
                      <ArrowUp size={14} className="text-text-primary" />
                    </button>
                    <button
                      onClick={() => handleMoveRoute(index, 'down')}
                      disabled={index === routes.length - 1}
                      className="p-2 bg-bg-secondary border border-border-dark  hover:bg-border-dark transition-all disabled:opacity-30"
                      title="Move down"
                    >
                      <ArrowDown size={14} className="text-text-primary" />
                    </button>
                    <button
                      onClick={() => handleDeleteRoute(route.id, route.hostname)}
                      className="p-2 bg-red-900/20 border border-red-700  hover:bg-red-900/40 transition-all"
                      title="Delete route"
                    >
                      <Trash2 size={14} className="text-red-400" />
                    </button>
                  </div>
                </div>
              </motion.div>
            ))}

            {/* Catch-all Rule */}
            <div className="p-4 bg-bg-secondary/50  border border-border-dark/50">
              <div className="flex items-center gap-3">
                <span className="font-mono text-small text-text-secondary">
                  Default — returns 404 for unmatched requests
                </span>
                <span className="font-mono text-label text-text-secondary/50">(required)</span>
              </div>
            </div>
          </div>
        )}
      </div>
    </motion.div>
  )
}

export default function TunnelConfigPage() {
  return (
    <Suspense fallback={
      <div className="flex items-center justify-center h-64">
        <Loader2 size={48} className="animate-spin text-accent-lime" />
      </div>
    }>
      <TunnelConfigContent />
    </Suspense>
  )
}
