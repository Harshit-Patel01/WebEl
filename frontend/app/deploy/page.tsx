'use client'

import { useState, useEffect } from 'react'
import { motion } from 'framer-motion'
import { Github, Plus, Trash2, CheckCircle2, Upload, Eye, EyeOff, Lock, XCircle, Folder, ExternalLink, Globe, Package, Activity, Terminal, RefreshCw, Info } from 'lucide-react'
import Link from 'next/link'
import SectionBadge from '@/components/ui/SectionBadge'
import BuildProgress from '@/components/ui/BuildProgress'
import DeployLogStream from '@/components/ui/DeployLogStream'
import { deployApi, envApi, tunnelApi } from '@/lib/api'
import { useWebSocket } from '@/contexts/WebSocketContext'
import { apiKeyStorage } from '@/utils/apiKey'

type DeployState = 'form' | 'building' | 'success' | 'failed'
type ProjectType = 'web_service' | 'app_service' | 'full_stack'

export default function DeployPage() {
  const [state, setState] = useState<DeployState>('form')
  const [projectType, setProjectType] = useState<ProjectType>('web_service')
  const [projectName, setProjectName] = useState('')
  const [repoUrl, setRepoUrl] = useState('')
  const [branch, setBranch] = useState('main')
  const [buildCmd, setBuildCmd] = useState('npm run build')
  const [installCmd, setInstallCmd] = useState('')
  const [startCmd, setStartCmd] = useState('')
  const [localPort, setLocalPort] = useState('')
  const [outputDir, setOutputDir] = useState('')
  const [workingDir, setWorkingDir] = useState('')
  const [frontendWorkingDir, setFrontendWorkingDir] = useState('')
  const [backendWorkingDir, setBackendWorkingDir] = useState('')
  const [frontendBuildCmd, setFrontendBuildCmd] = useState('build')
  const [frontendOutputDir, setFrontendOutputDir] = useState('')
  const [frontendInstallCmd, setFrontendInstallCmd] = useState('')
  const [backendInstallCmd, setBackendInstallCmd] = useState('')
  const [backendBuildCmd, setBackendBuildCmd] = useState('')
  const [envVars, setEnvVars] = useState<{ key: string; value: string; is_secret: boolean; visible: boolean }[]>([])
  // Separate env vars for full-stack deployments
  const [frontendEnvVars, setFrontendEnvVars] = useState<{ key: string; value: string; is_secret: boolean; visible: boolean }[]>([])
  const [backendEnvVars, setBackendEnvVars] = useState<{ key: string; value: string; is_secret: boolean; visible: boolean }[]>([])
  const [showBulkImport, setShowBulkImport] = useState(false)
  const [bulkContent, setBulkContent] = useState('')
  const [bulkIsSecret, setBulkIsSecret] = useState(false)
  const [showFrontendBulkImport, setShowFrontendBulkImport] = useState(false)
  const [frontendBulkContent, setFrontendBulkContent] = useState('')
  const [frontendBulkIsSecret, setFrontendBulkIsSecret] = useState(false)
  const [showBackendBulkImport, setShowBackendBulkImport] = useState(false)
  const [backendBulkContent, setBackendBulkContent] = useState('')
  const [backendBulkIsSecret, setBackendBulkIsSecret] = useState(false)
  const [backendBulkContent, setBackendBulkContent] = useState('')
  const [backendBulkIsSecret, setBackendBulkIsSecret] = useState(false)
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
  const [deploymentTarget, setDeploymentTarget] = useState<'local' | 'internet'>('local')
  const [backendPort, setBackendPort] = useState('')
  const [hasTunnelSetup, setHasTunnelSetup] = useState(false)
  const [checkingTunnel, setCheckingTunnel] = useState(true)
  const [availableProjects, setAvailableProjects] = useState<any[]>([])
  const [attachToProjectId, setAttachToProjectId] = useState('')

  const { subscribe, send, connectionStatus } = useWebSocket()

  // Load projects for attachment
  useEffect(() => {
    if (projectType === 'app_service') {
      deployApi.listProjects().then(projects => {
        // Filter for web_service projects that have a domain set
        setAvailableProjects(projects.filter((p: any) =>
          p.project_type === 'web_service' && p.domain
        ))
      }).catch(console.error)
    }
  }, [projectType])

  // Subscribe to deploy updates when deployId becomes available and WS is connected
  useEffect(() => {
    if (currentDeployId && connectionStatus === 'connected') {
      // Subscribe to receive messages for this deploy
      send({ type: 'subscribe_deploy', deployId: currentDeployId })
    }
  }, [currentDeployId, connectionStatus, send])

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

    const checkTunnel = async () => {
      setCheckingTunnel(true)
      try {
        const apiKey = apiKeyStorage.get()
        if (!apiKey) {
          setHasTunnelSetup(false)
          setCheckingTunnel(false)
          return
        }

        const status = await fetch('/api/v1/tunnel/status', {
          credentials: 'include'
        }).then(r => r.json())

        setHasTunnelSetup(status.status !== 'not_configured')
      } catch (err) {
        setHasTunnelSetup(false)
      } finally {
        setCheckingTunnel(false)
      }
    }

    loadZones()
    checkTunnel()
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
    if (projectType === 'app_service') {
      setBuildCmd('')
      setOutputDir('')
      setBackendPort('8000')
    } else if (projectType === 'web_service') {
      setBuildCmd('build')
      setOutputDir('')
      setBackendPort('')
    } else if (projectType === 'full_stack') {
      setFrontendBuildCmd('build')
      setFrontendOutputDir('')
      setBuildCmd('')
      setBackendPort('8000')
    }
  }, [projectType])

  // Subscribe to phase updates via WS for build progress timeline
  useEffect(() => {
    // Only subscribe once we're connected and have a deployId
    if (connectionStatus !== 'connected' || !currentDeployId) {
      return
    }

    const unsubProgress = subscribe('progress', (message) => {
      // Filter out messages not for our current deploy
      if (message.jobId && message.jobId !== currentDeployId) {
        return
      }
      const phaseMap: Record<string, number> = {
        clone: 0, detect: 1, build: 2, service: 3, done: 4,
        install: 2, // Map install to build phase
      }
      if (message.phase && phaseMap[message.phase] !== undefined) {
        const newPhase = phaseMap[message.phase]
        // Only update if the new phase is greater than or equal to current phase
        // This prevents the timeline from going backwards
        setBuildPhase(prev => Math.max(prev, newPhase))
      }
    })

    return () => {
      unsubProgress()
    }
  }, [subscribe, currentDeployId, connectionStatus])

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
          project_type: projectType === 'app_service' ? 'python' : projectType === 'full_stack' ? 'fullstack' : 'node',
          build_command: projectType === 'full_stack' ? frontendBuildCmd : buildCmd,
          install_command: projectType === 'full_stack' ? frontendInstallCmd : installCmd,
          start_command: startCmd,
          output_dir: projectType === 'full_stack' ? frontendOutputDir : outputDir,
          working_directory: projectType === 'full_stack' ? frontendWorkingDir : workingDir,
          backend_working_directory: projectType === 'full_stack' ? backendWorkingDir : '',
          backend_install_command: projectType === 'full_stack' ? backendInstallCmd : '',
          backend_build_command: projectType === 'full_stack' ? backendBuildCmd : '',
          local_port: localPort ? parseInt(localPort) : (backendPort ? parseInt(backendPort) : 0),
          domain: domain || subdomain ? (subdomain ? `${subdomain}.${domain}` : domain) : '',
          deployment_target: deploymentTarget,
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

      // 2. Save env vars based on project type
      if (projectType === 'full_stack') {
        // Save frontend env vars with FRONTEND_ prefix
        for (const v of frontendEnvVars) {
          if (v.key) {
            await envApi.create(project.id, {
              key: `FRONTEND_${v.key}`,
              value: v.value,
              is_secret: v.is_secret,
            })
          }
        }
        // Save backend env vars with BACKEND_ prefix
        for (const v of backendEnvVars) {
          if (v.key) {
            await envApi.create(project.id, {
              key: `BACKEND_${v.key}`,
              value: v.value,
              is_secret: v.is_secret,
            })
          }
        }
      } else {
        // Save single set of env vars for web_service and app_service
        for (const v of envVars) {
          if (v.key) {
            await envApi.create(project.id, {
              key: v.key,
              value: v.value,
              is_secret: v.is_secret,
            })
          }
        }
      }

      // 3. Trigger deploy
      const deployOptions: any = {}

      if (attachToProjectId) {
        deployOptions.attach_to_project_id = attachToProjectId
      } else if (domain || subdomain) {
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

  const handleDeployComplete = async (result: {
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
      // Create tunnel route if internet deployment is selected
      if (deploymentTarget === 'internet' && hasTunnelSetup && domain && selectedZoneId) {
        try {
          const apiKey = apiKeyStorage.get()
          if (apiKey) {
            const fullDomain = subdomain ? `${subdomain}.${domain}` : domain

            // For Cloudflare Tunnel, always route to nginx (port 80)
            // Nginx will handle the proxy to Docker containers
            const tunnelPort = 80

            await fetch('/api/v1/tunnel/routes', {
              method: 'POST',
              headers: {
                'Content-Type': 'application/json',
                'X-CF-API-Key': apiKey
              },
              credentials: 'include',
              body: JSON.stringify({
                hostname: fullDomain,
                zone_id: selectedZoneId,
                local_scheme: 'http',
                local_port: tunnelPort
              })
            })
          }
        } catch (err) {
          console.error('Failed to create tunnel route:', err)
        }
      }

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
            className="bg-bg-secondary  border-2 border-accent-lime p-8 mb-6"
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
              {deploymentTarget === 'internet' && domain && (
                <div className="mt-4 p-3 bg-blue-900/20 border border-blue-800/50 ">
                  <div className="flex items-center gap-2 mb-1">
                    <Globe size={14} className="text-blue-400" />
                    <span className="text-text-secondary text-xs">Cloudflare Tunnel:</span>
                  </div>
                  <a
                    href={`https://${subdomain ? `${subdomain}.${domain}` : domain}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-blue-400 text-sm break-all hover:underline flex items-center gap-1"
                  >
                    https://{subdomain ? `${subdomain}.${domain}` : domain}
                    <ExternalLink size={12} />
                  </a>
                  <p className="text-text-secondary text-[10px] mt-2">
                    Your app is now accessible via Cloudflare Tunnel with automatic HTTPS
                  </p>
                </div>
              )}
              {deployResult.outputPath && !deployResult.isBackend && (
                <div className="mt-4 p-3 bg-bg-primary  border border-accent-lime/30">
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
                className="inline-flex items-center gap-2 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider  hover:bg-accent-lime-muted transition-all"
              >
                View Deployments &rarr;
              </Link>
              {!deployResult.isBackend && (
                <Link
                  href="/nginx"
                  className="inline-flex items-center gap-2 px-6 py-3 bg-bg-primary text-text-primary border border-border-dark font-mono font-bold text-small uppercase tracking-wider  hover:border-accent-lime hover:text-accent-lime transition-all"
                >
                  <ExternalLink size={14} /> Configure Domain
                </Link>
              )}
              <button
                onClick={resetForm}
                className="inline-flex items-center gap-2 px-6 py-3 bg-bg-primary text-text-primary border border-border-dark font-mono font-bold text-small uppercase tracking-wider  hover:border-text-primary transition-all"
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
            className="bg-bg-secondary  border-2 border-red-500/50 p-8 mb-6"
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
                className="inline-flex items-center gap-2 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider  hover:bg-accent-lime-muted transition-all"
              >
                Try Again
              </button>
              <Link
                href="/deployments"
                className="inline-flex items-center gap-2 px-6 py-3 bg-bg-primary text-text-primary border border-border-dark font-mono font-bold text-small uppercase tracking-wider  hover:border-text-primary transition-all"
              >
                View Deployments
              </Link>
            </div>

            {currentDeployId && (
              <div className="mt-6">
                <BuildProgress
                  currentPhase={buildPhase}
                  isBackend={projectType === 'backend'}
                  isFullStack={projectType === 'fullstack'}
                />
                <div className="mt-4">
                  <DeployLogStream deployId={currentDeployId} maxHeight="400px" />
                </div>
              </div>
            )}
          </motion.div>
        )}

        {/* Form state - Full screen */}
        {state === 'form' && !currentDeployId && (
          <div className="mx-auto px-4 px-md-0">
            <div className="bg-bg-secondary border border-border-dark overflow-hidden">
            {/* Form */}
            <div>
              {/* Header */}
              <div className="px-8 py-6 border-b border-border-dark bg-bg-primary/30">
                <h2 className="font-serif text-h2 mb-2">Deploy from GitHub</h2>
                <p className="font-mono text-[11px] text-text-secondary">
                  Configure your project settings and deployment options
                </p>
              </div>

              {error && (
                <div className="mx-8 mt-6 p-4 bg-red-900/20 border border-red-800/50  text-red-400 font-mono text-xs flex items-start gap-2">
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
                  <div className="flex flex-col sm:flex-row gap-2 bg-bg-primary p-1.5">
                    {(['web_service', 'app_service', 'full_stack'] as ProjectType[]).map(type => (
                      <button
                        key={type}
                        onClick={() => setProjectType(type)}
                        disabled={deploying}
                        className={`flex-1 px-4 sm:px-6 py-3 font-mono text-[10px] sm:text-small uppercase tracking-wider transition-all ${
                          projectType === type
                            ? 'bg-accent-lime text-text-dark font-bold shadow-lg'
                            : 'text-text-secondary hover:text-text-primary hover:bg-bg-secondary'
                        }`}
                      >
                        {type === 'web_service' ? 'Web Service' : type === 'app_service' ? 'App Service' : 'Full Stack'}
                      </button>
                    ))}
                  </div>
                  <p className="mt-2 font-mono text-[10px] text-text-secondary">
                    {projectType === 'web_service'
                      ? 'Static sites, React, Vue, Next.js, etc. Served via nginx or containerized with serve.'
                      : projectType === 'app_service'
                      ? 'Node.js, Python, Go backends. Run in Docker containers with port mapping.'
                      : 'Combined frontend + backend deployment with nginx proxy configuration.'}
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
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <div>
                      <label className="block font-mono text-[11px] text-text-secondary mb-2">
                        Project Name <span className="text-red-400">*</span>
                      </label>
                      <input
                        value={projectName}
                        onChange={e => setProjectName(e.target.value)}
                        disabled={deploying}
                        className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary placeholder:text-text-secondary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                        placeholder="my-awesome-app"
                      />
                      <p className="mt-1.5 font-mono text-[10px] text-text-secondary">
                        Auto-fills from repository URL
                      </p>
                    </div>

                    <div>
                      <label className="block font-mono text-[11px] text-text-secondary mb-2">
                        Branch
                      </label>
                      <input
                        value={branch}
                        onChange={e => setBranch(e.target.value)}
                        disabled={deploying}
                        className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
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

                    <div className="md:col-span-2">
                      <label className="block font-mono text-[11px] text-text-secondary mb-2">
                        Repository URL <span className="text-red-400">*</span>
                      </label>
                      <div className="relative">
                        <Github size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-text-secondary" />
                        <input
                          value={repoUrl}
                          onChange={e => setRepoUrl(e.target.value)}
                          disabled={deploying}
                          className="w-full pl-10 pr-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary placeholder:text-text-secondary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                          placeholder="https://github.com/user/repo"
                        />
                      </div>
                    </div>

                    {projectType !== 'full_stack' ? (
                      <div className="md:col-span-2">
                        <label className="block font-mono text-[11px] text-text-secondary mb-2">
                          Working Directory
                        </label>
                        <input
                          value={workingDir}
                          onChange={e => setWorkingDir(e.target.value)}
                          disabled={deploying}
                          className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                          placeholder="(root)"
                        />
                        <p className="mt-1.5 font-mono text-[10px] text-text-secondary">
                          Subfolder containing your project (e.g., &quot;frontend&quot;, &quot;app&quot;)
                        </p>
                      </div>
                    ) : (
                      <>
                        <div>
                          <label className="block font-mono text-[11px] text-text-secondary mb-2">
                            Frontend Working Directory
                          </label>
                          <input
                            value={frontendWorkingDir}
                            onChange={e => setFrontendWorkingDir(e.target.value)}
                            disabled={deploying}
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                            placeholder="frontend"
                          />
                          <p className="mt-1.5 font-mono text-[10px] text-text-secondary">
                            Subfolder containing frontend code
                          </p>
                        </div>
                        <div>
                          <label className="block font-mono text-[11px] text-text-secondary mb-2">
                            Backend Working Directory
                          </label>
                          <input
                            value={backendWorkingDir}
                            onChange={e => setBackendWorkingDir(e.target.value)}
                            disabled={deploying}
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                            placeholder="backend"
                          />
                          <p className="mt-1.5 font-mono text-[10px] text-text-secondary">
                            Subfolder containing backend code
                          </p>
                        </div>
                      </>
                    )}
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
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    {projectType === 'web_service' && (
                      <>
                        <div>
                          <label className="block font-mono text-[11px] text-text-secondary mb-2">
                            Build Command
                          </label>
                          <input
                            value={buildCmd}
                            onChange={e => setBuildCmd(e.target.value)}
                            disabled={deploying}
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                            placeholder="npm run build"
                          />
                          <p className="mt-1.5 font-mono text-[10px] text-text-secondary">
                            Full command (e.g., "npm run build", "yarn build")
                          </p>
                        </div>
                        <div>
                          <label className="block font-mono text-[11px] text-text-secondary mb-2">
                            Output Directory
                          </label>
                          <input
                            value={outputDir}
                            onChange={e => setOutputDir(e.target.value)}
                            disabled={deploying}
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                            placeholder="Auto-detect (dist, build, out)"
                          />
                          <p className="mt-1.5 font-mono text-[10px] text-text-secondary">
                            Leave blank to auto-detect
                          </p>
                        </div>
                      </>
                    )}

                    {projectType === 'full_stack' && (
                      <>
                        <div>
                          <label className="block font-mono text-[11px] text-text-secondary mb-2">
                            Frontend Build Command
                          </label>
                          <input
                            value={frontendBuildCmd}
                            onChange={e => setFrontendBuildCmd(e.target.value)}
                            disabled={deploying}
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                            placeholder="npm run build"
                          />
                          <p className="mt-1.5 font-mono text-[10px] text-text-secondary">
                            Full build command (e.g., "npm run build", "yarn build")
                          </p>
                        </div>
                        <div>
                          <label className="block font-mono text-[11px] text-text-secondary mb-2">
                            Frontend Output Directory
                          </label>
                          <input
                            value={frontendOutputDir}
                            onChange={e => setFrontendOutputDir(e.target.value)}
                            disabled={deploying}
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                            placeholder="Auto-detect (dist, build, out)"
                          />
                          <p className="mt-1.5 font-mono text-[10px] text-text-secondary">
                            Leave blank to auto-detect
                          </p>
                        </div>
                        <div>
                          <label className="block font-mono text-[11px] text-text-secondary mb-2">
                            Frontend Install Command
                          </label>
                          <input
                            value={frontendInstallCmd}
                            onChange={e => setFrontendInstallCmd(e.target.value)}
                            disabled={deploying}
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                            placeholder="npm install"
                          />
                          <p className="mt-1.5 font-mono text-[10px] text-text-secondary">
                            Full install command (e.g., "npm install", "yarn install", "pip install -r requirements.txt")
                          </p>
                        </div>
                        <div>
                          <label className="block font-mono text-[11px] text-text-secondary mb-2">
                            Backend Build Command
                          </label>
                          <input
                            value={backendBuildCmd}
                            onChange={e => setBackendBuildCmd(e.target.value)}
                            disabled={deploying}
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                            placeholder="(optional - for custom build steps)"
                          />
                          <p className="mt-1.5 font-mono text-[10px] text-text-secondary">
                            Full backend build command (e.g., "go build", "npm run build")
                          </p>
                        </div>
                        <div>
                          <label className="block font-mono text-[11px] text-text-secondary mb-2">
                            Backend Install Command
                          </label>
                          <input
                            value={backendInstallCmd}
                            onChange={e => setBackendInstallCmd(e.target.value)}
                            disabled={deploying}
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                            placeholder="pip install -r requirements.txt"
                          />
                          <p className="mt-1.5 font-mono text-[10px] text-text-secondary">
                            Full install command (e.g., "npm install", "pip install -r requirements.txt", "go mod download")
                          </p>
                        </div>
                      </>
                    )}

                    {projectType !== 'full_stack' && (
                      <div className={projectType === 'web_service' ? 'md:col-span-2' : ''}>
                        <label className="block font-mono text-[11px] text-text-secondary mb-2">
                          Install Command (Optional)
                        </label>
                        <input
                          value={installCmd}
                          onChange={e => setInstallCmd(e.target.value)}
                          disabled={deploying}
                          className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                          placeholder="npm install"
                        />
                        <p className="mt-1.5 font-mono text-[10px] text-text-secondary">
                          Full install command (e.g., "npm install", "pip install -r requirements.txt", "go mod download")
                        </p>
                      </div>
                    )}

                    {(projectType === 'app_service' || projectType === 'full_stack') && (
                      <>
                        <div>
                          <label className="block font-mono text-[11px] text-text-secondary mb-2">
                            {projectType === 'full_stack' ? 'Backend Start Command' : 'Start Command'}
                          </label>
                          <input
                            value={startCmd}
                            onChange={e => setStartCmd(e.target.value)}
                            disabled={deploying}
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                            placeholder="npm start"
                          />
                          <p className="mt-1.5 font-mono text-[10px] text-text-secondary">
                            Full start command (e.g. "npm start", "node server.js", "python main.py")
                          </p>
                        </div>
                        <div>
                          <label className="block font-mono text-[11px] text-text-secondary mb-2">
                            {projectType === 'full_stack' ? 'Backend Internal Port' : 'Service Internal Port'}
                          </label>
                          <input
                            value={backendPort}
                            onChange={e => setBackendPort(e.target.value)}
                            disabled={deploying}
                            type="number"
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary disabled:opacity-50 focus:border-accent-lime focus:outline-none transition-colors"
                            placeholder="8000"
                          />
                          <p className="mt-1.5 font-mono text-[10px] text-text-secondary">
                            The internal port your app listens on. (A free external port will be mapped automatically)
                          </p>
                        </div>
                      </>
                    )}
                  </div>
                </div>

                {projectType === 'app_service' && availableProjects.length > 0 && (
                  <div className="border-t border-border-dark pt-6">
                    <div className="flex items-center gap-2 mb-4">
                      <Globe size={16} className="text-accent-lime" />
                      <label className="font-mono text-label uppercase tracking-wider text-text-secondary">
                        Attach to Web Service (Optional)
                      </label>
                    </div>
                    <div className="mb-4">
                      <p className="font-mono text-[10px] text-text-secondary mb-3">
                        Deploy this backend to an existing frontend's domain under the "/api" path.
                      </p>
                      <select
                        value={attachToProjectId}
                        onChange={(e) => {
                          setAttachToProjectId(e.target.value)
                          if (e.target.value) {
                            // Clear domain settings if we attach to a project
                            setDomain('')
                            setSubdomain('')
                            setSelectedZoneId('')
                          }
                        }}
                        disabled={deploying}
                        className="w-full px-4 py-3 bg-bg-primary border border-border-dark font-mono text-small text-text-primary disabled:opacity-50"
                      >
                        <option value="">-- Do not attach --</option>
                        {availableProjects.map(proj => (
                          <option key={proj.id} value={proj.id}>
                            {proj.name} ({proj.domain})
                          </option>
                        ))}
                      </select>
                      {attachToProjectId && (
                        <div className="mt-3 p-3 bg-blue-900/20 border border-blue-800/50">
                          <p className="font-mono text-[10px] text-blue-300">
                            <strong>Note:</strong> Make sure your frontend fetches API requests from <code>/api</code> (e.g., <code>fetch('/api/users')</code>). OpenDeploy will automatically proxy these requests to your backend without the <code>/api</code> prefix.
                          </p>
                        </div>
                      )}
                    </div>
                  </div>
                )}

                {/* Domain Configuration */}
                {attachToProjectId === '' && (
                  <div className="border-t border-border-dark pt-6">
                    <div className="flex items-center gap-2 mb-4">
                      <Globe size={16} className="text-accent-lime" />
                      <label className="font-mono text-label uppercase tracking-wider text-text-secondary">
                        Domain Configuration (Optional)
                      </label>
                    </div>

                    {/* Tunnel Toggle */}
                    <div className="mb-4">
                      <label className="flex items-center gap-2 font-mono text-[11px] text-text-secondary">
                        <input
                          type="checkbox"
                          checked={deploymentTarget === 'internet'}
                          onChange={(e) => {
                            if (e.target.checked) {
                              if (!hasTunnelSetup) {
                                if (confirm('Cloudflare Tunnel is not set up. Would you like to set it up now?')) {
                                  window.location.href = '/tunnel/dashboard'
                                }
                                return
                              }
                              setDeploymentTarget('internet')
                            } else {
                              setDeploymentTarget('local')
                            }
                          }}
                          disabled={deploying || (!hasTunnelSetup && deploymentTarget === 'internet')}
                          className="accent-accent-lime"
                        />
                        Enable Cloudflare Tunnel (Expose to Internet)
                      </label>
                      <p className="mt-1 font-mono text-[10px] text-text-secondary">
                        {deploymentTarget === 'internet'
                          ? "Deploy via Cloudflare Tunnel with automatic HTTPS, DNS, and CDN"
                          : "Deploy locally with optional nginx configuration"}
                      </p>
                    </div>

                    {/* Domain Configuration when Tunnel Enabled */}
                    {deploymentTarget === 'internet' && (
                      <div className="space-y-3">
                        {cloudflareZones.length > 0 ? (
                          <>
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
                                className="w-full px-4 py-3 bg-bg-primary border border-border-dark font-mono text-small text-text-primary disabled:opacity-50"
                              >
                                <option value="">-- Select a domain --</option>
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
                                    className="flex-1 px-4 py-3 bg-bg-primary border border-border-dark font-mono text-small text-text-primary placeholder:text-text-secondary disabled:opacity-50"
                                    placeholder="project-1"
                                  />
                                  <span className="font-mono text-small text-text-secondary">.{domain}</span>
                                </div>
                                <p className="mt-1 font-mono text-[10px] text-text-secondary">
                                  Leave blank to deploy on root domain ({domain})
                                </p>
                              </div>
                            )}
                          </>
                        ) : (
                          <div className="p-4 bg-yellow-900/20 border border-yellow-800/50">
                            <p className="font-mono text-[11px] text-yellow-500 mb-2">
                              No Cloudflare domains found. You need to configure a domain in Cloudflare first to deploy to the internet.
                            </p>
                            <Link href="/tunnel/dashboard" className="text-accent-lime hover:underline font-mono text-[11px]">
                              Go to Cloudflare Tunnel Dashboard
                            </Link>
                          </div>
                        )}
                      </div>
                    )}

                    {/* Local Domain Configuration */}
                    {deploymentTarget === 'local' && (
                      <div className="space-y-3">
                        <div>
                          <label className="block font-mono text-[11px] text-text-secondary mb-2">
                            Local Domain or IP (Optional)
                          </label>
                          <input
                            value={domain}
                            onChange={(e) => setDomain(e.target.value)}
                            disabled={deploying}
                            className="w-full px-4 py-3 bg-bg-primary border border-border-dark font-mono text-small text-text-primary placeholder:text-text-secondary disabled:opacity-50"
                            placeholder="e.g., myserver.local or 192.168.1.100"
                          />
                        </div>

                        <label className="flex items-center gap-2 font-mono text-[11px] text-text-secondary mt-3">
                          <input
                            type="checkbox"
                            checked={enableNginx}
                            onChange={(e) => setEnableNginx(e.target.checked)}
                            disabled={deploying}
                            className="accent-accent-lime"
                          />
                          Auto-configure nginx for this domain
                        </label>
                      </div>
                    )}

                    {domain && (
                      <div className="mt-3 p-3 bg-bg-secondary border border-border-dark">
                        <p className="font-mono text-[10px] text-text-secondary">
                          {deploymentTarget === 'internet'
                            ? <>Your app will be accessible at <strong>https://{subdomain ? `${subdomain}.${domain}` : domain}</strong> via Cloudflare Tunnel.</>
                            : <>Nginx will route requests for <strong>{domain}</strong> to your {projectType === 'web_service' ? 'frontend' : 'backend/frontend'}.</>
                          }
                        </p>
                      </div>
                    )}

                    {loadingZones && (
                      <p className="mt-2 font-mono text-[10px] text-text-secondary animate-pulse">
                        Loading Cloudflare zones...
                      </p>
                    )}
                  </div>
                )}

                {/* Environment Variables */}
                <div className="border-t border-border-dark pt-6">
                  <div className="flex items-center gap-2 mb-4">
                    <Lock size={16} className="text-accent-lime" />
                    <h3 className="font-mono text-[12px] uppercase tracking-wider text-text-primary font-bold">
                      Environment Variables
                    </h3>
                  </div>
                  <div className="flex items-center justify-between mb-3">
                    <p className="font-mono text-[10px] text-text-secondary">
                      Configure runtime environment variables
                    </p>
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

,
                    <div className="mb-3 p-3 bg-bg-primary border border-border-dark ">
                      <p className="font-mono text-[10px] text-text-secondary mb-2">
                        Paste .env format (KEY=VALUE per line)
                      </p>
                      <textarea
                        value={bulkContent}
                        onChange={e => setBulkContent(e.target.value)}
                        className="w-full px-3 py-2 bg-bg-secondary border border-border-dark  font-mono text-small text-text-primary mb-2"
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
                          className="px-3 py-1.5 bg-accent-lime text-text-dark font-mono text-[11px] font-bold "
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
                        className="w-[35%] px-3 py-2 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary disabled:opacity-50"
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
                          className="w-full px-3 py-2 pr-8 bg-bg-primary border border-border-dark  font-mono text-small text-text-primary disabled:opacity-50"
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

                  {/* Frontend Backend Env Vars handlers for full-stack - will be conditionally rendered */}
                </div>

                {/* Deploy Button */}
                <div className="border-t border-border-dark pt-6">
                  <button
                    onClick={handleDeploy}
                    disabled={deploying || !repoUrl || !projectName}
                    className="w-full px-6 py-4 bg-accent-lime text-text-dark font-mono font-bold text-body uppercase tracking-wider  hover:bg-accent-lime-muted transition-all hover:shadow-[0_0_20px_rgba(170,255,69,0.3)] disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2"
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
            </div>
          </div>
        )}

        {/* Building state - Show build logs full screen */}
        {state === 'building' && currentDeployId && (
          <div className="bg-bg-secondary border border-border-dark overflow-hidden">
            <div className="px-8 py-6 border-b border-border-dark bg-bg-primary/30">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Terminal size={16} className="text-accent-lime" />
                  <h3 className="font-mono text-[12px] uppercase tracking-wider text-text-primary font-bold">
                    Build Output
                  </h3>
                </div>
                <button
                  onClick={resetForm}
                  className="text-text-secondary hover:text-text-primary text-xs"
                >
                  ✕ Cancel
                </button>
              </div>
            </div>
            <div className="p-6">
              <div className="mb-4">
                <BuildProgress
                  currentPhase={buildPhase}
                  isBackend={deployResult.isBackend}
                  isFullStack={projectType === 'fullstack'}
                />
              </div>
              <div>
                <DeployLogStream
                  deployId={currentDeployId}
                  onComplete={handleDeployComplete}
                  maxHeight="600px"
                />
              </div>
            </div>
          </div>
        )}
      </motion.div>
    </>
  )
}
