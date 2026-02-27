'use client'

import './globals.css'
import { WebSocketProvider } from '@/contexts/WebSocketContext'
import { SystemProvider } from '@/contexts/SystemContext'
import Sidebar from '@/components/layout/Sidebar'
import TopStatusBar from '@/components/layout/TopStatusBar'
import { usePathname } from 'next/navigation'
import { useEffect, useState } from 'react'

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const pathname = usePathname()
  const isWelcomePage = pathname === '/'
  const [mobileOpen, setMobileOpen] = useState(false)
  const [sidebarExpanded, setSidebarExpanded] = useState(true)

  // Load sidebar state from localStorage
  useEffect(() => {
    const saved = localStorage.getItem('sidebar-expanded')
    if (saved !== null) {
      setSidebarExpanded(saved === 'true')
    }
  }, [])

  // Listen for sidebar state changes
  useEffect(() => {
    const handleStorageChange = () => {
      const saved = localStorage.getItem('sidebar-expanded')
      if (saved !== null) {
        setSidebarExpanded(saved === 'true')
      }
    }

    window.addEventListener('storage', handleStorageChange)

    // Also listen for custom event from sidebar
    const handleSidebarToggle = (e: CustomEvent) => {
      setSidebarExpanded(e.detail.expanded)
    }

    window.addEventListener('sidebar-toggle' as any, handleSidebarToggle as any)

    return () => {
      window.removeEventListener('storage', handleStorageChange)
      window.removeEventListener('sidebar-toggle' as any, handleSidebarToggle as any)
    }
  }, [])

  // Initialize theme before render
  useEffect(() => {
    const savedTheme = localStorage.getItem('theme') as 'dark' | 'light' | 'auto' | null
    const theme = savedTheme || 'dark'

    if (theme === 'auto') {
      const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
      document.documentElement.setAttribute('data-theme', prefersDark ? 'dark' : 'light')
    } else {
      document.documentElement.setAttribute('data-theme', theme)
    }

    // Listen for system theme changes when in auto mode
    if (theme === 'auto') {
      const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
      const handleChange = (e: MediaQueryListEvent) => {
        document.documentElement.setAttribute('data-theme', e.matches ? 'dark' : 'light')
      }
      mediaQuery.addEventListener('change', handleChange)
      return () => mediaQuery.removeEventListener('change', handleChange)
    }
  }, [])

  // Set document title
  useEffect(() => {
    document.title = 'OpenDeploy — Dashboard'
  }, [])

  if (isWelcomePage) {
    return (
      <html lang="en" data-theme="dark" suppressHydrationWarning>
        <head>
          <script
            dangerouslySetInnerHTML={{
              __html: `
                (function() {
                  const theme = localStorage.getItem('theme') || 'dark';
                  if (theme === 'auto') {
                    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
                    document.documentElement.setAttribute('data-theme', prefersDark ? 'dark' : 'light');
                  } else {
                    document.documentElement.setAttribute('data-theme', theme);
                  }
                })();
              `,
            }}
          />
        </head>
        <body className="bg-bg-primary text-text-primary antialiased overflow-x-hidden">
          {children}
        </body>
      </html>
    )
  }

  return (
    <html lang="en" data-theme="dark" suppressHydrationWarning>
      <head>
        <script
          dangerouslySetInnerHTML={{
            __html: `
              (function() {
                const theme = localStorage.getItem('theme') || 'dark';
                if (theme === 'auto') {
                  const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
                  document.documentElement.setAttribute('data-theme', prefersDark ? 'dark' : 'light');
                } else {
                  document.documentElement.setAttribute('data-theme', theme);
                }
              })();
            `,
          }}
        />
      </head>
      <body className="bg-bg-primary text-text-primary antialiased overflow-x-hidden">
        <WebSocketProvider>
          <SystemProvider>
            <div className="flex min-h-screen overflow-x-hidden">
              <Sidebar mobileOpen={mobileOpen} setMobileOpen={setMobileOpen} />
              <div
                className={`flex-1 transition-all duration-300 min-w-0 ${
                  sidebarExpanded ? 'lg:ml-[240px]' : 'lg:ml-16'
                }`}
              >
                <TopStatusBar onMenuClick={() => setMobileOpen(true)} />
                <main className="p-4 md:p-6 lg:p-8 max-w-full overflow-x-hidden">
                  {children}
                </main>
              </div>
            </div>
          </SystemProvider>
        </WebSocketProvider>
      </body>
    </html>
  )
}
