// API Key management for Cloudflare tunnel operations
// Stored in browser localStorage, never sent to backend for storage

const API_KEY_STORAGE_KEY = 'opendeploy_cf_api_key'

export const apiKeyStorage = {
  /**
   * Get the stored API key from localStorage
   */
  get(): string | null {
    if (typeof window === 'undefined') return null
    return localStorage.getItem(API_KEY_STORAGE_KEY)
  },

  /**
   * Save API key to localStorage
   */
  set(apiKey: string): void {
    if (typeof window === 'undefined') return
    localStorage.setItem(API_KEY_STORAGE_KEY, apiKey)
  },

  /**
   * Remove API key from localStorage
   */
  clear(): void {
    if (typeof window === 'undefined') return
    localStorage.removeItem(API_KEY_STORAGE_KEY)
  },

  /**
   * Check if API key exists
   */
  exists(): boolean {
    return this.get() !== null
  },

  /**
   * Get masked version of API key for display (shows last 3 chars)
   */
  getMasked(): string {
    const key = this.get()
    if (!key) return ''
    if (key.length <= 3) return '•••'
    return '•••••••••' + key.slice(-3)
  }
}
