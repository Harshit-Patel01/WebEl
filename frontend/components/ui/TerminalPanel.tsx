'use client'

import { useEffect, useRef, useState } from 'react'

interface TerminalPanelProps {
  lines: string[]
  streaming?: boolean
  maxHeight?: string
}

export default function TerminalPanel({ lines, streaming = false, maxHeight = '400px' }: TerminalPanelProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [visibleLines, setVisibleLines] = useState<string[]>(streaming ? [] : lines)

  useEffect(() => {
    if (!streaming) {
      setVisibleLines(lines)
      return
    }

    setVisibleLines([])
    let idx = 0
    const interval = setInterval(() => {
      if (idx < lines.length) {
        setVisibleLines(prev => [...prev, lines[idx]])
        idx++
      } else {
        clearInterval(interval)
      }
    }, 200)

    return () => clearInterval(interval)
  }, [lines, streaming])

  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight
    }
  }, [visibleLines])

  return (
    <div
      ref={containerRef}
      className="bg-bg-primary border border-border-dark rounded-card p-4 overflow-auto font-mono text-small"
      style={{ maxHeight }}
    >
      {visibleLines.map((line, i) => {
        if (!line) return null
        let textColor = 'text-accent-lime'
        if (line.includes('ERROR') || line.includes('error')) textColor = 'text-status-error'
        else if (line.includes('WARN') || line.includes('warn')) textColor = 'text-status-warning'

        return (
          <div key={i} className={`${textColor} leading-relaxed whitespace-pre`}>
            {line}
          </div>
        )
      })}
      {streaming && visibleLines.length < lines.length && (
        <span className="terminal-cursor text-accent-lime"> </span>
      )}
    </div>
  )
}
