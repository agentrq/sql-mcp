import { defineStore } from 'pinia'

export const THEMES = ['light', 'dark', 'system']

function getStoredTheme() {
  try {
    const t = localStorage.getItem('theme')
    return THEMES.includes(t) ? t : null
  } catch {
    return null
  }
}

function prefersDark() {
  return typeof window.matchMedia === 'function' && window.matchMedia('(prefers-color-scheme: dark)').matches
}

export const useThemeStore = defineStore('theme', {
  state: () => ({
    theme: getStoredTheme() || 'system',
  }),
  getters: {
    isDark(state) {
      if (state.theme === 'dark') return true
      if (state.theme === 'light') return false
      return prefersDark()
    },
  },
  actions: {
    setTheme(newTheme) {
      if (!THEMES.includes(newTheme)) return
      this.theme = newTheme
      try {
        localStorage.setItem('theme', newTheme)
      } catch {
        // storage unavailable: the choice lasts for this page only
      }
      this.applyTheme()
    },
    applyTheme() {
      // Not the cached getter: in 'system' mode the answer changes with the OS.
      const dark = this.theme === 'dark' || (this.theme === 'system' && prefersDark())
      document.documentElement.classList.toggle('dark', dark)
      document.querySelector('meta[name="theme-color"]')?.setAttribute('content', dark ? '#09090b' : '#ffffff')
      document.querySelector('meta[name="apple-mobile-web-app-status-bar-style"]')?.setAttribute('content', dark ? 'black' : 'default')
    },
    init() {
      this.applyTheme()
      if (typeof window.matchMedia === 'function') {
        window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
          if (this.theme === 'system') this.applyTheme()
        })
      }
    },
  },
})
