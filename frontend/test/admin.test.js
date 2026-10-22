import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { setActivePinia, createPinia } from 'pinia'
import {
  adminBase, checkAdminEmail, fetchAdminConfig, fetchAdminSession, fetchAdminSettings,
  resetAdminSettings, saveAdminSettings,
} from '../src/api'
import { clickOutside, createAdminGuard, createSqlMcpApp, routes } from '../src/app'
import App from '../src/App.vue'
import AdminLoginView from '../src/views/AdminLoginView.vue'
import AdminView from '../src/views/AdminView.vue'
import { useAdminStore } from '../src/stores/adminStore'
import { useToasts } from '../src/composables/useToasts'
import { flush, jsonResponse, mockFetch, mountAt } from './helpers'

const SETTINGS = {
  allowedEmails: ['*@example.com', 'root@sqlmcp.local'],
  queryMaxRows: 1000,
  queryInlineRows: 50,
  queryTimeoutSeconds: 30,
  source: 'env',
  server: { baseUrl: 'https://sql.example.com', sslEnabled: true, domain: 'sql.example.com', httpsPort: 3443, googleLoginEnabled: true, rootLoginEnabled: false, appDbDriver: 'sqlite', tmphostApiUrl: 'https://app.tmphost.io' },
}

const settle = async () => { for (let i = 0; i < 5; i++) await flush() }

afterEach(() => { vi.unstubAllGlobals(); document.body.innerHTML = ''; useToasts().toasts.value = []; delete window.__SQLMCP_BASE_PATH__ })

describe('admin api', () => {
  it('lives under /admin/api/v1 and the base path', () => {
    expect(adminBase()).toBe('/admin/api/v1')
    window.__SQLMCP_BASE_PATH__ = '/sql/'
    expect(adminBase()).toBe('/sql/admin/api/v1')
  })

  it('calls each route without refreshing a session', async () => {
    const fetch = mockFetch({
      'GET /config': { enabled: 1 },
      'GET /session': [401, { error: { message: 'unauthorized' } }],
      'GET /settings': SETTINGS,
      'PUT /settings': (init) => ({ ...SETTINGS, ...JSON.parse(init.body), source: 'saved' }),
      'DELETE /settings': SETTINGS,
      'POST /settings/check-email': (init) => ({ allowed: true, matchedEntry: JSON.parse(init.body).allowedEmails[0] }),
    })
    expect(await fetchAdminConfig()).toEqual({ enabled: true })
    expect(await fetchAdminSession()).toBeNull()
    expect(fetch.mock.calls.some(([url]) => String(url).includes('/auth/refresh'))).toBe(false)
    expect((await fetchAdminSettings()).source).toBe('env')
    const saved = await saveAdminSettings({ allowedEmails: ['a@b.c'], queryMaxRows: 5, queryInlineRows: 2, queryTimeoutSeconds: 9, extra: 1 })
    expect(saved).toMatchObject({ allowedEmails: ['a@b.c'], source: 'saved' })
    expect(JSON.parse(fetch.mock.calls.at(-1)[1].body)).not.toHaveProperty('extra')
    expect((await resetAdminSettings()).source).toBe('env')
    expect(await checkAdminEmail('x@y.z', ['*@y.z'])).toEqual({ allowed: true, matchedEntry: '*@y.z' })
    expect(String(fetch.mock.calls.at(-1)[0])).toBe('/admin/api/v1/settings/check-email')
  })

  it('passes other session errors on', async () => {
    mockFetch({ 'GET /session': [500, { error: { message: 'boom' } }] })
    await expect(fetchAdminSession()).rejects.toThrow('boom')
  })
})

describe('admin store and guard', () => {
  it('signs in, remembers the session and signs out even when the call fails', async () => {
    setActivePinia(createPinia())
    const fetch = mockFetch({
      'POST /login': { token: 't', expiresAt: '2030-01-01T00:00:00Z' },
      'POST /logout': [500, { error: { message: 'down' } }],
    })
    const admin = useAdminStore()
    await admin.login('root-token')
    expect(admin.session).toEqual({ expiresAt: '2030-01-01T00:00:00Z' })
    expect(await admin.loadSession()).toEqual(admin.session)
    expect(fetch).toHaveBeenCalledTimes(1)
    await expect(admin.logout()).rejects.toThrow('down')
    expect(admin.session).toBeNull()
  })

  it('sends admin pages without a session to /admin/login', async () => {
    const guard = createAdminGuard(async () => null)
    expect(await guard({ meta: { admin: true } })).toEqual({ path: '/admin/login' })
    expect(await guard({ meta: { admin: true, public: true } })).toBe(true)
    expect(await guard({ meta: {} })).toBe(true)
    expect(await createAdminGuard(async () => ({ expiresAt: 'x' }))({ meta: { admin: true } })).toBe(true)
    expect(await createAdminGuard(async () => { throw new Error('x') })({ meta: { admin: true } })).toEqual({ path: '/admin/login' })
    expect(routes.map((r) => r.path)).toEqual(expect.arrayContaining(['/admin', '/admin/login']))
  })

  it('keeps admin and user guards apart in the app', async () => {
    mockFetch({ 'GET /session': [401, {}], 'GET /auth/user': [401, {}], 'POST /auth/refresh': [401, {}] })
    const { router } = createSqlMcpApp({ history: createMemoryHistory() })
    await router.push('/admin')
    expect(router.currentRoute.value.path).toBe('/admin/login')
    await router.push('/settings')
    expect(router.currentRoute.value.path).toBe('/login')
  })
})

