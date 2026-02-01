'use client'

import { useState, useEffect } from 'react'
import { motion } from 'framer-motion'
import { Github, Plus, Trash2, CheckCircle2, Upload, Eye, EyeOff, Lock } from 'lucide-react'
import Link from 'next/link'
import SectionBadge from '@/components/ui/SectionBadge'
import BuildProgress from '@/components/ui/BuildProgress'
import TerminalPanel from '@/components/ui/TerminalPanel'
import { deployApi, envApi } from '@/lib/api'
import { useWebSocket } from '@/contexts/WebSocketContext'

type DeployState = 'form' | 'building' | 'success'
type TabType = 'frontend' | 'backend' | 'both'

export default function DeployPage() {
  const [state, setState] = useState<DeployState>('form')
  const [activeTab, setActiveTab] = useState<TabType>('frontend')
  const [repoUrl, setRepoUrl] = useState('')
  const [branch, setBranch] = useState('main')
  const [buildCmd, setBuildCmd] = useState('npm run build')
  const [outputDir, setOutputDir] = useState('dist')
  const [envVars, setEnvVars] = useState<{ key: string; value: string; is_secret: boolean; visible: boolean }[]>([])
  const [showBulkImport, setShowBulkImport] = useState(false)
  const [bulkContent, setBulkContent] = useState('')
  const [bulkIsSecret, setBulkIsSecret] = useState(false)
  const [buildPhase, setBuildPhase] = useState(0)
  const [logs, setLogs] = useState<string[]>([])
  const [currentJobId, setCurrentJobId] = useState<string | null>(null)

  const { subscribe, send } = useWebSocket()

  const tabs: TabType[] = ['frontend', 'backend', 'both']

  useEffect(() => {
    if (activeTab === 'backend') {
      setBuildCmd('pip install -r requirements.txt')
      setOutputDir('')
    } else {
      setBuildCmd('npm run build')
      setOutputDir('dist')
    }
  }, [activeTab])

  // Subscribe to WebSocket messages for build logs
  useEffect(() => {
    const unsubscribeLogLine = subscribe('log_line', (message) => {
      if (message.line) {
        setLogs(prev => [...prev, message.line!.text])
      }
    })

    const unsubscribeProgress = subscribe('progress', (message) => {
      const phaseMap: Record<string, number> = {
        clone: 0, detect: 1, build: 2, service: 3, done: 4,
        install: 2,
      }
      const phase = message.phase ? (phaseMap[message.phase] ?? buildPhase) : buildPhase
      setBuildPhase(phase)
      if (message.phase === 'done') {
        setState('success')
      }
    })

    const unsubscribeComplete = subscribe('job_complete', () => {
      setBuildPhase(4)
      setState('success')
    })

    const unsubscribeFailed = subscribe('job_failed', (message) => {
      setLogs(prev => [...prev, `[ERROR] Build failed: ${message.error}`])
    })

    return () => {
      unsubscribeLogLine()
      unsubscribeProgress()
      unsubscribeComplete()
      unsubscribeFailed()
    }
  }, [subscribe, buildPhase])

  const handleDeploy = async () => {
    setState('building')
    setLogs(['Starting deployment...'])
    setBuildPhase(0)

    try {
      // Convert env vars array to object for project creation
      const envObj: Record<string, string> = {}
      envVars.forEach(v => {
        if (v.key) envObj[v.key] = v.value
      })

      // 1. Create project
      const project = await deployApi.createProject({
        name: `Project-${Date.now()}`,
        repo_url: repoUrl,
        branch: branch,
        project_type: activeTab === 'backend' ? 'python' : 'node',
        build_command: buildCmd,
        output_dir: outputDir,
        env_vars: JSON.stringify(envObj)
      })

      // 2. Save env vars via API for persistence
      for (const v of envVars) {
        if (v.key) {
          await envApi.create(project.id, {
            key: v.key,
            value: v.value,
            is_secret: v.is_secret,
          })
        }
      }

      // 3. Trigger deploy
      const res = await deployApi.triggerDeploy(project.id)

      // 4. Subscribe to live WS logs
      if (res.deploy_id) {
        setCurrentJobId(res.deploy_id)
        send({ type: 'subscribe_job', jobId: res.deploy_id })
      }
    } catch (err) {
      setLogs(prev => [...prev, `[ERROR] Failed to start deployment: ${err}`])
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
          <SectionBadge label="04 — SOURCE CODE" />
        </div>

        {/* Tabs */}
        <div className="flex gap-1 mb-8 bg-bg-secondary rounded-lg p-1 w-fit">
          {tabs.map(tab => (
            <button
              key={tab}
              onClick={() => setActiveTab(tab)}
              className={`
                px-6 py-2 rounded-md font-mono text-small uppercase tracking-wider transition-all
                ${activeTab === tab
                  ? 'bg-accent-lime text-text-dark font-bold'
                  : 'text-text-secondary hover:text-text-primary'
                }
              `}
            >
              {tab}
            </button>
          ))}
        </div>

        {state === 'success' ? (
          <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            className="bg-bg-secondary rounded-card border-2 border-accent-lime p-8"
          >
            <div className="flex items-center gap-3 mb-6">
              <CheckCircle2 size={24} className="text-accent-lime" />
              <h2 className="font-serif text-h2 text-accent-lime">Build Complete</h2>
            </div>
            <div className="font-mono text-small space-y-2 mb-6">
              <p>
                <span className="text-text-secondary">Frontend: </span>
                <span className="text-text-primary">/var/www/opendeploy/frontend/dist</span>
              </p>
              <p>
                <span className="text-text-secondary">Backend: </span>
                <span className="text-text-primary">Service running</span>
              </p>
              <p>
                <span className="text-text-secondary">Build time: </span>
                <span className="text-text-primary">1m 42s</span>
              </p>
            </div>
            <Link
              href="/nginx"
              className="inline-flex items-center gap-2 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all"
            >
              Configure Nginx &rarr;
            </Link>

            <div className="mt-6">
              <BuildProgress currentPhase={4} />
            </div>
            <div className="mt-4">
              <TerminalPanel lines={logs} />
            </div>
          </motion.div>
        ) : (
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
            {/* Form */}
            <div className="bg-bg-secondary rounded-card border border-border-dark p-8">
              <h2 className="font-serif text-h2 mb-6">Deploy from GitHub</h2>

              <div className="space-y-5">
                <div>
                  <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                    Repository URL
                  </label>
                  <div className="relative">
                    <Github size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-text-secondary" />
                    <input
                      value={repoUrl}
                      onChange={e => setRepoUrl(e.target.value)}
                      className="w-full pl-10 pr-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary placeholder:text-text-secondary"
                      placeholder="https://github.com/user/repo"
                    />
                  </div>
                </div>

                <div>
                  <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                    Branch
                  </label>
                  <select
                    value={branch}
                    onChange={e => setBranch(e.target.value)}
                    className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary"
                  >
                    <option value="main">main</option>
                    <option value="develop">develop</option>
                    <option value="staging">staging</option>
                  </select>
                </div>

                <div>
                  <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                    Build Command
                  </label>
                  <input
                    value={buildCmd}
                    onChange={e => setBuildCmd(e.target.value)}
                    className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary"
                  />
                </div>

                {activeTab !== 'backend' && (
                  <div>
                    <label className="block font-mono text-label uppercase tracking-wider text-text-secondary mb-2">
                      Output Directory
                    </label>
                    <input
                      value={outputDir}
                      onChange={e => setOutputDir(e.target.value)}
                      className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary"
                      placeholder="dist"
                    />
                  </div>
                )}

                {/* Environment Variables */}
                <div>
                  <div className="flex items-center justify-between mb-2">
                    <label className="font-mono text-label uppercase tracking-wider text-text-secondary">
                      Environment Variables
                    </label>
                    <div className="flex items-center gap-2">
                      <button
                        onClick={() => setShowBulkImport(!showBulkImport)}
                        className="inline-flex items-center gap-1 font-mono text-label text-text-secondary hover:text-accent-lime transition-colors"
                      >
                        <Upload size={12} /> Bulk
                      </button>
                      <button
                        onClick={() => setEnvVars([...envVars, { key: '', value: '', is_secret: false, visible: true }])}
                        className="inline-flex items-center gap-1 font-mono text-label text-accent-lime hover:text-accent-lime-muted transition-colors"
                      >
                        <Plus size={12} /> Add
                      </button>
                    </div>
                  </div>

                  {showBulkImport && (
                    <div className="mb-3 p-3 bg-bg-primary border border-border-dark rounded-lg">
                      <p className="font-mono text-[10px] text-text-secondary mb-2">
                        Paste .env format (KEY=VALUE per line)
                      </p>
                      <textarea
                        value={bulkContent}
                        onChange={e => setBulkContent(e.target.value)}
                        className="w-full px-3 py-2 bg-bg-secondary border border-border-dark rounded font-mono text-small text-text-primary mb-2"
                        rows={4}
                        placeholder={"DATABASE_URL=postgres://...\nAPI_KEY=abc123\nNODE_ENV=production"}
                        spellCheck={false}
                      />
                      <div className="flex items-center justify-between">
                        <label className="flex items-center gap-2 font-mono text-[11px] text-text-secondary">
                          <input
                            type="checkbox"
                            checked={bulkIsSecret}
                            onChange={e => setBulkIsSecret(e.target.checked)}
                            className="accent-accent-lime"
                          />
                          <Lock size={10} /> Mark all as secret
                        </label>
                        <button
                          onClick={() => {
                            const lines = bulkContent.split('\n')
                            const newVars = lines
                              .map(l => l.trim())
                              .filter(l => l && !l.startsWith('#'))
                              .map(l => {
                                const [key, ...rest] = l.split('=')
                                let value = rest.join('=')
                                if (value.startsWith('"') && value.endsWith('"')) value = value.slice(1, -1)
                                if (value.startsWith("'") && value.endsWith("'")) value = value.slice(1, -1)
                                return { key: key.trim(), value, is_secret: bulkIsSecret, visible: !bulkIsSecret }
                              })
                              .filter(v => v.key)
                            setEnvVars([...envVars, ...newVars])
                            setBulkContent('')
                            setShowBulkImport(false)
                          }}
                          className="px-3 py-1.5 bg-accent-lime text-text-dark font-mono text-[11px] font-bold rounded"
                        >
                          Import
                        </button>
                      </div>
                    </div>
                  )}

                  {envVars.map((v, i) => (
                    <div key={i} className="flex gap-2 mb-2">
                      <input
                        value={v.key}
                        onChange={e => {
                          const updated = [...envVars]
                          updated[i].key = e.target.value
                          setEnvVars(updated)
                        }}
                        className="w-[35%] px-3 py-2 bg-bg-primary border border-border-dark rounded font-mono text-small text-text-primary"
                        placeholder="KEY"
                      />
                      <div className="flex-1 relative">
                        <input
                          type={v.visible ? 'text' : 'password'}
                          value={v.value}
                          onChange={e => {
                            const updated = [...envVars]
                            updated[i].value = e.target.value
                            setEnvVars(updated)
                          }}
                          className="w-full px-3 py-2 pr-8 bg-bg-primary border border-border-dark rounded font-mono text-small text-text-primary"
                          placeholder="VALUE"
                        />
                        <button
                          onClick={() => {
                            const updated = [...envVars]
                            updated[i].visible = !updated[i].visible
                            setEnvVars(updated)
                          }}
                          className="absolute right-2 top-1/2 -translate-y-1/2 text-text-secondary hover:text-text-primary"
                        >
                          {v.visible ? <Eye size={12} /> : <EyeOff size={12} />}
                        </button>
                      </div>
                      <button
                        onClick={() => {
                          const updated = [...envVars]
                          updated[i].is_secret = !updated[i].is_secret
                          setEnvVars(updated)
                        }}
                        className={`p-2 transition-colors ${v.is_secret ? 'text-accent-lime' : 'text-text-secondary hover:text-text-primary'}`}
                        title={v.is_secret ? 'Secret (encrypted)' : 'Not secret'}
                      >
                        <Lock size={14} />
                      </button>
                      <button
                        onClick={() => setEnvVars(envVars.filter((_, j) => j !== i))}
                        className="p-2 text-text-secondary hover:text-status-error transition-colors"
                      >
                        <Trash2 size={14} />
                      </button>
                    </div>
                  ))}
                </div>

                <button
                  onClick={handleDeploy}
                  className="w-full mt-2 px-6 py-4 bg-accent-lime text-text-dark font-mono font-bold text-body uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all hover:shadow-[0_0_20px_rgba(170,255,69,0.3)]"
                >
                  Pull &amp; Build &rarr;
                </button>
              </div>
            </div>

            {/* Build Log (shown during building) */}
            <div>
              {state === 'building' ? (
                <motion.div
                  initial={{ opacity: 0, y: 20 }}
                  animate={{ opacity: 1, y: 0 }}
                >
                  <div className="mb-4">
                    <BuildProgress currentPhase={buildPhase} />
                  </div>
                  <TerminalPanel lines={logs} streaming={state === 'building'} maxHeight="500px" />
                </motion.div>
              ) : (
                <div className="bg-bg-secondary rounded-card border border-border-dark p-12 flex flex-col items-center justify-center h-full text-center">
                  <Github size={48} className="text-border-dark mb-4" />
                  <p className="font-mono text-small text-text-secondary">
                    Enter a repository URL and click deploy to see the build log here.
                  </p>
                </div>
              )}
            </div>
          </div>
        )}
      </motion.div>
    </>
  )
}
