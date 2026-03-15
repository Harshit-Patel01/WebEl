# Deploy Page - Cloudflare Tunnel Integration

## Overview

The deploy page (`/deploy`) now includes comprehensive Cloudflare Tunnel deployment options, allowing users to choose between:
- **Nginx Only** - Traditional nginx deployment
- **Tunnel Only** - Deploy via Cloudflare Tunnel with automatic HTTPS
- **Both** - Deploy with both nginx and Cloudflare Tunnel

## Features Added

### 1. Deployment Target Selection

Users can now choose their deployment target with three options:

```typescript
const [deploymentTarget, setDeploymentTarget] = useState<'nginx' | 'tunnel' | 'both'>('nginx')
```

**Options:**
- **Nginx Only**: Traditional deployment with nginx configuration
- **Tunnel Only**: Deploy via Cloudflare Tunnel (automatic HTTPS, DNS, CDN)
- **Both**: Deploy with both nginx (local) and Cloudflare Tunnel (public)

### 2. Automatic Tunnel Setup Detection

The system checks if Cloudflare Tunnel is configured before allowing tunnel deployments:

```typescript
const checkTunnel = async () => {
  const apiKey = apiKeyStorage.get()
  if (!apiKey) {
    setHasTunnelSetup(false)
    return
  }

  const status = await fetch('/api/v1/tunnel/status')
  setHasTunnelSetup(status.status !== 'not_configured')
}
```

**Benefits:**
- Prevents errors when tunnel is not set up
- Guides users to tunnel setup page if needed
- Shows/hides tunnel options based on setup status

### 3. Tunnel Configuration Panel

When "Tunnel Only" or "Both" is selected, a configuration panel appears with:

- **Local Port**: Auto-filled based on project type (8000 for backend, 3001 for frontend)
- **Protocol**: HTTP or HTTPS
- **Benefits Display**: Shows automatic HTTPS, DNS, and CDN features

### 4. Automatic Tunnel Route Creation

After successful deployment, the system automatically creates a Cloudflare Tunnel route:

```typescript
const handleDeployComplete = async (result) => {
  if (result.status === 'success') {
    // Create tunnel route if tunnel deployment is selected
    if ((deploymentTarget === 'tunnel' || deploymentTarget === 'both') && hasTunnelSetup && domain) {
      const apiKey = apiKeyStorage.get()
      const fullDomain = subdomain ? `${subdomain}.${domain}` : domain

      await fetch('/api/v1/tunnel/routes', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CF-API-Key': apiKey
        },
        body: JSON.stringify({
          hostname: fullDomain,
          zone_id: selectedZoneId,
          local_scheme: tunnelScheme,
          local_port: parseInt(tunnelPort, 10)
        })
      })
    }
    setState('success')
  }
}
```

### 5. Success Page Enhancement

The success page now shows tunnel information when deployed via tunnel:

- **Tunnel URL**: Clickable link to the deployed app
- **HTTPS Badge**: Shows automatic HTTPS is enabled
- **DNS Status**: Confirms DNS record was created
- **Access Information**: Clear instructions on how to access the app

## User Workflows

### Workflow 1: Deploy Frontend via Tunnel

1. User goes to `/deploy`
2. Fills in repository details
3. Selects **Project Type**: Frontend
4. Selects **Deployment Target**: Tunnel Only
5. Selects Cloudflare domain from dropdown
6. Enters subdomain (e.g., "app")
7. System auto-fills port: 3001
8. Clicks "Deploy Project"
9. System:
   - Clones repository
   - Builds frontend
   - Creates tunnel route: `app.example.com` → port 3001
   - Creates DNS CNAME record
   - Restarts cloudflared
10. Success page shows: `https://app.example.com`

### Workflow 2: Deploy Backend via Tunnel

1. User goes to `/deploy`
2. Fills in repository details
3. Selects **Project Type**: Backend
4. Selects **Deployment Target**: Tunnel Only
5. Selects Cloudflare domain
6. Enters subdomain (e.g., "api")
7. System auto-fills port: 8000
8. Clicks "Deploy Project"
9. System:
   - Clones repository
   - Builds and starts backend in Docker
   - Creates tunnel route: `api.example.com` → port 8000
   - Creates DNS CNAME record
10. Success page shows: `https://api.example.com`

### Workflow 3: Deploy with Both Nginx and Tunnel

1. User goes to `/deploy`
2. Fills in repository details
3. Selects **Deployment Target**: Both
4. Selects Cloudflare domain
5. Enters subdomain
6. Clicks "Deploy Project"
7. System:
   - Deploys with nginx configuration (local access)
   - Creates tunnel route (public HTTPS access)
   - Both nginx and tunnel serve the same app