describe('AdminLoginView', () => {
  it('signs in with the root token and opens the setup', async () => {
    mockFetch({
      'GET /config': { enabled: true },
      'POST /login': (init) => (JSON.parse(init.body).rootToken === 'good-token-12345'
        ? { token: 't', expiresAt: '2030-01-01T00:00:00Z' }
        : [401, { error: { message: 'invalid root token' } }]),
    })
    const { wrapper, router } = await mountAt(AdminLoginView, { path: '/admin/login' })
    expect(wrapper.text()).toContain('SQL MCP Admin')
    await wrapper.find('input').setValue('wrong')
    await wrapper.find('form').trigger('submit')
    await settle()
    expect(wrapper.find('[data-test="admin-error"]').text()).toBe('invalid root token')
    await wrapper.find('input').setValue('good-token-12345')
    await wrapper.find('form').trigger('submit')
    await settle()
    expect(router.currentRoute.value.path).toBe('/admin')
  })

  it('explains when the setup is disabled', async () => {
    mockFetch({ 'GET /config': { enabled: false } })
    const { wrapper } = await mountAt(AdminLoginView, { path: '/admin/login' })
    expect(wrapper.find('[data-test="admin-disabled"]').text()).toContain('ROOT_TOKEN')
    expect(wrapper.find('form').exists()).toBe(false)
  })

  it('survives a config failure and a network error', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.stubGlobal('fetch', vi.fn(async () => { throw new Error('offline') }))
    const { wrapper } = await mountAt(AdminLoginView, { path: '/admin/login' })
    expect(wrapper.find('[data-test="admin-disabled"]').exists()).toBe(true)
    expect(warn).toHaveBeenCalled()
    warn.mockRestore()
  })

  it('shows a connection error and ignores an empty token', async () => {
    let calls = 0
    vi.stubGlobal('fetch', vi.fn(async (url) => {
      calls++
      if (String(url).endsWith('/config')) return jsonResponse({ enabled: true })
      throw new Error('offline')
    }))
    const { wrapper } = await mountAt(AdminLoginView, { path: '/admin/login' })
    await wrapper.find('form').trigger('submit')
    expect(calls).toBe(1)
    await wrapper.find('input').setValue('x')
    await wrapper.find('form').trigger('submit')
    await settle()
    expect(wrapper.find('[data-test="admin-error"]').text()).toBe('Connection error')
  })
})

