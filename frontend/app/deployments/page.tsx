'use client'

import { useState, useEffect, useCallback } from 'react'
import { motion } from 'framer-motion'
import { Package, Play, Square, RotateCw, Trash2, Terminal, ExternalLink, Clock, User, Key, RefreshCw, Plus, Eye, EyeOff, Upload, X, Folder, Activity, Wifi } from 'lucide-react'
import Link from 'next/link'
import SectionBadge from '@/components/ui/SectionBadge'
import DeployLogStream from '@/components/ui/DeployLogStream'
import { deployApi, cleanupApi } from '@/lib/api'

type Project = {
  id: string
  name: string
  repo_url: string
  branch: string
  project_type: string
  working_directory?: string
  created_at: string
  updated_at: string
}

type Deploy = {
  id: string
  project_id: string
  status: string
  commit_hash?: string
  commit_message?: string
  commit_author?: string
  started_at: string
  ended_at?: string
  exit_code: number
  output_path?: string
  framework?: string
  is_backend?: boolean
  build_duration?: number
  port_mappings?: string
}

type Container = {
  id: string
  project_id: string
  name: string
  image: string
  container_id: string
  status: string
  port_mappings: string
  created_at: string
}

type EnvVariable = {
  id: string
  project_id: string
  key: string
  value: string
  is_secret: boolean
  created_at: string
  updated_at: string
}

