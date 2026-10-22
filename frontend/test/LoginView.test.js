import { afterEach, describe, expect, it, vi } from 'vitest'
import LoginView from '../src/views/LoginView.vue'
import { flush, mockFetch, mountAt } from './helpers'

describe('LoginView', () => {
  afterEach(() => { vi.unstubAllGlobals(); document.body.innerHTML = '' })

  it('shows the allow-list banner for email_not_allowed', async () => {
    mockFetch({ 'GET /auth/config': { googleLoginEnabled: true, rootLoginEnabled: false } })
    const { wrapper } = await mountAt(LoginView, { path: '/login?error=email_not_allowed' })
    expect(wrapper.find('[data-test="login-error"]').text()).toBe('This email is not on the allow list')
  })

  it('shows a generic banner for other errors and none without', async () => {
    mockFetch({ 'GET /auth/config': { googleLoginEnabled: true } })
    const { wrapper } = await mountAt(LoginView, { path: '/login?error=oauth_failed' })
    expect(wrapper.find('[data-test="login-error"]').text()).toContain('oauth failed')
    const plain = await mountAt(LoginView, { path: '/login' })
    expect(plain.wrapper.find('[data-test="login-error"]').exists()).toBe(false)
  })

  it('shows only the Google button when only Google is enabled', async () => {
    mockFetch({ 'GET /auth/config': { googleLoginEnabled: true, rootLoginEnabled: false } })
    const { wrapper } = await mountAt(LoginView, { path: '/login?redirect_url=%2Fdatabases%2F1' })
    const google = wrapper.find('[data-test="google-login"]')
    expect(google.exists()).toBe(true)
    expect(google.attributes('href')).toBe(`/api/v1/auth/google/login?redirect_url=${encodeURIComponent(window.location.origin + '/databases/1')}`)
    expect(wrapper.find('[data-test="root-login"]').exists()).toBe(false)
  })

  it('drops a foreign redirect_url', async () => {
    mockFetch({ 'GET /auth/config': { googleLoginEnabled: true } })
    const { wrapper } = await mountAt(LoginView, { path: '/login?redirect_url=https%3A%2F%2Fevil.com' })
    expect(wrapper.find('[data-test="google-login"]').attributes('href')).toBe('/api/v1/auth/google/login')
  })

  it('shows only the token form when only root login is enabled', async () => {
    mockFetch({ 'GET /auth/config': { googleLoginEnabled: false, rootLoginEnabled: true } })
    const { wrapper } = await mountAt(LoginView, { path: '/login' })
    expect(wrapper.find('[data-test="google-login"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="root-login"]').exists()).toBe(true)
  })

  it('says so when nothing is enabled, or the config fails', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    mockFetch({ 'GET /auth/config': [500, {}] })
    const { wrapper } = await mountAt(LoginView, { path: '/login' })
    expect(wrapper.find('[data-test="no-login"]').exists()).toBe(true)
  })

  it('signs in with the root token and goes home', async () => {
    const f = mockFetch({
      'GET /auth/config': { rootLoginEnabled: true },
      'POST /auth/root/login': { status: 'ok' },
      'GET /auth/user': { id: 'u' },
    })
    const { wrapper, router } = await mountAt(LoginView, { path: '/login' })
    await wrapper.find('input[type="password"]').setValue('secret')
    await wrapper.find('form').trigger('submit')
    await flush(); await flush()
    expect(JSON.parse(f.mock.calls.find((c) => c[0].endsWith('/auth/root/login'))[1].body)).toEqual({ rootToken: 'secret' })
    expect(router.currentRoute.value.path).toBe('/')
  })

  it('follows a same-origin redirect after root login', async () => {
    mockFetch({ 'GET /auth/config': { rootLoginEnabled: true }, 'POST /auth/root/login': { status: 'ok' } })
    const assign = vi.fn()
    const original = window.location
    Object.defineProperty(window, 'location', { configurable: true, value: { origin: original.origin, assign } })
    try {
      const { wrapper } = await mountAt(LoginView, { path: '/login?redirect_url=%2Fmcp%2Fabc%2Foauth2%2Fauthorize' })
      await wrapper.find('input[type="password"]').setValue('secret')
      await wrapper.find('form').trigger('submit')
      await flush(); await flush()
      expect(assign).toHaveBeenCalledWith(`${original.origin}/mcp/abc/oauth2/authorize`)
    } finally {
      Object.defineProperty(window, 'location', { configurable: true, value: original })
    }
  })

  it('shows the server error on a bad token', async () => {
    mockFetch({ 'GET /auth/config': { rootLoginEnabled: true }, 'POST /auth/root/login': [403, { error: { message: 'Invalid root token' } }] })
    const { wrapper } = await mountAt(LoginView, { path: '/login' })
    await wrapper.find('input[type="password"]').setValue('bad')
    await wrapper.find('form').trigger('submit')
    await flush(); await flush()
    expect(wrapper.find('[data-test="root-error"]').text()).toBe('Invalid root token')
  })
})
