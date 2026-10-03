'use client'

import { createContext, useContext, useState, useEffect, ReactNode } from 'react'
import { systemApi, tunnelApi } from '@/lib/api'
import { useWebSocket } from '@/contexts/WebSocketContext'

interface SystemStats {
  cpu: number
  ram: number
  temp: number
  uptime: string
}

interface TunnelStatus {
  tunnel_id?: string
  tunnel_name?: string
  domain?: string
  status: string
}

interface SystemContextType {
  stats: SystemStats | null
  tunnelStatus: TunnelStatus | null
  loading: boolean
  refreshStats: () => Promise<void>
  refreshTunnel: () => Promise<void>
}

const SystemContext = createContext<SystemContextType | undefined>(undefined)

export function SystemProvider({ children }: { children: ReactNode }) {
  const [stats, setStats] = useState<SystemStats | null>(null)
  const [tunnelStatus, setTunnelStatus] = useState<TunnelStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const { subscribe } = useWebSocket()

  const refreshStats = async () => {
    try {
      const data = await systemApi.getStats()
      setStats(data)
    } catch (err) {
      console.error('Failed to fetch stats:', err)
    }
  }

  const refreshTunnel = async () => {
    try {
      const data = await tunnelApi.getStatus()
      setTunnelStatus(data)
    } catch (err) {
      console.error('Failed to fetch tunnel status:', err)
    }
  }

  // Initial load
  useEffect(() => {
    const loadInitialData = async () => {
      setLoading(true)
      await Promise.all([refreshStats(), refreshTunnel()])
      setLoading(false)
    }
    loadInitialData()
  }, [])

  // Subscribe to WebSocket system stats updates
  useEffect(() => {
    const unsubscribe = subscribe('system_stats', (message) => {
      if (message.cpu !== undefined && message.ram !== undefined) {
        setStats({
          cpu: message.cpu,
          ram: message.ram,
          temp: message.temp || 0,
          uptime: message.uptime || '',
        })
      }
    })

    return unsubscribe
  }, [subscribe])

  // Subscribe to tunnel events
  useEffect(() => {
    const unsubscribe = subscribe('tunnel_event', () => {
      refreshTunnel()
    })

    return unsubscribe
  }, [subscribe])

  return (
    <SystemContext.Provider
      value={{
        stats,
        tunnelStatus,
        loading,
        refreshStats,
        refreshTunnel,
      }}
    >
      {children}
    </SystemContext.Provider>
  )
}

export function useSystem() {
  const context = useContext(SystemContext)
  if (context === undefined) {
    throw new Error('useSystem must be used within a SystemProvider')
  }
  return context
}
