export type ServiceStatus = 'running' | 'stopped' | 'error' | 'active' | 'inactive'
export type SetupStep = 'wifi' | 'internet' | 'tunnel' | 'deploy' | 'nginx'

export const NAV_ITEMS = [
  { label: 'Overview', path: '/dashboard', icon: 'LayoutDashboard' },
  { label: 'WiFi Setup', path: '/wifi', icon: 'Wifi' },
  { label: 'Internet', path: '/internet', icon: 'Globe' },
  { label: 'Cloudflare Tunnel', path: '/tunnel', icon: 'Shield' },
  { label: 'GitHub Deploy', path: '/deploy', icon: 'GitBranch' },
  { label: 'Nginx Config', path: '/nginx', icon: 'Server' },
  { label: 'Services', path: '/dashboard', icon: 'Activity' },
  { label: 'Logs', path: '/logs', icon: 'Terminal' },
] as const

export const NAV_ITEMS_BOTTOM = [
  { label: 'Settings', path: '/settings', icon: 'Settings' },
  { label: 'Help', path: '#', icon: 'HelpCircle' },
] as const
