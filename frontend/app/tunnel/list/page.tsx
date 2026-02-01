'use client'

import { useState, useEffect } from 'react'
import { motion } from 'framer-motion'
import { RefreshCw, Trash2, ExternalLink, AlertCircle, CheckCircle2, Loader2, Key } from 'lucide-react'
import Link from 'next/link'
import SectionBadge from '@/components/ui/SectionBadge'
import StatusPill from '@/components/ui/StatusPill'
import { tunnelApi } from '@/lib/api'
import { apiKeyStorage } from '@/utils/apiKey'

interface TunnelWithDomains {
  id: string
  name: string
  status: string
  created_at: string
  domains: string[]
  account_id: string
  is_managed: boolean
}

export default function TunnelListPage() {
  const [tunnels, setTunnels] = useState<TunnelWithDomains[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [deletingId, setDeletingId] = useState<string | null>(null)

  const loadTunnels = async () => {
    try {
      setLoading(true)
      setError('')
      const key = apiKeyStorage.get()
      if (!key) {
        setError('API key not found. Please set up tunnel first.')
        setLoading(false)
        return
      }
      const data = await tunnelApi.listAll(key)
      setTunnels(data)
    } catch (err) {
      setError(String(err))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadTunnels()
  }, [])

  const handleDelete = async (tunnel: TunnelWithDomains) => {
    if (!confirm(`Are you sure you want to delete tunnel "${tunnel.name}"? This will remove it from Cloudflare.`)) {
      return
    }

    const key = apiKeyStorage.get()
    if (!key) {
      alert('API key not found. Please set up tunnel first.')
      return
    }

    setDeletingId(tunnel.id)
    try {
      await tunnelApi.stopRemote(key, tunnel.account_id, tunnel.id)
      await loadTunnels() // Refresh list
    } catch (err) {
      alert(`Failed to delete tunnel: ${err}`)
    } finally {
      setDeletingId(null)
    }
  }

  if (loading) {
    return (
      <motion.div
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        className="flex items-center justify-center h-64"
      >
        <Loader2 size={48} className="animate-spin text-accent-lime" />
      </motion.div>
    )
  }

  return (
    <motion.div
      initial={{ opacity: 0, x: 20 }}
      animate={{ opacity: 1, x: 0 }}
      transition={{ duration: 0.3 }}
    >
      <div className="mb-8 flex items-center justify-between">
        <SectionBadge label="ALL TUNNELS" />
        <div className="flex gap-3">
          <button
            onClick={loadTunnels}
            className="inline-flex items-center gap-2 px-4 py-2 bg-bg-secondary border border-border-dark rounded-lg font-mono text-small text-text-primary hover:bg-border-dark transition-all"
          >
            <RefreshCw size={16} /> Refresh
          </button>
          <Link
            href="/tunnel/dashboard"
            className="inline-flex items-center gap-2 px-4 py-2 bg-bg-secondary border border-border-dark rounded-lg font-mono text-small text-text-primary hover:bg-border-dark transition-all"
          >
            ← Back to Dashboard
          </Link>
        </div>
      </div>

      {error && (
        <div className="mb-6 p-4 bg-red-900/20 border border-red-700 rounded-lg flex items-start gap-3">
          <AlertCircle size={20} className="text-red-500 flex-shrink-0 mt-0.5" />
          <div>
            <p className="font-mono text-small text-red-400">{error}</p>
            <Link
              href="/tunnel"
              className="mt-2 inline-block font-mono text-small text-accent-lime hover:underline"
            >
              Set up a tunnel first →
            </Link>
          </div>
        </div>
      )}

      {tunnels.length === 0 && !error && (
        <div className="bg-bg-secondary rounded-card border border-border-dark p-12 text-center">
          <p className="font-mono text-text-secondary mb-4">No tunnels found in your Cloudflare account.</p>
          <Link
            href="/tunnel"
            className="inline-flex items-center gap-2 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all"
          >
            Create Tunnel →
          </Link>
        </div>
      )}

      {tunnels.length > 0 && (
        <div className="space-y-4">
          {tunnels.map((tunnel) => (
            <motion.div
              key={tunnel.id}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              className="bg-bg-secondary rounded-card border border-border-dark p-6 hover:border-accent-lime/30 transition-colors"
            >
              <div className="flex items-start justify-between mb-4">
                <div className="flex-1">
                  <div className="flex items-center gap-3 mb-2">
                    <h3 className="font-serif text-h3">{tunnel.name}</h3>
                    {tunnel.is_managed && (
                      <span className="px-2 py-1 bg-accent-lime/20 border border-accent-lime/50 rounded text-[10px] font-mono text-accent-lime uppercase">
                        Managed by OpenDeploy
                      </span>
                    )}
                    <StatusPill
                      status={tunnel.status === 'active' ? 'healthy' : 'inactive'}
                      label={tunnel.status}
                      size="sm"
                    />
                  </div>
                  <p className="font-mono text-small text-text-secondary">
                    ID: <span className="text-text-primary">{tunnel.id}</span>
                  </p>
                  <p className="font-mono text-small text-text-secondary">
                    Created: <span className="text-text-primary">{tunnel.created_at}</span>
                  </p>
                </div>

                <button
                  onClick={() => handleDelete(tunnel)}
                  disabled={deletingId === tunnel.id}
                  className="p-2 text-text-secondary hover:text-status-error transition-colors rounded hover:bg-bg-primary disabled:opacity-50"
                  title="Delete tunnel"
                >
                  {deletingId === tunnel.id ? (
                    <Loader2 size={18} className="animate-spin" />
                  ) : (
                    <Trash2 size={18} />
                  )}
                </button>
              </div>

              {tunnel.domains.length > 0 ? (
                <div className="mt-4 pt-4 border-t border-border-dark">
                  <p className="font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                    Connected Domains ({tunnel.domains.length})
                  </p>
                  <div className="space-y-2">
                    {tunnel.domains.map((domain) => (
                      <div
                        key={domain}
                        className="flex items-center gap-2 font-mono text-small text-text-primary"
                      >
                        <CheckCircle2 size={14} className="text-accent-lime" />
                        <a
                          href={`https://${domain}`}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="text-accent-lime hover:underline"
                        >
                          {domain}
                        </a>
                        <ExternalLink size={12} className="text-text-secondary" />
                      </div>
                    ))}
                  </div>
                </div>
              ) : (
                <div className="mt-4 pt-4 border-t border-border-dark">
                  <p className="font-mono text-small text-text-secondary italic">
                    No domains connected to this tunnel
                  </p>
                </div>
              )}
            </motion.div>
          ))}
        </div>
      )}

      <div className="mt-8 flex gap-3">
        <Link
          href="/tunnel/dashboard"
          className="inline-flex items-center gap-2 px-4 py-2 bg-bg-secondary border border-border-dark rounded-lg font-mono text-small text-text-primary hover:bg-border-dark transition-all"
        >
          ← Back to Tunnel Dashboard
        </Link>
        <Link
          href="/tunnel"
          className="inline-flex items-center gap-2 px-4 py-2 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all"
        >
          Create New Tunnel
        </Link>
      </div>
    </motion.div>
  )
}
