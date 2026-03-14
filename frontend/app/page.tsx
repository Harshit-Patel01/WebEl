'use client'

import { motion } from 'framer-motion'
import Link from 'next/link'

export default function WelcomePage() {
  return (
    <div className="min-h-screen bg-bg-primary flex flex-col items-center justify-center relative overflow-hidden dot-pattern">
      <div className="max-w-3xl mx-auto text-center px-6 relative z-10">
        {/* Logo */}
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6 }}
          className="mb-12"
        >
          <div className="inline-flex items-center gap-3">
            <img
              src="/favicon.svg"
              alt="OpenDeploy Logo"
              className="w-12 h-12"
            />
            <span className="font-serif font-bold text-3xl tracking-tight text-text-primary">OpenDeploy</span>
          </div>
        </motion.div>

        {/* Headline */}
        <motion.h1
          initial={{ opacity: 0, y: 30 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.8, delay: 0.3 }}
          className="font-serif font-bold text-display-sm md:text-display text-text-primary leading-none mb-8"
        >
          Your Server.{' '}
          <span className="text-accent-lime">Your Cloud.</span>
        </motion.h1>

        {/* Subtext */}
        <motion.p
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6, delay: 0.8 }}
          className="font-sans text-body md:text-lg text-text-secondary max-w-xl mx-auto mb-12 leading-relaxed"
        >
          OpenDeploy turns your Linux device into a self-hosted deployment platform.
          No terminal. No complexity. Just deploy.
        </motion.p>

        {/* CTA Button */}
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6, delay: 1.2 }}
        >
          <Link
            href="/wifi"
            className="inline-flex items-center gap-2 px-8 py-4 bg-accent-lime text-text-dark font-mono font-bold text-body uppercase tracking-wider  hover:bg-accent-lime-muted transition-all hover:shadow-[0_0_30px_rgba(170,255,69,0.3)] active:scale-95"
          >
            Begin Setup <span className="text-xl">&rarr;</span>
          </Link>
        </motion.div>
      </div>

      {/* Decorative grid lines */}
      <div className="absolute inset-0 pointer-events-none">
        <div className="absolute top-1/4 left-0 right-0 h-px bg-border-dark opacity-20" />
        <div className="absolute top-3/4 left-0 right-0 h-px bg-border-dark opacity-20" />
        <div className="absolute top-0 bottom-0 left-1/4 w-px bg-border-dark opacity-20" />
        <div className="absolute top-0 bottom-0 right-1/4 w-px bg-border-dark opacity-20" />
      </div>
    </div>
  )
}
