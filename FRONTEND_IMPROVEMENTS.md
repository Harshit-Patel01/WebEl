# Frontend Improvements Based on Backend Changes

## 1. Application Service Controls (NEW)

The backend now supports OpenRC service management inside containers. Add service-level controls:

### API Endpoints Available:
- `POST /api/v1/containers/{containerId}/service/start`
- `POST /api/v1/containers/{containerId}/service/stop`
- `POST /api/v1/containers/{containerId}/service/restart`
- `GET /api/v1/containers/{containerId}/service/status`
- `GET /api/v1/containers/{containerId}/service/logs?lines=100`

### UI Enhancement:
Add a new "Service" section in the container card showing:
- Service status (running/stopped/failed)
- Service control buttons (Start/Stop/Restart Service)
- Quick access to service logs (separate from container logs)

**Location:** `frontend/app/deployments/page.tsx` - Container Status section (line 645-743)

```tsx
// Add after container actions
<div className="mt-3 pt-3 border-t border-border-dark">
  <div className="flex items-center justify-between mb-2">
    <span className="font-mono text-[10px] text-text-secondary">Application Service</span>
    <span className={`font-mono text-[9px] px-2 py-0.5 ${serviceStatus === 'running' ? 'bg-emerald-900/30 text-emerald-400' : 'bg-red-900/30 text-red-400'}`}>
      {serviceStatus.toUpperCase()}
    </span>
  </div>
  <div className="flex gap-1.5">
    <button onClick={() => handleServiceAction(container.id, 'start')} className="...">
      Start Service
    </button>
    <button onClick={() => handleServiceAction(container.id, 'stop')} className="...">
      Stop Service
    </button>
    <button onClick={() => handleServiceAction(container.id, 'restart')} className="...">
      Restart Service
    </button>
    <button onClick={() => handleViewServiceLogs(container.id)} className="...">
      Service Logs
    </button>
  </div>
</div>
```

---

## 2. Framework Detection Display (NEW)

The backend now detects frameworks automatically. Show this in the UI:

### Display Framework Info:
- Show detected framework badge on deployment cards
- Display framework-specific icons (React, Next.js, Vue, etc.)
- Show auto-detected commands (install, build, start)

**Location:** `frontend/app/deployments/page.tsx` - Deploy card (line 774-849)

Already partially implemented at line 794-797, but can be enhanced:

```tsx
{deploy.framework && (
  <div className="flex items-center gap-2">
    <FrameworkIcon framework={deploy.framework} />
    <span className="font-mono text-[10px] text-accent-lime">
      {deploy.framework}
    </span>
    {deploy.is_backend && (
      <span className="text-[9px] text-cyan-400">(Backend)</span>
    )}
  </div>
)}
```

---

## 3. Working Directory Indicator (NEW)

Show the working directory used for builds:

**Location:** `frontend/app/deployments/page.tsx` - Project info section

```tsx
{project.working_directory && project.working_directory !== '.' && (
  <p className="flex items-center gap-1">
    <Folder size={10} />
    <span className="text-text-secondary">Working dir:</span>
    <span className="text-accent-lime">{project.working_directory}</span>
  </p>
)}
```

---

## 4. Build Command Override UI (ENHANCEMENT)

Add ability to override auto-detected commands in the deploy form:

**Location:** `frontend/app/deploy/page.tsx`

Add fields for:
- Custom install command (overrides auto-detected)
- Custom build command (overrides auto-detected)
- Custom start command (overrides auto-detected)
- Working directory (subdirectory within repo)

```tsx
<div className="space-y-4">
  <div>
    <label className="block font-mono text-label mb-2">
      Working Directory (optional)
    </label>
    <input
      type="text"
      placeholder="frontend/ or backend/ or leave empty for root"
      className="w-full px-4 py-3 bg-bg-primary border border-border-dark"
    />
    <p className="mt-1 text-[11px] text-text-secondary font-mono">
      Subdirectory where package.json is located
    </p>
  </div>

  <div>
    <label className="block font-mono text-label mb-2">
      Install Command (optional)
    </label>
    <input
      type="text"
      placeholder="Auto-detected from framework"
      className="w-full px-4 py-3 bg-bg-primary border border-border-dark"
    />
  </div>

  <div>
    <label className="block font-mono text-label mb-2">
      Build Command (optional)
    </label>
    <input
      type="text"
      placeholder="Auto-detected from framework"
      className="w-full px-4 py-3 bg-bg-primary border border-border-dark"
    />
  </div>

  <div>
    <label className="block font-mono text-label mb-2">
      Start Command (optional, backend only)
    </label>
    <input
      type="text"
      placeholder="Auto-detected from framework"
      className="w-full px-4 py-3 bg-bg-primary border border-border-dark"
    />
  </div>
</div>
```

---

## 5. Port Mapping Visualization (ENHANCEMENT)

Better visualization of the port flow: Container → LXD Proxy → Nginx → Cloudflare Tunnel

**Location:** `frontend/app/deployments/page.tsx` - Container card