8. App accessible via:
   - Local nginx: `http://localhost` or configured domain
   - Public tunnel: `https://subdomain.example.com`

### Workflow 4: User Without Tunnel Setup

1. User selects "Tunnel Only" or "Both"
2. System detects no tunnel setup
3. Shows confirmation dialog:
   > "Cloudflare Tunnel is not set up. Would you like to set it up now?"
4. If Yes: Redirects to `/tunnel/dashboard`
5. If No: Reverts to "Nginx Only" option

### Workflow 5: Deploy Without Domain

1. User goes to `/deploy`
2. Fills in repository details
3. Leaves domain configuration empty
4. Clicks "Deploy Project"
5. System deploys without nginx or tunnel
6. App runs but not publicly accessible
7. User can add domain later from deployments page

## UI Components

### Deployment Target Selector

```tsx
<div className="grid grid-cols-3 gap-2 bg-bg-primary p-1.5">
  <button
    onClick={() => setDeploymentTarget('nginx')}
    className={deploymentTarget === 'nginx' ? 'bg-accent-lime' : ''}
  >
    Nginx Only
  </button>
  <button
    onClick={() => setDeploymentTarget('tunnel')}
    className={deploymentTarget === 'tunnel' ? 'bg-accent-lime' : ''}
  >
    Tunnel Only
  </button>
  <button
    onClick={() => setDeploymentTarget('both')}
    className={deploymentTarget === 'both' ? 'bg-accent-lime' : ''}
  >
    Both
  </button>
</div>
```

### Tunnel Configuration Panel

```tsx
{(deploymentTarget === 'tunnel' || deploymentTarget === 'both') && (
  <div className="p-4 bg-blue-900/20 border border-blue-800/50">
    <div className="space-y-3">
      <input
        type="number"
        value={tunnelPort}
        placeholder="8000"
      />
      <select value={tunnelScheme}>
        <option value="http">HTTP</option>
        <option value="https">HTTPS</option>
      </select>
      <p>
        ✓ Automatic HTTPS via Cloudflare<br/>
        ✓ DNS record created automatically<br/>
        ✓ Global CDN and DDoS protection
      </p>
    </div>
  </div>
)}
```

### Success Page with Tunnel Info

```tsx
{(deploymentTarget === 'tunnel' || deploymentTarget === 'both') && domain && (
  <div className="p-3 bg-blue-900/20 border border-blue-800/50">
    <a href={`https://${subdomain}.${domain}`} target="_blank">
      https://{subdomain}.{domain}
    </a>
    <p>Your app is now accessible via Cloudflare Tunnel with automatic HTTPS</p>
  </div>
)}
```

## State Management

### New State Variables

```typescript
const [deploymentTarget, setDeploymentTarget] = useState<'nginx' | 'tunnel' | 'both'>('nginx')
const [tunnelPort, setTunnelPort] = useState('')
const [tunnelScheme, setTunnelScheme] = useState('http')
const [hasTunnelSetup, setHasTunnelSetup] = useState(false)
const [checkingTunnel, setCheckingTunnel] = useState(true)
```

### Auto-Fill Logic

```typescript
useEffect(() => {
  if (projectType === 'backend') {
    setTunnelPort('8000')
  } else {
    setTunnelPort('3001')
  }
}, [projectType])
```

## API Integration

### Check Tunnel Status

```typescript
GET /api/v1/tunnel/status
Response: { status: "active" | "not_configured" }
```

### Create Tunnel Route

```typescript
POST /api/v1/tunnel/routes
Headers: { X-CF-API-Key: "..." }
Body: {
  hostname: "app.example.com",
  zone_id: "...",
  local_scheme: "http",
  local_port: 3001
}
```

### Get Cloudflare Zones

```typescript
GET /api/v1/tunnel/zones/stored
Headers: { X-CF-API-Key: "..." }
Response: [{ id: "...", name: "example.com" }]
```

## Deployment Flow

### Complete Deployment Flow with Tunnel

```
1. User fills form
   ↓
2. User selects "Tunnel Only"
   ↓
3. System checks tunnel setup
   ↓
4. User selects domain and subdomain
   ↓
5. System auto-fills port based on project type
   ↓
6. User clicks "Deploy Project"
   ↓
7. System creates project in database
   ↓
8. System saves environment variables
   ↓
9. System triggers deployment
   ↓
10. Deployment runs (clone, build, start)
    ↓
11. On success, system creates tunnel route
    ↓
