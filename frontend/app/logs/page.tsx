'use client'

import { useState, useEffect, useCallback, useRef } from 'react'
import { RefreshCw, AlertCircle, AlertTriangle, CheckCircle2, Info, Terminal, Download, ArrowDown } from 'lucide-react'
import { eventsApi } from '@/lib/api'

type Level = 'error' | 'warning' | 'success' | 'info'
type Source = 'ALL' | 'NGINX' | 'APP' | 'CLOUDFLARED'

interface LogEvent {
  id: string
  ts: string
  level: Level
  source: string
  event: string
  message: string
  summary?: string
}

const LEVELS: Level[] = ['error', 'warning', 'success', 'info']
const SOURCES: Source[] = ['ALL', 'NGINX', 'APP', 'CLOUDFLARED']

// Server-side time ranges, in minutes. 0 means all time.
const RANGES: { label: string; minutes: number }[] = [
  { label: 'All', minutes: 0 },
  { label: '10M', minutes: 10 },
  { label: '1H', minutes: 60 },
  { label: '24H', minutes: 60 * 24 },
]

// Colour + icon per level. No background box or ring — just the glyph.
const LEVEL_STYLES: Record<Level, { icon: typeof Info; color: string }> = {
  error: { icon: AlertCircle, color: 'text-status-error' },
  warning: { icon: AlertTriangle, color: 'text-status-warning' },
  success: { icon: CheckCircle2, color: 'text-status-success' },
  info: { icon: Info, color: 'text-text-secondary' },
}

// One flat button style so all three filter groups read as a single control.
function filterBtn(active: boolean, inactive = '') {
  return `px-2.5 py-1 font-mono text-[11px] uppercase tracking-wider transition-colors ${
    active ? 'bg-accent-lime text-text-dark font-bold' : `${inactive} hover:text-text-primary`
  }`
}

