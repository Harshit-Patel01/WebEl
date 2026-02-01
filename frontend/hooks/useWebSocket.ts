'use client'

import { useEffect, useState, useRef } from 'react'

export interface WSMessage {
  type: string
  jobId?: string
  line?: {
    timestamp: string
    stream: string
    text: string
    level: string
  }
  percent?: number
  phase?: string
  cpu?: number
  ram?: number
  temp?: number
  uptime?: string
  service?: string
  status?: string
  error?: string
}

export function useWebSocket() {
  const [lastMessage, setLastMessage] = useState<WSMessage | null>(null)
  const [systemStats, setSystemStats] = useState<{ cpu: number; ram: number; temp: number; uptime: string } | null>(null)
  const [isConnected, setIsConnected] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    // Only connect in browser environment
    if (typeof window === 'undefined') return

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const host = window.location.host
    const wsUrl = `${protocol}//${host}/ws`

    // Fallback for development mode when running frontend on port 3000 but backend is on 3001
    // In production, they are on the same port
    const url = process.env.NODE_ENV === 'development'
      ? 'ws://localhost:3000/ws' // Corrected backend port
      : wsUrl
    const connect = () => {
      try {
        const ws = new WebSocket(url)

        ws.onopen = () => {
          console.log('WebSocket connected')
          setIsConnected(true)
        }

        ws.onmessage = (event) => {
          try {
            // Handle multiple messages separated by newline
            const messages = event.data.split('\n').filter((msg: string) => msg.trim() !== '')

            for (const msgStr of messages) {
              const data = JSON.parse(msgStr) as WSMessage
              setLastMessage(data)

              // Auto-handle system stats updates
              if (data.type === 'system_stats') {
                setSystemStats({
                  cpu: data.cpu || 0,
                  ram: data.ram || 0,
                  temp: data.temp || 0,
                  uptime: data.uptime || '',
                })
              }
            }
          } catch (e) {
            console.error('Failed to parse WS message', e)
          }
        }

        ws.onclose = () => {
          console.log('WebSocket disconnected')
          setIsConnected(false)
          // Try to reconnect in 5 seconds
          setTimeout(connect, 5000)
        }

        ws.onerror = (err) => {
          console.error('WebSocket error', err)
          ws.close()
        }

        wsRef.current = ws
      } catch (err) {
        console.error('Failed to create WebSocket', err)
      }
    }

    connect()

    return () => {
      if (wsRef.current) {
        wsRef.current.close()
      }
    }
  }, [])

  const subscribeToJob = (jobId: string) => {
    if (wsRef.current && isConnected) {
      wsRef.current.send(JSON.stringify({ type: 'subscribe_job', jobId }))
    }
  }

  return { lastMessage, systemStats, isConnected, subscribeToJob }
}
