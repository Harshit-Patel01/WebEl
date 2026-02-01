'use client'

import { useState, useEffect } from 'react'
import { motion } from 'framer-motion'
import { Monitor, Wifi, Shield, Palette, Info } from 'lucide-react'
import SectionBadge from '@/components/ui/SectionBadge'
import { systemApi, authApi, wifiApi } from '@/lib/api'

type SettingsSection = 'system' | 'network' | 'security' | 'appearance' | 'about'

const sections: { key: SettingsSection; label: string; icon: React.ElementType }[] = [
  { key: 'system', label: 'System', icon: Monitor },
  { key: 'network', label: 'Network', icon: Wifi },
  { key: 'security', label: 'Security', icon: Shield },
  { key: 'appearance', label: 'Appearance', icon: Palette },
  { key: 'about', label: 'About OpenDeploy', icon: Info },
]

export default function SettingsPage() {
  const [activeSection, setActiveSection] = useState<SettingsSection>('system')
  const [theme, setTheme] = useState<'dark' | 'light' | 'auto'>('dark')
  const [lanOnly, setLanOnly] = useState(false)
  const [password, setPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [tokenVisible, setTokenVisible] = useState(false)
  const [passwordLoading, setPasswordLoading] = useState(false)
  const [passwordMessage, setPasswordMessage] = useState('')

  const [systemInfo, setSystemInfo] = useState<any>(null)
  const [systemStats, setSystemStats] = useState<any>(null)
  const [wifiStatus, setWifiStatus] = useState<any>(null)

  // Load theme from localStorage on mount
  useEffect(() => {
    const savedTheme = localStorage.getItem('theme') as 'dark' | 'light' | 'auto' | null
    if (savedTheme) {
      setTheme(savedTheme)
      applyTheme(savedTheme)
    }
  }, [])

  // Apply theme to document
  const applyTheme = (selectedTheme: 'dark' | 'light' | 'auto') => {
    const root = document.documentElement

    if (selectedTheme === 'auto') {
      // Use system preference
      const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
      root.setAttribute('data-theme', prefersDark ? 'dark' : 'light')
    } else {
      root.setAttribute('data-theme', selectedTheme)
    }

    // Save to localStorage
    localStorage.setItem('theme', selectedTheme)
  }

  // Handle theme change
  const handleThemeChange = (newTheme: 'dark' | 'light' | 'auto') => {
    setTheme(newTheme)
    applyTheme(newTheme)
  }

  useEffect(() => {
    const fetchSystemData = async () => {
      try {
        const info = await systemApi.getInfo()
        setSystemInfo(info)
        const stats = await systemApi.getStats()
        setSystemStats(stats)
        const wifi = await wifiApi.getStatus()
        setWifiStatus(wifi)
      } catch (err) {
        console.error('Failed to load system data', err)
      }
    }
    fetchSystemData()
  }, [])

  const handlePasswordChange = async () => {
    if (!password || !newPassword) return
    setPasswordLoading(true)
    setPasswordMessage('')
    try {
      await authApi.changePassword({ current_password: password, new_password: newPassword })
      setPasswordMessage('Password updated successfully')
      setPassword('')
      setNewPassword('')
    } catch (err: any) {
      setPasswordMessage(err.message || 'Failed to update password')
    } finally {
      setPasswordLoading(false)
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
          <SectionBadge label="SETTINGS" />
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-4 gap-6">
          {/* Left - Navigation */}
          <div className="lg:col-span-1">
            <nav className="space-y-1">
              {sections.map(section => (
                <button
                  key={section.key}
                  onClick={() => setActiveSection(section.key)}
                  className={`
                    w-full flex items-center gap-3 px-4 py-3 rounded-lg transition-all text-left
                    ${activeSection === section.key
                      ? 'bg-bg-secondary text-accent-lime border border-border-dark'
                      : 'text-text-secondary hover:text-text-primary hover:bg-bg-secondary/50'
                    }
                  `}
                >
                  <section.icon size={18} />
                  <span className="font-mono text-small">{section.label}</span>
                </button>
              ))}
            </nav>
          </div>

          {/* Right - Content */}
          <div className="lg:col-span-3">
            <div className="bg-bg-secondary rounded-card border border-border-dark p-8">
              {activeSection === 'system' && (
                <div>
                  <h2 className="font-serif text-h2 mb-6">System Information</h2>
                  <div className="space-y-4 font-mono text-small">
                    {[
                      ['Hostname', systemInfo?.hostname || 'Loading...'],
                      ['Model', systemInfo?.model || 'Loading...'],
                      ['OS', systemInfo?.os || 'Loading...'],
                      ['Kernel', systemInfo?.kernel || 'Loading...'],
                      ['CPU Temp', systemStats?.temp ? `${systemStats.temp}°C` : 'Loading...'],
                      ['Memory', systemStats?.ram ? `${systemStats.ram}% used` : 'Loading...'],
                      ['Uptime', systemStats?.uptime || 'Loading...'],
                    ].map(([label, value]) => (
                      <div key={label} className="flex items-center justify-between py-2 border-b border-border-dark">
                        <span className="text-text-secondary">{label}</span>
                        <span className="text-text-primary">{value}</span>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {activeSection === 'network' && (
                <div>
                  <h2 className="font-serif text-h2 mb-6">Network Configuration</h2>
                  <div className="space-y-4 font-mono text-small">
                    {[
                      ['WiFi SSID', wifiStatus?.ssid || 'Not Connected'],
                      ['IP Address', systemInfo?.ip || 'Loading...'],
                      ['Status', wifiStatus?.state || 'Unknown'],
                    ].map(([label, value]) => (
                      <div key={label} className="flex items-center justify-between py-2 border-b border-border-dark">
                        <span className="text-text-secondary">{label}</span>
                        <span className="text-text-primary">{value}</span>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {activeSection === 'security' && (
                <div>
                  <h2 className="font-serif text-h2 mb-6">Security</h2>

                  <div className="space-y-8">
                    {/* Change password */}
                    <div>
                      <h3 className="font-mono text-label uppercase tracking-wider text-text-secondary mb-4">
                        Change Dashboard Password
                      </h3>
                      <div className="space-y-3 max-w-md">
                        <input
                          type="password"
                          value={password}
                          onChange={e => setPassword(e.target.value)}
                          className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary"
                          placeholder="Current password"
                        />
                        <input
                          type="password"
                          value={newPassword}
                          onChange={e => setNewPassword(e.target.value)}
                          className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary"
                          placeholder="New password"
                        />
                        <button
                          onClick={handlePasswordChange}
                          disabled={passwordLoading || !password || !newPassword}
                          className="px-6 py-2.5 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all disabled:opacity-50"
                        >
                          {passwordLoading ? 'Updating...' : 'Update Password'}
                        </button>
                        {passwordMessage && (
                          <p className={`font-mono text-[11px] ${passwordMessage.includes('Failed') ? 'text-status-error' : 'text-accent-lime'}`}>
                            {passwordMessage}
                          </p>
                        )}
                      </div>
                    </div>

                    {/* LAN only toggle */}
                    <div className="flex items-center justify-between py-4 border-t border-border-dark">
                      <div>
                        <h3 className="font-mono text-small font-bold mb-1">LAN-Only Access</h3>
                        <p className="font-mono text-[11px] text-text-secondary">
                          Restrict dashboard access to local network only
                        </p>
                      </div>
                      <button
                        onClick={() => setLanOnly(!lanOnly)}
                        className={`
                          w-10 h-5 rounded-full transition-colors relative
                          ${lanOnly ? 'bg-accent-lime' : 'bg-border-dark'}
                        `}
                      >
                        <span
                          className={`
                            absolute top-0.5 w-4 h-4 rounded-full bg-white transition-transform
                            ${lanOnly ? 'left-5' : 'left-0.5'}
                          `}
                        />
                      </button>
                    </div>

                    {/* API Token */}
                    <div className="py-4 border-t border-border-dark">
                      <h3 className="font-mono text-small font-bold mb-3">Cloudflare API Token</h3>
                      <div className="flex items-center gap-3">
                        <div className="flex-1 px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small">
                          {tokenVisible ? 'cf-abc123def456ghi789jkl012' : '••••••••••••••••••••••••••'}
                        </div>
                        <button
                          onClick={() => setTokenVisible(!tokenVisible)}
                          className="px-3 py-3 border border-border-dark rounded-lg font-mono text-[10px] text-text-secondary hover:text-text-primary transition-colors"
                        >
                          {tokenVisible ? 'HIDE' : 'SHOW'}
                        </button>
                        <button className="px-3 py-3 border border-status-error/30 rounded-lg font-mono text-[10px] text-status-error hover:bg-status-error/10 transition-colors">
                          REVOKE
                        </button>
                      </div>
                    </div>
                  </div>
                </div>
              )}

              {activeSection === 'appearance' && (
                <div>
                  <h2 className="font-serif text-h2 mb-6">Appearance</h2>
                  <h3 className="font-mono text-label uppercase tracking-wider text-text-secondary mb-4">
                    Theme
                  </h3>
                  <div className="flex gap-3">
                    {(['dark', 'light', 'auto'] as const).map(t => (
                      <button
                        key={t}
                        onClick={() => handleThemeChange(t)}
                        className={`
                          px-6 py-3 rounded-lg font-mono text-small uppercase tracking-wider transition-all
                          ${theme === t
                            ? 'bg-accent-lime text-text-dark font-bold'
                            : 'bg-bg-primary border border-border-dark text-text-secondary hover:text-text-primary'
                          }
                        `}
                      >
                        {t}
                      </button>
                    ))}
                  </div>
                  <p className="mt-4 font-sans text-small text-text-secondary">
                    {theme === 'auto'
                      ? 'Theme will automatically match your system preferences'
                      : `Using ${theme} theme`
                    }
                  </p>
                </div>
              )}

              {activeSection === 'about' && (
                <div>
                  <h2 className="font-serif text-h2 mb-6">About OpenDeploy</h2>
                  <div className="space-y-4 font-mono text-small">
                    {[
                      ['Version', '1.0.0'],
                      ['Build', '2024.01.15-abc123'],
                      ['License', 'MIT'],
                      ['Author', 'OpenDeploy Team'],
                    ].map(([label, value]) => (
                      <div key={label} className="flex items-center justify-between py-2 border-b border-border-dark">
                        <span className="text-text-secondary">{label}</span>
                        <span className="text-text-primary">{value}</span>
                      </div>
                    ))}
                  </div>
                  <p className="mt-8 font-sans text-body text-text-secondary">
                    OpenDeploy turns your Linux device into a self-hosted deployment platform. No terminal. No complexity. Just deploy.
                  </p>
                </div>
              )}
            </div>
          </div>
        </div>
      </motion.div>
    </>
  )
}
