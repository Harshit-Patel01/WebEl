'use client'

import { createContext, useContext, useEffect, useRef, useState, useCallback, ReactNode } from 'react'

export interface WSMessage {
  type: 'system_stats' | 'service_update' | 'tunnel_event' | 'wifi_event' | 'log_line' | 'progress' | 'job_complete' | 'job_failed' | 'deploy_log' | 'deploy_complete' | 'job_started'
  jobId?: string
  deployId?: string
  line?: {
    timestamp: string
    stream: string
    text: string
    level: string
  }
  percent?: number
  phase?: string
  message?: string
  stream?: string
  cpu?: number
  ram?: number
  temp?: number
  uptime?: string
  service?: string
  status?: string
  error?: string
}

type MessageCallback = (message: WSMessage) => void
type ConnectionStatus = 'connected' | 'connecting' | 'disconnected'

interface WebSocketContextType {
  subscribe: (messageType: string, callback: MessageCallback) => () => void
  send: (message: any) => void
  connectionStatus: ConnectionStatus
  lastMessage: WSMessage | null
}

const WebSocketContext = createContext<WebSocketContextType | undefined>(undefined)

export function WebSocketProvider({ children }: { children: ReactNode }) {
  const [connectionStatus, setConnectionStatus] = useState<ConnectionStatus>('connecting')
  const [lastMessage, setLastMessage] = useState<WSMessage | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<NodeJS.Timeout | null>(null)
  const reconnectAttemptsRef = useRef(0)
  const subscribersRef = useRef<Map<string, Set<MessageCallback>>>(new Map())
  const isIntentionalCloseRef = useRef(false)

  const getReconnectDelay = useCallback(() => {
    // Exponential backoff: 1s, 2s, 4s, 8s, max 30s
    const delay = Math.min(1000 * Math.pow(2, reconnectAttemptsRef.current), 30000)
    return delay
  }, [])

  const connect = useCallback(() => {
    if (typeof window === 'undefined') return
    if (wsRef.current?.readyState === WebSocket.OPEN) return

    setConnectionStatus('connecting')

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const host = window.location.host
    const wsUrl = `${protocol}//${host}/ws`

    // Development fallback
    const url = process.env.NODE_ENV === 'development'
      ? 'ws://localhost:3000/ws'
      : wsUrl

    try {
      const ws = new WebSocket(url)

      ws.onopen = () => {
        console.log('WebSocket connected')
        setConnectionStatus('connected')
        reconnectAttemptsRef.current = 0
      }

      ws.onmessage = (event) => {
        try {
          const messages = event.data.split('\n').filter((msg: string) => msg.trim() !== '')

          for (const msgStr of messages) {
            const data = JSON.parse(msgStr) as WSMessage
            setLastMessage(data)

            // Notify subscribers for this message type
            const subscribers = subscribersRef.current.get(data.type)
            if (subscribers) {
              subscribers.forEach(callback => callback(data))
            }

            // Notify wildcard subscribers (subscribed to '*')
            const wildcardSubscribers = subscribersRef.current.get('*')
            if (wildcardSubscribers) {
              wildcardSubscribers.forEach(callback => callback(data))
            }
          }
        } catch (e) {
          console.error('Failed to parse WS message', e)
        }
      }

      ws.onclose = () => {
        console.log('WebSocket disconnected')
        setConnectionStatus('disconnected')
        wsRef.current = null

        // Only reconnect if not intentionally closed
        if (!isIntentionalCloseRef.current) {
          const delay = getReconnectDelay()
          console.log(`Reconnecting in ${delay}ms (attempt ${reconnectAttemptsRef.current + 1})`)

          reconnectTimeoutRef.current = setTimeout(() => {
            reconnectAttemptsRef.current++
            connect()
          }, delay)
        }
      }

      ws.onerror = (err) => {
        console.error('WebSocket error', err)
        ws.close()
      }

      wsRef.current = ws
    } catch (err) {
      console.error('Failed to create WebSocket', err)
      setConnectionStatus('disconnected')

      // Retry connection
      const delay = getReconnectDelay()
      reconnectTimeoutRef.current = setTimeout(() => {
        reconnectAttemptsRef.current++
        connect()
      }, delay)
    }
  }, [getReconnectDelay])

  useEffect(() => {
    connect()

    return () => {
      isIntentionalCloseRef.current = true
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current)
      }
      if (wsRef.current) {
        wsRef.current.close()
      }
    }
  }, [connect])

  const subscribe = useCallback((messageType: string, callback: MessageCallback) => {
    if (!subscribersRef.current.has(messageType)) {
      subscribersRef.current.set(messageType, new Set())
    }
    subscribersRef.current.get(messageType)!.add(callback)

    // Return unsubscribe function
    return () => {
      const subscribers = subscribersRef.current.get(messageType)
      if (subscribers) {
        subscribers.delete(callback)
        if (subscribers.size === 0) {
          subscribersRef.current.delete(messageType)
        }
      }
    }
  }, [])

  const send = useCallback((message: any) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(message))
    } else {
      console.warn('WebSocket not connected, cannot send message')
    }
  }, [])

  return (
    <WebSocketContext.Provider value={{ subscribe, send, connectionStatus, lastMessage }}>
      {children}
    </WebSocketContext.Provider>
  )
}

export function useWebSocket() {
  const context = useContext(WebSocketContext)
  if (context === undefined) {
    throw new Error('useWebSocket must be used within a WebSocketProvider')
  }
  return context
}
