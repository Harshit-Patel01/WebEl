'use client'

import { useEffect, useRef, useState, useCallback } from 'react'
import { deployApi } from '@/lib/api'
import { useWebSocket } from '@/contexts/WebSocketContext'

interface LogLine {
  stream: string
  message: string
  timestamp: string
}

interface DeployLogStreamProps {
  deployId: string
  onComplete?: (result: {
    status: string
    outputPath?: string
    framework?: string
    isBackend?: boolean
    buildDuration?: number
  }) => void
  className?: string
  maxHeight?: string
}

type TransportMode = 'sse' | 'polling' | 'ws'

export default function DeployLogStream({
  deployId,
  onComplete,
  className = '',
  maxHeight = '500px',
}: DeployLogStreamProps) {
  const [logs, setLogs] = useState<LogLine[]>([])
  const [status, setStatus] = useState<string>('connecting')
  const [transport, setTransport] = useState<TransportMode>('sse')
  const [autoScroll, setAutoScroll] = useState(true)
  const [filter, setFilter] = useState('')
  const logContainerRef = useRef<HTMLDivElement>(null)
  const eventSourceRef = useRef<EventSource | null>(null)
  const pollingRef = useRef<NodeJS.Timeout | null>(null)
  const lastTimestampRef = useRef<string>('')
  const completedRef = useRef(false)
  const { subscribe } = useWebSocket()

  // Scroll to bottom when logs change
  useEffect(() => {
    if (autoScroll && logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight
    }
  }, [logs, autoScroll])

  // Handle scroll to detect if user scrolled up
  const handleScroll = useCallback(() => {
    if (!logContainerRef.current) return
    const { scrollTop, scrollHeight, clientHeight } = logContainerRef.current
    const isAtBottom = scrollHeight - scrollTop - clientHeight < 50
    setAutoScroll(isAtBottom)
  }, [])

  // Add log lines avoiding duplicates
  const addLogs = useCallback((newLogs: LogLine[]) => {
    setLogs(prev => {
      const existing = new Set(prev.map(l => `${l.timestamp}-${l.message}`))
      const unique = newLogs.filter(l => !existing.has(`${l.timestamp}-${l.message}`))
      if (unique.length === 0) return prev
      return [...prev, ...unique]
    })
    if (newLogs.length > 0) {
      lastTimestampRef.current = newLogs[newLogs.length - 1].timestamp
    }
  }, [])

  // SSE Transport
  const connectSSE = useCallback(() => {
    if (!deployId || completedRef.current) return

    const url = deployApi.getDeployLogStreamUrl(deployId)
    const es = new EventSource(url)
    eventSourceRef.current = es

    es.addEventListener('status', (e) => {
      const data = JSON.parse(e.data)
      setStatus(data.status === 'running' ? 'streaming' : data.status)
    })

    es.addEventListener('log', (e) => {
      const data = JSON.parse(e.data)
      addLogs([{
        stream: data.stream,
        message: data.message,
        timestamp: data.timestamp,
      }])
      setStatus('streaming')
    })

    es.addEventListener('done', (e) => {
      const data = JSON.parse(e.data)
      completedRef.current = true
      setStatus(data.status)
      es.close()
      if (onComplete) {
        onComplete({
          status: data.status,
          outputPath: data.outputPath,
          framework: data.framework,
          isBackend: data.isBackend,
          buildDuration: data.buildDuration,
        })
      }
    })

    es.onerror = () => {
      es.close()
      if (!completedRef.current) {
        // Fallback to long-polling
        setTransport('polling')
      }
    }

    es.onopen = () => {
      setTransport('sse')
      setStatus('streaming')
    }
  }, [deployId, addLogs, onComplete])

  // Long-polling Transport (fallback)
  const startPolling = useCallback(() => {
    if (!deployId || completedRef.current) return

    const poll = async () => {
      try {
        const logs = await deployApi.pollDeployLogs(
          deployId,
          lastTimestampRef.current || undefined
        )
        if (logs && logs.length > 0) {
          addLogs(logs.map((l: any) => ({
            stream: l.stream,
            message: l.message,
            timestamp: l.log_timestamp || l.timestamp,
          })))
          setStatus('streaming')
        }

        // Check deploy status
        const deploy = await deployApi.getDeploy(deployId)
        if (deploy && (deploy.status === 'success' || deploy.status === 'failed')) {
          completedRef.current = true
          setStatus(deploy.status)
          if (onComplete) {
            onComplete({
              status: deploy.status,
              outputPath: deploy.output_path,
              framework: deploy.framework,
              isBackend: deploy.is_backend,
              buildDuration: deploy.build_duration,
            })
          }
          return
        }
      } catch (err) {
        console.error('Polling error:', err)
      }

      if (!completedRef.current) {
        pollingRef.current = setTimeout(poll, 1000)
      }
    }

    poll()
  }, [deployId, addLogs, onComplete])

  // WebSocket Transport (final fallback)
  useEffect(() => {
    if (transport !== 'ws' || !deployId || completedRef.current) return

    const unsubLog = subscribe('deploy_log', (msg) => {
      if (msg.deployId !== deployId) return
      addLogs([{
        stream: msg.stream || 'stdout',
        message: msg.message || '',
        timestamp: (msg as any).timestamp || new Date().toISOString(),
      }])
      setStatus('streaming')
    })

    const unsubResult = subscribe('deploy_result', (msg) => {
      if (msg.deployId !== deployId) return
      completedRef.current = true
      setStatus(msg.status || 'success')
      if (onComplete) {
        onComplete({
          status: msg.status || 'success',
          outputPath: (msg as any).outputPath,
          framework: (msg as any).framework,
          isBackend: (msg as any).isBackend,
          buildDuration: (msg as any).buildDuration,
        })
      }
    })

    return () => {
      unsubLog()
      unsubResult()
    }
  }, [transport, deployId, subscribe, addLogs, onComplete])

  // Start the appropriate transport
  useEffect(() => {
    completedRef.current = false
    // Don't clear logs on reconnect, only on new deployId
    setStatus('connecting')
    lastTimestampRef.current = ''

    if (transport === 'sse') {
      connectSSE()
    } else if (transport === 'polling') {
      startPolling()
    }
    // ws handled by separate useEffect

    return () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close()
        eventSourceRef.current = null
      }
      if (pollingRef.current) {
        clearTimeout(pollingRef.current)
        pollingRef.current = null
      }
    }
  }, [deployId, transport, connectSSE, startPolling])

  // Clear logs only when deployId changes
  useEffect(() => {
    setLogs([])
    lastTimestampRef.current = ''
    completedRef.current = false
  }, [deployId])

  // Filter logs
  const filteredLogs = filter
    ? logs.filter(l => l.message.toLowerCase().includes(filter.toLowerCase()))
    : logs

  const getStreamColor = (stream: string) => {
    if (stream === 'stderr') return 'text-orange-400'
    return 'text-cyan-300'
  }

  const getStatusBadge = () => {
    switch (status) {
      case 'connecting':
        return <span className="inline-flex items-center gap-1 px-2 py-0.5  text-xs bg-yellow-900/30 text-yellow-400 border border-yellow-800/50">
          <span className="w-1.5 h-1.5  bg-yellow-400 animate-pulse" />
          Connecting
        </span>
      case 'streaming':
        return <span className="inline-flex items-center gap-1 px-2 py-0.5  text-xs bg-blue-900/30 text-blue-400 border border-blue-800/50">
          <span className="w-1.5 h-1.5  bg-blue-400 animate-pulse" />
          Live
        </span>
      case 'success':
        return <span className="inline-flex items-center gap-1 px-2 py-0.5  text-xs bg-emerald-900/30 text-emerald-400 border border-emerald-800/50">
          <span className="w-1.5 h-1.5  bg-emerald-400" />
          Success
        </span>
      case 'failed':
        return <span className="inline-flex items-center gap-1 px-2 py-0.5  text-xs bg-red-900/30 text-red-400 border border-red-800/50">
          <span className="w-1.5 h-1.5  bg-red-400" />
          Failed
        </span>
      default:
        return <span className="inline-flex items-center gap-1 px-2 py-0.5  text-xs bg-zinc-800 text-zinc-400 border border-zinc-700">
          {status}
        </span>
    }
  }

  return (
    <div className={`flex flex-col gap-2 ${className}`}>
      {/* Header with status and controls */}
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          {getStatusBadge()}
          <span className="text-xs text-zinc-500">
            via {transport.toUpperCase()} · {logs.length} lines
          </span>
        </div>
        <div className="flex items-center gap-2">
          <input
            type="text"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter logs..."
            className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700  text-zinc-300 placeholder-zinc-600 w-40 focus:outline-none focus:border-zinc-500"
          />
          <button
            onClick={() => setAutoScroll(!autoScroll)}
            className={`px-2 py-1 text-xs  border ${
              autoScroll
                ? 'bg-blue-900/30 text-blue-400 border-blue-800/50'
                : 'bg-zinc-800 text-zinc-400 border-zinc-700'
            }`}
            title={autoScroll ? 'Auto-scroll ON' : 'Auto-scroll OFF'}
          >
            {autoScroll ? '⬇ Auto' : '⬇ Manual'}
          </button>
        </div>
      </div>

      {/* Log output */}
      <div
        ref={logContainerRef}
        onScroll={handleScroll}
        className="bg-zinc-950 border border-zinc-800  p-3 overflow-y-auto font-mono text-xs leading-5 select-text"
        style={{ maxHeight }}
      >
        {filteredLogs.length === 0 && status === 'connecting' && (
          <div className="text-zinc-600 animate-pulse">Waiting for deployment logs...</div>
        )}
        {filteredLogs.map((line, i) => (
          <div key={i} className={`whitespace-pre-wrap break-all ${getStreamColor(line.stream)}`}>
            {line.message}
          </div>
        ))}
        {status === 'streaming' && (
          <div className="text-zinc-600 animate-pulse mt-1">▌</div>
        )}
      </div>
    </div>
  )
}
