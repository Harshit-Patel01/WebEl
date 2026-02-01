'use client'

import { useState, useEffect } from 'react'
import { motion } from 'framer-motion'
import { Loader2 } from 'lucide-react'
import Link from 'next/link'
import SectionBadge from '@/components/ui/SectionBadge'
import StatusPill from '@/components/ui/StatusPill'
import WifiRow from '@/components/ui/WifiRow'
import { wifiApi } from '@/lib/api'

type WifiState = 'scanning' | 'list' | 'password' | 'connecting' | 'connected'

export default function WifiPage() {
  const [state, setState] = useState<WifiState>('scanning')
  const [networks, setNetworks] = useState<any[]>([])
  const [selectedNetwork, setSelectedNetwork] = useState<string | null>(null)
  const [password, setPassword] = useState('')
  const [ip, setIp] = useState('unknown')
  const [connectedSSID, setConnectedSSID] = useState<string | null>(null)
  const [showSaved, setShowSaved] = useState(false)

  useEffect(() => {
    checkStatus()
    handleScan()
  }, [])

  const checkStatus = async () => {
    try {
      const status = await wifiApi.getStatus()
      if (status.connected && status.ssid) {
        setConnectedSSID(status.ssid)
        setSelectedNetwork(status.ssid)
        setIp(status.ip || 'unknown')
        setState('connected')
      }
    } catch (err) {
      console.error('Failed to check WiFi status:', err)
    }
  }

  const handleScan = async () => {
    if (state !== 'connected') {
      setState('scanning')
    }
    try {
      const results = await wifiApi.getNetworks()
      setNetworks(results || [])
      if (state !== 'connected') {
        setState('list')
      }
    } catch (err) {
      console.error(err)
      if (state !== 'connected') {
        setState('list')
      }
    }
  }

  const handleConnect = async () => {
    if (!selectedNetwork) return
    setState('connecting')
    try {
      const res = await wifiApi.connect(selectedNetwork, password)
      if (res.success) {
        const status = await wifiApi.getStatus()
        if (status.ip) setIp(status.ip)
        setConnectedSSID(selectedNetwork)
        setState('connected')
        setPassword('') // Clear password after successful connection
        // Refresh network list to update saved status
        handleScan()
      } else {
        alert('Failed to connect. Please check your password.')
        setState('password')
      }
    } catch (err) {
      const errorMsg = String(err)
      if (errorMsg.includes('password') || errorMsg.includes('authentication')) {
        alert('Connection failed. The password may be incorrect. Please try again.')
      } else {
        alert(errorMsg)
      }
      setState('password')
    }
  }

  const handleUpdatePassword = async () => {
    if (!selectedNetwork || !password) return
    try {
      await wifiApi.updatePassword(selectedNetwork, password)
      alert('Password updated successfully')
      handleConnect()
    } catch (err) {
      alert('Failed to update password: ' + String(err))
    }
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
          {/* Left - Info */}
          <div>
            <h1 className="font-serif text-h1 mb-4">Connect to WiFi</h1>
            <p className="text-body text-text-secondary mb-8 max-w-md">
              Select your wireless network to get your Pi online. This is the first step
              to making your deployment accessible from anywhere.
            </p>

            {state === 'connected' && (
              <motion.div
                initial={{ opacity: 0, y: 20 }}
                animate={{ opacity: 1, y: 0 }}
                className="bg-bg-surface text-text-dark rounded-card p-6 border border-border-light"
              >
                <div className="flex items-center gap-2 mb-4">
                  <StatusPill status="healthy" label="Connected" />
                </div>
                <div className="font-mono text-small space-y-2">
                  <p>Connected to <span className="font-bold">&quot;{selectedNetwork}&quot;</span></p>
                  <p>IP Address: <span className="font-bold">{ip}</span></p>
                  <div className="flex items-center gap-1 mt-2">
                    <span>Signal:</span>
                    <div className="flex items-end gap-0.5 h-3 mx-1">
                      {[1, 2, 3, 4].map(l => (
                        <div key={l} className={`signal-bar ${l <= 4 ? 'active' : ''}`} style={{ height: `${l * 3 + 2}px` }} />
                      ))}
                    </div>
                    <span className="font-bold">Strong</span>
                  </div>
                </div>
                <Link
                  href="/internet"
                  className="inline-flex items-center gap-2 mt-6 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-full hover:bg-accent-lime-muted transition-all"
                >
                  Continue to Internet Check &rarr;
                </Link>
              </motion.div>
            )}

            {state !== 'connected' && (
              <div className="space-y-3">
                <div className="flex items-center gap-2 text-text-secondary">
                  <StatusPill status="inactive" label="Not Connected" />
                </div>
                <button
                  onClick={() => setShowSaved(true)}
                  className="mt-4 px-4 py-2 bg-bg-surface text-text-dark font-mono text-small border border-border-light rounded-lg hover:bg-border-light transition-all"
                >
                  Show Saved WiFi Connections
                </button>
              </div>
            )}
          </div>

          {/* Right - WiFi Scanner */}
          <div className="bg-bg-surface rounded-card border border-border-light overflow-hidden">
            <div className="flex items-center justify-between px-6 py-4 border-b border-border-light">
              <span className="font-mono text-small font-bold uppercase tracking-wider text-text-dark">
                Available Networks
              </span>
              <button
                onClick={handleScan}
                className="font-mono text-label uppercase tracking-wider text-accent-lime-muted hover:text-accent-lime transition-colors"
              >
                {state === 'scanning' ? 'Scanning...' : 'Rescan'}
              </button>
            </div>

            {state === 'scanning' ? (
              <div className="p-12 flex flex-col items-center justify-center text-text-dark">
                <Loader2 size={32} className="animate-spin text-accent-lime-muted mb-4" />
                <span className="font-mono text-small text-text-secondary">Scanning for networks...</span>
              </div>
            ) : (
              <div>
                {networks.map(network => (
                  <WifiRow
                    key={network.ssid}
                    ssid={network.ssid}
                    signal={Math.ceil(network.signal / 25)} // Convert 0-100 to 1-4 scale
                    secured={network.security !== 'Open'}
                    saved={network.saved}
                    selected={selectedNetwork === network.ssid}
                    onClick={() => {
                      setSelectedNetwork(network.ssid)
                      setPassword('')
                      setState('password')
                    }}
                  />
                ))}
              </div>
            )}

            {/* Password input */}
            {(state === 'password' || state === 'connecting') && selectedNetwork && (
              <motion.div
                initial={{ opacity: 0, height: 0 }}
                animate={{ opacity: 1, height: 'auto' }}
                className="px-6 py-4 border-t border-border-light bg-white"
              >
                <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                  Password for {selectedNetwork}
                </label>
                <input
                  type="password"
                  value={password}
                  onChange={e => setPassword(e.target.value)}
                  className="w-full px-4 py-3 bg-bg-surface border border-border-light rounded-lg font-mono text-small text-text-dark placeholder:text-text-secondary"
                  placeholder="Enter WiFi password"
                  disabled={state === 'connecting'}
                />
                <div className="flex gap-2 mt-3">
                  <button
                    onClick={handleConnect}
                    disabled={state === 'connecting'}
                    className="flex-1 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all disabled:opacity-50 flex items-center justify-center gap-2"
                  >
                    {state === 'connecting' ? (
                      <>
                        <Loader2 size={16} className="animate-spin" />
                        Connecting...
                      </>
                    ) : (
                      'Connect'
                    )}
                  </button>
                  {networks.find(n => n.ssid === selectedNetwork)?.saved && (
                    <button
                      onClick={handleUpdatePassword}
                      disabled={state === 'connecting' || !password}
                      className="px-6 py-3 bg-bg-surface text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-border-light transition-all disabled:opacity-50 border border-border-light"
                    >
                      Update Password
                    </button>
                  )}
                </div>
              </motion.div>
            )}
          </div>
        </div>

        {/* Saved Networks Modal */}
        {showSaved && (
          <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 " onClick={() => setShowSaved(false)}>
            <motion.div
              initial={{ opacity: 0, scale: 0.95 }}
              animate={{ opacity: 1, scale: 1 }}
              className="bg-bg-surface rounded-card border border-border-light p-6 max-w-md w-full mx-4"
              onClick={(e) => e.stopPropagation()}
            >
              <h2 className="font-serif text-h2 text-text-secondary mb-4">Saved WiFi Networks</h2>
              <SavedNetworksList onConnect={(ssid) => {
                setSelectedNetwork(ssid)
                setShowSaved(false)
                handleConnect()
              }} />
              <button
                onClick={() => setShowSaved(false)}
                className="mt-4 w-full px-4 py-2 bg-bg-secondary text-text-secondary font-mono text-small rounded-lg hover:bg-border-dark transition-all"
              >
                Close
              </button>
            </motion.div>
          </div>
        )}
      </motion.div>
  )
}

