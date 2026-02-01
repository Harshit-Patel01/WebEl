'use client'

import { useEffect, useState } from 'react'
import { Menu } from 'lucide-react'
import StatusPill from '@/components/ui/StatusPill'
import { useWebSocket } from '@/contexts/WebSocketContext'
import { useSystem } from '@/contexts/SystemContext'
import { systemApi, wifiApi } from '@/lib/api'

interface TopStatusBarProps {
  onMenuClick?: () => void
}

export default function TopStatusBar({ onMenuClick }: TopStatusBarProps) {
  const { connectionStatus } = useWebSocket()
  const { stats: systemStats, tunnelStatus } = useSystem()

  const [hostname, setHostname] = useState('opendeploy.local')
  const [ip, setIp] = useState('unknown')
  const [wifiConnected, setWifiConnected] = useState(false)
  const [uptime, setUptime] = useState('unknown')
  const [dataLoaded, setDataLoaded] = useState(false)

  useEffect(() => {
    // Only fetch once on mount
    if (!dataLoaded) {
      const fetchData = async () => {
        try {
          const info = await systemApi.getInfo()
          if (info.hostname) setHostname(info.hostname)
          if (info.ip) setIp(info.ip)

          const wifi = await wifiApi.getStatus()
          setWifiConnected(wifi.connected)
          setDataLoaded(true)
        } catch (err) {
          console.error('Failed to fetch status bar initial data', err)
        }
      }
      fetchData()
    }
  }, [dataLoaded])

  // Update uptime from system stats
  useEffect(() => {
    if (systemStats?.uptime) {
      setUptime(systemStats.uptime)
    }
  }, [systemStats])

  const isConnected = connectionStatus === 'connected'

  return (
    <div className="h-12 bg-bg-secondary border-b border-border-dark flex items-center px-4 md:px-6 gap-3 md:gap-6 overflow-x-auto">
      {/* Mobile hamburger button */}
      <button
        onClick={onMenuClick}
        className="lg:hidden p-2 -ml-2 text-text-primary hover:text-accent-lime transition-colors"
        aria-label="Open menu"
      >
        <Menu size={20} />
      </button>

      <StatusPill status={isConnected ? "healthy" : "error"} label={hostname} size="sm" />

      {/* Desktop dividers and stats */}
      <div className="hidden md:block w-px h-5 bg-border-dark" />
      <span className="hidden md:flex font-mono text-[11px] text-text-secondary whitespace-nowrap items-center gap-2">
        CPU: <span className="text-text-primary w-8">{systemStats ? `${systemStats.cpu.toFixed(0)}%` : '--'}</span>
      </span>

      <div className="hidden md:block w-px h-5 bg-border-dark" />
      <span className="hidden md:flex font-mono text-[11px] text-text-secondary whitespace-nowrap items-center gap-2">
        RAM: <span className="text-text-primary w-8">{systemStats ? `${systemStats.ram.toFixed(0)}%` : '--'}</span>
      </span>

      <div className="hidden lg:block w-px h-5 bg-border-dark" />
      <span className="hidden lg:flex font-mono text-[11px] text-text-secondary whitespace-nowrap">
        IP: <span className="text-text-primary">{ip}</span>
      </span>

      <div className="hidden sm:block w-px h-5 bg-border-dark" />
      <StatusPill status={wifiConnected ? "healthy" : "error"} label={wifiConnected ? "WiFi" : "WiFi"} size="sm" />

      {tunnelStatus && tunnelStatus.status !== 'not_configured' && (
        <>
          <div className="hidden sm:block w-px h-5 bg-border-dark" />
          <StatusPill
            status={tunnelStatus.status === 'active' ? "healthy" : "inactive"}
            label={`Tunnel`}
            size="sm"
          />
        </>
      )}

      <div className="hidden lg:block w-px h-5 bg-border-dark" />
      <span className="hidden lg:flex font-mono text-[11px] text-text-secondary whitespace-nowrap">
        Uptime: <span className="text-text-primary">{uptime}</span>
      </span>
    </div>
  )
}

