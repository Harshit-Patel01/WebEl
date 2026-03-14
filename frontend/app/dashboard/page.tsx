'use client'

import { useState, useEffect } from 'react'
import { motion } from 'framer-motion'
import { RotateCw, RefreshCw, FileText, Settings, Server, Github, ExternalLink, Activity, Clock, Play, Square, Trash2, CheckCircle2 } from 'lucide-react'
import Link from 'next/link'
import SectionBadge from '@/components/ui/SectionBadge'
import ServiceCard from '@/components/ui/ServiceCard'
import PipelineDiagram from '@/components/ui/PipelineDiagram'
import { servicesApi, systemApi, deployApi, tunnelApi } from '@/lib/api'
import { useWebSocket } from '@/contexts/WebSocketContext'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'

dayjs.extend(relativeTime)

export default function DashboardPage() {
  const [services, setServices] = useState<Array<{
    name: string
    status: 'healthy' | 'error' | 'warning' | 'inactive'
    statusLabel: string
    detail: string
    variant: 'dark' | 'light'
  }>>([
    { name: 'Nginx', status: 'inactive', statusLabel: 'Unknown', detail: 'Loading...', variant: 'dark' },
    { name: 'Cloudflare', status: 'inactive', statusLabel: 'Unknown', detail: 'Loading...', variant: 'light' },
    { name: 'System', status: 'healthy', statusLabel: 'Healthy', detail: 'Loading...', variant: 'light' },
  ])

  const [logs, setLogs] = useState<any[]>([])
  const [projects, setProjects] = useState<any[]>([])
  const [loading, setLoading] = useState(true)
  const [dataLoaded, setDataLoaded] = useState(false)

  useEffect(() => {
    const fetchData = async () => {
      try {
        // Fetch service statuses
        const svcs = await servicesApi.list()
        const info = await systemApi.getInfo()

        const newServices = [...services]

        svcs.forEach(s => {
          if (s.name === 'nginx') {
            newServices[0].status = s.status === 'active' ? 'healthy' : 'error'
            newServices[0].statusLabel = s.status === 'active' ? 'Running' : 'Stopped'
            newServices[0].detail = s.uptime ? `Uptime: ${s.uptime}` : 'Web Server'
          }
          if (s.name === 'opendeploy-cloudflared') {
            newServices[1].status = s.status === 'active' ? 'healthy' : 'error'
            newServices[1].statusLabel = s.status === 'active' ? 'Active' : 'Offline'
            newServices[1].detail = s.uptime ? `Uptime: ${s.uptime}` : 'Tunnel Service'
          }
        })

        // Also check tunnel config status for richer info
        try {
          const tunnelStatus = await tunnelApi.getStatus()
          if (tunnelStatus.status === 'not_configured') {
            newServices[1].status = 'inactive'
            newServices[1].statusLabel = 'Not Configured'
            newServices[1].detail = 'No tunnel set up'
          } else if (tunnelStatus.domain) {
            newServices[1].detail = tunnelStatus.domain
          }
        } catch (e) {
          // tunnel API may fail if not configured
        }

        newServices[2].detail = info.model || 'Linux System'

        setServices(newServices)

        // Fetch recent nginx access logs
        const nginxLogs = await servicesApi.getLogs('nginx')
        setLogs(nginxLogs || [])

        // Fetch projects and their latest deploy statuses
        const projs = await deployApi.listProjects()

        // Enhance projects with latest deploy and service info
        const enhancedProjs = await Promise.all(projs.map(async (p: any) => {
          try {
            const deploys = await deployApi.listDeploys(p.id)
            const latestDeploy = deploys.length > 0 ? deploys[0] : null

            let serviceStatus = null
            if (p.project_type === 'python' || p.project_type === 'node') {
              try {
                const svcName = `opendeploy-app-${p.name}`
                serviceStatus = await servicesApi.get(svcName)
              } catch (e) {
                console.error(`Failed to get service status for ${p.name}`, e)
              }
            }

            return {
              ...p,
              latestDeploy,
              serviceStatus
            }
          } catch (e) {
            return p
          }
        }))

        setProjects(enhancedProjs)
        setLoading(false)
        setDataLoaded(true)
      } catch (err) {
        console.error('Failed to load dashboard data', err)
        setLoading(false)
        setDataLoaded(true)
      }
    }

    if (!dataLoaded) {
      fetchData()
    }

    const interval = setInterval(fetchData, 10000)
    return () => clearInterval(interval)
  }, [dataLoaded])

  const handleDeleteProject = async (id: string) => {
    if (confirm('Are you sure you want to delete this project? This will remove all files and configurations.')) {
      try {
        await deployApi.deleteProject(id)
        setProjects(projects.filter(p => p.id !== id))
      } catch (err) {
        alert(`Failed to delete project: ${err}`)
      }
    }
  }

  const quickActions = [
    { label: 'New Project', desc: 'Deploy a new app', icon: RotateCw, href: '/deploy' },
    { label: 'View Logs', desc: 'Jump to logs screen', icon: FileText, href: '/logs' },
    { label: 'Edit Nginx', desc: 'Jump to nginx screen', icon: Settings, href: '/nginx' },
  ]

  return (
    <motion.div
      initial={{ opacity: 0, x: 20 }}
      animate={{ opacity: 1, x: 0 }}
      transition={{ duration: 0.3 }}
    >
      <div className="mb-8 flex items-center gap-3">
          <SectionBadge label="LIVE" />
          <span className="relative flex h-3 w-3">
            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-accent-lime opacity-75" />
            <span className="relative inline-flex rounded-full h-3 w-3 bg-accent-lime" />
          </span>
        </div>

        {/* Status Cards */}
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 mb-8">
          {services.map((svc, i) => (
            <motion.div
              key={svc.name}
              initial={{ opacity: 0, y: 20 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: i * 0.1 }}
            >
              <ServiceCard
                name={svc.name}
                status={svc.status}
                statusLabel={svc.statusLabel}
                detail={svc.detail}
                variant={svc.variant}
              />
            </motion.div>
          ))}
        </div>

        {/* Deployed Projects Overview */}
        <div className="mb-8">
          <div className="flex items-center justify-between mb-4">
            <h2 className="font-serif text-h2">Deployed Projects</h2>
            <Link
              href="/deploy"
              className="inline-flex items-center gap-2 px-4 py-2 bg-bg-secondary border border-border-dark rounded-lg font-mono text-small hover:text-accent-lime hover:border-accent-lime transition-all"
            >
              <RotateCw size={16} /> New Deploy
            </Link>
          </div>

          {loading ? (
            <div className="bg-bg-secondary rounded-card border border-border-dark p-12 text-center">
              <span className="font-mono text-text-secondary animate-pulse">Loading projects...</span>
            </div>
          ) : projects.length === 0 ? (
            <div className="bg-bg-secondary rounded-card border border-border-dark p-12 flex flex-col items-center justify-center text-center">
              <Server size={48} className="text-border-dark mb-4" />
              <p className="font-mono text-small text-text-secondary mb-4">
                No projects deployed yet.
              </p>
              <Link
                href="/deploy"
                className="inline-flex items-center gap-2 px-6 py-3 bg-accent-lime text-text-dark font-mono font-bold text-small uppercase tracking-wider rounded-lg hover:bg-accent-lime-muted transition-all"
              >
                Deploy First App &rarr;
              </Link>
            </div>
          ) : (
            <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
              {projects.map((project, i) => (
                <motion.div
                  key={project.id}
                  initial={{ opacity: 0, scale: 0.95 }}
                  animate={{ opacity: 1, scale: 1 }}
                  transition={{ delay: 0.2 + i * 0.1 }}
                  className="bg-bg-secondary rounded-card border border-border-dark overflow-hidden hover:border-accent-lime/50 transition-colors flex flex-col"
                >
                  <div className="p-5 border-b border-border-dark flex justify-between items-start">
                    <div>
                      <h3 className="font-serif text-h3 mb-1 flex items-center gap-2">
                        {project.name}
                        {project.project_type && (
                          <span className="px-2 py-0.5 bg-bg-primary rounded text-[10px] font-mono text-accent-lime uppercase border border-accent-lime/30">
                            {project.project_type}
                          </span>
                        )}
                      </h3>
                      <a href={project.repo_url} target="_blank" rel="noreferrer" className="flex items-center gap-1.5 font-mono text-[11px] text-text-secondary hover:text-text-primary transition-colors">
                        <Github size={12} /> {project.repo_url.replace('https://github.com/', '')} <span className="px-1.5 py-0.5 bg-bg-primary rounded">{project.branch}</span>
                      </a>
                    </div>

                    <div className="flex items-center gap-2">
                      <Link href={`/deploy?id=${project.id}`} className="p-2 text-text-secondary hover:text-accent-lime transition-colors rounded hover:bg-bg-primary" title="Redeploy">
                        <RefreshCw size={16} />
                      </Link>
                      <button onClick={() => handleDeleteProject(project.id)} className="p-2 text-text-secondary hover:text-status-error transition-colors rounded hover:bg-bg-primary" title="Delete Project">
                        <Trash2 size={16} />
                      </button>
                    </div>
                  </div>

                  <div className="p-5 bg-bg-primary/30 flex-1 grid grid-cols-2 gap-4">
                    {/* Status Column */}
                    <div>
                      <h4 className="font-mono text-[10px] uppercase text-text-secondary mb-2">Service Status</h4>
                      {project.project_type === 'static' ? (
                        <div className="flex items-center gap-2 font-mono text-small text-text-primary">
                          <CheckCircle2 size={14} className="text-accent-lime" /> Static Files (Nginx)
                        </div>
                      ) : project.serviceStatus ? (
                        <div className="space-y-2">
                          <div className="flex items-center gap-2 font-mono text-small">
                            <span className={`relative flex h-2 w-2`}>
                              {project.serviceStatus.status === 'active' && <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-accent-lime opacity-75" />}
                              <span className={`relative inline-flex rounded-full h-2 w-2 ${project.serviceStatus.status === 'active' ? 'bg-accent-lime' : 'bg-status-error'}`} />
                            </span>
                            <span className={project.serviceStatus.status === 'active' ? 'text-accent-lime' : 'text-status-error'}>
                              {project.serviceStatus.status === 'active' ? 'Running' : 'Stopped'}
                            </span>
                            <span className="text-text-secondary text-[11px]">
                              PID: {project.serviceStatus.main_pid || 'none'}
                            </span>
                          </div>

                          <div className="flex items-center gap-1 mt-1">
                            <button className="p-1 text-text-secondary hover:text-text-primary bg-bg-primary rounded border border-border-dark" title="Start">
                              <Play size={12} />
                            </button>
                            <button className="p-1 text-text-secondary hover:text-text-primary bg-bg-primary rounded border border-border-dark" title="Restart">
                              <RefreshCw size={12} />
                            </button>
                            <button className="p-1 text-text-secondary hover:text-status-error bg-bg-primary rounded border border-border-dark" title="Stop">
                              <Square size={12} />
                            </button>
                            <Link href={`/logs?service=opendeploy-app-${project.name}`} className="ml-2 font-mono text-[10px] text-text-secondary hover:text-accent-lime">View Logs &rarr;</Link>
                          </div>
                        </div>
                      ) : (
                        <div className="flex items-center gap-2 font-mono text-small text-status-warning">
                          <Activity size={14} /> Service not found
                        </div>
                      )}
                    </div>

                    {/* Deploy Column */}
                    <div>
                      <h4 className="font-mono text-[10px] uppercase text-text-secondary mb-2">Latest Deploy</h4>
                      {project.latestDeploy ? (
                        <div className="space-y-1">
                          <div className="flex items-center gap-2">
                            <span className={`w-1.5 h-1.5 rounded-full ${project.latestDeploy.status === 'success' ? 'bg-accent-lime' : project.latestDeploy.status === 'running' ? 'bg-blue-400 animate-pulse' : 'bg-status-error'}`} />
                            <span className="font-mono text-small capitalize text-text-primary">
                              {project.latestDeploy.status}
                            </span>
                          </div>
                          <div className="flex items-center gap-1.5 font-mono text-[11px] text-text-secondary">
                            <Clock size={10} /> {dayjs(project.latestDeploy.created_at).fromNow()}
                          </div>
                          {project.latestDeploy.commit_hash && (
                            <div className="font-mono text-[10px] text-text-secondary truncate mt-1" title={project.latestDeploy.commit_message}>
                              <span className="text-text-primary">{project.latestDeploy.commit_hash.substring(0, 7)}</span>: {project.latestDeploy.commit_message}
                            </div>
                          )}
                        </div>
                      ) : (
                        <div className="font-mono text-small text-text-secondary italic">Never deployed</div>
                      )}
                    </div>
                  </div>
                </motion.div>
              ))}
            </div>
          )}
        </div>

        {/* Activity Feed + Pipeline */}
        <div className="grid grid-cols-1 lg:grid-cols-5 gap-6 mb-8">
          {/* Activity Feed - 60% */}
          <div className="lg:col-span-3 bg-bg-secondary rounded-card border border-border-dark overflow-hidden">
            <div className="px-6 py-4 border-b border-border-dark flex items-center justify-between">
              <span className="font-mono text-label uppercase tracking-wider text-text-secondary">Live Request Log</span>
              <span className="font-mono text-[10px] text-text-secondary">Auto-refresh: ON</span>
            </div>
            <div className="overflow-x-auto">
              <table className="w-full font-mono text-small">
                <thead>
                  <tr className="text-text-secondary border-b border-border-dark">
                    <th className="px-6 py-3 text-left font-normal text-label uppercase tracking-wider">Time</th>
                    <th className="px-3 py-3 text-left font-normal text-label uppercase tracking-wider">Method</th>
                    <th className="px-3 py-3 text-left font-normal text-label uppercase tracking-wider">Path</th>
                    <th className="px-3 py-3 text-right font-normal text-label uppercase tracking-wider">Status</th>
                    <th className="px-6 py-3 text-right font-normal text-label uppercase tracking-wider">Duration</th>
                  </tr>
                </thead>
                <tbody>
                  {logs.slice(0, 10).map((entry: any, i) => (
                    <tr key={i} className="border-b border-border-dark/50 hover:bg-bg-primary/50 transition-colors">
                      <td className="px-6 py-2.5 text-text-secondary">{entry.timestamp?.split('T')[1]?.split('.')[0] || entry.timestamp}</td>
                      <td className="px-3 py-2.5">
                        <span className={`
                          ${entry.method === 'GET' ? 'text-accent-lime' : ''}
                          ${entry.method === 'POST' ? 'text-blue-400' : ''}
                          ${entry.method === 'PUT' ? 'text-status-warning' : ''}
                          ${entry.method === 'DELETE' ? 'text-status-error' : ''}
                        `}>
                          {entry.method || 'GET'}
                        </span>
                      </td>
                      <td className="px-3 py-2.5 text-text-primary">{entry.path || entry.message?.split(' ')[1] || entry.message}</td>
                      <td className="px-3 py-2.5 text-right">
                        <span className={`
                          ${entry.status >= 200 && entry.status < 300 ? 'text-accent-lime' : ''}
                          ${entry.status >= 300 && entry.status < 400 ? 'text-text-secondary' : ''}
                          ${entry.status >= 400 ? 'text-status-error' : ''}
                        `}>
                          {entry.status || 200}
                        </span>
                      </td>
                      <td className="px-6 py-2.5 text-right text-text-secondary">{entry.duration || '--'}</td>
                    </tr>
                  ))}
                  {logs.length === 0 && (
                    <tr>
                      <td colSpan={5} className="px-6 py-4 text-center text-text-secondary">No recent requests</td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>

          {/* Pipeline Diagram - 40% */}
          <div className="lg:col-span-2 bg-bg-secondary rounded-card border border-border-dark p-6 flex flex-col justify-center">
            <span className="font-mono text-label uppercase tracking-wider text-text-secondary mb-4">Data Flow</span>
            <PipelineDiagram />
          </div>
        </div>

        {/* Quick Actions */}
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
          {quickActions.map((action, i) => (
            <motion.div
              key={action.label}
              initial={{ opacity: 0, y: 20 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: 0.4 + i * 0.1 }}
            >
              <Link
                href={action.href}
                className="block bg-bg-secondary rounded-card border border-border-dark p-6 hover:-translate-y-1 hover:shadow-xl hover:border-accent-lime/30 transition-all cursor-pointer group h-full"
              >
                <action.icon size={24} className="text-accent-lime mb-3 group-hover:scale-110 transition-transform" />
                <h3 className="font-serif text-h3 mb-1">{action.label}</h3>
                <p className="font-mono text-small text-text-secondary">{action.desc}</p>
              </Link>
            </motion.div>
          ))}
        </div>
      </motion.div>
  )
}