export default function DeploymentsPage() {
  const [projects, setProjects] = useState<Project[]>([])
  const [deploys, setDeploys] = useState<Record<string, Deploy[]>>({})
  const [containers, setContainers] = useState<Record<string, Container[]>>({})
  const [loading, setLoading] = useState(true)
  const [liveDeployId, setLiveDeployId] = useState<string | null>(null)
  const [liveDeployProjectId, setLiveDeployProjectId] = useState<string | null>(null)
  const [showDeployLogs, setShowDeployLogs] = useState(false)
  const [selectedDeployId, setSelectedDeployId] = useState<string | null>(null)
  const [showContainerLogs, setShowContainerLogs] = useState(false)
  const [containerLogs, setContainerLogs] = useState<string[]>([])
  const [showEnvModal, setShowEnvModal] = useState(false)
  const [selectedProjectId, setSelectedProjectId] = useState<string | null>(null)
  const [envVars, setEnvVars] = useState<EnvVariable[]>([])
  const [showValues, setShowValues] = useState<Record<string, boolean>>({})
  const [newKey, setNewKey] = useState('')
  const [newValue, setNewValue] = useState('')
  const [newIsSecret, setNewIsSecret] = useState(false)
  const [bulkContent, setBulkContent] = useState('')
  const [showBulkImport, setShowBulkImport] = useState(false)
  const [cleanupReport, setCleanupReport] = useState<{orphan_containers_removed: number, stale_deploys_fixed: number} | null>(null)

  useEffect(() => {
    loadProjects()
  }, [])

  // Auto-refresh running deploys every 5 seconds
  useEffect(() => {
    const hasRunning = Object.values(deploys).flat().some(d => d.status === 'running')
    if (!hasRunning) return

    const interval = setInterval(() => {
      Object.keys(deploys).forEach(pid => loadDeploys(pid))
    }, 5000)

    return () => clearInterval(interval)
  }, [deploys])

  // Auto-refresh container status every 10 seconds
  useEffect(() => {
    if (projects.length === 0) return

    const interval = setInterval(() => {
      projects.forEach(p => loadContainers(p.id))
    }, 10000)

    return () => clearInterval(interval)
  }, [projects])

  const loadProjects = async () => {
    try {
      const data = await deployApi.listProjects()
      setProjects(data)
      for (const project of data) {
        loadDeploys(project.id)
        loadContainers(project.id)
      }
    } catch (err) {
      console.error('Failed to load projects:', err)
    } finally {
      setLoading(false)
    }
  }

  const loadDeploys = async (projectId: string) => {
    try {
      const data = await deployApi.listDeploys(projectId)
      setDeploys(prev => ({ ...prev, [projectId]: data || [] }))
    } catch (err) {
      console.error('Failed to load deploys:', err)
    }
  }

  const loadContainers = async (projectId: string) => {
    try {
      const data = await fetch(`/api/v1/projects/${projectId}/containers`, {
        credentials: 'include'
      }).then(r => r.json())
      setContainers(prev => ({ ...prev, [projectId]: data || [] }))
    } catch (err) {
      console.error('Failed to load containers:', err)
    }
  }

  const handleStartContainer = async (projectId: string, containerId: string) => {
    await fetch(`/api/v1/projects/${projectId}/containers/${containerId}/start`, { method: 'POST', credentials: 'include' })
    loadContainers(projectId)
  }

  const handleStopContainer = async (projectId: string) => {
    await fetch(`/api/v1/projects/${projectId}/containers/stop`, { method: 'POST', credentials: 'include' })
    loadContainers(projectId)
  }

  const handleRestartContainer = async (projectId: string) => {
    await fetch(`/api/v1/projects/${projectId}/containers/restart`, { method: 'POST', credentials: 'include' })
    loadContainers(projectId)
  }

  const handleRemoveContainer = async (projectId: string) => {
    if (!confirm('Remove this container?')) return
    await fetch(`/api/v1/projects/${projectId}/containers`, { method: 'DELETE', credentials: 'include' })
    loadContainers(projectId)
  }

  const handleViewContainerLogs = async (containerId: string) => {
    const response = await fetch(`/api/v1/containers/${containerId}/logs?lines=100`, { credentials: 'include' })
    const data = await response.json()
    setContainerLogs(data.logs || [])
    setShowContainerLogs(true)
  }

  const handleDeleteProject = async (projectId: string) => {
    if (!confirm('Delete this project and all deployments?')) return
    await deployApi.deleteProject(projectId)
    loadProjects()
  }

  const handleRedeploy = async (projectId: string) => {
    const res = await deployApi.triggerDeploy(projectId)
    if (res.deploy_id) {
      setLiveDeployId(res.deploy_id)
      setLiveDeployProjectId(projectId)
    }
    setTimeout(() => loadDeploys(projectId), 1000)
  }

  const handleRebuild = async (projectId: string) => {
    if (!confirm('This will stop any running container, remove old code, and rebuild from git. Continue?')) return
    const res = await deployApi.rebuildProject(projectId)
    if (res.deploy_id) {
      setLiveDeployId(res.deploy_id)
      setLiveDeployProjectId(projectId)
    }
    setTimeout(() => {
      loadDeploys(projectId)
      loadContainers(projectId)
    }, 1000)
  }

  const handleCleanup = async () => {
    const report = await cleanupApi.runCleanup()
    setCleanupReport(report)
    loadProjects()
  }

  const loadEnvVars = async (projectId: string) => {
    const res = await fetch(`/api/v1/projects/${projectId}/env`, { credentials: 'include' })
    if (res.ok) {
      const data = await res.json()
      setEnvVars(data || [])
    }
  }

  const createEnvVar = async () => {
    if (!newKey || !newValue || !selectedProjectId) return
    const res = await fetch(`/api/v1/projects/${selectedProjectId}/env`, {
      method: 'POST', credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ key: newKey, value: newValue, is_secret: newIsSecret })
    })
    if (res.ok) {
      setNewKey(''); setNewValue(''); setNewIsSecret(false)
      loadEnvVars(selectedProjectId)
    }
  }

  const deleteEnvVar = async (envId: string) => {
    if (!confirm('Delete this variable?')) return
    const res = await fetch(`/api/v1/env/${envId}`, { method: 'DELETE', credentials: 'include' })
    if (res.ok && selectedProjectId) loadEnvVars(selectedProjectId)
  }

  const bulkImport = async () => {
    if (!bulkContent.trim() || !selectedProjectId) return
    const res = await fetch(`/api/v1/projects/${selectedProjectId}/env/bulk`, {
      method: 'POST', credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content: bulkContent, is_secret: false })
    })
    if (res.ok) { setBulkContent(''); setShowBulkImport(false); loadEnvVars(selectedProjectId) }
  }

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'running': case 'success': return 'text-accent-lime'
      case 'stopped': case 'pending': return 'text-text-secondary'
      case 'failed': case 'exited': case 'unhealthy': return 'text-status-error'
      default: return 'text-text-secondary'
    }
  }

  const getStatusBg = (status: string) => {
    switch (status) {
      case 'running': case 'success': return 'bg-accent-lime/10 border-accent-lime/30'
      case 'stopped': case 'pending': return 'bg-text-secondary/10 border-text-secondary/30'
      case 'failed': case 'exited': case 'unhealthy': return 'bg-status-error/10 border-status-error/30'
      default: return 'bg-text-secondary/10 border-text-secondary/30'
    }
  }

  const relativeTime = (dateStr: string) => {
    const diff = Date.now() - new Date(dateStr).getTime()
    const mins = Math.floor(diff / 60000)
    if (mins < 1) return 'just now'
    if (mins < 60) return `${mins}m ago`
    const hours = Math.floor(mins / 60)
    if (hours < 24) return `${hours}h ago`
    const days = Math.floor(hours / 24)
    return `${days}d ago`
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <p className="font-mono text-small text-text-secondary">Loading deployments...</p>
      </div>
    )
  }

  return (
    <>
      <motion.div
        initial={{ opacity: 0, x: 20 }}
        animate={{ opacity: 1, x: 0 }}
        transition={{ duration: 0.3 }}
      >
        <div className="mb-8 flex items-start justify-between">
          <div>
            <SectionBadge label="DEPLOYMENTS" />
            <p className="mt-2 text-sm text-text-secondary font-mono">
              {projects.length} project{projects.length !== 1 ? 's' : ''} deployed
            </p>
          </div>
          <div className="flex items-center gap-2 flex-wrap">
            <button
              onClick={handleCleanup}
              className="px-2 sm:px-3 py-1.5 sm:py-2 bg-bg-secondary text-text-secondary font-mono text-[10px] sm:text-[11px] border border-border-dark rounded hover:text-accent-lime hover:border-accent-lime transition-colors flex items-center gap-1"
              title="Cleanup orphan containers and fix stale deploys"
            >
              <Activity size={12} className="flex-shrink-0" /> <span className="hidden xs:inline">Cleanup</span>
            </button>
            <Link
              href="/deploy"
              className="px-2 sm:px-4 py-1.5 sm:py-2 bg-accent-lime text-text-dark font-mono text-[10px] sm:text-[11px] font-bold rounded hover:bg-accent-lime-muted transition-colors flex items-center gap-1"
            >
              <Plus size={12} className="flex-shrink-0" /> <span>New Deploy</span>
            </Link>
          </div>
        </div>

        {/* Cleanup report notification */}
        {cleanupReport && (
          <div className="mb-4 p-3 bg-blue-900/20 border border-blue-800/50 rounded-lg font-mono text-xs text-blue-400 flex items-center justify-between">
            <span>
              Cleanup: {cleanupReport.stale_deploys_fixed} stale deploys fixed, {cleanupReport.orphan_containers_removed} orphan containers removed
            </span>
            <button onClick={() => setCleanupReport(null)} className="text-blue-400 hover:text-blue-300">✕</button>
          </div>
        )}

        {/* Live deploy stream */}
        {liveDeployId && (
          <motion.div
            initial={{ opacity: 0, y: -10 }}
            animate={{ opacity: 1, y: 0 }}
            className="mb-6 bg-bg-secondary rounded-card border border-blue-800/50 p-4"
          >
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2">
                <Wifi size={14} className="text-blue-400 animate-pulse" />
                <span className="font-mono text-xs text-blue-400 uppercase tracking-wider">Live Deployment</span>
              </div>
              <button
                onClick={() => { setLiveDeployId(null); setLiveDeployProjectId(null) }}
                className="text-text-secondary hover:text-text-primary text-xs"
              >
                ✕ Close
              </button>
            </div>
            <DeployLogStream
              deployId={liveDeployId}
              maxHeight="400px"
              onComplete={(result) => {
                if (liveDeployProjectId) {
                  loadDeploys(liveDeployProjectId)
                  loadContainers(liveDeployProjectId)
                }
              }}
            />
          </motion.div>
        )}

        <div className="space-y-6">
          {projects.length === 0 ? (
            <div className="bg-bg-secondary rounded-card border border-border-dark p-12 text-center">
              <Package size={48} className="mx-auto mb-4 text-border-dark" />
              <p className="font-mono text-small text-text-secondary mb-4">
                No deployments yet. Deploy your first project to get started.
              </p>
              <Link
                href="/deploy"
                className="inline-flex items-center gap-2 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all"
              >
                Deploy Now →
              </Link>
            </div>
          ) : (
            projects.map(project => {
              const projectDeploys = deploys[project.id] || []
              const projectContainers = containers[project.id] || []
              const latestDeploy = projectDeploys[0]
              const container = projectContainers[0]
              const isRunningDeploy = latestDeploy?.status === 'running'

              return (
                <motion.div
                  key={project.id}
                  initial={{ opacity: 0, y: 10 }}
                  animate={{ opacity: 1, y: 0 }}
                  className="bg-bg-secondary rounded-card border border-border-dark overflow-hidden"
                >
                  {/* Project Header */}
                  <div className="p-6 border-b border-border-dark">
                    <div className="flex items-start justify-between">
                      <div className="flex-1">
                        <div className="flex items-center gap-3 mb-2">
                          <h3 className="font-serif text-h3">{project.name}</h3>
                          {isRunningDeploy && (
                            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] bg-blue-900/30 text-blue-400 border border-blue-800/50">
                              <span className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-pulse" />
                              Deploying
                            </span>
                          )}
                          {container?.status === 'running' && !isRunningDeploy && (
                            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] bg-emerald-900/30 text-emerald-400 border border-emerald-800/50">
                              <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse" />
                              Running
                            </span>
                          )}
                          {latestDeploy && !isRunningDeploy && !latestDeploy.is_backend && latestDeploy.status === 'success' && (
                            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] bg-emerald-900/30 text-emerald-400 border border-emerald-800/50">
                              <span className="w-1.5 h-1.5 rounded-full bg-emerald-400" />
                              Static
                            </span>
                          )}
                        </div>
                        <div className="font-mono text-[11px] text-text-secondary space-y-0.5">
                          <p>
                            <a href={project.repo_url} target="_blank" rel="noopener noreferrer"
                              className="text-accent-lime hover:underline inline-flex items-center gap-1">
                              {project.repo_url.replace('https://github.com/', '')} <ExternalLink size={10} />
                            </a>
                            <span className="text-text-secondary ml-2">({project.branch})</span>
                          </p>
                          {latestDeploy?.framework && (
                            <p>Framework: <span className="text-text-primary">{latestDeploy.framework}</span></p>
                          )}
                          {latestDeploy?.output_path && !latestDeploy.is_backend && (
                            <p className="flex items-center gap-1">
                              <Folder size={10} />
                              <span className="text-accent-lime">{latestDeploy.output_path}</span>
                            </p>
                          )}
                        </div>
                      </div>

                      <div className="flex items-center gap-1.5 sm:gap-2 flex-shrink-0 flex-wrap">
                        <button
                          onClick={() => { setSelectedProjectId(project.id); setShowEnvModal(true); loadEnvVars(project.id) }}
                          className="px-2 sm:px-3 py-1 sm:py-1.5 bg-bg-primary text-text-secondary font-mono text-[9px] sm:text-[10px] font-bold rounded hover:text-accent-lime hover:border-accent-lime border border-border-dark transition-colors flex items-center gap-1"
                        >
                          <Key size={11} className="flex-shrink-0" /> <span className="hidden xs:inline">Env</span>
                        </button>
                        <button
                          onClick={() => handleRebuild(project.id)}
                          className="px-2 sm:px-3 py-1 sm:py-1.5 bg-bg-primary text-text-secondary font-mono text-[9px] sm:text-[10px] font-bold rounded hover:text-accent-lime hover:border-accent-lime border border-border-dark transition-colors flex items-center gap-1"
                        >
                          <RefreshCw size={11} className="flex-shrink-0" /> <span className="hidden xs:inline">Rebuild</span>
                        </button>
                        <button
                          onClick={() => handleRedeploy(project.id)}
                          className="px-2 sm:px-3 py-1 sm:py-1.5 bg-accent-lime text-text-dark font-mono text-[9px] sm:text-[10px] font-bold rounded hover:bg-accent-lime-muted transition-colors"
                        >
                          Deploy
                        </button>
                        <button
                          onClick={() => handleDeleteProject(project.id)}
                          className="p-1 sm:p-1.5 text-text-secondary hover:text-status-error transition-colors"
                        >
                          <Trash2 size={13} />
                        </button>
                      </div>
                    </div>
                  </div>

                  {/* Container Status (for backends) */}
                  {container && (
                    <div className="px-6 py-4 bg-bg-primary/50 border-b border-border-dark">
                      <div className="flex items-center justify-between mb-3">
                        <div className="flex items-center gap-3">
                          <div className={`w-2 h-2 rounded-full ${container.status === 'running' ? 'bg-accent-lime animate-pulse' : container.status === 'unhealthy' ? 'bg-red-400' : 'bg-text-secondary'}`} />
                          <span className="font-mono text-[11px] text-text-secondary">Container Status:</span>
                          <span className={`font-mono text-[11px] font-bold ${getStatusColor(container.status)}`}>
                            {container.status.toUpperCase()}
                          </span>
                          <span className="font-mono text-[10px] text-text-secondary bg-bg-secondary px-2 py-0.5 rounded">
                            {container.container_id.substring(0, 12)}
                          </span>
                        </div>

                        <div className="flex items-center gap-1.5 sm:gap-2 flex-wrap">
                          <button
                            onClick={() => handleViewContainerLogs(container.container_id)}
                            className="px-2 sm:px-3 py-1 sm:py-1.5 bg-bg-secondary text-text-secondary font-mono text-[9px] sm:text-[10px] border border-border-dark rounded hover:text-accent-lime hover:border-accent-lime transition-colors flex items-center gap-1"
                            title="View container logs"
                          >
                            <Terminal size={11} className="flex-shrink-0" /> <span className="hidden xs:inline">Logs</span>
                          </button>
                          {container.status === 'running' ? (
                            <>
                              <button
                                onClick={() => handleRestartContainer(project.id)}
                                className="px-2 sm:px-3 py-1 sm:py-1.5 bg-bg-secondary text-text-secondary font-mono text-[9px] sm:text-[10px] border border-border-dark rounded hover:text-accent-lime hover:border-accent-lime transition-colors flex items-center gap-1"
                                title="Restart container"
                              >
                                <RotateCw size={11} className="flex-shrink-0" /> <span className="hidden xs:inline">Restart</span>
                              </button>
                              <button
                                onClick={() => handleStopContainer(project.id)}
                                className="px-2 sm:px-3 py-1 sm:py-1.5 bg-bg-secondary text-text-secondary font-mono text-[9px] sm:text-[10px] border border-border-dark rounded hover:text-status-error hover:border-status-error transition-colors flex items-center gap-1"
                                title="Stop container"
                              >
                                <Square size={11} className="flex-shrink-0" /> <span className="hidden xs:inline">Stop</span>
                              </button>
                            </>
                          ) : (
                            <button
                              onClick={() => handleStartContainer(project.id, container.id)}
                              className="px-2 sm:px-3 py-1 sm:py-1.5 bg-accent-lime text-text-dark font-mono text-[9px] sm:text-[10px] font-bold rounded hover:bg-accent-lime-muted transition-colors flex items-center gap-1"
                              title="Start container"
                            >
                              <Play size={11} className="flex-shrink-0" /> <span>Start</span>
                            </button>
                          )}
                          <button
                            onClick={() => handleRemoveContainer(project.id)}
                            className="px-2 sm:px-3 py-1 sm:py-1.5 bg-bg-secondary text-text-secondary font-mono text-[9px] sm:text-[10px] border border-border-dark rounded hover:text-status-error hover:border-status-error transition-colors flex items-center gap-1"
                            title="Remove container"
                          >
                            <Trash2 size={11} className="flex-shrink-0" /> <span className="hidden xs:inline">Remove</span>
                          </button>
                        </div>
                      </div>

                      {/* Port Mappings */}
                      {container.port_mappings && (() => {
                        try {
                          const mapping = JSON.parse(container.port_mappings)
                          return (
                            <div className="flex items-center gap-2 text-[10px] font-mono">
                              <span className="text-text-secondary">Port Mapping:</span>
                              <span className="bg-bg-secondary px-2 py-1 rounded border border-border-dark">
                                <span className="text-accent-lime">Host {mapping.host}</span>
                                <span className="text-text-secondary mx-1">→</span>
                                <span className="text-cyan-400">Container {mapping.container}</span>
                              </span>
                            </div>
                          )
                        } catch {
                          return null
                        }
                      })()}
                    </div>
                  )}

                  {/* Deploy History */}
                  <div className="p-6">
                    <div className="flex items-center justify-between mb-4">
                      <h4 className="font-mono text-[11px] uppercase tracking-wider text-text-secondary">
                        Recent Deployments
                      </h4>
                      {projectDeploys.length > 0 && (
                        <span className="font-mono text-[10px] text-text-secondary">
                          {projectDeploys.length} total
                        </span>
                      )}
                    </div>
                    {projectDeploys.length === 0 ? (
                      <div className="text-center py-8 bg-bg-primary/30 rounded-lg border border-border-dark">
                        <Package size={32} className="mx-auto mb-2 text-border-dark" />
                        <p className="font-mono text-[11px] text-text-secondary">No deployments yet</p>
                      </div>
                    ) : (
                      <div className="space-y-2">
                        {projectDeploys.slice(0, 5).map(deploy => {
                          let portMapping = null
                          if (deploy.port_mappings) {
                            try {
                              portMapping = JSON.parse(deploy.port_mappings)
                            } catch {}
                          }

                          return (
                            <div
                              key={deploy.id}
                              className={`flex items-start justify-between p-4 rounded-lg border ${getStatusBg(deploy.status)} hover:border-accent-lime/50 transition-colors`}
                            >
                              <div className="flex-1 min-w-0">
                                <div className="flex items-center gap-2 mb-2 flex-wrap">
                                  <span className={`font-mono text-[11px] font-bold ${getStatusColor(deploy.status)}`}>
                                    {deploy.status === 'running' && <span className="inline-block w-1.5 h-1.5 rounded-full bg-blue-400 animate-pulse mr-1 align-middle" />}
                                    {deploy.status.toUpperCase()}
                                  </span>
                                  {deploy.commit_hash && (
                                    <code className="font-mono text-[10px] text-text-secondary bg-bg-primary px-2 py-0.5 rounded">
                                      {deploy.commit_hash.substring(0, 7)}
                                    </code>
                                  )}
                                  {deploy.build_duration && deploy.build_duration > 0 && (
                                    <span className="font-mono text-[10px] text-text-secondary flex items-center gap-1">
                                      <Clock size={10} /> {deploy.build_duration.toFixed(1)}s
                                    </span>
                                  )}
                                  {deploy.framework && (
                                    <span className="font-mono text-[10px] text-accent-lime bg-accent-lime/10 px-2 py-0.5 rounded border border-accent-lime/30">
                                      {deploy.framework}
                                    </span>
                                  )}
                                  {deploy.is_backend && (
                                    <span className="font-mono text-[10px] text-cyan-400 bg-cyan-400/10 px-2 py-0.5 rounded border border-cyan-400/30">
                                      Backend
                                    </span>
                                  )}
                                </div>
                                {deploy.commit_message && (
                                  <p className="font-mono text-[11px] text-text-secondary mb-2 line-clamp-2">
                                    {deploy.commit_message}
                                  </p>
                                )}
                                <div className="flex items-center gap-4 flex-wrap">
                                  {deploy.commit_author && (
                                    <span className="font-mono text-[10px] text-text-secondary flex items-center gap-1">
                                      <User size={10} /> {deploy.commit_author}
                                    </span>
                                  )}
                                  <span className="font-mono text-[10px] text-text-secondary flex items-center gap-1">
                                    <Clock size={10} /> {relativeTime(deploy.started_at)}
                                  </span>
                                  {portMapping && (
                                    <span className="font-mono text-[10px] bg-bg-primary px-2 py-0.5 rounded border border-border-dark">
                                      <span className="text-accent-lime">{portMapping.host}</span>
                                      <span className="text-text-secondary mx-1">→</span>
                                      <span className="text-cyan-400">{portMapping.container}</span>
                                    </span>
                                  )}
                                </div>
                              </div>

                              <div className="flex items-center gap-2 flex-shrink-0 ml-4">
                                {deploy.status === 'running' ? (
                                  <button
                                    onClick={() => { setLiveDeployId(deploy.id); setLiveDeployProjectId(project.id) }}
                                    className="px-3 py-1.5 text-[10px] font-mono font-bold bg-blue-900/30 text-blue-400 border border-blue-800/50 rounded hover:bg-blue-900/50 transition-colors flex items-center gap-1.5"
                                  >
                                    <Wifi size={12} /> View Live
                                  </button>
                                ) : (
                                  <button
                                    onClick={() => { setSelectedDeployId(deploy.id); setShowDeployLogs(true) }}
                                    className="px-3 py-1.5 text-[10px] font-mono bg-bg-secondary text-text-secondary border border-border-dark rounded hover:text-accent-lime hover:border-accent-lime transition-colors flex items-center gap-1.5"
                                    title="View deployment logs"
                                  >
                                    <Terminal size={12} /> Logs
                                  </button>
                                )}
                              </div>
                            </div>
                          )
                        })}
                      </div>
                    )}
                  </div>
                </motion.div>
              )
            })
          )}
        </div>

        {/* Deploy Logs Modal */}
        {showDeployLogs && selectedDeployId && (
          <div
            className="fixed inset-0 bg-black/80 flex items-center justify-center z-50 p-4"
            onClick={() => setShowDeployLogs(false)}
          >
            <motion.div
              initial={{ opacity: 0, scale: 0.95 }}
              animate={{ opacity: 1, scale: 1 }}
              className="bg-bg-secondary rounded-card border border-border-dark p-6 max-w-4xl w-full max-h-[80vh] overflow-hidden flex flex-col"
              onClick={e => e.stopPropagation()}
            >
              <div className="flex items-center justify-between mb-4">
                <h3 className="font-serif text-h3">Deployment Logs</h3>
                <button onClick={() => setShowDeployLogs(false)} className="text-text-secondary hover:text-text-primary">✕</button>
              </div>
              <div className="flex-1 overflow-auto">
                <DeployLogStream deployId={selectedDeployId} maxHeight="60vh" />
              </div>
            </motion.div>
          </div>
        )}

        {/* Container Logs Modal */}
        {showContainerLogs && (
          <div
            className="fixed inset-0 bg-black/80 flex items-center justify-center z-50 p-4"
            onClick={() => setShowContainerLogs(false)}
          >
            <motion.div
              initial={{ opacity: 0, scale: 0.95 }}
              animate={{ opacity: 1, scale: 1 }}
              className="bg-bg-secondary rounded-card border border-border-dark p-6 max-w-4xl w-full max-h-[80vh] overflow-hidden flex flex-col"
              onClick={e => e.stopPropagation()}
            >
              <div className="flex items-center justify-between mb-4">
                <h3 className="font-serif text-h3">Container Logs</h3>
                <button onClick={() => setShowContainerLogs(false)} className="text-text-secondary hover:text-text-primary">✕</button>
              </div>
              <div className="flex-1 overflow-auto bg-bg-primary rounded-lg p-4 font-mono text-[11px]">
                {containerLogs.map((log, i) => (
                  <div key={i} className="text-text-primary whitespace-pre-wrap">{log}</div>
                ))}
              </div>
            </motion.div>
          </div>
        )}

        {/* Environment Variables Modal */}
        {showEnvModal && (
          <div
            className="fixed inset-0 bg-black/80 flex items-center justify-center z-50 p-4"
            onClick={() => setShowEnvModal(false)}
          >
            <motion.div
              initial={{ opacity: 0, scale: 0.95 }}
              animate={{ opacity: 1, scale: 1 }}
              className="bg-bg-secondary rounded-card border border-border-dark p-6 max-w-2xl w-full max-h-[80vh] overflow-hidden flex flex-col"
              onClick={e => e.stopPropagation()}
            >
              <div className="flex items-center justify-between mb-6">
                <h3 className="font-serif text-h3">Environment Variables</h3>
                <button onClick={() => setShowEnvModal(false)} className="text-text-secondary hover:text-text-primary">
                  <X size={20} />
                </button>
              </div>

              <div className="flex-1 overflow-auto space-y-4">
                {/* Add New */}
                <div className="bg-bg-primary border border-border-dark rounded-lg p-4">
                  <h4 className="font-mono text-[11px] uppercase tracking-wider text-text-secondary mb-3">Add Variable</h4>
                  <div className="grid grid-cols-2 gap-3 mb-3">
                    <input type="text" value={newKey} onChange={e => setNewKey(e.target.value)} placeholder="KEY"
                      className="px-3 py-2 bg-bg-secondary border border-border-dark rounded text-text-primary text-sm focus:outline-none focus:border-accent-lime" />
                    <input type="text" value={newValue} onChange={e => setNewValue(e.target.value)} placeholder="value"
                      className="px-3 py-2 bg-bg-secondary border border-border-dark rounded text-text-primary text-sm focus:outline-none focus:border-accent-lime" />
                  </div>
                  <div className="flex items-center gap-3">
                    <label className="flex items-center gap-2 text-sm text-text-secondary cursor-pointer">
                      <input type="checkbox" checked={newIsSecret} onChange={e => setNewIsSecret(e.target.checked)} className="w-4 h-4" />
                      Secret
                    </label>
                    <button onClick={createEnvVar} disabled={!newKey || !newValue}
                      className="ml-auto px-4 py-2 bg-accent-lime text-bg-primary rounded hover:bg-accent-lime/90 disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2 text-sm font-bold transition-colors">
                      <Plus size={16} /> Add
                    </button>
                  </div>
                </div>

                {/* Bulk Import */}
                <div className="bg-bg-primary border border-border-dark rounded-lg p-4">
                  <div className="flex items-center justify-between mb-3">
                    <h4 className="font-mono text-[11px] uppercase tracking-wider text-text-secondary">Bulk Import</h4>
                    <button onClick={() => setShowBulkImport(!showBulkImport)}
                      className="text-accent-lime hover:text-accent-lime/80 text-sm transition-colors">
                      {showBulkImport ? 'Hide' : 'Show'}
                    </button>
                  </div>
                  {showBulkImport && (
                    <>
                      <textarea value={bulkContent} onChange={e => setBulkContent(e.target.value)}
                        placeholder={"KEY=value\nAPI_KEY=abc123"} rows={4}
                        className="w-full px-3 py-2 bg-bg-secondary border border-border-dark rounded text-text-primary font-mono text-sm focus:outline-none focus:border-accent-lime mb-3" />
                      <button onClick={bulkImport} disabled={!bulkContent.trim()}
                        className="px-4 py-2 bg-accent-lime text-bg-primary rounded hover:bg-accent-lime/90 disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2 text-sm font-bold transition-colors">
                        <Upload size={16} /> Import
                      </button>
                    </>
                  )}
                </div>

                {/* Variables List */}
                <div className="bg-bg-primary border border-border-dark rounded-lg overflow-hidden">
                  <div className="p-4 border-b border-border-dark">
                    <h4 className="font-mono text-[11px] uppercase tracking-wider text-text-secondary">
                      Variables ({envVars.length})
                    </h4>
                  </div>
                  {envVars.length === 0 ? (
                    <div className="p-8 text-center text-text-secondary text-sm">No variables</div>
                  ) : (
                    <div className="divide-y divide-border-dark max-h-64 overflow-auto">
                      {envVars.map(envVar => (
                        <div key={envVar.id} className="p-3 hover:bg-bg-secondary/50 transition-colors">
                          <div className="flex items-start gap-3">
                            <div className="flex-1 min-w-0">
                              <div className="flex items-center gap-2 mb-1">
                                <span className="font-mono text-sm font-semibold text-accent-lime">{envVar.key}</span>
                                {envVar.is_secret && (
                                  <span className="px-2 py-0.5 bg-yellow-500/20 text-yellow-500 text-xs rounded">Secret</span>
                                )}
                              </div>
                              <div className="flex items-center gap-2">
                                <code className="text-sm text-text-secondary font-mono break-all">
                                  {envVar.is_secret && !showValues[envVar.id] ? '••••••••' : envVar.value}
                                </code>
                                {envVar.is_secret && (
                                  <button onClick={() => setShowValues(p => ({ ...p, [envVar.id]: !p[envVar.id] }))}
                                    className="text-text-secondary hover:text-text-primary transition-colors">
                                    {showValues[envVar.id] ? <EyeOff size={14} /> : <Eye size={14} />}
                                  </button>
                                )}
                              </div>
                            </div>
                            <button onClick={() => deleteEnvVar(envVar.id)}
                              className="text-red-500 hover:text-red-400 transition-colors">
                              <Trash2 size={16} />
                            </button>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>

                <div className="p-3 bg-blue-500/10 border border-blue-500/30 rounded-lg">
                  <p className="text-sm text-blue-400">
                    <strong>Note:</strong> Rebuild the project after modifying variables to apply changes.
                  </p>
                </div>
              </div>
            </motion.div>
          </div>
        )}
      </motion.div>
    </>
  )
}
