'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useState, useEffect } from 'react'
import {
  LayoutDashboard, Wifi, Globe, Shield, GitBranch,
  Server, Activity, Terminal, Settings, HelpCircle,
  ChevronLeft, ChevronRight, X, Menu, Package,
} from 'lucide-react'
import StatusPill from '@/components/ui/StatusPill'

const iconMap: Record<string, React.ElementType> = {
  LayoutDashboard, Wifi, Globe, Shield, GitBranch,
  Server, Activity, Terminal, Settings, HelpCircle, Package,
}

const mainNav = [
  { label: 'Overview', path: '/dashboard', icon: 'LayoutDashboard' },
  { label: 'WiFi Setup', path: '/wifi', icon: 'Wifi', complete: false},
  { label: 'Internet', path: '/internet', icon: 'Globe', complete: false},
  { label: 'Cloudflare Tunnel', path: '/tunnel/dashboard', icon: 'Shield' },
  { label: 'GitHub Deploy', path: '/deploy', icon: 'GitBranch' },
  { label: 'Deployments', path: '/deployments', icon: 'Package' },
  { label: 'Nginx Config', path: '/nginx', icon: 'Server' },
  { label: 'Logs', path: '/logs', icon: 'Terminal' },
]

const bottomNav = [
  { label: 'Settings', path: '/settings', icon: 'Settings' },
  { label: 'Help', path: '/help', icon: 'HelpCircle' },
]

interface SidebarProps {
  mobileOpen: boolean
  setMobileOpen: (open: boolean) => void
}

export default function Sidebar({ mobileOpen, setMobileOpen }: SidebarProps) {
  const pathname = usePathname()
  const [expanded, setExpanded] = useState(true)

  // Load expanded state from localStorage
  useEffect(() => {
    const saved = localStorage.getItem('sidebar-expanded')
    if (saved !== null) {
      setExpanded(saved === 'true')
    }
  }, [])

  // Save expanded state to localStorage
  const toggleExpanded = () => {
    const newState = !expanded
    setExpanded(newState)
    localStorage.setItem('sidebar-expanded', String(newState))

    // Dispatch custom event to notify layout
    window.dispatchEvent(new CustomEvent('sidebar-toggle', { detail: { expanded: newState } }))
  }

  // Close mobile drawer when route changes
  useEffect(() => {
    setMobileOpen(false)
  }, [pathname])

  return (
    <>
      {/* Mobile overlay */}
      {mobileOpen && (
        <div
          className="lg:hidden fixed inset-0 bg-black/60 z-40"
          onClick={() => setMobileOpen(false)}
        />
      )}

      {/* Sidebar */}
      <aside
        className={`
          fixed left-0 top-0 h-screen bg-bg-secondary border-r border-border-dark
          flex flex-col z-50 transition-all duration-300
          ${expanded ? 'w-[240px]' : 'w-16'}
          ${mobileOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0'}
        `}
      >
        {/* Logo */}
        <div className="px-4 py-5 flex items-center gap-3 border-b border-border-dark">
          <img
            src="/favicon.svg"
            alt="OpenDeploy Logo"
            className="w-8 h-8 flex-shrink-0"
          />
          {expanded && (
            <span className="font-serif font-bold text-xl tracking-tight">OpenDeploy</span>
          )}
          {/* Mobile close button */}
          <button
            onClick={() => setMobileOpen(false)}
            className="lg:hidden ml-auto p-1 text-text-secondary hover:text-text-primary"
            aria-label="Close menu"
          >
            <X size={20} />
          </button>
        </div>

        {/* Main nav */}
        <nav className="flex-1 py-4 overflow-y-auto">
          <div className="space-y-1 px-2">
            {mainNav.map(item => {
              const Icon = iconMap[item.icon]
              const isActive = pathname === item.path
              return (
                <Link
                  key={item.label}
                  href={item.path}
                  className={`
                    flex items-center gap-3 px-3 py-2.5  transition-colors
                    min-h-[44px] lg:min-h-[40px]
                    ${isActive
                      ? 'bg-bg-primary text-accent-lime'
                      : 'text-text-secondary hover:text-text-primary hover:bg-bg-primary/50'
                    }
                  `}
                  title={!expanded ? item.label : undefined}
                >
                  <span className="flex items-center justify-center w-[18px] h-[18px] flex-shrink-0">
                    <Icon size={18} className="flex-shrink-0" />
                  </span>
                  {expanded && (
                    <span className="font-mono text-small flex-1 whitespace-nowrap overflow-hidden">{item.label}</span>
                  )}
                  {expanded && item.complete && (
                    <span className="w-2 h-2  bg-accent-lime flex-shrink-0" />
                  )}
                </Link>
              )
            })}
          </div>
        </nav>

        {/* Bottom nav */}
        <div className="border-t border-border-dark py-4 px-2 space-y-1">
          {bottomNav.map(item => {
            const Icon = iconMap[item.icon]
            const isActive = pathname === item.path
            return (
              <Link
                key={item.label}
                href={item.path}
                className={`
                  flex items-center gap-3 px-3 py-2.5  transition-colors
                  min-h-[44px] lg:min-h-[40px]
                  ${isActive
                    ? 'bg-bg-primary text-accent-lime'
                    : 'text-text-secondary hover:text-text-primary hover:bg-bg-primary/50'
                  }
                `}
                title={!expanded ? item.label : undefined}
              >
                <span className="flex items-center justify-center w-[18px] h-[18px] flex-shrink-0">
                  <Icon size={18} className="flex-shrink-0" />
                </span>
                {expanded && (
                  <span className="font-mono text-small whitespace-nowrap overflow-hidden">{item.label}</span>
                )}
              </Link>
            )
          })}
        </div>

        {/* Collapse toggle - hidden on mobile */}
        <button
          onClick={toggleExpanded}
          className="hidden lg:flex p-3 border-t border-border-dark text-text-secondary hover:text-text-primary transition-colors items-center justify-center"
          aria-label={expanded ? 'Collapse sidebar' : 'Expand sidebar'}
        >
          {expanded ? <ChevronLeft size={18} /> : <ChevronRight size={18} />}
        </button>
      </aside>
    </>
  )
}
