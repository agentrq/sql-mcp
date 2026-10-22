import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useThemeStore } from '../src/stores/themeStore'

function stubMatchMedia(dark) {
  const listeners = []
  const mql = { matches: dark, addEventListener: (_e, fn) => listeners.push(fn) }
  window.matchMedia = vi.fn(() => mql)
  return { mql, fire: () => listeners.forEach((fn) => fn()) }
}

describe('themeStore', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.classList.remove('dark')
    document.head.innerHTML = '<meta name="theme-color" content="#ffffff"><meta name="apple-mobile-web-app-status-bar-style" content="default">'
    setActivePinia(createPinia())
  })
  afterEach(() => { delete window.matchMedia; vi.restoreAllMocks() })

  it('defaults to system and follows the OS', () => {
    const mm = stubMatchMedia(true)
    const store = useThemeStore()
    expect(store.theme).toBe('system')
    expect(store.isDark).toBe(true)
    store.init()
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(document.querySelector('meta[name="theme-color"]').getAttribute('content')).toBe('#09090b')
    mm.mql.matches = false
    mm.fire()
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(document.querySelector('meta[name="apple-mobile-web-app-status-bar-style"]').getAttribute('content')).toBe('default')
  })

  it('ignores OS changes when a theme is chosen', () => {
    const mm = stubMatchMedia(false)
    const store = useThemeStore()
    store.init()
    store.setTheme('dark')
    expect(localStorage.getItem('theme')).toBe('dark')
    expect(store.isDark).toBe(true)
    mm.fire()
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    store.setTheme('light')
    expect(store.isDark).toBe(false)
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('reads a stored theme and rejects unknown ones', () => {
    localStorage.setItem('theme', 'dark')
    expect(useThemeStore().theme).toBe('dark')
    setActivePinia(createPinia())
    localStorage.setItem('theme', 'neon')
    const store = useThemeStore()
    expect(store.theme).toBe('system')
    store.setTheme('neon')
    expect(store.theme).toBe('system')
  })

  it('works without matchMedia or storage', () => {
    delete window.matchMedia
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked') })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    const store = useThemeStore()
    expect(store.theme).toBe('system')
    expect(store.isDark).toBe(false)
    store.init()
    store.setTheme('dark')
    expect(store.theme).toBe('dark')
  })
})
