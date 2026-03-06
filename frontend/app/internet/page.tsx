'use client'

import { useState, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { CheckCircle2, Loader2 } from 'lucide-react'
import Link from 'next/link'
import SectionBadge from '@/components/ui/SectionBadge'
import { internetApi } from '@/lib/api'

type CheckState = 'checking' | 'done' | 'error'

interface CheckResult {
  label: string
  value: string
  success: boolean
}

export default function InternetPage() {
  const [state, setState] = useState<CheckState>('checking')
  const [currentCheck, setCurrentCheck] = useState(0)
  const [checks, setChecks] = useState<CheckResult[]>([
    { label: 'DNS Resolution', value: '...', success: false },
    { label: 'Cloudflare Ping', value: '...', success: false },
    { label: 'Download Speed', value: '...', success: false },
  ])

  const statusMessages = [
    'Checking DNS...',
    'Pinging Cloudflare...',
    'Speed test...',
    'Internet Active',
  ]

  useEffect(() => {
    runChecks()
  }, [])

  const runChecks = async () => {
    setState('checking')

    try {
      // Run DNS check
      setCurrentCheck(0)
      await new Promise(resolve => setTimeout(resolve, 500))

      // Run all checks
      const results = await internetApi.runChecks()

      const newChecks: CheckResult[] = [
        {
          label: 'DNS Resolution',
          value: results.dns_resolution.value,
          success: results.dns_resolution.success,
        },
        {
          label: 'Cloudflare Ping',
          value: results.cloudflare_ping.value,
          success: results.cloudflare_ping.success,
        },
        {
          label: 'Download Speed',
          value: results.download_speed.value,
          success: results.download_speed.success,
        },
      ]

      // Animate through checks
      for (let i = 0; i < newChecks.length; i++) {
        setCurrentCheck(i)
        setChecks(prev => {
          const updated = [...prev]
          updated[i] = newChecks[i]
          return updated
        })
        await new Promise(resolve => setTimeout(resolve, 800))
      }

      setCurrentCheck(3)
      setState('done')
    } catch (err) {
      console.error('Internet check failed:', err)
      setState('error')
    }
  }

  return (
    <>
      <motion.div
        initial={{ opacity: 0, x: 20 }}
        animate={{ opacity: 1, x: 0 }}
        transition={{ duration: 0.3 }}
      >
        <div className="mb-8">
          <SectionBadge label="02 — INTERNET" />
        </div>

        <div className="max-w-lg mx-auto">
          <div className="bg-bg-secondary rounded-card border border-border-dark p-8 text-center">
            {/* Animated circles */}
            <div className="relative w-32 h-32 mx-auto mb-8">
              {[1, 2, 3].map(ring => (
                <motion.div
                  key={ring}
                  className="absolute inset-0 rounded-full border border-accent-lime"
                  initial={{ scale: 0.5, opacity: 0.8 }}
                  animate={{
                    scale: [0.5, 1 + ring * 0.3],
                    opacity: [0.6, 0],
                  }}
                  transition={{
                    duration: 2,
                    repeat: state === 'checking' ? Infinity : 0,
                    delay: ring * 0.4,
                    ease: 'easeOut',
                  }}
                />
              ))}
              <div className="absolute inset-0 flex items-center justify-center">
                {state === 'done' ? (
                  <motion.div
                    initial={{ scale: 0 }}
                    animate={{ scale: 1 }}
                    transition={{ type: 'spring', stiffness: 200 }}
                  >
                    <CheckCircle2 size={48} className="text-accent-lime" />
                  </motion.div>
                ) : (
                  <div className="w-4 h-4 rounded-full bg-accent-lime animate-pulse" />
                )}
              </div>
            </div>

            {/* Status message */}
            <AnimatePresence mode="wait">
              <motion.p
                key={currentCheck}
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: -10 }}
                className="font-mono text-body text-text-primary mb-8"
              >
                {state === 'done' ? (
                  <span className="text-accent-lime font-bold">&check; Internet Active</span>
                ) : (
                  statusMessages[currentCheck]
                )}
              </motion.p>
            </AnimatePresence>

            {/* Results readout */}
            <div className="bg-bg-primary rounded-lg p-4 text-left font-mono text-small space-y-2">
              {checks.map((check, i) => (
                <motion.div
                  key={check.label}
                  initial={{ opacity: 0, x: -10 }}
                  animate={{
                    opacity: check.success || currentCheck > i ? 1 : 0.3,
                    x: 0,
                  }}
                  transition={{ delay: i * 0.2 }}
                  className="flex items-center justify-between"
                >
                  <span className="text-text-secondary">{check.label}</span>
                  <span className="flex items-center gap-2">
                    {check.success && (
                      <span className="text-accent-lime">&check;</span>
                    )}
                    {!check.success && currentCheck > i && (
                      <span className="text-status-error">&times;</span>
                    )}
                    <span className={check.success ? "text-text-primary" : "text-text-secondary"}>
                      {check.value}
                    </span>
                  </span>
                </motion.div>
              ))}
            </div>

            {/* Continue button */}
            {state === 'done' && (
              <motion.div
                initial={{ opacity: 0, y: 20 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: 0.3 }}
              >
                <Link
                  href="/tunnel/dashboard"
                  className="inline-flex items-center gap-2 mt-8 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-full hover:bg-accent-lime-muted transition-all hover:shadow-[0_0_20px_rgba(170,255,69,0.3)]"
                >
                  Proceed to Cloudflare Tunnel Setup &rarr;
                </Link>
              </motion.div>
            )}

            {/* Retry button on error */}
            {state === 'error' && (
              <motion.div
                initial={{ opacity: 0, y: 20 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: 0.3 }}
              >
                <button
                  onClick={runChecks}
                  className="inline-flex items-center gap-2 mt-8 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-full hover:bg-accent-lime-muted transition-all"
                >
                  Retry Checks
                </button>
              </motion.div>
            )}
          </div>
        </div>
      </motion.div>
    </>
  )
}