```tsx
<div className="mt-2 p-2 bg-bg-primary border border-border-dark">
  <div className="font-mono text-[9px] text-text-secondary mb-1">Port Flow:</div>
  <div className="flex items-center gap-1 text-[10px] font-mono">
    <span className="text-cyan-400">Container:{containerPort}</span>
    <span className="text-text-secondary">→</span>
    <span className="text-accent-lime">Host:{hostPort}</span>
    {nginxPort && (
      <>
        <span className="text-text-secondary">→</span>
        <span className="text-purple-400">Nginx:{nginxPort}</span>
      </>
    )}
    {tunnelDomain && (
      <>
        <span className="text-text-secondary">→</span>
        <span className="text-blue-400">Tunnel:{tunnelDomain}</span>
      </>
    )}
  </div>
</div>
```

---

## 6. Real-time Service Status (NEW)

Poll service status every 10 seconds for running containers:

```tsx
useEffect(() => {
  if (projectContainers.length === 0) return

  const interval = setInterval(async () => {
    for (const container of projectContainers) {
      if (container.status === 'running') {
        const status = await fetch(
          `/api/v1/containers/${container.id}/service/status`,
          { credentials: 'include' }
        ).then(r => r.json())

        setServiceStatuses(prev => ({
          ...prev,
          [container.id]: status.status
        }))
      }
    }
  }, 10000)

  return () => clearInterval(interval)
}, [projectContainers])
```

---

## 7. Deployment Phase Indicators (ENHANCEMENT)

Show current deployment phase with progress:

**Phases from backend:**
- `clone` - Cloning repository
- `detect` - Detecting framework
- `build` - Installing dependencies & building
- `service` - Setting up service
- `done` - Deployment complete

```tsx
<div className="flex items-center gap-2 mb-3">
  {phases.map(phase => (
    <div key={phase} className={`flex items-center gap-1 ${currentPhase === phase ? 'text-accent-lime' : 'text-text-secondary'}`}>
      {currentPhase === phase && <Loader className="animate-spin" size={12} />}
      <span className="text-[10px] font-mono">{phase}</span>
    </div>
  ))}
</div>
```

---

## 8. Framework-Specific Help Text (NEW)

Show framework-specific guidance based on detected framework:

```tsx
{detectedFramework === 'nextjs' && (
  <div className="p-3 bg-blue-900/20 border border-blue-800/50">
    <p className="text-[11px] font-mono text-blue-400">
      Next.js detected. Make sure your next.config.js has output: 'standalone' for optimal deployment.
    </p>
  </div>
)}

{detectedFramework === 'react' && (
  <div className="p-3 bg-blue-900/20 border border-blue-800/50">
    <p className="text-[11px] font-mono text-blue-400">
      React app detected. Build output will be served from the 'dist' directory.
    </p>
  </div>
)}
```

---

## 9. Output Directory Detection Display (NEW)

Show detected output directory:

```tsx
{deploy.output_dir && (
  <span className="font-mono text-[10px] text-text-secondary flex items-center gap-1">
    <Folder size={10} />
    Output: <span className="text-accent-lime">{deploy.output_dir}</span>
  </span>
)}
```

---

## 10. Full-Stack Deployment Indicator (ENHANCEMENT)

Better visualization for full-stack deployments with frontend + backend containers:

```tsx
{project.project_type === 'fullstack' && (
  <div className="p-3 bg-purple-900/20 border border-purple-800/50">
    <div className="flex items-center gap-2 mb-2">
      <span className="font-mono text-[10px] text-purple-400 font-bold">FULL-STACK DEPLOYMENT</span>
    </div>
    <div className="grid grid-cols-2 gap-2 text-[10px] font-mono">
      <div className="p-2 bg-bg-primary border border-border-dark">
        <div className="text-purple-400 mb-1">Frontend</div>
        <div className="text-text-secondary">Port: {frontendPort}</div>
        <div className="text-text-secondary">Path: {frontendDir}</div>
      </div>
      <div className="p-2 bg-bg-primary border border-border-dark">
        <div className="text-blue-400 mb-1">Backend</div>
        <div className="text-text-secondary">Port: {backendPort}</div>
        <div className="text-text-secondary">Path: {backendDir}</div>
      </div>
    </div>
  </div>
)}
```

---

## Summary of Changes Needed

### High Priority:
1. ✅ **Service lifecycle controls** - Start/Stop/Restart application service
2. ✅ **Service status polling** - Real-time service health
3. ✅ **Framework detection display** - Show auto-detected framework
4. ✅ **Command override UI** - Allow custom install/build/start commands

### Medium Priority:
5. ✅ **Working directory input** - Support subdirectory deployments
6. ✅ **Port flow visualization** - Better port mapping display
7. ✅ **Deployment phase indicators** - Show current build phase

### Low Priority:
8. ✅ **Framework-specific help** - Context-aware guidance
9. ✅ **Output directory display** - Show detected output path
10. ✅ **Full-stack visualization** - Better full-stack deployment UI

---

## Implementation Order

1. Start with **service lifecycle controls** (most impactful)
2. Add **framework detection display** (already partially done)
3. Implement **command override UI** in deploy form
4. Add **working directory support** in deploy form
5. Polish with **phase indicators** and **help text**
