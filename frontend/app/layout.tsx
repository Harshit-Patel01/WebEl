'use client'

import './globals.css'
import { WebSocketProvider } from '@/contexts/WebSocketContext'
import { SystemProvider } from '@/contexts/SystemContext'
import Sidebar from '@/components/layout/Sidebar'
import TopStatusBar from '@/components/layout/TopStatusBar'
import { usePathname, useRouter } from 'next/navigation'
import { useEffect, useState, useCallback } from 'react'
import { authApi } from '@/lib/api'
import { Loader2, Lock, Eye, EyeOff } from 'lucide-react'

type AuthState = 'loading' | 'setup' | 'login' | 'authenticated'

function LoginScreen({ onLogin }: { onLogin: () => void }) {
  const router = useRouter()
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!password) return
    setLoading(true)
    setError('')
    try {
      await authApi.login(password)
      onLogin()
      router.push('/dashboard')
    } catch (err: any) {
      setError(err.message || 'Invalid password')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-bg-primary flex items-center justify-center dot-pattern">
      <div className="max-w-md w-full mx-4">
        <div className="bg-bg-secondary  border border-border-dark p-8">
          <div className="text-center mb-8">
            <div className="inline-flex items-center gap-3 mb-4">
              <img src="/favicon.svg" alt="OpenDeploy" className="w-10 h-10" />
              <span className="font-serif font-bold text-2xl text-text-primary">OpenDeploy</span>
            </div>
            <div className="flex items-center justify-center gap-2 mb-2">
              <Lock size={16} className="text-accent-lime" />
              <h2 className="font-mono text-small uppercase tracking-wider text-text-secondary">Dashboard Login</h2>
            </div>
          </div>

          <form onSubmit={handleLogin} className="space-y-4">
            <div>
              <input
                type="password"
                value={password}
                onChange={e => setPassword(e.target.value)}
                placeholder="Enter your password"
                autoFocus
                className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary placeholder:text-text-secondary focus:outline-none focus:border-accent-lime focus:ring-2 focus:ring-accent-lime/20"
              />
            </div>
            {error && (
              <p className="font-mono text-[11px] text-status-error">{error}</p>
            )}
            <button
              type="submit"
              disabled={loading || !password}
              className="w-full px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider  hover:bg-accent-lime-muted transition-all disabled:opacity-50 flex items-center justify-center gap-2"
            >
              {loading ? <><Loader2 size={16} className="animate-spin" /> Signing in...</> : 'Sign In'}
            </button>
          </form>
        </div>
      </div>
    </div>
  )
}