function SavedNetworksList({ onConnect }: { onConnect: (ssid: string) => void }) {
  const [savedNetworks, setSavedNetworks] = useState<any[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    loadSavedNetworks()
  }, [])

  const loadSavedNetworks = async () => {
    try {
      const networks = await wifiApi.getSavedNetworks()
      setSavedNetworks(networks || [])
    } catch (err) {
      console.error('Failed to load saved networks:', err)
    } finally {
      setLoading(false)
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-8 text-text-secondary">
        <Loader2 size={24} className="animate-spin text-accent-lime-muted" />
      </div>
    )
  }

  if (savedNetworks.length === 0) {
    return (
      <p className="text-text-secondary text-small font-mono py-4">
        No saved networks found.
      </p>
    )
  }

  return (
    <div className="space-y-2 max-h-64 overflow-y-auto">
      {savedNetworks.map((network) => (
        <div
          key={network.ssid}
          className="flex items-center justify-between p-3 bg-bg-primary rounded-lg border border-border-light hover:border-accent-lime transition-all text-text-secondary cursor-pointer"
        >
          <div>
            <p className="font-mono text-small font-bold text-text-secondary">{network.ssid}</p>
            <p className="font-mono text-label text-text-secondary">{network.security}</p>
          </div>
          <button
            onClick={() => onConnect(network.ssid)}
            className="px-4 py-2 bg-accent-lime text-text-dark font-mono text-label uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all"
          >
            Connect
          </button>
        </div>
      ))}
    </div>
  )
}
