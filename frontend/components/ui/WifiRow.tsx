'use client'

import { Lock, Unlock, Check } from 'lucide-react'

interface WifiRowProps {
  ssid: string
  signal: number
  secured: boolean
  saved?: boolean
  selected?: boolean
  onClick?: () => void
}

export default function WifiRow({ ssid, signal, secured, saved = false, selected = false, onClick }: WifiRowProps) {
  return (
    <div
      className={`
        flex items-center justify-between px-4 py-3 cursor-pointer
        transition-all duration-150 group
        ${selected
          ? 'bg-accent-lime/10 border-l-4 border-l-accent-lime'
          : 'border-l-4 border-l-transparent hover:bg-bg-primary'
        }
      `}
      onClick={onClick}
    >
      <div className="flex items-center gap-3">
        {/* Signal bars */}
        <div className="flex items-end gap-0.5 h-4">
          {[1, 2, 3, 4].map(level => (
            <div
              key={level}
              className={`signal-bar ${signal >= level ? 'active' : ''}`}
              style={{ height: `${level * 4 + 2}px` }}
            />
          ))}
        </div>
        <span className="font-mono text-small text-text-dark font-medium group-hover:text-accent-lime-muted transition-colors">{ssid}</span>
        {saved && (
          <Check size={14} className="text-accent-lime-muted" aria-label="Saved network" />
        )}
      </div>

      <div className="flex items-center gap-3">
        {secured ? (
          <Lock size={14} className="text-text-secondary" />
        ) : (
          <Unlock size={14} className="text-text-secondary" />
        )}
        <button
          className={`
            font-mono text-label uppercase tracking-wider px-3 py-1 rounded-full
            transition-all opacity-0 group-hover:opacity-100
            ${selected ? 'opacity-100' : ''}
            bg-accent-lime text-text-dark hover:bg-accent-lime-muted
          `}
        >
          Connect &rarr;
        </button>
      </div>
    </div>
  )
}