export default function LogsPage() {
  const [events, setEvents] = useState<LogEvent[]>([])
  const [source, setSource] = useState<Source>('ALL')
  const [level, setLevel] = useState<Level | 'ALL'>('ALL')
  const [range, setRange] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showJump, setShowJump] = useState(false)
  const [fetchedAt, setFetchedAt] = useState<Date | null>(null)

  const listRef = useRef<HTMLUListElement>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await eventsApi.getEvents(500, range || undefined)
      setEvents(res.events || [])
      setFetchedAt(new Date())
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load events')
    } finally {
      setLoading(false)
    }
  }, [range])

  // Fetch once on mount and whenever the range changes. Deliberately no timer:
  // polling would spam the host with journalctl work on a Pi, and the Refresh
  // button is the explicit "get me what is there right now" action.
  useEffect(() => { load() }, [load])

  // Counts are computed within the already range-filtered set, so each button
  // shows how many events it would yield. This replaces the old stat tiles.
  const countFor = (group: 'source' | 'level', key: string) => {
    if (key === 'ALL') return events.length
    return events.filter(e => (group === 'source' ? e.source : e.level) === key).length
  }

  const visible = events.filter(e =>
    (source === 'ALL' || e.source === source) && (level === 'ALL' || e.level === level)
  )

  // Only offer the jump button when there is actually content below the fold.
  const onScroll = () => {
    const el = listRef.current
    if (!el) return
    setShowJump(el.scrollTop < el.scrollHeight - el.clientHeight - 24)
  }

  const jumpToBottom = () => {
    const el = listRef.current
    if (el) el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
  }

  const download = () => {
    // The backend renders the file so the export matches the server's range
    // filter exactly, rather than re-deriving it from what the page happens to
    // hold. It uses fetch rather than a bare <a> so the session cookie rides
    // along; without it the protected route would reject the download.
    const p = new URLSearchParams()
    p.set('lines', '1000')
    if (range) p.set('since', String(range))
    if (source !== 'ALL') p.set('source', source)

    fetch(`${eventsApi.eventsUrl()}?${p.toString()}`, { credentials: 'include' })
      .then(r => {
        if (!r.ok) throw new Error(`Download failed (${r.status})`)
        return r.blob()
      })
      .then(blob => {
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = `opendeploy-logs-${source.toLowerCase()}-${range ? `last${range}m` : 'all'}.log`
        a.click()
        URL.revokeObjectURL(url)
      })
      .catch(e => setError(e instanceof Error ? e.message : 'Download failed'))
  }

  // Height budget: viewport minus TopStatusBar (h-12 = 3rem) minus the layout
  // <main> padding (p-4 / md:p-6 / lg:p-8) on both top and bottom.
  return (
    <main className="flex flex-col h-[calc(100vh-5rem)] md:h-[calc(100vh-6rem)] lg:h-[calc(100vh-7rem)] min-h-[360px]">
      {/* Header — stays fixed above the scroll area */}
      <header className="flex items-center justify-between gap-3 mb-3 shrink-0">
        <div className="min-w-0">
          <h1 className="font-serif text-h3 text-text-primary">Logs</h1>
        </div>
        <div className="flex gap-2 shrink-0">
          <button
            onClick={download}
            disabled={visible.length === 0}
            className="flex items-center justify-center gap-1.5 px-3 py-1.5 border border-border-dark font-mono text-[11px] uppercase tracking-wider text-text-secondary hover:text-text-primary hover:border-accent-lime transition-colors disabled:opacity-50"
          >
            <Download size={13} />
            Download
          </button>
          <button
            onClick={load}
            disabled={loading}
            className="flex items-center justify-center gap-1.5 px-3 py-1.5 border border-border-dark font-mono text-[11px] uppercase tracking-wider text-text-secondary hover:text-text-primary hover:border-accent-lime transition-colors disabled:opacity-50"
          >
            <RefreshCw size={13} className={loading ? 'animate-spin' : ''} />
            Refresh
          </button>
        </div>
      </header>

      {/* All three filters in one control bar, divided by hairlines */}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 mb-3 shrink-0 bg-bg-secondary border border-border-dark p-1">
        {/* Time range */}
        <div className="flex items-center gap-1">
          <span className="font-mono text-[10px] uppercase tracking-wider text-text-secondary opacity-60 pr-1">
            Time
          </span>
          {RANGES.map(r => (
            <button
              key={r.minutes}
              onClick={() => setRange(r.minutes)}
              className={filterBtn(range === r.minutes, 'text-text-secondary')}
            >
              {r.label}
            </button>
          ))}
        </div>

        <span className="w-px h-4 bg-border-dark" />

        {/* Source */}
        <div className="flex items-center gap-1">
          <span className="font-mono text-[10px] uppercase tracking-wider text-text-secondary opacity-60 pr-1">
            Src
          </span>
          {SOURCES.map(s => (
            <button
              key={s}
              onClick={() => setSource(s)}
              className={filterBtn(source === s, 'text-text-secondary')}
            >
              {s}
              <span className="ml-1.5 opacity-60">{countFor('source', s)}</span>
            </button>
          ))}
        </div>

        <span className="w-px h-4 bg-border-dark" />

        {/* Level */}
        <div className="flex items-center gap-1">
          <span className="font-mono text-[10px] uppercase tracking-wider text-text-secondary opacity-60 pr-1">
            Lvl
          </span>
          <button
            onClick={() => setLevel('ALL')}
            className={filterBtn(level === 'ALL', 'text-text-secondary')}
          >
            All
            <span className="ml-1.5 opacity-60">{countFor('level', 'ALL')}</span>
          </button>
          {LEVELS.map(l => {
            const Icon = LEVEL_STYLES[l].icon
            return (
              <button
                key={l}
                onClick={() => setLevel(l)}
                className={`flex items-center gap-1 ${filterBtn(level === l, LEVEL_STYLES[l].color)}`}
              >
                <Icon size={11} />
                {l}
                <span className="ml-1 opacity-60">{countFor('level', l)}</span>
              </button>
            )
          })}
        </div>
      </div>

      {error && (
        <div className="mb-2 border border-status-error px-3 py-2 font-mono text-[11px] text-status-error shrink-0">
          {error}
        </div>
      )}

      {/* The only scrollable region */}
      <div className="relative flex-1 min-h-0">
        {loading && events.length === 0 ? (
          <div className="h-full bg-bg-secondary border border-border-dark p-10 text-center font-mono text-small text-text-secondary">
            <Terminal size={22} className="mx-auto mb-2 animate-pulse" />
            Loading events…
          </div>
        ) : visible.length === 0 ? (
          <div className="h-full bg-bg-secondary border border-border-dark p-10 text-center font-mono text-small text-text-secondary">
            No events match this filter.
          </div>
        ) : (
          <ul
            ref={listRef}
            onScroll={onScroll}
            className="h-full overflow-y-auto bg-bg-secondary border border-border-dark divide-y divide-border-dark"
          >
            {visible.map(e => {
              const style = LEVEL_STYLES[e.level] || LEVEL_STYLES.info
              const Icon = style.icon
              return (
                <li key={e.id} className="flex items-start gap-3 px-3 py-1.5 hover:bg-bg-primary transition-colors">
                  <Icon size={13} className={`${style.color} shrink-0 mt-0.5`} />
                  <time className="font-mono text-[11px] text-text-secondary whitespace-nowrap shrink-0 w-40">
                    {new Date(e.ts).toLocaleString()}
                  </time>
                  <span className="font-mono text-[11px] text-accent-lime uppercase shrink-0 w-24 truncate">
                    {e.source}
                  </span>
                  <span className="font-mono text-[11px] text-text-secondary uppercase shrink-0 w-28 truncate">
                    {e.event}
                  </span>
                  <span className="font-mono text-small text-text-primary break-all min-w-0 flex-1">
                    {e.message}
                  </span>
                </li>
              )
            })}
          </ul>
        )}

        {/* Jump to oldest — only when scrolled up */}
        {showJump && (
          <button
            onClick={jumpToBottom}
            title="Jump to oldest entries"
            aria-label="Scroll to bottom"
            className="absolute bottom-4 right-4 flex items-center justify-center w-10 h-10 bg-accent-lime text-text-dark font-mono font-bold hover:bg-accent-lime-muted transition-all shadow-lg"
          >
            <ArrowDown size={18} />
          </button>
        )}
      </div>

      <p className="mt-2 font-mono text-[11px] text-text-secondary shrink-0">
        Showing {visible.length} of {events.length} events · newest first
        {fetchedAt && ` · fetched ${fetchedAt.toLocaleTimeString()}`}
      </p>
    </main>
  )
}