function SetupScreen({ onComplete }: { onComplete: () => void }) {
  const router = useRouter()
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSetup = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!password) return
    if (password !== confirmPassword) {
      setError('Passwords do not match')
      return
    }
    if (password.length < 6) {
      setError('Password must be at least 6 characters')
      return
    }
    setLoading(true)
    setError('')
    try {
      await authApi.setupPassword(password)
      onComplete()
      router.push('/dashboard')
    } catch (err: any) {
      setError(err.message || 'Failed to set password')
    } finally {
      setLoading(false)
    }
  }

  const handleSkip = () => {
    localStorage.setItem('opendeploy_setup_skipped', 'true')
    onComplete()
    router.push('/dashboard')
  }

  return (
    <div className="min-h-screen bg-bg-primary flex items-center justify-center dot-pattern">
      <div className="max-w-md w-full mx-4">
        <div className="bg-bg-secondary  border border-border-dark p-8">
          <div className="text-center mb-8">
            <div className="inline-flex items-center gap-3 mb-4">
              <img src="/favicon.svg" alt="OpenDeploy" className="w-10 h-10" />
              <span className="font-serif font-bold text-2xl text-text-primary">OpenDeploy</span>
            </div>
            <h2 className="font-serif text-h2 text-text-primary mb-2">Secure Your Dashboard</h2>
            <p className="font-sans text-small text-text-secondary">
              Set a password to protect your OpenDeploy dashboard. You can also skip this step for now.
            </p>
          </div>

          <form onSubmit={handleSetup} className="space-y-4">
            <div className="relative">
              <input
                type={showPassword ? 'text' : 'password'}
                value={password}
                onChange={e => setPassword(e.target.value)}
                placeholder="Create a password (min 6 chars)"
                autoFocus
                className="w-full px-4 py-3 pr-12 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary placeholder:text-text-secondary focus:outline-none focus:border-accent-lime focus:ring-2 focus:ring-accent-lime/20"
              />
              <button
                type="button"
                onClick={() => setShowPassword(!showPassword)}
                className="absolute right-3 top-1/2 -translate-y-1/2 p-1 text-text-secondary hover:text-accent-lime"
                tabIndex={-1}
              >
                {showPassword ? <EyeOff size={16} /> : <Eye size={16} />}
              </button>
            </div>
            <input
              type={showPassword ? 'text' : 'password'}
              value={confirmPassword}
              onChange={e => setConfirmPassword(e.target.value)}
              placeholder="Confirm password"
              className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary placeholder:text-text-secondary focus:outline-none focus:border-accent-lime focus:ring-2 focus:ring-accent-lime/20"
            />
            {error && (
              <p className="font-mono text-[11px] text-status-error">{error}</p>
            )}
            <button
              type="submit"
              disabled={loading || !password || !confirmPassword}
              className="w-full px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider  hover:bg-accent-lime-muted transition-all disabled:opacity-50 flex items-center justify-center gap-2"
            >
              {loading ? <><Loader2 size={16} className="animate-spin" /> Setting up...</> : 'Set Password'}
            </button>
            <button
              type="button"
              onClick={handleSkip}
              className="w-full px-6 py-2.5 bg-transparent border border-border-dark text-text-secondary font-mono text-small uppercase tracking-wider  hover:text-text-primary hover:border-border-light transition-all"
            >
              Skip for now
            </button>
          </form>
        </div>
      </div>
    </div>
  )
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const pathname = usePathname()
  const router = useRouter()
  const isWelcomePage = pathname === '/'
  const [mobileOpen, setMobileOpen] = useState(false)
  const [sidebarExpanded, setSidebarExpanded] = useState(true)
  const [authState, setAuthState] = useState<AuthState>('loading')

  // Check auth status on mount
  const checkAuth = useCallback(async () => {
    try {
      const status = await authApi.getStatus()

      if (!status.password_set) {
        const skipped = localStorage.getItem('opendeploy_setup_skipped')
        if (skipped === 'true') {
          setAuthState('authenticated')
        } else {
          setAuthState('setup')
        }
      } else if (!status.authenticated) {
        setAuthState('login')
      } else {
        setAuthState('authenticated')
      }
    } catch {
      // If we can't reach API, allow access (could be first-boot)
      setAuthState('authenticated')
    }
  }, [])

  useEffect(() => {
    checkAuth()
  }, [checkAuth])

  // Listen for logout events from settings page
  useEffect(() => {
    const handleLogout = () => {
      setAuthState('login')
    }
    window.addEventListener('opendeploy-logout', handleLogout)
    return () => window.removeEventListener('opendeploy-logout', handleLogout)
  }, [])

  // Redirect / to /dashboard for authenticated users
  useEffect(() => {
    if (authState === 'authenticated' && isWelcomePage) {
      const setupCompleted = localStorage.getItem('opendeploy_setup_completed')
      if (setupCompleted === 'true') {
        router.replace('/dashboard')
      }
    }
  }, [authState, isWelcomePage, router])

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

  const themeScript = `
    (function() {
      const theme = localStorage.getItem('theme') || 'dark';
      if (theme === 'auto') {
        const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
        document.documentElement.setAttribute('data-theme', prefersDark ? 'dark' : 'light');
      } else {
        document.documentElement.setAttribute('data-theme', theme);
      }
    })();
  `

  // Show loading state while checking auth
  if (authState === 'loading') {
    return (
      <html lang="en" data-theme="dark" suppressHydrationWarning>
        <head>
          <link rel="icon" href="/favicon.svg" type="image/svg+xml" />
          <script dangerouslySetInnerHTML={{ __html: themeScript }} />
        </head>
        <body className="bg-bg-primary text-text-primary antialiased overflow-x-hidden">
          <div className="min-h-screen flex items-center justify-center">
            <Loader2 size={32} className="animate-spin text-accent-lime" />
          </div>
        </body>
      </html>
    )
  }

  // Show setup screen for first boot
  if (authState === 'setup') {
    return (
      <html lang="en" data-theme="dark" suppressHydrationWarning>
        <head>
          <link rel="icon" href="/favicon.svg" type="image/svg+xml" />
          <script dangerouslySetInnerHTML={{ __html: themeScript }} />
        </head>
        <body className="bg-bg-primary text-text-primary antialiased overflow-x-hidden">
          <SetupScreen onComplete={() => setAuthState('authenticated')} />
        </body>
      </html>
    )
  }

  // Show login screen if password is set but not authenticated
  if (authState === 'login') {
    return (
      <html lang="en" data-theme="dark" suppressHydrationWarning>
        <head>
          <link rel="icon" href="/favicon.svg" type="image/svg+xml" />
          <script dangerouslySetInnerHTML={{ __html: themeScript }} />
        </head>
        <body className="bg-bg-primary text-text-primary antialiased overflow-x-hidden">
          <LoginScreen onLogin={() => setAuthState('authenticated')} />
        </body>
      </html>
    )
  }

  // Welcome page (initial setup flow)
  if (isWelcomePage) {
    return (
      <html lang="en" data-theme="dark" suppressHydrationWarning>
        <head>
          <link rel="icon" href="/favicon.svg" type="image/svg+xml" />
          <script dangerouslySetInnerHTML={{ __html: themeScript }} />
        </head>
        <body className="bg-bg-primary text-text-primary antialiased overflow-x-hidden">
          {children}
        </body>
      </html>
    )
  }

  // Main dashboard layout
  return (
    <html lang="en" data-theme="dark" suppressHydrationWarning>
      <head>
        <link rel="icon" href="/favicon.svg" type="image/svg+xml" />
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
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
