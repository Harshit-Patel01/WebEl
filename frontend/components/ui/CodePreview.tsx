'use client'

import { Copy, Check } from 'lucide-react'
import { useState } from 'react'

interface CodePreviewProps {
  code: string
  title?: string
}

export default function CodePreview({ code, title }: CodePreviewProps) {
  const [copied, setCopied] = useState(false)
  const lines = code.split('\n')

  const handleCopy = () => {
    navigator.clipboard.writeText(code)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  const highlightLine = (line: string) => {
    // Comments
    if (line.trim().startsWith('#')) {
      return <span className="text-text-secondary">{line}</span>
    }

    // Nginx directives
    const parts = line.match(/^(\s*)([\w_]+)(\s+)(.+?)(;?)$/)
    if (parts) {
      return (
        <>
          <span>{parts[1]}</span>
          <span className="text-accent-lime">{parts[2]}</span>
          <span>{parts[3]}</span>
          <span className="text-text-primary">{parts[4]}</span>
          <span className="text-text-secondary">{parts[5]}</span>
        </>
      )
    }

    // Block markers
    if (line.includes('{') || line.includes('}')) {
      return <span className="text-text-secondary">{line}</span>
    }

    return <span>{line}</span>
  }

  return (
    <div className="bg-bg-surface text-text-dark border border-border-light  overflow-hidden">
      {title && (
        <div className="flex items-center justify-between px-4 py-3 border-b border-border-light">
          <span className="font-mono text-small font-bold uppercase tracking-wider">{title}</span>
          <button
            onClick={handleCopy}
            className="p-1.5 hover:bg-gray-200  transition-colors"
          >
            {copied ? <Check size={14} className="text-accent-lime-muted" /> : <Copy size={14} />}
          </button>
        </div>
      )}
      <div className="bg-bg-primary text-text-primary p-4 overflow-x-auto">
        <pre className="font-mono text-small leading-relaxed">
          {lines.map((line, i) => (
            <div key={i} className="flex">
              <span className="text-text-secondary w-8 text-right mr-4 select-none">{i + 1}</span>
              <span>{highlightLine(line)}</span>
            </div>
          ))}
        </pre>
      </div>
    </div>
  )
}
