import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { createSqlMcpApp, clickOutside } from '../src/bootstrap'
import App from '../src/App.vue'
import { flush, jsonResponse, mockFetch } from './helpers'

describe('App shell', () => {
  afterEach(() => { vi.unstubAllGlobals(); document.body.innerHTML = '' })

  it('lists databases in the sidebar and logs out', async () => {
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener() {} }))
    mockFetch({
      'GET /auth/user': { id: 'u', email: 'dev@example.com', name: 'Dev' },
      'GET /databases': { databases: [{ id: 'd1', name: 'Analytics DB', piiColumnCount: 2, mcpUrl: 'u' }] },
      'GET /settings/tmphost': { connected: false, serverUrl: 'https://app.tmphost.io' },
      'POST /auth/logout': jsonResponse(null, 204),
    })
    const { router, pinia } = createSqlMcpApp({ history: createMemoryHistory() })
    await router.push('/settings')
    const w = mount(App, { attachTo: document.body, global: { plugins: [router, pinia], directives: { 'click-outside': clickOutside } } })
    for (let i = 0; i < 6; i++) await flush()
    const rows = w.findAll('[data-test="sidebar-database"]')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toBe('analytics-db')
    expect(rows[0].attributes('href')).toBe('/databases/d1')
    expect(w.text()).toContain('SQL MCP')
    await w.find('button.flex.items-center.gap-3.w-full').trigger('click')
    await w.find('[data-test="theme-dark"]').trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    await w.find('[data-test="logout"]').trigger('click')
    for (let i = 0; i < 4; i++) await flush()
    expect(router.currentRoute.value.path).toBe('/login')
    delete window.matchMedia
  })
})