12. DNS CNAME record created in Cloudflare
    ↓
13. Cloudflared service restarted
    ↓
14. Success page shows tunnel URL
    ↓
15. App accessible at https://subdomain.example.com
```

## Configuration Examples

### Example 1: React App via Tunnel

**Configuration:**
- Project Type: Frontend
- Deployment Target: Tunnel Only
- Domain: example.com
- Subdomain: app
- Port: 3001 (auto-filled)
- Protocol: http

**Result:**
- App accessible at: `https://app.example.com`
- DNS: CNAME `app.example.com` → `<tunnel-id>.cfargotunnel.com`
- Tunnel route: `app.example.com` → `http://localhost:3001`

### Example 2: Node.js API via Tunnel

**Configuration:**
- Project Type: Backend
- Deployment Target: Tunnel Only
- Domain: example.com
- Subdomain: api
- Port: 8000 (auto-filled)
- Protocol: http

**Result:**
- API accessible at: `https://api.example.com`
- DNS: CNAME `api.example.com` → `<tunnel-id>.cfargotunnel.com`
- Tunnel route: `api.example.com` → `http://localhost:8000`

### Example 3: Full Stack with Both

**Configuration:**
- Project Type: Frontend
- Deployment Target: Both
- Domain: example.com
- Subdomain: app
- Port: 3001
- Nginx: Enabled

**Result:**
- Local access: `http://localhost` (via nginx)
- Public access: `https://app.example.com` (via tunnel)
- Both serve the same app
- Nginx can proxy to backend on same server
- Tunnel provides public HTTPS access

## Benefits

### For Users

1. **One-Click Deployment**: Deploy to Cloudflare Tunnel in one step
2. **Automatic HTTPS**: No SSL certificate configuration needed
3. **Automatic DNS**: No manual DNS record creation
4. **Smart Defaults**: Port auto-filled based on project type
5. **Flexible Options**: Choose nginx, tunnel, or both
6. **Clear Feedback**: Success page shows tunnel URL and status

### For Developers

1. **Integrated Workflow**: Deploy and tunnel in single flow
2. **Error Prevention**: Checks tunnel setup before allowing deployment
3. **Automatic Configuration**: No manual tunnel route creation
4. **Consistent Experience**: Same UI for all deployment types
5. **Easy Testing**: Deploy to tunnel for quick testing

## Error Handling

### Tunnel Not Set Up

```typescript
if (!hasTunnelSetup) {
  if (confirm('Cloudflare Tunnel is not set up. Would you like to set it up now?')) {
    window.location.href = '/tunnel/dashboard'
  }
  return
}
```

### Zone Not Found

```typescript
if (!zone) {
  throw new Error(`Zone not found for domain ${rootDomain}. Make sure the domain is added to your Cloudflare account.`)
}
```

### Tunnel Route Creation Failed

```typescript
try {
  await createTunnelRoute()
} catch (err) {
  console.error('Failed to create tunnel route:', err)
  // Don't fail the deployment, just log the error
  // User can create tunnel route manually later
}
```

## Testing Checklist

- [ ] Nginx Only deployment works
- [ ] Tunnel Only deployment works
- [ ] Both deployment works
- [ ] Tunnel setup detection works
- [ ] Port auto-fill works for frontend
- [ ] Port auto-fill works for backend
- [ ] Domain selection works
- [ ] Subdomain input works
- [ ] Tunnel route created after deployment
- [ ] DNS record created in Cloudflare
- [ ] Success page shows tunnel URL
- [ ] Tunnel URL is clickable and works
- [ ] Error handling for no tunnel setup
- [ ] Error handling for invalid domain
- [ ] Deployment without domain works

## Files Modified

1. **`frontend/app/deploy/page.tsx`**
   - Added deployment target selection (nginx/tunnel/both)
   - Added tunnel setup detection
   - Added tunnel configuration panel
   - Added automatic tunnel route creation
   - Added tunnel info to success page
   - Added auto-fill for tunnel port
   - Enhanced error handling

## Summary

The deploy page now provides a complete, integrated Cloudflare Tunnel deployment experience:

✅ **Three deployment options**: Nginx, Tunnel, or Both
✅ **Automatic tunnel setup detection**
✅ **Smart port auto-fill** based on project type
✅ **Automatic tunnel route creation** after deployment
✅ **Clear success feedback** with tunnel URL
✅ **Comprehensive error handling**
✅ **User-friendly UI** with clear instructions

Users can now deploy their applications via Cloudflare Tunnel directly from the deploy page with minimal configuration and maximum convenience! 🚀
