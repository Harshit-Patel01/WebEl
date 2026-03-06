'use client'

import { useState, useEffect } from 'react'
import { motion } from 'framer-motion'
import { Github, Plus, Trash2, CheckCircle2, Upload, Eye, EyeOff, Lock, XCircle, Folder, ExternalLink, Globe, Package, Activity, Terminal, RefreshCw } from 'lucide-react'
import Link from 'next/link'
import SectionBadge from '@/components/ui/SectionBadge'
import BuildProgress from '@/components/ui/BuildProgress'
import DeployLogStream from '@/components/ui/DeployLogStream'
import { deployApi, envApi, tunnelApi } from '@/lib/api'
import { useWebSocket } from '@/contexts/WebSocketContext'
import { apiKeyStorage } from '@/utils/apiKey'

type DeployState = 'form' | 'building' | 'success' | 'failed'
type ProjectType = 'frontend' | 'backend'

export default function DeployPage() {
  const [state, setState] = useState<DeployState>('form')
  const [projectType, setProjectType] = useState<ProjectType>('frontend')
  const [projectName, setProjectName] = useState('')
  const [repoUrl, setRepoUrl] = useState('')
  const [branch, setBranch] = useState('main')
  const [buildCmd, setBuildCmd] = useState('npm run build')
  const [installCmd, setInstallCmd] = useState('')
  const [startCmd, setStartCmd] = useState('')
  const [localPort, setLocalPort] = useState('')
  const [outputDir, setOutputDir] = useState('')
  const [workingDir, setWorkingDir] = useState('')
  const [envVars, setEnvVars] = useState<{ key: string; value: string; is_secret: boolean; visible: boolean }[]>([])
  const [showBulkImport, setShowBulkImport] = useState(false)
  const [bulkContent, setBulkContent] = useState('')
  const [bulkIsSecret, setBulkIsSecret] = useState(false)
  const [buildPhase, setBuildPhase] = useState(0)
  const [currentDeployId, setCurrentDeployId] = useState<string | null>(null)
  const [deployResult, setDeployResult] = useState<{
    status: string
    projectId?: string
    projectName?: string
    deployId?: string
    outputPath?: string
    framework?: string
    isBackend?: boolean
    buildDuration?: number
  }>({status: ''})
  const [error, setError] = useState('')
  const [deploying, setDeploying] = useState(false)

  // Domain configuration
  const [domain, setDomain] = useState('')
  const [subdomain, setSubdomain] = useState('')
  const [manualDomain, setManualDomain] = useState(false)
  const [enableNginx, setEnableNginx] = useState(true)
  const [cloudflareZones, setCloudflareZones] = useState<{ id: string; name: string }[]>([])
  const [selectedZoneId, setSelectedZoneId] = useState('')
  const [loadingZones, setLoadingZones] = useState(false)

  const { subscribe } = useWebSocket()

  // Load Cloudflare zones on mount
  useEffect(() => {
    const loadZones = async () => {
      const apiKey = apiKeyStorage.get()
      if (!apiKey) return

      setLoadingZones(true)
      try {
        const zones = await tunnelApi.getStoredZones(apiKey)
        setCloudflareZones(zones)
      } catch (err) {
        console.error('Failed to load Cloudflare zones:', err)
      } finally {
        setLoadingZones(false)
      }
    }

    loadZones()
  }, [])

  // Auto-detect project name from repo URL
  useEffect(() => {
    if (repoUrl && !projectName) {
      const match = repoUrl.match(/\/([^/]+?)(?:\.git)?$/)
      if (match) {
        setProjectName(match[1])
      }
    }
  }, [repoUrl, projectName])

  // Update defaults when project type changes
  useEffect(() => {
    if (projectType === 'backend') {
      setBuildCmd('')
      setOutputDir('')
    } else {
      setBuildCmd('npm run build')
      setOutputDir('')
    }
  }, [projectType])

  // Subscribe to phase updates via WS for build progress timeline
  useEffect(() => {
    const unsubProgress = subscribe('progress', (message) => {
      const phaseMap: Record<string, number> = {
        clone: 0, detect: 1, build: 2, service: 3, done: 4,
        install: 2,
      }
      if (message.phase && phaseMap[message.phase] !== undefined) {
        setBuildPhase(phaseMap[message.phase])
      }
    })

    return () => {
      unsubProgress()
    }
  }, [subscribe])

  const handleDeploy = async () => {
    if (!repoUrl) {
      setError('Repository URL is required')
      return
    }
    if (!projectName) {
      setError('Project name is required')
      return
    }

    setError('')
    setState('building')
    setBuildPhase(0)
    setDeploying(true)

    try {
      // Convert env vars to object
      const envObj: Record<string, string> = {}
      envVars.forEach(v => {
        if (v.key) envObj[v.key] = v.value
      })

      // 1. Create project
      const response = await fetch('/api/v1/projects', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: projectName,
          repo_url: repoUrl,
          branch,
          project_type: projectType === 'backend' ? 'python' : 'node',
          build_command: buildCmd,
          install_command: installCmd,
          start_command: startCmd,
          output_dir: outputDir,
          working_directory: workingDir,
          local_port: localPort ? parseInt(localPort) : 0,
          env_vars: JSON.stringify(envObj),
        }),
      })

      if (!response.ok) {
        const err = await response.json()
        if (response.status === 409) {
          setError(err.error || 'A project with this repository and branch already exists. Use the Deployments page to redeploy.')
          setState('form')
          setDeploying(false)
          return
        }
        throw new Error(err.error || 'Failed to create project')
      }

      const project = await response.json()

      // 2. Save env vars
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
      const deployOptions: any = {}
      if (domain || subdomain) {
        // Construct full domain with subdomain if provided
        const fullDomain = subdomain ? `${subdomain}.${domain}` : domain
        deployOptions.domain = fullDomain
        deployOptions.enable_nginx = enableNginx
        deployOptions.manual_domain = manualDomain
        if (selectedZoneId) {
          deployOptions.zone_id = selectedZoneId
        }
      }

      const res = await deployApi.triggerDeploy(project.id, Object.keys(deployOptions).length > 0 ? deployOptions : undefined)

      if (res.deploy_id) {
        setCurrentDeployId(res.deploy_id)
        setDeployResult(prev => ({
          ...prev,
          projectId: project.id,
          projectName: project.name,
          deployId: res.deploy_id,
        }))
      }
    } catch (err: any) {
      setError(`Failed to start deployment: ${err.message || err}`)
      setState('form')
      setDeploying(false)
    }
  }

  const handleDeployComplete = (result: {
    status: string
    outputPath?: string
    framework?: string
    isBackend?: boolean
    buildDuration?: number
  }) => {
    setDeploying(false)
    setBuildPhase(4)
    setDeployResult(prev => ({
      ...prev,
      ...result,
    }))
    if (result.status === 'success') {
      setState('success')
    } else {
      setState('failed')
    }
  }

  const resetForm = () => {
    setState('form')
    setCurrentDeployId(null)
    setDeployResult({status: ''})
    setBuildPhase(0)
    setError('')
    setDeploying(false)
  }

  return (
    <>
      <motion.div
        initial={{ opacity: 0, x: 20 }}
        animate={{ opacity: 1, x: 0 }}
        transition={{ duration: 0.3 }}
      >
        <div className="mb-8">
          <SectionBadge label="DEPLOY PROJECT" />
          <p className="mt-2 text-sm text-text-secondary font-mono">
            Clone, build, and deploy
          </p>
        </div>

        {/* Success state */}
        {state === 'success' && (
          <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            className="bg-bg-secondary rounded-card border-2 border-accent-lime p-8 mb-6"
          >
            <div className="flex items-center gap-3 mb-6">
              <CheckCircle2 size={24} className="text-accent-lime" />
              <h2 className="font-serif text-h2 text-accent-lime">Deployment Successful</h2>
            </div>
            <div className="font-mono text-small space-y-2 mb-6">
              {deployResult.projectName && (
                <p>
                  <span className="text-text-secondary">Project: </span>
                  <span className="text-text-primary">{deployResult.projectName}</span>
                </p>
              )}
              {deployResult.framework && (
                <p>
                  <span className="text-text-secondary">Framework: </span>
                  <span className="text-text-primary">{deployResult.framework}</span>
                </p>
              )}
              {deployResult.buildDuration && (
                <p>
                  <span className="text-text-secondary">Build time: </span>
                  <span className="text-text-primary">{deployResult.buildDuration.toFixed(1)}s</span>
                </p>
              )}
              {deployResult.outputPath && !deployResult.isBackend && (
                <div className="mt-4 p-3 bg-bg-primary rounded-lg border border-accent-lime/30">
                  <div className="flex items-center gap-2 mb-1">
                    <Folder size={14} className="text-accent-lime" />
                    <span className="text-text-secondary text-xs">Nginx serve directory:</span>
                  </div>
                  <code className="text-accent-lime text-sm break-all">{deployResult.outputPath}</code>
                  <p className="text-text-secondary text-[10px] mt-2">
                    Point your nginx server block root to this directory to serve the site.
                  </p>
                </div>
              )}
              {deployResult.isBackend && (
                <p>
                  <span className="text-text-secondary">Type: </span>
                  <span className="text-text-primary">Backend service (running)</span>
                </p>
              )}
            </div>
            <div className="flex gap-3 flex-wrap">
              <Link
                href="/deployments"
                className="inline-flex items-center gap-2 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all"
              >
                View Deployments &rarr;
              </Link>
              {!deployResult.isBackend && (
                <Link
                  href="/nginx"
                  className="inline-flex items-center gap-2 px-6 py-3 bg-bg-primary text-text-primary border border-border-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:border-accent-lime hover:text-accent-lime transition-all"
                >
                  <ExternalLink size={14} /> Configure Domain
                </Link>
              )}
              <button
                onClick={resetForm}
                className="inline-flex items-center gap-2 px-6 py-3 bg-bg-primary text-text-primary border border-border-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:border-text-primary transition-all"
              >
                Deploy Another
              </button>
            </div>

            {/* Show build logs below */}
            {currentDeployId && (
              <div className="mt-6">
                <BuildProgress currentPhase={4} />
                <div className="mt-4">
                  <DeployLogStream deployId={currentDeployId} maxHeight="300px" />
                </div>
              </div>
            )}
          </motion.div>
        )}

        {/* Failed state */}
        {state === 'failed' && (
          <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            className="bg-bg-secondary rounded-card border-2 border-red-500/50 p-8 mb-6"
          >
            <div className="flex items-center gap-3 mb-6">
              <XCircle size={24} className="text-red-400" />
              <h2 className="font-serif text-h2 text-red-400">Deployment Failed</h2>
            </div>
            <p className="text-text-secondary font-mono text-sm mb-4">
              The build encountered errors. Check the logs below for details.
            </p>
            <div className="flex gap-3">
              <button
                onClick={resetForm}
                className="inline-flex items-center gap-2 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all"
              >
                Try Again
              </button>
              <Link
                href="/deployments"
                className="inline-flex items-center gap-2 px-6 py-3 bg-bg-primary text-text-primary border border-border-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:border-text-primary transition-all"
              >
                View Deployments
              </Link>
            </div>

            {currentDeployId && (
              <div className="mt-6">
                <BuildProgress currentPhase={buildPhase} />
                <div className="mt-4">
                  <DeployLogStream deployId={currentDeployId} maxHeight="400px" />
                </div>
              </div>
            )}
          </motion.div>
        )}

        {/* Form + Build state */}
        {(state === 'form' || state === 'building') && (
          <div className="space-y-6">
            {/* Form */}
            <div className="bg-bg-secondary rounded-card border border-border-dark overflow-hidden">
              {/* Header */}
              <div className="px-8 py-6 border-b border-border-dark bg-bg-primary/30">
                <h2 className="font-serif text-h2 mb-2">Deploy from GitHub</h2>
                <p className="font-mono text-[11px] text-text-secondary">
                  Configure your project settings and deployment options
                </p>
              </div>

              {error && (
                <div className="mx-8 mt-6 p-4 bg-red-900/20 border border-red-800/50 rounded-lg text-red-400 font-mono text-xs flex items-start gap-2">
                  <XCircle size={16} className="flex-shrink-0 mt-0.5" />
                  <span>{error}</span>
                </div>
              )}

              <div className="p-8 space-y-8">
                {/* Project Type Section */}
                <div>
                  <div className="flex items-center gap-2 mb-4">
                    <Package size={16} className="text-accent-lime" />
                    <h3 className="font-mono text-[12px] uppercase tracking-wider text-text-primary font-bold">
                      Project Type
                    </h3>
                  </div>
                  <div className="flex gap-2 bg-bg-primary rounded-lg p-1.5">
                    {(['frontend', 'backend'] as ProjectType[]).map(type => (
                      <button
                        key={type}
                        onClick={() => setProjectType(type)}
                        disabled={deploying}
                        className={`flex-1 px-6 py-3 rounded-md font-mono text-small uppercase tracking-wider transition-all ${
                          projectType === type
                            ? 'bg-accent-lime text-text-dark font-bold shadow-lg'
                            : 'text-text-secondary hover:text-text-primary hover:bg-bg-secondary'
                        }`}
                      >
                        {type}
                      </button>
                    ))}
                  </div>
                  <p className="mt-2 font-mono text-[10px] text-text-secondary">
                    {projectType === 'frontend'
                      ? 'Static sites, React, Vue, Next.js, etc. Will be served via nginx.'
                      : 'Node.js, Python, Go backends. Will run in Docker containers with port mapping.'}
                  </p>
                </div>

                {/* Repository Section */}
                <div className="border-t border-border-dark pt-6">
                  <div className="flex items-center gap-2 mb-4">
                    <Github size={16} className="text-accent-lime" />
                    <h3 className="font-mono text-[12px] uppercase tracking-wider text-text-primary font-bold">
                      Repository
                    </h3>
                  </div>
                  <div className="space-y-4">
                    <div>
                      <label className="block font-mono text-[11px] text-text-secondary mb-2">
                        Project Name <span className="text-red-400">*</span>
                      </label>
                      <input
                        value={projectName}
                        onChange={e => setProjectName(e.target.value)}
                        disabled={deploying}
                        className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary placeholder:text-text-secondary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                        placeholder="my-awesome-app"
                      />
                      <p className="mt-1.5 font-mono text-[10px] text-text-secondary">
                        Auto-fills from repository URL
                      </p>
                    </div>

                    <div>
                      <label className="block font-mono text-[11px] text-text-secondary mb-2">
                        Repository URL <span className="text-red-400">*</span>
                      </label>
                      <div className="relative">
                        <Github size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-text-secondary" />
                        <input
                          value={repoUrl}
                          onChange={e => setRepoUrl(e.target.value)}
                          disabled={deploying}
                          className="w-full pl-10 pr-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary placeholder:text-text-secondary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                          placeholder="https://github.com/user/repo"
                        />
                      </div>
                    </div>

                    <div>
                      <label className="block font-mono text-[11px] text-text-secondary mb-2">
                        Branch
                      </label>
                      <input
                        value={branch}
                        onChange={e => setBranch(e.target.value)}
                        disabled={deploying}
                        className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                        placeholder="main"
                        list="branch-suggestions"
                      />
                      <datalist id="branch-suggestions">
                        <option value="main" />
                        <option value="master" />
                        <option value="develop" />
                        <option value="staging" />
                      </datalist>
                    </div>

                    <div>
                      <label className="block font-mono text-[11px] text-text-secondary mb-2">
                        Working Directory
                      </label>
                      <input
                        value={workingDir}
                        value={workingDir}
                        onChange={e => setWorkingDir(e.target.value)}
                        disabled={deploying}
                        className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                        placeholder="(root)"
                      />
                      <p className="mt-1.5 font-mono text-[10px] text-text-secondary">
                        Subfolder containing your project (e.g., &quot;frontend&quot;, &quot;app&quot;)
                      </p>
                    </div>
                  </div>
                </div>

                {/* Build Settings Section */}
                <div className="border-t border-border-dark pt-6">
                  <div className="flex items-center gap-2 mb-4">
                    <Activity size={16} className="text-accent-lime" />
                    <h3 className="font-mono text-[12px] uppercase tracking-wider text-text-primary font-bold">
                      Build Settings
                    </h3>
                  </div>
                  <div className="space-y-4">
                    {projectType === 'frontend' && (
                      <>
                        <div>
                          <label className="block font-mono text-[11px] text-text-secondary mb-2">
                            Build Command
                          </label>
                          <input
                            value={buildCmd}
                            onChange={e => setBuildCmd(e.target.value)}
                            disabled={deploying}
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                            placeholder="npm run build"
                          />
                        </div>
                        <div>
                          <label className="block font-mono text-[11px] text-text-secondary mb-2">
                            Output Directory
                          </label>
                          <input
                            value={outputDir}
                            onChange={e => setOutputDir(e.target.value)}
                            disabled={deploying}
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                            placeholder="Auto-detect (dist, build, out)"
                          />
                          <p className="mt-1.5 font-mono text-[10px] text-text-secondary">
                            Leave blank to auto-detect
                          </p>
                        </div>
                      </>
                    )}

                    <div>
                      <label className="block font-mono text-[11px] text-text-secondary mb-2">
                        Install Command (Optional)
                      </label>
                      <input
                        value={installCmd}
                        onChange={e => setInstallCmd(e.target.value)}
                        disabled={deploying}
                        className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                        placeholder="npm install"
                      />
                    </div>

                    {projectType === 'backend' && (
                      <>
                        <div>
                          <label className="block font-mono text-[11px] text-text-secondary mb-2">
                            Start Command
                          </label>
                          <input
                            value={startCmd}
                            onChange={e => setStartCmd(e.target.value)}
                            disabled={deploying}
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                            placeholder="npm start"
                          />
                        </div>
                        <div>
                          <label className="block font-mono text-[11px] text-text-secondary mb-2">
                            Container Port
                          </label>
                          <input
                            value={localPort}
                            onChange={e => setLocalPort(e.target.value)}
                            disabled={deploying}
                            type="number"
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                            placeholder="3000"
                          />
                          <p className="mt-1.5 font-mono text-[10px] text-text-secondary">
                            Internal port your app listens on (host port auto-allocated from pool)
                          </p>
                        </div>
                      </>
                    )}
                  </div>
                </div>

                {/* Domain Configuration */}
                <div className="border-t border-border-dark pt-6">
                  <div className="flex items-center gap-2 mb-4">
                    <Globe size={16} className="text-accent-lime" />
                    <label className="font-mono text-label uppercase tracking-wider text-text-secondary">
                      Domain Configuration (Optional)
                    </label>
                  </div>

                  {cloudflareZones.length > 0 && !manualDomain ? (
                    <div className="space-y-3">
                      <div>
                        <label className="block font-mono text-[11px] text-text-secondary mb-2">
                          Select Cloudflare Domain
                        </label>
                        <select
                          value={selectedZoneId}
                          onChange={(e) => {
                            setSelectedZoneId(e.target.value)
                            const zone = cloudflareZones.find(z => z.id === e.target.value)
                            if (zone) {
                              setDomain(zone.name)
                              setSubdomain('')
                            } else {
                              setDomain('')
                              setSubdomain('')
                            }
                          }}
                          disabled={deploying}
                          className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary disabled:opacity-50"
                        >
                          <option value="">-- No domain (skip nginx) --</option>
                          {cloudflareZones.map(zone => (
                            <option key={zone.id} value={zone.id}>{zone.name}</option>
                          ))}
                        </select>
                      </div>

                      {selectedZoneId && (
                        <div>
                          <label className="block font-mono text-[11px] text-text-secondary mb-2">
                            Subdomain (Optional)
                          </label>
                          <div className="flex items-center gap-2">
                            <input
                              value={subdomain}
                              onChange={(e) => setSubdomain(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))}
                              disabled={deploying}
                              className="flex-1 px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary placeholder:text-text-secondary disabled:opacity-50"
                              placeholder="project-1"
                            />
                            <span className="font-mono text-small text-text-secondary">.{domain}</span>
                          </div>
                          <p className="mt-1 font-mono text-[10px] text-text-secondary">
                            Leave blank to deploy on root domain ({domain})
                          </p>
                        </div>
                      )}

                      <button
                        onClick={() => setManualDomain(true)}
                        disabled={deploying}
                        className="font-mono text-[11px] text-text-secondary hover:text-accent-lime transition-colors disabled:opacity-50"
                      >
                        Or enter domain manually
                      </button>
                    </div>
                  ) : (
                    <div className="space-y-3">
                      <div>
                        <label className="block font-mono text-[11px] text-text-secondary mb-2">
                          Domain Name
                        </label>
                        <input
                          value={domain}
                          onChange={(e) => setDomain(e.target.value)}
                          disabled={deploying}
                          className="w-full px-4 py-3 bg-bg-primary border border-border-dark rounded-lg font-mono text-small text-text-primary placeholder:text-text-secondary disabled:opacity-50"
                          placeholder="example.com or subdomain.example.com"
                        />
                      </div>
                      {cloudflareZones.length > 0 && (
                        <button
                          onClick={() => {
                            setManualDomain(false)
                            setDomain('')
                            setSubdomain('')
                          }}
                          disabled={deploying}
                          className="font-mono text-[11px] text-text-secondary hover:text-accent-lime transition-colors disabled:opacity-50"
                        >
                          Back to Cloudflare domains
                        </button>
                      )}
                    </div>
                  )}

                  {domain && (
                    <div className="mt-3">
                      <label className="flex items-center gap-2 font-mono text-[11px] text-text-secondary">
                        <input
                          type="checkbox"
                          checked={enableNginx}
                          onChange={(e) => setEnableNginx(e.target.checked)}
                          disabled={deploying}
                          className="accent-accent-lime"
                        />
                        Auto-configure nginx for this domain
                      </label>
                      <p className="mt-2 font-mono text-[10px] text-text-secondary">
                        {subdomain ? (
                          <>Nginx will route {subdomain}.{domain} (/ to frontend, /api and /ws to backend if applicable)</>
                        ) : (
                          <>Nginx will route / to frontend and /api, /ws to backend (if applicable)</>
                        )}
                      </p>
                    </div>
                  )}

                  {loadingZones && (
                    <p className="mt-2 font-mono text-[10px] text-text-secondary animate-pulse">
                      Loading Cloudflare zones...
                    </p>
                  )}
                </div>

                {/* Environment Variables */}
                <div>
                  <div className="flex items-center justify-between mb-2">
                    <label className="font-mono text-label uppercase tracking-wider text-text-secondary">
                      Environment Variables
                    </label>
                    <div className="flex items-center gap-2">
                      <button
                        onClick={() => setShowBulkImport(!showBulkImport)}
                        disabled={deploying}
                        className="inline-flex items-center gap-1 font-mono text-label text-text-secondary hover:text-accent-lime transition-colors disabled:opacity-50"
                      >
                        <Upload size={12} /> Bulk
                      </button>
                      <button
                        onClick={() => setEnvVars([...envVars, { key: '', value: '', is_secret: false, visible: true }])}
                        disabled={deploying}
                        className="inline-flex items-center gap-1 font-mono text-label text-accent-lime hover:text-accent-lime-muted transition-colors disabled:opacity-50"
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
                        disabled={deploying}
                        className="w-[35%] px-3 py-2 bg-bg-primary border border-border-dark rounded font-mono text-small text-text-primary disabled:opacity-50"
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
                          disabled={deploying}
                          className="w-full px-3 py-2 pr-8 bg-bg-primary border border-border-dark rounded font-mono text-small text-text-primary disabled:opacity-50"
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
                        disabled={deploying}
                        className={`p-2 transition-colors ${v.is_secret ? 'text-accent-lime' : 'text-text-secondary hover:text-text-primary'}`}
                        title={v.is_secret ? 'Secret (encrypted)' : 'Not secret'}
                      >
                        <Lock size={14} />
                      </button>
                      <button
                        onClick={() => setEnvVars(envVars.filter((_, j) => j !== i))}
                        disabled={deploying}
                        className="p-2 text-text-secondary hover:text-status-error transition-colors disabled:opacity-50"
                      >
                        <Trash2 size={14} />
                      </button>
                    </div>
                  ))}
                </div>

                {/* Deploy Button */}
                <div className="border-t border-border-dark pt-6">
                  <button
                    onClick={handleDeploy}
                    disabled={deploying || !repoUrl || !projectName}
                    className="w-full px-6 py-4 bg-accent-lime text-text-dark font-mono font-bold text-body uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all hover:shadow-[0_0_20px_rgba(170,255,69,0.3)] disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2"
                  >
                    {deploying ? (
                      <>
                        <RefreshCw size={16} className="animate-spin" />
                        Deploying...
                      </>
                    ) : (
                      <>
                        <Upload size={16} />
                        Deploy Project →
                      </>
                    )}
                  </button>
                  <p className="mt-3 text-center font-mono text-[10px] text-text-secondary">
                    Your project will be cloned, built, and deployed automatically
                  </p>
                </div>
              </div>
            </div>

            {/* Build Log Panel */}
            {state === 'building' && currentDeployId && (
              <motion.div
                initial={{ opacity: 0, y: 20 }}
                animate={{ opacity: 1, y: 0 }}
                className="bg-bg-secondary rounded-card border border-border-dark overflow-hidden"
              >
                <div className="px-8 py-6 border-b border-border-dark bg-bg-primary/30">
                  <div className="flex items-center gap-2">
                    <Terminal size={16} className="text-accent-lime" />
                    <h3 className="font-mono text-[12px] uppercase tracking-wider text-text-primary font-bold">
                      Build Output
                    </h3>
                  </div>
                </div>
                <div className="p-6">
                  <div className="mb-4">
                    <BuildProgress currentPhase={buildPhase} />
                  </div>
                  <DeployLogStream
                    deployId={currentDeployId}
                    onComplete={handleDeployComplete}
                    maxHeight="600px"
                  />
                </div>
              </motion.div>
            )}
          </div>
        )}
      </motion.div>
    </>
  )
}
