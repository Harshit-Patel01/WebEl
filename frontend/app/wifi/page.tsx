'use client'

import { useState, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import {
  Loader2,
  Wifi,
  Lock,
  WifiOff,
  RefreshCw,
  Shield,
  Eye,
  EyeOff,
  CheckCircle2,
  AlertCircle,
  Signal,
  Radio,
  ChevronRight,
  Trash2,
  Router,
  Users,
  Power,
  Settings2,
  Save
} from 'lucide-react'
import Link from 'next/link'
import SectionBadge from '@/components/ui/SectionBadge'
import { wifiApi, apApi } from '@/lib/api'

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
  const [showPassword, setShowPassword] = useState(false)
  const [status, setStatus] = useState<WifiStatus | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState(false)
  const [connectionLogs, setConnectionLogs] = useState<string[]>([])
  const [showLogs, setShowLogs] = useState(false)

  // AP State
  const [apStatus, setApStatus] = useState<{
    running: boolean
    interface: string
    ssid: string
    ip_address: string
    connected_clients: number
    enabled: boolean
  } | null>(null)
  const [apConfig, setApConfig] = useState<{
    ssid: string
    password: string
    enabled: boolean
    channel: number
  } | null>(null)
  const [apSsid, setApSsid] = useState('')
  const [apPassword, setApPassword] = useState('')
  const [apChannel, setApChannel] = useState(6)
  const [showApPassword, setShowApPassword] = useState(false)
  const [apLoading, setApLoading] = useState(false)
  const [apMessage, setApMessage] = useState<{ text: string; type: 'success' | 'error' } | null>(null)
  const [apToggling, setApToggling] = useState(false)

  useEffect(() => {
    loadStatus()
    loadNetworks()
    loadAPData()
  }, [])

  const loadAPData = async () => {
    try {
      const [statusData, configData] = await Promise.all([
        apApi.getStatus(),
        apApi.getConfig(),
      ])
      setApStatus(statusData)
      setApConfig(configData)
      setApSsid(configData.ssid)
      setApPassword(configData.password)
      setApChannel(configData.channel)
    } catch (err) {
      console.error('Failed to load AP data:', err)
    }
  }

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

    if (selectedNetwork.security === 'Open' || selectedNetwork.security === '') {
      await connectToNetwork(selectedNetwork.ssid, '')
      return
    }

    if (!password) {
      setError('Password required for secured network')
      return
    }

    await connectToNetwork(selectedNetwork.ssid, password)
  }

  const connectToNetwork = async (ssid: string, pwd: string) => {
    setConnecting(true)
    setError(null)
    setSuccess(false)
    setConnectionLogs([])
    setShowLogs(false)

    try {
      const res = await wifiApi.connect(ssid, pwd)

      if (res.success) {
        setConnectionLogs(['Connection initiated', 'Verifying network...', 'Successfully connected!'])
        setPassword('')
        setSelectedNetwork(null)
        setSuccess(true)
        await new Promise(resolve => setTimeout(resolve, 3000))
        await loadStatus()
        await loadNetworks()
        setSuccess(false)
      } else {
        setConnectionLogs(['Connection failed', 'Check password and try again'])
        setShowLogs(true)
        setError('Failed to connect. Check password and try again.')
      }
    } catch (err) {
      const errorMsg = String(err)
      setConnectionLogs(['Connection error', errorMsg])
      setShowLogs(true)
      setError('Connection failed: ' + errorMsg)
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
    if (signal >= 75) return 4
    if (signal >= 50) return 3
    if (signal >= 25) return 2
    return 1
  }

  const getSecurityLabel = (security: string) => {
    if (security === 'Open' || security === '') return 'Open'
    if (security.includes('WPA3')) return 'WPA3'
    if (security.includes('WPA2')) return 'WPA2'
    if (security.includes('WPA')) return 'WPA'
    if (security.includes('WEP')) return 'WEP'
    return 'Secured'
  }

  const getSignalStrengthLabel = (signal: number) => {
    if (signal >= 75) return 'Excellent'
    if (signal >= 50) return 'Good'
    if (signal >= 25) return 'Fair'
    return 'Weak'
  }

  return (
    <motion.div
      initial={{ opacity: 0, y: 20 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.4 }}
      className="min-h-screen"
    >
      {/* Header Section */}
      <div className="mb-8">
        <SectionBadge label="01 — NETWORK" />
      </div>

      <div className="mb-10">
        <div className="flex items-center gap-3 mb-3">
          <div className="p-2 bg-accent-lime/10 ">
            <Radio className="w-6 h-6 text-accent-lime" />
          </div>
          <h1 className="font-serif text-h1 text-text-dark">WiFi Configuration</h1>
        </div>
        <p className="text-body text-text-secondary max-w-xl">
          Scan for available networks and connect to get your device online.
          Saved networks will connect automatically.
        </p>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-3 gap-6">
        {/* Left Column - Connection Status */}
        <div className="xl:col-span-1 space-y-6">
          {/* Status Card */}
          <motion.div
            initial={{ opacity: 0, x: -20 }}
            animate={{ opacity: 1, x: 0 }}
            transition={{ delay: 0.1 }}
            className="bg-bg-surface  border border-border-light overflow-hidden"
          >
            <div className="bg-accent-lime/5 px-6 py-4 border-b border-border-light">
              <div className="flex items-center justify-between">
                <h2 className="font-mono text-small font-bold uppercase tracking-wider text-text-dark">
                  Connection Status
                </h2>
                {status?.connected ? (
                  <CheckCircle2 className="w-5 h-5 text-status-success" />
                ) : (
                  <WifiOff className="w-5 h-5 text-text-secondary" />
                )}
              </div>
            </div>

            <div className="p-6">
              {status?.connected && status.ssid ? (
                <div className="space-y-4">
                  <div className="flex items-center gap-3 p-4 bg-accent-lime/10  border border-accent-lime/20">
                    <div className="p-2 bg-accent-lime ">
                      <Wifi className="w-5 h-5 text-text-dark" />
                    </div>
                    <div className="flex-1">
                      <p className="text-xs text-text-secondary uppercase tracking-wider mb-0.5">Connected to</p>
                      <p className="font-mono text-base font-bold text-text-dark">{status.ssid}</p>
                    </div>
                  </div>

                  <Link
                    href="/internet"
                    className="flex items-center justify-center gap-2 w-full px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider  hover:bg-accent-lime-muted transition-all group"
                  >
                    Verify Internet
                    <ChevronRight className="w-4 h-4 group-hover:translate-x-1 transition-transform" />
                  </Link>
                </div>
              ) : (
                <div className="text-center py-8">
                  <div className="w-16 h-16 mx-auto mb-4 bg-bg-secondary  flex items-center justify-center">
                    <WifiOff className="w-8 h-8 text-text-secondary" />
                  </div>
                  <p className="font-mono text-small text-text-secondary mb-2">Not Connected</p>
                  <p className="text-xs text-text-secondary">Select a network from the list to connect</p>
                </div>
              )}
            </div>
          </motion.div>

          {/* Quick Info Card */}
          <motion.div
            initial={{ opacity: 0, x: -20 }}
            animate={{ opacity: 1, x: 0 }}
            transition={{ delay: 0.2 }}
            className="bg-bg-surface  border border-border-light p-6"
          >
            <h3 className="font-mono text-small font-bold uppercase tracking-wider text-text-dark mb-4">
              Quick Tips
            </h3>
            <ul className="space-y-3">
              <li className="flex items-start gap-2 text-small text-text-secondary">
                <Signal className="w-4 h-4 text-accent-lime mt-0.5 flex-shrink-0" />
                <span>Choose networks with stronger signal for better performance</span>
              </li>
              <li className="flex items-start gap-2 text-small text-text-secondary">
                <Lock className="w-4 h-4 text-accent-lime mt-0.5 flex-shrink-0" />
                <span>WPA3 networks offer the best security</span>
              </li>
              <li className="flex items-start gap-2 text-small text-text-secondary">
                <Shield className="w-4 h-4 text-accent-lime mt-0.5 flex-shrink-0" />
                <span>Saved networks connect automatically when in range</span>
              </li>
            </ul>
          </motion.div>
        </div>

        {/* Right Column - Networks List */}
        <div className="xl:col-span-2">
          <motion.div
            initial={{ opacity: 0, x: 20 }}
            animate={{ opacity: 1, x: 0 }}
            transition={{ delay: 0.15 }}
            className="bg-bg-surface  border border-border-light overflow-hidden"
          >
            {/* Header */}
            <div className="flex items-center justify-between px-6 py-4 border-b border-border-light bg-bg-secondary/30">
              <div className="flex items-center gap-3">
                <div className="relative">
                  <Wifi className="w-5 h-5 text-accent-lime" />
                  {scanning && (
                    <span className="absolute -top-1 -right-1 w-2 h-2 bg-accent-lime  animate-pulse" />
                  )}
                </div>
                <span className="font-mono text-small font-bold uppercase tracking-wider text-text-dark">
                  Available Networks
                </span>
              </div>
              <button
                onClick={loadNetworks}
                disabled={scanning}
                className="flex items-center gap-2 px-4 py-2 font-mono text-label uppercase tracking-wider text-accent-lime-muted hover:text-accent-lime hover:bg-accent-lime/10  transition-all disabled:opacity-50"
              >
                <RefreshCw className={`w-4 h-4 ${scanning ? 'animate-spin' : ''}`} />
                {scanning ? 'Scanning...' : 'Refresh'}
              </button>
            </div>

            {/* Networks List */}
            <div className="max-h-[500px] overflow-y-auto">
              {scanning ? (
                <div className="p-16 flex flex-col items-center justify-center">
                  <Loader2 size={40} className="animate-spin text-accent-lime-muted mb-4" />
                  <span className="font-mono text-small text-text-secondary">Scanning for networks...</span>
                </div>
              ) : networks.length === 0 ? (
                <div className="p-16 text-center">
                  <div className="w-20 h-20 mx-auto mb-4 bg-bg-secondary  flex items-center justify-center">
                    <WifiOff className="w-10 h-10 text-text-secondary opacity-50" />
                  </div>
                  <p className="font-mono text-small text-text-dark mb-2">No networks found</p>
                  <p className="text-xs text-text-secondary">Try refreshing or move closer to a WiFi source</p>
                </div>
              ) : (
                <div className="divide-y divide-border-light">
                  {networks.map((network, index) => (
                    <motion.div
                      key={network.ssid}
                      initial={{ opacity: 0, y: 10 }}
                      animate={{ opacity: 1, y: 0 }}
                      transition={{ delay: index * 0.03 }}
                      className={`
                        px-6 py-4 hover:bg-bg-secondary/30 transition-all cursor-pointer group
                        ${selectedNetwork?.ssid === network.ssid ? 'bg-accent-lime/5 border-l-4 border-l-accent-lime' : 'border-l-4 border-l-transparent'}
                      `}
                      onClick={() => {
                        setSelectedNetwork(network)
                        setPassword('')
                        setError(null)
                        setSuccess(false)
                      }}
                    >
                      <div className="flex items-center justify-between">
                        <div className="flex-1 flex items-center gap-4">
                          {/* Signal Icon */}
                          <div className="flex items-end gap-0.5 h-5">
                            {[1, 2, 3, 4].map((bar) => (
                              <div
                                key={bar}
                                className={`w-1.5 -sm transition-all ${
                                  bar <= getSignalBars(network.signal)
                                    ? 'bg-accent-lime'
                                    : 'bg-border-light'
                                }`}
                                style={{ height: `${bar * 20}%` }}
                              />
                            ))}
                          </div>

                          {/* Network Info */}
                          <div className="flex-1">
                            <div className="flex items-center gap-2 mb-1">
                              <span className="font-mono text-small font-bold text-text-dark">
                                {network.ssid}
                              </span>
                              {network.connected && (
                                <span className="px-2 py-0.5 bg-accent-lime text-text-dark text-xs font-mono font-bold ">
                                  ACTIVE
                                </span>
                              )}
                              {network.saved && (
                                <span className="px-2 py-0.5 bg-accent-lime/10 text-accent-lime-muted text-xs font-mono font-bold  border border-accent-lime/20">
                                  SAVED
                                </span>
                              )}
                            </div>
                            <div className="flex items-center gap-3 text-xs font-mono text-text-secondary">
                              <span className="font-bold">
                                {network.signal}% - {getSignalStrengthLabel(network.signal)}
                              </span>
                              <span className="flex items-center gap-1">
                                {network.security === 'Open' ? (
                                  <WifiOff className="w-3 h-3" />
                                ) : (
                                  <Lock className="w-3 h-3" />
                                )}
                                {getSecurityLabel(network.security)}
                              </span>
                            </div>
                          </div>
                        </div>

                        {/* Actions */}
                        <div className="flex items-center gap-3">
                          {network.saved && (
                            <button
                              onClick={(e) => {
                                e.stopPropagation()
                                handleDeleteNetwork(network.ssid)
                              }}
                              className="p-2 text-text-secondary hover:text-status-error hover:bg-status-error/10  transition-all opacity-0 group-hover:opacity-100"
                              title="Delete saved network"
                            >
                              <Trash2 className="w-4 h-4" />
                            </button>
                          )}
                          <ChevronRight className={`w-5 h-5 text-text-secondary transition-transform ${
                            selectedNetwork?.ssid === network.ssid ? 'rotate-90 text-accent-lime' : ''
                          }`} />
                        </div>
                      </div>
                    </motion.div>
                  ))}
                </div>
              )}
            </div>

            {/* Connection Panel */}
            <AnimatePresence>
              {selectedNetwork && (
                <motion.div
                  initial={{ opacity: 0, height: 0 }}
                  animate={{ opacity: 1, height: 'auto' }}
                  exit={{ opacity: 0, height: 0 }}
                  transition={{ duration: 0.2 }}
                  className="border-t-2 border-accent-lime bg-accent-lime/5"
                >
                  <div className="p-6">
                    {/* Panel Header */}
                    <div className="flex items-center gap-3 mb-5">
                      <div className="p-2 bg-accent-lime ">
                        <Shield className="w-5 h-5 text-text-dark" />
                      </div>
                      <div>
                        <h3 className="font-mono text-base font-bold text-text-dark">
                          {selectedNetwork.ssid}
                        </h3>
                        <p className="text-xs text-text-secondary">
                          {selectedNetwork.security === 'Open' ? 'Open network' : 'Secure network - password required'}
                        </p>
                      </div>
                    </div>

                    {/* Network Details */}
                    <div className="grid grid-cols-3 gap-3 mb-5">
                      <div className="p-3 bg-bg-surface  border border-border-light">
                        <p className="text-xs text-text-secondary uppercase tracking-wider mb-1">Security</p>
                        <p className="font-mono text-small font-bold text-text-dark">
                          {getSecurityLabel(selectedNetwork.security)}
                        </p>
                      </div>
                      <div className="p-3 bg-bg-surface  border border-border-light">
                        <p className="text-xs text-text-secondary uppercase tracking-wider mb-1">Signal</p>
                        <p className="font-mono text-small font-bold text-text-dark">
                          {selectedNetwork.signal}%
                        </p>
                      </div>
                      <div className="p-3 bg-bg-surface  border border-border-light">
                        <p className="text-xs text-text-secondary uppercase tracking-wider mb-1">Quality</p>
                        <p className="font-mono text-small font-bold text-text-dark">
                          {getSignalStrengthLabel(selectedNetwork.signal)}
                        </p>
                      </div>
                    </div>

                    {/* Password Input (for secured networks) */}
                    {selectedNetwork.security !== 'Open' && selectedNetwork.security !== '' ? (
                      <div className="mb-5">
                        <label className="block text-xs font-mono uppercase tracking-wider text-text-secondary mb-2">
                          Network Password
                        </label>
                        <div className="relative">
                          <input
                            type={showPassword ? 'text' : 'password'}
                            value={password}
                            onChange={(e) => setPassword(e.target.value)}
                            onKeyDown={(e) => e.key === 'Enter' && handleConnect()}
                            placeholder="Enter WiFi password"
                            disabled={connecting}
                            className="w-full px-4 py-3 pr-12 bg-bg-surface border border-border-light  font-mono text-small text-text-dark placeholder:text-text-secondary focus:outline-none focus:border-accent-lime focus:ring-2 focus:ring-accent-lime/20 transition-all disabled:opacity-50"
                          />
                          <button
                            type="button"
                            onClick={() => setShowPassword(!showPassword)}
                            disabled={connecting}
                            className="absolute right-3 top-1/2 -translate-y-1/2 p-1 text-text-secondary hover:text-accent-lime transition-colors disabled:opacity-50"
                            tabIndex={-1}
                          >
                            {showPassword ? (
                              <EyeOff className="w-5 h-5" />
                            ) : (
                              <Eye className="w-5 h-5" />
                            )}
                          </button>
                        </div>
                      </div>
                    ) : (
                      <div className="mb-5 p-4 bg-accent-lime/10 border border-accent-lime/20 ">
                        <div className="flex items-center gap-2">
                          <WifiOff className="w-4 h-4 text-accent-lime-muted" />
                          <p className="text-small font-mono text-text-dark">
                            This is an open network - no password required
                          </p>
                        </div>
                      </div>
                    )}

                    {/* Status Messages */}
                    <AnimatePresence>
                      {showLogs && connectionLogs.length > 0 && (
                        <motion.div
                          initial={{ opacity: 0, y: -10 }}
                          animate={{ opacity: 1, y: 0 }}
                          exit={{ opacity: 0, y: -10 }}
                          className="mb-4 p-4 bg-bg-secondary border border-border-light "
                        >
                          <div className="flex items-center justify-between mb-2">
                            <p className="text-xs font-mono uppercase tracking-wider text-text-secondary">Connection Log</p>
                            <button
                              onClick={() => setShowLogs(false)}
                              className="text-xs text-text-secondary hover:text-accent-lime"
                            >
                              Hide
                            </button>
                          </div>
                          <div className="max-h-32 overflow-y-auto space-y-1 font-mono text-xs">
                            {connectionLogs.map((log, i) => (
                              <div
                                key={i}
                                className={`${
                                  log.startsWith('ERROR:') ? 'text-status-error' :
                                  log.startsWith('SUCCESS:') ? 'text-status-success' :
                                  'text-text-secondary'
                                }`}
                              >
                                {log}
                              </div>
                            ))}
                          </div>
                        </motion.div>
                      )}
                      {error && (
                        <motion.div
                          initial={{ opacity: 0, y: -10 }}
                          animate={{ opacity: 1, y: 0 }}
                          exit={{ opacity: 0, y: -10 }}
                          className="mb-4 p-4 bg-status-error/10 border border-status-error/20  flex items-start gap-3"
                        >
                          <AlertCircle className="w-5 h-5 text-status-error flex-shrink-0 mt-0.5" />
                          <div className="flex-1">
                            <p className="text-small text-status-error font-mono">{error}</p>
                            {!showLogs && connectionLogs.length > 0 && (
                              <button
                                onClick={() => setShowLogs(true)}
                                className="text-xs text-status-error/70 hover:text-status-error underline mt-1"
                              >
                                Show connection log
                              </button>
                            )}
                          </div>
                        </motion.div>
                      )}
                      {success && (
                        <motion.div
                          initial={{ opacity: 0, y: -10 }}
                          animate={{ opacity: 1, y: 0 }}
                          exit={{ opacity: 0, y: -10 }}
                          className="mb-4 p-4 bg-status-success/10 border border-status-success/20  flex items-start gap-3"
                        >
                          <CheckCircle2 className="w-5 h-5 text-status-success flex-shrink-0 mt-0.5" />
                          <p className="text-small text-status-success font-mono">Successfully connected to {selectedNetwork.ssid}!</p>
                        </motion.div>
                      )}
                    </AnimatePresence>

                    {/* Action Buttons */}
                    <div className="flex gap-3">
                      <button
                        onClick={handleConnect}
                        disabled={connecting}
                        className="flex-1 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider  hover:bg-accent-lime-muted transition-all disabled:opacity-50 flex items-center justify-center gap-2"
                      >
                        {connecting ? (
                          <>
                            <Loader2 size={18} className="animate-spin" />
                            Connecting...
                          </>
                        ) : (
                          <>
                            <Wifi className="w-4 h-4" />
                            Connect to Network
                          </>
                        )}
                      </button>
                      <button
                        onClick={() => {
                          setSelectedNetwork(null)
                          setPassword('')
                          setShowPassword(false)
                          setError(null)
                        }}
                        disabled={connecting}
                        className="px-6 py-3 bg-bg-surface text-text-dark font-mono font-bold text-small uppercase tracking-wider  hover:bg-border-light transition-all disabled:opacity-50 border border-border-light"
                      >
                        Cancel
                      </button>
                    </div>
                  </div>
                </motion.div>
              )}
            </AnimatePresence>
          </motion.div>
        </div>
      </div>

      {/* Access Point Management Section */}
      <div className="mt-10">
        <div className="mb-6">
          <div className="flex items-center gap-3 mb-3">
            <div className="p-2 bg-accent-lime/10 ">
              <Router className="w-6 h-6 text-accent-lime" />
            </div>
            <h2 className="font-serif text-h2 text-text-dark">Access Point</h2>
          </div>
          <p className="text-body text-text-secondary max-w-xl">
            Manage your device's WiFi access point. Other devices can connect to this AP to access the dashboard
            and share internet when connected.
          </p>
        </div>

        <div className="grid grid-cols-1 xl:grid-cols-3 gap-6">
          {/* AP Status Card */}
          <motion.div
            initial={{ opacity: 0, x: -20 }}
            animate={{ opacity: 1, x: 0 }}
            transition={{ delay: 0.3 }}
            className="xl:col-span-1"
          >
            <div className="bg-bg-surface  border border-border-light overflow-hidden">
              <div className="bg-accent-lime/5 px-6 py-4 border-b border-border-light">
                <div className="flex items-center justify-between">
                  <h3 className="font-mono text-small font-bold uppercase tracking-wider text-text-dark">
                    AP Status
                  </h3>
                  {apStatus?.running ? (
                    <span className="flex items-center gap-1.5">
                      <span className="w-2 h-2  bg-status-success animate-pulse" />
                      <span className="font-mono text-[10px] text-status-success uppercase">Active</span>
                    </span>
                  ) : (
                    <span className="flex items-center gap-1.5">
                      <span className="w-2 h-2  bg-text-secondary" />
                      <span className="font-mono text-[10px] text-text-secondary uppercase">Inactive</span>
                    </span>
                  )}
                </div>
              </div>
              <div className="p-6 space-y-4">
                <div className="grid grid-cols-2 gap-3">
                  <div className="p-3 bg-bg-secondary ">
                    <p className="text-[10px] text-text-secondary uppercase tracking-wider mb-1">SSID</p>
                    <p className="font-mono text-small font-bold truncate">
                      {apStatus?.ssid || '—'}
                    </p>
                  </div>
                  <div className="p-3 bg-bg-secondary ">
                    <p className="text-[10px] text-text-secondary uppercase tracking-wider mb-1">Clients</p>
                    <p className="font-mono text-small font-bold flex items-center gap-1.5">
                      <Users className="w-3.5 h-3.5 text-accent-lime" />
                      {apStatus?.connected_clients ?? 0}
                    </p>
                  </div>
                  <div className="p-3 bg-bg-secondary ">
                    <p className="text-[10px] text-text-secondary uppercase tracking-wider mb-1">IP</p>
                    <p className="font-mono text-small font-bold">
                      {apStatus?.ip_address || '—'}
                    </p>
                  </div>
                  <div className="p-3 bg-bg-secondary ">
                    <p className="text-[10px] text-text-secondary uppercase tracking-wider mb-1">Interface</p>
                    <p className="font-mono text-small font-bold">
                      {apStatus?.interface || '—'}
                    </p>
                  </div>
                </div>

                {/* Enable/Disable Toggle */}
                <div className="flex items-center justify-between pt-2 border-t border-border-light">
                  <div className="flex items-center gap-2">
                    <Power className="w-4 h-4 text-text-secondary" />
                    <span className="font-mono text-small text-text-dark">
                      {apStatus?.enabled ? 'Enabled' : 'Disabled'}
                    </span>
                  </div>
                  <button
                    onClick={async () => {
                      setApToggling(true)
                      setApMessage(null)
                      try {
                        if (apStatus?.enabled) {
                          await apApi.disable()
                        } else {
                          await apApi.enable()
                        }
                        await loadAPData()
                        setApMessage({ text: `AP ${apStatus?.enabled ? 'disabled' : 'enabled'} successfully`, type: 'success' })
                      } catch (err: any) {
                        setApMessage({ text: err.message || 'Failed to toggle AP', type: 'error' })
                      } finally {
                        setApToggling(false)
                      }
                    }}
                    disabled={apToggling}
                    className={`
                      w-10 h-5  transition-colors relative disabled:opacity-50
                      ${apStatus?.enabled ? 'bg-accent-lime' : 'bg-border-light'}
                    `}
                  >
                    <span
                      className={`
                        absolute top-0.5 w-4 h-4  bg-white transition-transform
                        ${apStatus?.enabled ? 'left-5' : 'left-0.5'}
                      `}
                    />
                  </button>
                </div>
              </div>
            </div>
          </motion.div>

          {/* AP Configuration Card */}
          <motion.div
            initial={{ opacity: 0, x: 20 }}
            animate={{ opacity: 1, x: 0 }}
            transition={{ delay: 0.35 }}
            className="xl:col-span-2"
          >
            <div className="bg-bg-surface  border border-border-light overflow-hidden">
              <div className="flex items-center justify-between px-6 py-4 border-b border-border-light bg-bg-secondary/30">
                <div className="flex items-center gap-3">
                  <Settings2 className="w-5 h-5 text-accent-lime" />
                  <span className="font-mono text-small font-bold uppercase tracking-wider text-text-dark">
                    AP Configuration
                  </span>
                </div>
              </div>

              <div className="p-6 space-y-5">
                {/* SSID */}
                <div>
                  <label className="block text-xs font-mono uppercase tracking-wider text-text-secondary mb-2">
                    Network Name (SSID)
                  </label>
                  <input
                    type="text"
                    value={apSsid}
                    onChange={e => setApSsid(e.target.value)}
                    placeholder="Access Point SSID"
                    className="w-full px-4 py-3 bg-bg-surface border border-border-light  font-mono text-small text-text-dark placeholder:text-text-secondary focus:outline-none focus:border-accent-lime focus:ring-2 focus:ring-accent-lime/20 transition-all"
                  />
                </div>

                {/* Password */}
                <div>
                  <label className="block text-xs font-mono uppercase tracking-wider text-text-secondary mb-2">
                    Password (min 8 characters)
                  </label>
                  <div className="relative">
                    <input
                      type={showApPassword ? 'text' : 'password'}
                      value={apPassword}
                      onChange={e => setApPassword(e.target.value)}
                      placeholder="WiFi password"
                      className="w-full px-4 py-3 pr-12 bg-bg-surface border border-border-light  font-mono text-small text-text-dark placeholder:text-text-secondary focus:outline-none focus:border-accent-lime focus:ring-2 focus:ring-accent-lime/20 transition-all"
                    />
                    <button
                      type="button"
                      onClick={() => setShowApPassword(!showApPassword)}
                      className="absolute right-3 top-1/2 -translate-y-1/2 p-1 text-text-secondary hover:text-accent-lime transition-colors"
                      tabIndex={-1}
                    >
                      {showApPassword ? <EyeOff className="w-5 h-5" /> : <Eye className="w-5 h-5" />}
                    </button>
                  </div>
                </div>

                {/* Channel */}
                <div>
                  <label className="block text-xs font-mono uppercase tracking-wider text-text-secondary mb-2">
                    Channel
                  </label>
                  <select
                    value={apChannel}
                    onChange={e => setApChannel(Number(e.target.value))}
                    className="w-full px-4 py-3 bg-bg-surface border border-border-light  font-mono text-small text-text-dark focus:outline-none focus:border-accent-lime focus:ring-2 focus:ring-accent-lime/20 transition-all appearance-none cursor-pointer"
                  >
                    {[1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11].map(ch => (
                      <option key={ch} value={ch}>
                        Channel {ch} {ch === 1 || ch === 6 || ch === 11 ? '(Recommended)' : ''}
                      </option>
                    ))}
                  </select>
                </div>

                {/* AP Message */}
                <AnimatePresence>
                  {apMessage && (
                    <motion.div
                      initial={{ opacity: 0, y: -10 }}
                      animate={{ opacity: 1, y: 0 }}
                      exit={{ opacity: 0, y: -10 }}
                      className={`flex items-center gap-2 px-4 py-3  ${
                        apMessage.type === 'success'
                          ? 'bg-status-success/10 border border-status-success/20'
                          : 'bg-status-error/10 border border-status-error/20'
                      }`}
                    >
                      {apMessage.type === 'success' ? (
                        <CheckCircle2 className="w-4 h-4 text-status-success flex-shrink-0" />
                      ) : (
                        <AlertCircle className="w-4 h-4 text-status-error flex-shrink-0" />
                      )}
                      <p className={`font-mono text-small ${
                        apMessage.type === 'success' ? 'text-status-success' : 'text-status-error'
                      }`}>
                        {apMessage.text}
                      </p>
                    </motion.div>
                  )}
                </AnimatePresence>

                {/* Apply Changes Button */}
                <div className="flex gap-3 pt-2">
                  <button
                    onClick={async () => {
                      setApLoading(true)
                      setApMessage(null)
                      try {
                        const updates: { ssid?: string; password?: string; channel?: number } = {}
                        if (apSsid !== apConfig?.ssid) updates.ssid = apSsid
                        if (apPassword !== apConfig?.password) updates.password = apPassword
                        if (apChannel !== apConfig?.channel) updates.channel = apChannel

                        if (Object.keys(updates).length === 0) {
                          setApMessage({ text: 'No changes to apply', type: 'error' })
                          setApLoading(false)
                          return
                        }

                        await apApi.updateConfig(updates)
                        await loadAPData()
                        setApMessage({ text: 'AP configuration updated and applied', type: 'success' })
                      } catch (err: any) {
                        setApMessage({ text: err.message || 'Failed to update AP config', type: 'error' })
                      } finally {
                        setApLoading(false)
                      }
                    }}
                    disabled={apLoading}
                    className="flex-1 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider  hover:bg-accent-lime-muted transition-all disabled:opacity-50 flex items-center justify-center gap-2"
                  >
                    {apLoading ? (
                      <>
                        <Loader2 size={18} className="animate-spin" />
                        Applying...
                      </>
                    ) : (
                      <>
                        <Save className="w-4 h-4" />
                        Apply Changes
                      </>
                    )}
                  </button>
                </div>
              </div>
            </div>
          </motion.div>
        </div>
      </div>
    </motion.div>
  )
}
