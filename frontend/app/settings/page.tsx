'use client'

import { useState, useEffect } from 'react'
import { motion } from 'framer-motion'
import { Monitor, Wifi, Shield, Palette, Info, Eye, EyeOff, Trash2, Plus, CheckCircle2, AlertCircle, LogOut } from 'lucide-react'
import SectionBadge from '@/components/ui/SectionBadge'
import { systemApi, authApi, wifiApi, tunnelApi } from '@/lib/api'
import { apiKeyStorage } from '@/utils/apiKey'

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

  // Password state
  const [passwordSet, setPasswordSet] = useState(false)
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmNewPassword, setConfirmNewPassword] = useState('')
  const [setupPassword, setSetupPassword] = useState('')
  const [setupConfirm, setSetupConfirm] = useState('')
  const [showCurrentPassword, setShowCurrentPassword] = useState(false)
  const [showNewPassword, setShowNewPassword] = useState(false)
  const [passwordLoading, setPasswordLoading] = useState(false)
  const [passwordMessage, setPasswordMessage] = useState<{ text: string; type: 'success' | 'error' } | null>(null)

  // API Key state
  const [cfApiKey, setCfApiKey] = useState('')
  const [newApiKey, setNewApiKey] = useState('')
  const [showApiKey, setShowApiKey] = useState(false)
  const [apiKeyLoading, setApiKeyLoading] = useState(false)
  const [apiKeyMessage, setApiKeyMessage] = useState<{ text: string; type: 'success' | 'error' } | null>(null)

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

  // Load API key from localStorage
  useEffect(() => {
    const stored = apiKeyStorage.get()
    if (stored) {
      setCfApiKey(stored)
    }
  }, [])

  // Check auth status
  useEffect(() => {
    const checkAuth = async () => {
      try {
        const status = await authApi.getStatus()
        setPasswordSet(status.password_set)
      } catch (err) {
        console.error('Failed to check auth status:', err)
      }
    }
    checkAuth()
  }, [])

  const applyTheme = (selectedTheme: 'dark' | 'light' | 'auto') => {
    const root = document.documentElement
    if (selectedTheme === 'auto') {
      const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
      root.setAttribute('data-theme', prefersDark ? 'dark' : 'light')
    } else {
      root.setAttribute('data-theme', selectedTheme)
    }
    localStorage.setItem('theme', selectedTheme)
  }

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

  // Handle setting initial password
  const handleSetupPassword = async () => {
    if (!setupPassword || !setupConfirm) return
    if (setupPassword !== setupConfirm) {
      setPasswordMessage({ text: 'Passwords do not match', type: 'error' })
      return
    }
    if (setupPassword.length < 6) {
      setPasswordMessage({ text: 'Password must be at least 6 characters', type: 'error' })
      return
    }
    setPasswordLoading(true)
    setPasswordMessage(null)
    try {
      await authApi.setupPassword(setupPassword)
      setPasswordMessage({ text: 'Password created successfully', type: 'success' })
      setPasswordSet(true)
      setSetupPassword('')
      setSetupConfirm('')
    } catch (err: any) {
      setPasswordMessage({ text: err.message || 'Failed to set password', type: 'error' })
    } finally {
      setPasswordLoading(false)
    }
  }

  // Handle changing existing password
  const handlePasswordChange = async () => {
    if (!currentPassword || !newPassword) return
    if (newPassword !== confirmNewPassword) {
      setPasswordMessage({ text: 'New passwords do not match', type: 'error' })
      return
    }
    if (newPassword.length < 6) {
      setPasswordMessage({ text: 'Password must be at least 6 characters', type: 'error' })
      return
    }
    setPasswordLoading(true)
    setPasswordMessage(null)
    try {
      await authApi.changePassword({ current_password: currentPassword, new_password: newPassword })
      setPasswordMessage({ text: 'Password updated successfully', type: 'success' })
      setCurrentPassword('')
      setNewPassword('')
      setConfirmNewPassword('')
    } catch (err: any) {
      setPasswordMessage({ text: err.message || 'Failed to update password', type: 'error' })
    } finally {
      setPasswordLoading(false)
    }
  }

  // Handle adding a new Cloudflare API key
  const handleAddApiKey = async () => {
    if (!newApiKey) return
    setApiKeyLoading(true)
    setApiKeyMessage(null)
    try {
      // Validate the token with Cloudflare
      const result = await tunnelApi.validateToken(newApiKey)
      if (result.valid) {
        apiKeyStorage.set(newApiKey)
        setCfApiKey(newApiKey)
        setNewApiKey('')
        setApiKeyMessage({ text: 'API token saved and validated successfully', type: 'success' })
      } else {
        setApiKeyMessage({ text: `Token validation failed: ${result.status}`, type: 'error' })
      }
    } catch (err: any) {
      setApiKeyMessage({ text: err.message || 'Failed to validate API token', type: 'error' })
    } finally {
      setApiKeyLoading(false)
    }
  }

  // Handle deleting the API key
  const handleDeleteApiKey = () => {
    if (!confirm('Are you sure you want to remove the stored Cloudflare API token?')) return
    apiKeyStorage.clear()
    setCfApiKey('')
    setShowApiKey(false)
    setApiKeyMessage({ text: 'API token removed', type: 'success' })
  }

  // Handle logout
  const handleLogout = async () => {
    try {
      await authApi.logout()
    } catch (err) {
      console.error('Logout failed:', err)
    }
    // Dispatch event so layout.tsx can switch to login screen
    window.dispatchEvent(new Event('opendeploy-logout'))
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
                    w-full flex items-center gap-3 px-4 py-3  transition-all text-left
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
            <div className="bg-bg-secondary  border border-border-dark p-8">
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
                    {/* Password Section */}
                    {!passwordSet ? (
                      <div>
                        <h3 className="font-mono text-label uppercase tracking-wider text-text-secondary mb-4">
                          Set Dashboard Password
                        </h3>
                        <p className="font-mono text-[11px] text-text-secondary mb-4">
                          No password is currently set. Set a password to protect your dashboard.
                        </p>
                        <div className="space-y-3 max-w-md">
                          <div className="relative">
                            <input
                              type={showNewPassword ? 'text' : 'password'}
                              value={setupPassword}
                              onChange={e => setSetupPassword(e.target.value)}
                              className="w-full px-4 py-3 pr-12 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary placeholder:text-text-secondary focus:outline-none focus:border-accent-lime focus:ring-2 focus:ring-accent-lime/20"
                              placeholder="Create a password (min 6 chars)"
                            />
                            <button
                              type="button"
                              onClick={() => setShowNewPassword(!showNewPassword)}
                              className="absolute right-3 top-1/2 -translate-y-1/2 p-1 text-text-secondary hover:text-accent-lime"
                              tabIndex={-1}
                            >
                              {showNewPassword ? <EyeOff size={16} /> : <Eye size={16} />}
                            </button>
                          </div>
                          <input
                            type={showNewPassword ? 'text' : 'password'}
                            value={setupConfirm}
                            onChange={e => setSetupConfirm(e.target.value)}
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary placeholder:text-text-secondary focus:outline-none focus:border-accent-lime focus:ring-2 focus:ring-accent-lime/20"
                            placeholder="Confirm password"
                          />
                          <button
                            onClick={handleSetupPassword}
                            disabled={passwordLoading || !setupPassword || !setupConfirm}
                            className="px-6 py-2.5 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider  hover:bg-accent-lime-muted transition-all disabled:opacity-50"
                          >
                            {passwordLoading ? 'Setting...' : 'Set Password'}
                          </button>
                        </div>
                      </div>
                    ) : (
                      <div>
                        <h3 className="font-mono text-label uppercase tracking-wider text-text-secondary mb-4">
                          Change Dashboard Password
                        </h3>
                        <div className="space-y-3 max-w-md">
                          <div className="relative">
                            <input
                              type={showCurrentPassword ? 'text' : 'password'}
                              value={currentPassword}
                              onChange={e => setCurrentPassword(e.target.value)}
                              className="w-full px-4 py-3 pr-12 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary placeholder:text-text-secondary focus:outline-none focus:border-accent-lime focus:ring-2 focus:ring-accent-lime/20"
                              placeholder="Current password"
                            />
                            <button
                              type="button"
                              onClick={() => setShowCurrentPassword(!showCurrentPassword)}
                              className="absolute right-3 top-1/2 -translate-y-1/2 p-1 text-text-secondary hover:text-accent-lime"
                              tabIndex={-1}
                            >
                              {showCurrentPassword ? <EyeOff size={16} /> : <Eye size={16} />}
                            </button>
                          </div>
                          <div className="relative">
                            <input
                              type={showNewPassword ? 'text' : 'password'}
                              value={newPassword}
                              onChange={e => setNewPassword(e.target.value)}
                              className="w-full px-4 py-3 pr-12 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary placeholder:text-text-secondary focus:outline-none focus:border-accent-lime focus:ring-2 focus:ring-accent-lime/20"
                              placeholder="New password"
                            />
                            <button
                              type="button"
                              onClick={() => setShowNewPassword(!showNewPassword)}
                              className="absolute right-3 top-1/2 -translate-y-1/2 p-1 text-text-secondary hover:text-accent-lime"
                              tabIndex={-1}
                            >
                              {showNewPassword ? <EyeOff size={16} /> : <Eye size={16} />}
                            </button>
                          </div>
                          <input
                            type={showNewPassword ? 'text' : 'password'}
                            value={confirmNewPassword}
                            onChange={e => setConfirmNewPassword(e.target.value)}
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary placeholder:text-text-secondary focus:outline-none focus:border-accent-lime focus:ring-2 focus:ring-accent-lime/20"
                            placeholder="Confirm new password"
                          />
                          <button
                            onClick={handlePasswordChange}
                            disabled={passwordLoading || !currentPassword || !newPassword || !confirmNewPassword}
                            className="px-6 py-2.5 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider  hover:bg-accent-lime-muted transition-all disabled:opacity-50"
                          >
                            {passwordLoading ? 'Updating...' : 'Update Password'}
                          </button>
                        </div>
                      </div>
                    )}

                    {/* Password message */}
                    {passwordMessage && (
                      <div className={`flex items-center gap-2 px-4 py-3  ${
                        passwordMessage.type === 'success'
                          ? 'bg-status-success/10 border border-status-success/20'
                          : 'bg-status-error/10 border border-status-error/20'
                      }`}>
                        {passwordMessage.type === 'success' ? (
                          <CheckCircle2 size={16} className="text-status-success flex-shrink-0" />
                        ) : (
                          <AlertCircle size={16} className="text-status-error flex-shrink-0" />
                        )}
                        <p className={`font-mono text-[11px] ${
                          passwordMessage.type === 'success' ? 'text-status-success' : 'text-status-error'
                        }`}>
                          {passwordMessage.text}
                        </p>
                      </div>
                    )}

                    {/* Session / Logout */}
                    <div className="flex items-center justify-between py-4 border-t border-border-dark">
                      <div>
                        <h3 className="font-mono text-small font-bold mb-1">Session</h3>
                        <p className="font-mono text-[11px] text-text-secondary">
                          Sign out of the dashboard
                        </p>
                      </div>
                      <button
                        onClick={handleLogout}
                        className="flex items-center gap-2 px-4 py-2 border border-status-error/30  font-mono text-[11px] text-status-error hover:bg-status-error/10 transition-colors"
                      >
                        <LogOut size={14} />
                        Sign Out
                      </button>
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
                          w-10 h-5  transition-colors relative
                          ${lanOnly ? 'bg-accent-lime' : 'bg-border-dark'}
                        `}
                      >
                        <span
                          className={`
                            absolute top-0.5 w-4 h-4  bg-white transition-transform
                            ${lanOnly ? 'left-5' : 'left-0.5'}
                          `}
                        />
                      </button>
                    </div>

                    {/* Cloudflare API Token */}
                    <div className="py-4 border-t border-border-dark">
                      <h3 className="font-mono text-label uppercase tracking-wider text-text-secondary mb-4">
                        Cloudflare API Token
                      </h3>

                      {cfApiKey ? (
                        <div className="space-y-3">
                          <div className="flex items-center gap-3">
                            <div className="flex-1 px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary">
                              {showApiKey ? cfApiKey : apiKeyStorage.getMasked()}
                            </div>
                            <button
                              onClick={() => setShowApiKey(!showApiKey)}
                              className="px-3 py-3 border border-border-dark  font-mono text-[10px] text-text-secondary hover:text-text-primary transition-colors"
                              title={showApiKey ? 'Hide token' : 'Show token'}
                            >
                              {showApiKey ? <EyeOff size={14} /> : <Eye size={14} />}
                            </button>
                            <button
                              onClick={handleDeleteApiKey}
                              className="px-3 py-3 border border-status-error/30  font-mono text-[10px] text-status-error hover:bg-status-error/10 transition-colors"
                              title="Delete API token"
                            >
                              <Trash2 size={14} />
                            </button>
                          </div>
                          <p className="font-mono text-[11px] text-text-secondary">
                            Token is stored locally in your browser. It is never sent to the backend for storage.
                          </p>
                        </div>
                      ) : (
                        <div className="space-y-3 max-w-md">
                          <p className="font-mono text-[11px] text-text-secondary mb-2">
                            No Cloudflare API token configured. Add one to manage tunnels.
                          </p>
                          <input
                            type="text"
                            value={newApiKey}
                            onChange={e => setNewApiKey(e.target.value)}
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary placeholder:text-text-secondary focus:outline-none focus:border-accent-lime focus:ring-2 focus:ring-accent-lime/20"
                            placeholder="Paste your Cloudflare API token..."
                          />
                          <button
                            onClick={handleAddApiKey}
                            disabled={apiKeyLoading || !newApiKey}
                            className="flex items-center gap-2 px-6 py-2.5 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider  hover:bg-accent-lime-muted transition-all disabled:opacity-50"
                          >
                            {apiKeyLoading ? 'Validating...' : <><Plus size={14} /> Add Token</>}
                          </button>
                        </div>
                      )}

                      {/* API Key message */}
                      {apiKeyMessage && (
                        <div className={`mt-3 flex items-center gap-2 px-4 py-3  ${
                          apiKeyMessage.type === 'success'
                            ? 'bg-status-success/10 border border-status-success/20'
                            : 'bg-status-error/10 border border-status-error/20'
                        }`}>
                          {apiKeyMessage.type === 'success' ? (
                            <CheckCircle2 size={16} className="text-status-success flex-shrink-0" />
                          ) : (
                            <AlertCircle size={16} className="text-status-error flex-shrink-0" />
                          )}
                          <p className={`font-mono text-[11px] ${
                            apiKeyMessage.type === 'success' ? 'text-status-success' : 'text-status-error'
                          }`}>
                            {apiKeyMessage.text}
                          </p>
                        </div>
                      )}
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
                          px-6 py-3  font-mono text-small uppercase tracking-wider transition-all
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
