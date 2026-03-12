'use client'

import { useState, useEffect } from 'react'
import { motion } from 'framer-motion'
import { Loader2, Wifi, Lock, WifiOff, RefreshCw, Shield } from 'lucide-react'
import Link from 'next/link'
import SectionBadge from '@/components/ui/SectionBadge'
import StatusPill from '@/components/ui/StatusPill'
import { wifiApi } from '@/lib/api'

type WifiNetwork = {
  ssid: string
  signal: number
  security: string
  connected: boolean
  saved: boolean
}

type WifiStatus = {
  connected: boolean
  ssid: string
  ip: string
  state: string
}

export default function WifiPage() {
  const [scanning, setScanning] = useState(false)
  const [connecting, setConnecting] = useState(false)
  const [networks, setNetworks] = useState<WifiNetwork[]>([])
  const [selectedNetwork, setSelectedNetwork] = useState<WifiNetwork | null>(null)
  const [password, setPassword] = useState('')
  const [status, setStatus] = useState<WifiStatus | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    loadStatus()
    loadNetworks()
  }, [])

  const loadStatus = async () => {
    try {
      const data = await wifiApi.getStatus()
      setStatus(data)
    } catch (err) {
      console.error('Failed to load status:', err)
    }
  }

  const loadNetworks = async () => {
    setScanning(true)
    setError(null)
    try {
      const data = await wifiApi.getNetworks()
      setNetworks(data || [])
    } catch (err) {
      setError('Failed to scan networks')
      console.error(err)
    } finally {
      setScanning(false)
    }
  }

  const handleConnect = async () => {
    if (!selectedNetwork) return

    // Open networks don't need password
    if (selectedNetwork.security === 'Open' || selectedNetwork.security === '') {
      await connectToNetwork(selectedNetwork.ssid, '')
      return
    }

    // Secured networks need password
    if (!password) {
      setError('Password required for secured network')
      return
    }

    await connectToNetwork(selectedNetwork.ssid, password)
  }

  const connectToNetwork = async (ssid: string, pwd: string) => {
    setConnecting(true)
    setError(null)
    try {
      const res = await wifiApi.connect(ssid, pwd)
      if (res.success) {
        setPassword('')
        setSelectedNetwork(null)
        // Wait a bit for connection to establish
        await new Promise(resolve => setTimeout(resolve, 3000))
        await loadStatus()
        await loadNetworks()
      } else {
        setError('Failed to connect. Check password and try again.')
      }
    } catch (err) {
      setError('Connection failed: ' + String(err))
    } finally {
      setConnecting(false)
    }
  }

  const handleDeleteNetwork = async (ssid: string) => {
    if (!confirm(`Delete saved network "${ssid}"?`)) return

    try {
      await wifiApi.deleteSaved(ssid)
      await loadNetworks()
    } catch (err) {
      setError('Failed to delete network: ' + String(err))
    }
  }

  const getSignalBars = (signal: number) => {
    // signal is 0-100, convert to 1-4 bars
    if (signal >= 75) return 4
    if (signal >= 50) return 3
    if (signal >= 25) return 2
    return 1
  }

  const getSecurityIcon = (security: string) => {
    if (security === 'Open' || security === '') {
      return <WifiOff className="w-4 h-4 text-text-secondary" />
    }
    return <Lock className="w-4 h-4 text-accent-lime" />
  }

  const getSecurityLabel = (security: string) => {
    if (security === 'Open' || security === '') return 'Open'
    if (security.includes('WPA3')) return 'WPA3'
    if (security.includes('WPA2')) return 'WPA2'
    if (security.includes('WPA')) return 'WPA'
    if (security.includes('WEP')) return 'WEP'
    return 'Secured'
  }

  return (
    <motion.div
      initial={{ opacity: 0, x: 20 }}
      animate={{ opacity: 1, x: 0 }}
      transition={{ duration: 0.3 }}
    >
      <div className="mb-8">
        <SectionBadge label="01 — NETWORK" />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
        {/* Left - Status */}
        <div>
          <h1 className="font-serif text-h1 mb-4">WiFi Connection</h1>
          <p className="text-body text-text-secondary mb-8 max-w-md">
            Connect to a wireless network to get your Pi online.
          </p>

          {/* Current Status */}
          <div className="bg-bg-surface rounded-card border border-border-light p-6 mb-6">
            <div className="flex items-center gap-2 mb-4">
              {status?.connected ? (
                <StatusPill status="healthy" label="Connected" />
              ) : (
                <StatusPill status="inactive" label="Disconnected" />
              )}
            </div>

            {status?.connected && status.ssid ? (
              <div className="space-y-3">
                <div className="flex items-center gap-2">
                  <Wifi className="w-5 h-5 text-accent-lime text-text-dark" />
                  <span className="font-mono text-small font-bold text-text-dark">{status.ssid}</span>
                </div>
                <div className="font-mono text-small text-text-secondary">
                  <p>IP: <span className="text-text-dark font-bold">{status.ip || 'N/A'}</span></p>
                  <p>State: <span className="text-text-dark font-bold ">{status.state || 'connected'}</span></p>
                </div>
                <Link
                  href="/internet"
                  className="inline-flex items-center gap-2 mt-4 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-full hover:bg-accent-lime-muted transition-all"
                >
                  Continue to Internet Check &rarr;
                </Link>
              </div>
            ) : (
              <p className="text-small text-text-secondary font-mono">
                Not connected to any network
              </p>
            )}
          </div>

          {/* Error Display */}
          {error && (
            <motion.div
              initial={{ opacity: 0, y: -10 }}
              animate={{ opacity: 1, y: 0 }}
              className="bg-red-50 border border-red-200 rounded-lg p-4 mb-6"
            >
              <p className="text-small text-red-700 font-mono">{error}</p>
            </motion.div>
          )}
        </div>

        {/* Right - Networks List */}
        <div className="bg-bg-surface rounded-card border border-border-light overflow-hidden">
          <div className="flex items-center justify-between px-6 py-4 border-b border-border-light">
            <span className="font-mono text-small font-bold uppercase tracking-wider text-text-dark">
              Available Networks
            </span>
            <button
              onClick={loadNetworks}
              disabled={scanning}
              className="flex items-center gap-2 font-mono text-label uppercase tracking-wider text-accent-lime-muted hover:text-accent-lime transition-colors disabled:opacity-50"
            >
              <RefreshCw className={`w-4 h-4 ${scanning ? 'animate-spin' : ''}`} />
              {scanning ? 'Scanning...' : 'Scan'}
            </button>
          </div>

          {scanning ? (
            <div className="p-12 flex flex-col items-center justify-center">
              <Loader2 size={32} className="animate-spin text-accent-lime-muted mb-4" />
              <span className="font-mono text-small text-text-secondary">Scanning networks...</span>
            </div>
          ) : networks.length === 0 ? (
            <div className="p-12 text-center text-text-secondary">
              <WifiOff className="w-12 h-12 mx-auto mb-4 opacity-50" />
              <p className="font-mono text-small">No networks found</p>
            </div>
          ) : (
            <div className="max-h-[500px] overflow-y-auto">
              {networks.map((network) => (
                <div
                  key={network.ssid}
                  className={`px-6 py-4 border-b border-border-light hover:bg-gray-50 transition-colors cursor-pointer ${
                    selectedNetwork?.ssid === network.ssid ? 'bg-gray-50 border-l-4 border-l-accent-lime' : ''
                  }`}
                  onClick={() => {
                    setSelectedNetwork(network)
                    setPassword('')
                    setError(null)
                  }}
                >
                  <div className="flex items-center justify-between">
                    <div className="flex-1">
                      <div className="flex items-center gap-2 mb-1">
                        <span className="font-mono text-small font-bold text-text-dark">
                          {network.ssid}
                        </span>
                        {network.connected && (
                          <span className="px-2 py-0.5 bg-accent-lime text-text-dark text-label font-mono rounded">
                            CURRENT
                          </span>
                        )}
                        {network.saved && (
                          <span className="px-2 py-0.5 bg-blue-100 text-blue-700 text-label font-mono rounded">
                            SAVED
                          </span>
                        )}
                      </div>
                      <div className="flex items-center gap-3 text-label font-mono text-text-secondary">
                        <div className="flex items-center gap-1">
                          {getSecurityIcon(network.security)}
                          <span>{getSecurityLabel(network.security)}</span>
                        </div>
                        <div className="flex items-center gap-1">
                          <Wifi className="w-4 h-4" />
                          <span>{network.signal}%</span>
                        </div>
                      </div>
                    </div>
                    <div className="flex items-center gap-3">
                      {network.saved && (
                        <button
                          onClick={(e) => {
                            e.stopPropagation()
                            handleDeleteNetwork(network.ssid)
                          }}
                          className="px-3 py-1 text-label font-mono text-red-600 hover:bg-red-50 rounded transition-colors"
                        >
                          Delete
                        </button>
                      )}
                      <div className="flex items-end gap-0.5 h-4">
                        {[1, 2, 3, 4].map((bar) => (
                          <div
                            key={bar}
                            className={`w-1 rounded-sm ${
                              bar <= getSignalBars(network.signal)
                                ? 'bg-accent-lime'
                                : 'bg-border-light'
                            }`}
                            style={{ height: `${bar * 25}%` }}
                          />
                        ))}
                      </div>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}

          {/* Password Input */}
          {selectedNetwork && (
            <motion.div
              initial={{ opacity: 0, height: 0 }}
              animate={{ opacity: 1, height: 'auto' }}
              className="px-6 py-4 border-t-2 border-accent-lime bg-gray-50"
            >
              <div className="flex items-center gap-2 mb-3">
                <Shield className="w-4 h-4 text-accent-lime" />
                <span className="font-mono text-label uppercase tracking-wider text-text-secondary">
                  Connect to {selectedNetwork.ssid}
                </span>
              </div>

              {selectedNetwork.security !== 'Open' && selectedNetwork.security !== '' ? (
                <>
                  <input
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    onKeyDown={(e) => e.key === 'Enter' && handleConnect()}
                    placeholder="Enter password"
                    disabled={connecting}
                    className="w-full px-4 py-3 bg-bg-surface border border-border-light rounded-lg font-mono text-small text-text-dark placeholder:text-text-secondary mb-3"
                  />
                  <p className="text-label font-mono text-text-secondary mb-3">
                    Security: {getSecurityLabel(selectedNetwork.security)}
                  </p>
                </>
              ) : (
                <p className="text-small font-mono text-text-secondary mb-3">
                  This is an open network (no password required)
                </p>
              )}

              <div className="flex gap-2">
                <button
                  onClick={handleConnect}
                  disabled={connecting}
                  className="flex-1 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all disabled:opacity-50 flex items-center justify-center gap-2"
                >
                  {connecting ? (
                    <>
                      <Loader2 size={16} className="animate-spin" />
                      Connecting...
                    </>
                  ) : (
                    'Connect'
                  )}
                </button>
                <button
                  onClick={() => {
                    setSelectedNetwork(null)
                    setPassword('')
                    setError(null)
                  }}
                  disabled={connecting}
                  className="px-6 py-3 bg-bg-surface text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-border-light transition-all disabled:opacity-50 border border-border-light"
                >
                  Cancel
                </button>
              </div>
            </motion.div>
          )}
        </div>
      </div>
    </motion.div>
  )
}
