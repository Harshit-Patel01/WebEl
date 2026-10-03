import type { Config } from 'tailwindcss'

const config: Config = {
  content: [
    './app/**/*.{js,ts,jsx,tsx,mdx}',
    './components/**/*.{js,ts,jsx,tsx,mdx}',
  ],
  theme: {
    extend: {
      colors: {
        'bg-primary': 'var(--color-bg-primary)',
        'bg-secondary': 'var(--color-bg-secondary)',
        'bg-surface': 'var(--color-bg-surface)',
        'accent-lime': 'var(--color-accent)',
        'accent-lime-muted': 'var(--color-accent-hover)',
        'text-primary': 'var(--color-text-primary)',
        'text-secondary': 'var(--color-text-secondary)',
        'text-dark': 'var(--color-text-dark)',
        'border-dark': 'var(--color-border)',
        'border-light': 'var(--color-border-light)',
        'status-success': 'var(--color-success)',
        'status-error': 'var(--color-error)',
        'status-warning': 'var(--color-warning)',
      },
      fontFamily: {
        serif: ['Cormorant Garamond', 'serif'],
        sans: ['DM Sans', 'sans-serif'],
        mono: ['IBM Plex Mono', 'monospace'],
      },
      fontSize: {
        'display': ['96px', { lineHeight: '1.0', fontWeight: '700' }],
        'display-sm': ['72px', { lineHeight: '1.05', fontWeight: '700' }],
        'h1': ['48px', { lineHeight: '1.1', fontWeight: '700' }],
        'h2': ['32px', { lineHeight: '1.2', fontWeight: '600' }],
        'h3': ['20px', { lineHeight: '1.3', fontWeight: '700' }],
        'body': ['15px', { lineHeight: '1.6' }],
        'small': ['13px', { lineHeight: '1.5' }],
        'label': ['11px', { lineHeight: '1.4', fontWeight: '700', letterSpacing: '0.08em' }],
      },
      borderRadius: {
        'card': '16px',
      },
      animation: {
        'pulse-slow': 'pulse 3s ease-in-out infinite',
        'glow': 'glow 2s ease-in-out infinite',
        'shimmer': 'shimmer 2s infinite linear',
        'dash': 'dash 1.5s linear infinite',
        'fade-in': 'fadeIn 0.5s ease-out',
        'slide-in-right': 'slideInRight 0.3s ease-out',
      },
      keyframes: {
        glow: {
          '0%, 100%': { boxShadow: '0 0 20px rgba(170, 255, 69, 0.3)' },
          '50%': { boxShadow: '0 0 30px rgba(170, 255, 69, 0.5)' },
        },
        shimmer: {
          '0%': { backgroundPosition: '-200% 0' },
          '100%': { backgroundPosition: '200% 0' },
        },
        dash: {
          '0%': { strokeDashoffset: '20' },
          '100%': { strokeDashoffset: '0' },
        },
        fadeIn: {
          '0%': { opacity: '0', transform: 'translateY(10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' },
        },
        slideInRight: {
          '0%': { opacity: '0', transform: 'translateX(20px)' },
          '100%': { opacity: '1', transform: 'translateX(0)' },
        },
      },
    },
  },
  plugins: [],
}
export default config
