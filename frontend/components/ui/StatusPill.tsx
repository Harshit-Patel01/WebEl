'use client'

interface StatusPillProps {
  status: 'healthy' | 'error' | 'warning' | 'inactive'
  label: string
  size?: 'sm' | 'md'
}

export default function StatusPill({ status, label, size = 'md' }: StatusPillProps) {
  const dotColor = {
    healthy: 'bg-status-success',
    error: 'bg-status-error',
    warning: 'bg-status-warning',
    inactive: 'bg-text-secondary',
  }[status]

  const textSize = size === 'sm' ? 'text-[11px]' : 'text-small'

  return (
    <span className="inline-flex items-center gap-2 font-mono">
      <span className={`relative w-2 h-2  ${dotColor} ${status === 'healthy' ? 'pulse-dot' : ''}`}>
        <span className={`block w-2 h-2  ${dotColor}`} />
      </span>
      <span className={`${textSize} uppercase tracking-wider font-bold`}>
        {label}
      </span>
    </span>
  )
}
