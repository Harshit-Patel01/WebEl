'use client'

import StatusPill from './StatusPill'

interface ServiceCardProps {
  name: string
  status: 'healthy' | 'error' | 'warning' | 'inactive'
  statusLabel: string
  detail: string
  variant: 'dark' | 'light'
  onAction?: () => void
}

export default function ServiceCard({ name, status, statusLabel, detail, variant, onAction }: ServiceCardProps) {
  const isDark = variant === 'dark'

  return (
    <div
      className={`
         p-6 transition-all duration-200 cursor-pointer
        hover:-translate-y-1 hover:shadow-xl
        ${isDark
          ? 'bg-bg-secondary border border-border-dark text-text-primary'
          : 'bg-bg-surface border border-border-light text-text-dark'
        }
      `}
      onClick={onAction}
    >
      <div className="flex items-start justify-between mb-4">
        <h3 className="font-serif text-h3">{name}</h3>
        <StatusPill status={status} label={statusLabel} size="sm" />
      </div>
      <p className={`font-mono text-small ${isDark ? 'text-text-secondary' : 'text-text-secondary'}`}>
        {detail}
      </p>
      <button
        className={`
          mt-4 font-mono text-small uppercase tracking-wider
          ${isDark ? 'text-accent-lime hover:text-accent-lime-muted' : 'text-text-dark hover:text-text-secondary'}
          transition-colors
        `}
      >
        Manage &nearr;
      </button>
    </div>
  )
}
