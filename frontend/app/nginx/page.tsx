'use client'

import { useState, useEffect } from 'react'
import { motion } from 'framer-motion'
import { CheckCircle2, ExternalLink, Plus, Trash2, RefreshCw, Save, ToggleLeft, ToggleRight, FileText, XCircle } from 'lucide-react'
import Link from 'next/link'
import SectionBadge from '@/components/ui/SectionBadge'
import { nginxApi } from '@/lib/api'

type NginxTab = 'files' | 'generator'

interface ConfigFile {
  name: string
  enabled: boolean
  size: number
}

export default function NginxPage() {
  const [tab, setTab] = useState<NginxTab>('files')

  // File management state
  const [files, setFiles] = useState<ConfigFile[]>([])
  const [selectedFile, setSelectedFile] = useState<string | null>(null)
  const [fileContent, setFileContent] = useState('')
  const [originalContent, setOriginalContent] = useState('')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

  // New file state
  const [showNewFile, setShowNewFile] = useState(false)
  const [newFileName, setNewFileName] = useState('')

  // Test state
  const [testResult, setTestResult] = useState<{ success: boolean; output: string } | null>(null)
  const [testing, setTesting] = useState(false)

  // Generator state (existing form)
  const [domain, setDomain] = useState('app.yourdomain.com')
  const [frontendPath, setFrontendPath] = useState('/var/www/opendeploy/frontend/dist')
  const [proxyEnabled, setProxyEnabled] = useState(true)
  const [proxyPort, setProxyPort] = useState('8000')
  const [generatorState, setGeneratorState] = useState<'form' | 'testing' | 'success'>('form')

  const fetchFiles = async () => {
    setLoading(true)
    try {
      const data = await nginxApi.listFiles()
      setFiles(data)
    } catch (err) {
      console.error('Failed to list nginx files', err)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchFiles()
  }, [])

  const openFile = async (name: string) => {
    setLoading(true)
    setMessage(null)
    try {
      const data = await nginxApi.readFile(name)
      setSelectedFile(name)
      setFileContent(data.content)
      setOriginalContent(data.content)
    } catch (err: any) {
      setMessage({ type: 'error', text: err.message || 'Failed to read file' })
    } finally {
      setLoading(false)
    }
  }

  const saveFile = async () => {
    if (!selectedFile) return
    setSaving(true)
    setMessage(null)
    try {
      await nginxApi.writeFile(selectedFile, fileContent)
      setOriginalContent(fileContent)
      setMessage({ type: 'success', text: 'Config saved successfully' })
      fetchFiles()
    } catch (err: any) {
      setMessage({ type: 'error', text: err.message || 'Failed to save file' })
    } finally {
      setSaving(false)
    }
  }

  const createFile = async () => {
    if (!newFileName.trim()) return
    const name = newFileName.endsWith('.conf') ? newFileName : newFileName
    setSaving(true)
    try {
      await nginxApi.writeFile(name, `# New nginx config: ${name}\nserver {\n    listen 80;\n    server_name example.com;\n\n    location / {\n        root /var/www/html;\n        index index.html;\n    }\n}\n`)
      setShowNewFile(false)
      setNewFileName('')
      await fetchFiles()
      openFile(name)
    } catch (err: any) {
      setMessage({ type: 'error', text: err.message || 'Failed to create file' })
    } finally {
      setSaving(false)
    }
  }

  const deleteFile = async (name: string) => {
    if (!confirm(`Delete config "${name}"? This cannot be undone.`)) return
    try {
      await nginxApi.deleteFile(name)
      if (selectedFile === name) {
        setSelectedFile(null)
        setFileContent('')
      }
      fetchFiles()
      setMessage({ type: 'success', text: `Deleted ${name}` })
    } catch (err: any) {
      setMessage({ type: 'error', text: err.message || 'Failed to delete file' })
    }
  }

  const toggleSite = async (name: string, enabled: boolean) => {
    try {
      if (enabled) {
        await nginxApi.disableSite(name)
      } else {
        await nginxApi.enableSite(name)
      }
      fetchFiles()
    } catch (err: any) {
      setMessage({ type: 'error', text: err.message || 'Failed to toggle site' })
    }
  }

  const testConfig = async () => {
    setTesting(true)
    setTestResult(null)
    try {
      const result = await nginxApi.testConfig()
      setTestResult(result)
    } catch (err: any) {
      setTestResult({ success: false, output: err.message || 'Test failed' })
    } finally {
      setTesting(false)
    }
  }

  const reloadNginx = async () => {
    try {
      await nginxApi.reload()
      setMessage({ type: 'success', text: 'Nginx reloaded successfully' })
    } catch (err: any) {
      setMessage({ type: 'error', text: err.message || 'Reload failed' })
    }
  }

  const handleApplyGenerator = async () => {
    setGeneratorState('testing')
    try {
      await nginxApi.applyConfig({
        domain,
        project_id: 'default',
        frontend_path: frontendPath,
        proxy_enabled: proxyEnabled,
        proxy_port: parseInt(proxyPort, 10)
      })
      const testRes = await nginxApi.testConfig()
      if (testRes.success) {
        setGeneratorState('success')
        fetchFiles()
      } else {
        setGeneratorState('form')
        setMessage({ type: 'error', text: 'Config test failed: ' + testRes.output })
      }
    } catch (err: any) {
      setMessage({ type: 'error', text: String(err) })
      setGeneratorState('form')
    }
  }

  const hasUnsavedChanges = fileContent !== originalContent

  return (
    <>
      <motion.div
        initial={{ opacity: 0, x: 20 }}
        animate={{ opacity: 1, x: 0 }}
        transition={{ duration: 0.3 }}
      >
        <div className="mb-8 flex items-center justify-between">
          <SectionBadge label="NGINX" />
          <div className="flex gap-2">
            <button
              onClick={testConfig}
              disabled={testing}
              className="flex items-center gap-2 px-4 py-2 border border-border-dark  font-mono text-small text-text-secondary hover:text-text-primary transition-colors"
            >
              <RefreshCw size={14} className={testing ? "animate-spin" : ""} />
              {testing ? 'Testing...' : 'Test Config'}
            </button>
            <button
              onClick={reloadNginx}
              className="flex items-center gap-2 px-4 py-2 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider  hover:bg-accent-lime-muted transition-all"
            >
              Reload Nginx
            </button>
          </div>
        </div>

        {/* Message */}
        {message && (
          <motion.div
            initial={{ opacity: 0, y: -10 }}
            animate={{ opacity: 1, y: 0 }}
            className={`mb-4 px-4 py-3  font-mono text-small flex items-center gap-2 ${
              message.type === 'success'
                ? 'bg-accent-lime/10 text-accent-lime border border-accent-lime/30'
                : 'bg-status-error/10 text-status-error border border-status-error/30'
            }`}
          >
            {message.type === 'success' ? <CheckCircle2 size={16} /> : <XCircle size={16} />}
            {message.text}
            <button onClick={() => setMessage(null)} className="ml-auto opacity-50 hover:opacity-100">×</button>
          </motion.div>
        )}

        {/* Test Result */}
        {testResult && (
          <motion.div
            initial={{ opacity: 0, y: -10 }}
            animate={{ opacity: 1, y: 0 }}
            className={`mb-4 px-4 py-3  font-mono text-small border ${
              testResult.success
                ? 'bg-accent-lime/10 border-accent-lime/30'
                : 'bg-status-error/10 border-status-error/30'
            }`}
          >
            <div className="flex items-center gap-2 mb-2">
              {testResult.success ? (
                <CheckCircle2 size={16} className="text-accent-lime" />
              ) : (
                <XCircle size={16} className="text-status-error" />
              )}
              <span className={testResult.success ? 'text-accent-lime' : 'text-status-error'}>
                {testResult.success ? 'Configuration test passed' : 'Configuration test failed'}
              </span>
              <button onClick={() => setTestResult(null)} className="ml-auto opacity-50 hover:opacity-100 text-text-secondary">×</button>
            </div>
            <pre className="text-[11px] text-text-secondary whitespace-pre-wrap">{testResult.output}</pre>
          </motion.div>
        )}

        {/* Tabs */}
        <div className="flex gap-1 mb-6 bg-bg-secondary  p-1">
          <button
            onClick={() => setTab('files')}
            className={`px-4 py-2 -md font-mono text-small uppercase tracking-wider transition-all ${
              tab === 'files'
                ? 'bg-accent-lime text-text-dark font-bold'
                : 'text-text-secondary hover:text-text-primary'
            }`}
          >
            Config Files
          </button>
          <button
            onClick={() => setTab('generator')}
            className={`px-4 py-2 -md font-mono text-small uppercase tracking-wider transition-all ${
              tab === 'generator'
                ? 'bg-accent-lime text-text-dark font-bold'
                : 'text-text-secondary hover:text-text-primary'
            }`}
          >
            Site Generator
          </button>
        </div>

        {tab === 'files' && (
          <div className="grid grid-cols-1 lg:grid-cols-4 gap-6">
            {/* File List */}
            <div className="lg:col-span-1">
              <div className="bg-bg-secondary  border border-border-dark overflow-hidden">
                <div className="px-4 py-3 border-b border-border-dark flex items-center justify-between">
                  <span className="font-mono text-label uppercase tracking-wider text-text-secondary">Sites Available</span>
                  <button
                    onClick={() => setShowNewFile(true)}
                    className="p-1 text-text-secondary hover:text-accent-lime transition-colors"
                    title="New config"
                  >
                    <Plus size={16} />
                  </button>
                </div>

                {showNewFile && (
                  <div className="px-4 py-3 border-b border-border-dark bg-bg-primary">
                    <input
                      value={newFileName}
                      onChange={e => setNewFileName(e.target.value)}
                      placeholder="site-name"
                      className="w-full px-3 py-2 bg-bg-secondary border border-border-dark  font-mono text-small text-text-primary mb-2"
                      autoFocus
                      onKeyDown={e => e.key === 'Enter' && createFile()}
                    />
                    <div className="flex gap-2">
                      <button
                        onClick={createFile}
                        className="flex-1 px-3 py-1.5 bg-accent-lime text-text-dark font-mono text-[11px] font-bold "
                      >
                        Create
                      </button>
                      <button
                        onClick={() => { setShowNewFile(false); setNewFileName('') }}
                        className="px-3 py-1.5 border border-border-dark text-text-secondary font-mono text-[11px] "
                      >
                        Cancel
                      </button>
                    </div>
                  </div>
                )}

                <div className="divide-y divide-border-dark/50">
                  {files.length === 0 && !loading && (
                    <p className="px-4 py-6 text-center font-mono text-small text-text-secondary">
                      No config files found
                    </p>
                  )}
                  {files.map(file => (
                    <div
                      key={file.name}
                      className={`px-4 py-3 cursor-pointer hover:bg-bg-primary/50 transition-colors group ${
                        selectedFile === file.name ? 'bg-bg-primary border-l-2 border-accent-lime' : ''
                      }`}
                    >
                      <div className="flex items-center gap-2">
                        <FileText size={14} className="text-text-secondary flex-shrink-0" />
                        <button
                          onClick={() => openFile(file.name)}
                          className="flex-1 text-left font-mono text-small text-text-primary truncate"
                        >
                          {file.name}
                        </button>
                        <button
                          onClick={() => toggleSite(file.name, file.enabled)}
                          className="flex-shrink-0 text-text-secondary hover:text-accent-lime transition-colors"
                          title={file.enabled ? 'Disable site' : 'Enable site'}
                        >
                          {file.enabled ? (
                            <ToggleRight size={18} className="text-accent-lime" />
                          ) : (
                            <ToggleLeft size={18} />
                          )}
                        </button>
                        <button
                          onClick={() => deleteFile(file.name)}
                          className="flex-shrink-0 opacity-0 group-hover:opacity-100 text-text-secondary hover:text-status-error transition-all"
                          title="Delete"
                        >
                          <Trash2 size={14} />
                        </button>
                      </div>
                      <span className={`font-mono text-[10px] ${file.enabled ? 'text-accent-lime' : 'text-text-secondary'}`}>
                        {file.enabled ? 'enabled' : 'disabled'}
                      </span>
                    </div>
                  ))}
                </div>
              </div>
            </div>

            {/* Editor */}
            <div className="lg:col-span-3">
              <div className="bg-bg-secondary  border border-border-dark overflow-hidden">
                <div className="px-4 py-3 border-b border-border-dark flex items-center justify-between">
                  <span className="font-mono text-label uppercase tracking-wider text-text-secondary">
                    {selectedFile ? selectedFile : 'Select a file to edit'}
                    {hasUnsavedChanges && <span className="ml-2 text-status-warning">• modified</span>}
                  </span>
                  {selectedFile && (
                    <div className="flex gap-2">
                      <button
                        onClick={saveFile}
                        disabled={saving || !hasUnsavedChanges}
                        className="flex items-center gap-2 px-3 py-1.5 bg-accent-lime text-text-dark font-mono text-[11px] font-bold  disabled:opacity-50"
                      >
                        <Save size={12} />
                        {saving ? 'Saving...' : 'Save'}
                      </button>
                    </div>
                  )}
                </div>
                <textarea
                  value={fileContent}
                  onChange={e => setFileContent(e.target.value)}
                  disabled={!selectedFile}
                  className="w-full bg-bg-primary text-text-primary font-mono text-[13px] leading-relaxed p-4 resize-none focus:outline-none"
                  style={{ minHeight: '500px', tabSize: 4 }}
                  spellCheck={false}
                  placeholder={selectedFile ? '' : 'Select a config file from the list to view and edit it.'}
                />
              </div>
            </div>
          </div>
        )}

        {tab === 'generator' && (
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            {/* Config Form */}
            <div className="bg-bg-secondary  border border-border-dark p-6">
              <h2 className="font-serif text-h3 mb-6">Generate Site Config</h2>

              <div className="space-y-5">
                <div>
                  <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                    Domain
                  </label>
                  <input
                    value={domain}
                    onChange={e => setDomain(e.target.value)}
                    className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary"
                  />
                </div>

                <div>
                  <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                    Frontend Path
                  </label>
                  <input
                    value={frontendPath}
                    onChange={e => setFrontendPath(e.target.value)}
                    className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary"
                  />
                </div>

                <div>
                  <div className="flex items-center justify-between mb-2">
                    <label className="font-mono text-label uppercase tracking-wider text-text-secondary">
                      Backend Proxy
                    </label>
                    <button
                      onClick={() => setProxyEnabled(!proxyEnabled)}
                      className={`w-10 h-5  transition-colors relative ${proxyEnabled ? 'bg-accent-lime' : 'bg-border-dark'}`}
                    >
                      <span className={`absolute top-0.5 w-4 h-4  bg-white transition-transform ${proxyEnabled ? 'left-5' : 'left-0.5'}`} />
                    </button>
                  </div>
                  {proxyEnabled && (
                    <input
                      value={proxyPort}
                      onChange={e => setProxyPort(e.target.value)}
                      className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary"
                      placeholder="8000"
                    />
                  )}
                </div>

                <button
                  onClick={handleApplyGenerator}
                  disabled={generatorState === 'testing'}
                  className="w-full px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider  hover:bg-accent-lime-muted transition-all disabled:opacity-50"
                >
                  {generatorState === 'testing' ? 'Generating & Testing...' : 'Generate & Apply →'}
                </button>
              </div>
            </div>

            {/* Result */}
            <div>
              {generatorState === 'success' ? (
                <motion.div
                  initial={{ opacity: 0, y: 20 }}
                  animate={{ opacity: 1, y: 0 }}
                  className="bg-bg-secondary  border-2 border-accent-lime p-6"
                >
                  <div className="flex items-center gap-2 mb-4">
                    <CheckCircle2 size={20} className="text-accent-lime" />
                    <span className="font-mono text-small font-bold text-accent-lime">Config Applied Successfully</span>
                  </div>
                  <div className="space-y-2 font-mono text-small">
                    <div className="flex items-center gap-2">
                      <CheckCircle2 size={14} className="text-accent-lime" />
                      <span>Config generated</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <CheckCircle2 size={14} className="text-accent-lime" />
                      <span>Syntax test passed</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <CheckCircle2 size={14} className="text-accent-lime" />
                      <span>Nginx reloaded</span>
                    </div>
                  </div>
                  <p className="mt-4 font-mono text-small text-text-secondary">
                    Site: <span className="text-accent-lime">{domain}</span>
                  </p>
                  <Link
                    href="/dashboard"
                    className="block w-full mt-6 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider  hover:bg-accent-lime-muted transition-all text-center"
                  >
                    Go to Dashboard &rarr;
                  </Link>
                </motion.div>
              ) : (
                <div className="bg-bg-secondary  border border-border-dark p-6 flex flex-col items-center justify-center h-full text-center">
                  {generatorState === 'testing' ? (
                    <>
                      <div className="w-8 h-8 border-2 border-accent-lime border-t-transparent  animate-spin mb-4" />
                      <p className="font-mono text-small text-text-secondary">Generating and testing...</p>
                    </>
                  ) : (
                    <p className="font-mono text-small text-text-secondary">
                      Fill in the form and click &quot;Generate &amp; Apply&quot; to create a config
                    </p>
                  )}
                </div>
              )}
            </div>
          </div>
        )}
      </motion.div>
    </>
  )
}