describe('AdminView', () => {
  it('shows the setup and saves a changed allowlist and limits', async () => {
    let current = { ...SETTINGS }
    const fetch = mockFetch({
      'GET /settings': () => current,
      'PUT /settings': (init) => { current = { ...current, ...JSON.parse(init.body), source: 'saved', updatedAt: '2026-10-22T02:00:00Z' }; return current },
    })
    const { wrapper } = await mountAt(AdminView, { path: '/admin' })
    expect(wrapper.find('[data-test="setup-source"]').text()).toContain('From the environment')
    expect(wrapper.find('[data-test="allowed-emails"]').element.value).toBe('*@example.com\nroot@sqlmcp.local')
    expect(wrapper.find('[data-test="server-card"]').text()).toContain('sql.example.com:3443')
    expect(wrapper.find('[data-test="server-card"]').text()).toContain('disabled')
    expect(wrapper.find('[data-test="setup-save"]').attributes('disabled')).toBeDefined()

    await wrapper.find('[data-test="allowed-emails"]').setValue(' *@agentrq.com, ops@example.org\n\n')
    await wrapper.find('[data-test="max-rows"]').setValue('500')
    await wrapper.find('[data-test="inline-rows"]').setValue('20')
    await wrapper.find('[data-test="timeout"]').setValue('15')
    expect(wrapper.find('[data-test="setup-save"]').attributes('disabled')).toBeUndefined()
    await wrapper.find('[data-test="setup-save"]').trigger('click')
    await settle()
    expect(JSON.parse(fetch.mock.calls.at(-1)[1].body)).toEqual({ allowedEmails: ['*@agentrq.com', 'ops@example.org'], queryMaxRows: 500, queryInlineRows: 20, queryTimeoutSeconds: 15 })
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'success', message: 'Setup saved' })
    expect(wrapper.find('[data-test="setup-source"]').text()).toContain('Saved configuration')
    expect(wrapper.find('[data-test="allowed-emails"]').element.value).toBe('*@agentrq.com\nops@example.org')
  })

  it('warns on an empty list and shows a refused save', async () => {
    mockFetch({ 'GET /settings': SETTINGS, 'PUT /settings': [400, { error: { message: 'allowed emails needs at least one entry' } }] })
    const { wrapper } = await mountAt(AdminView, { path: '/admin' })
    await wrapper.find('[data-test="allowed-emails"]').setValue('  ')
    expect(wrapper.text()).toContain('At least one entry is required')
    await wrapper.find('[data-test="setup-save"]').trigger('click')
    await settle()
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'error', message: 'allowed emails needs at least one entry' })
  })

  it('resets a saved configuration after confirming', async () => {
    let current = { ...SETTINGS, source: 'saved', updatedAt: '2026-10-22T02:00:00Z', allowedEmails: ['*@agentrq.com'] }
    mockFetch({
      'GET /settings': () => current,
      'DELETE /settings': () => { current = SETTINGS; return current },
    })
    const { wrapper } = await mountAt(AdminView, { path: '/admin' })
    await wrapper.find('[data-test="setup-reset"]').trigger('click')
    document.querySelector('[data-test="confirm-delete"]').click()
    await settle()
    expect(wrapper.find('[data-test="setup-reset"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="allowed-emails"]').element.value).toBe('*@example.com\nroot@sqlmcp.local')
  })

  it('reports a failed reset', async () => {
    mockFetch({ 'GET /settings': { ...SETTINGS, source: 'saved', updatedAt: '2026-10-22T02:00:00Z' }, 'DELETE /settings': [500, { error: { message: 'down' } }] })
    const { wrapper } = await mountAt(AdminView, { path: '/admin' })
    await wrapper.find('[data-test="setup-reset"]').trigger('click')
    document.querySelector('[data-test="confirm-delete"]').click()
    await settle()
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'error', message: 'down' })
  })

  it('tests an email against the unsaved entries', async () => {
    const fetch = mockFetch({
      'GET /settings': SETTINGS,
      'POST /settings/check-email': (init) => {
        const { email } = JSON.parse(init.body)
        if (email === 'bad@x') return [400, { error: { message: 'bad pattern' } }]
        return email.endsWith('@agentrq.com') ? { allowed: true, matchedEntry: '*@agentrq.com' } : { allowed: false }
      },
    })
    const { wrapper } = await mountAt(AdminView, { path: '/admin' })
    await wrapper.find('[data-test="allowed-emails"]').setValue('*@agentrq.com')
    await wrapper.find('[data-test="check-email"]').setValue('dev@agentrq.com')
    await wrapper.find('[data-test="check-email-submit"]').trigger('submit')
    await settle()
    expect(JSON.parse(fetch.mock.calls.at(-1)[1].body).allowedEmails).toEqual(['*@agentrq.com'])
    expect(wrapper.find('[data-test="check-result"]').text()).toBe('Allowed by *@agentrq.com')
    await wrapper.find('[data-test="check-email"]').setValue('dev@other.com')
    await wrapper.find('form').trigger('submit')
    await settle()
    expect(wrapper.find('[data-test="check-result"]').text()).toBe('Not allowed')
    await wrapper.find('[data-test="check-email"]').setValue('bad@x')
    await wrapper.find('form').trigger('submit')
    await settle()
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'error', message: 'bad pattern' })
  })

  it('shows a load error', async () => {
    mockFetch({ 'GET /settings': [500, { error: { message: 'internal server error' } }] })
    const { wrapper } = await mountAt(AdminView, { path: '/admin' })
    expect(wrapper.text()).toContain('internal server error')
  })
})

describe('App shell for the admin', () => {
  it('shows the admin sidebar without databases and signs out to /admin/login', async () => {
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener() {} }))
    const fetch = mockFetch({
      'GET /session': { expiresAt: '2030-01-01T00:00:00Z' },
      'GET /settings': SETTINGS,
      'POST /logout': jsonResponse(null, 204),
    })
    const { router, pinia } = createSqlMcpApp({ history: createMemoryHistory() })
    await router.push('/admin')
    const w = mount(App, { attachTo: document.body, global: { plugins: [router, pinia], directives: { 'click-outside': clickOutside } } })
    await settle()
    expect(w.find('[data-test="admin-badge"]').exists()).toBe(true)
    expect(w.text()).toContain('Setup')
    expect(w.text()).toContain('Administrator')
    expect(fetch.mock.calls.some(([url]) => String(url).includes('/databases'))).toBe(false)
    await w.find('button.flex.items-center.gap-3.w-full').trigger('click')
    await w.find('[data-test="logout"]').trigger('click')
    await settle()
    expect(router.currentRoute.value.path).toBe('/admin/login')
    delete window.matchMedia
  })

  it('signs out locally when the logout call fails', async () => {
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener() {} }))
    mockFetch({ 'GET /session': { expiresAt: 'x' }, 'GET /settings': SETTINGS, 'POST /logout': [500, {}] })
    const { router, pinia } = createSqlMcpApp({ history: createMemoryHistory() })
    await router.push('/admin')
    const w = mount(App, { attachTo: document.body, global: { plugins: [router, pinia], directives: { 'click-outside': clickOutside } } })
    await settle()
    await w.find('button.flex.items-center.gap-3.w-full').trigger('click')
    await w.find('[data-test="logout"]').trigger('click')
    await settle()
    expect(router.currentRoute.value.path).toBe('/admin/login')
    delete window.matchMedia
  })
})
