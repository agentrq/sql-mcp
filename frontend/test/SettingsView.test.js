import { afterEach, describe, expect, it, vi } from 'vitest'
import SettingsView from '../src/views/SettingsView.vue'
import { useToasts } from '../src/composables/useToasts'
import { flush, jsonResponse, mockFetch, mountAt } from './helpers'

describe('SettingsView', () => {
  afterEach(() => { vi.unstubAllGlobals(); document.body.innerHTML = ''; useToasts().toasts.value = [] })

  it('offers Connect as a navigation link when not connected', async () => {
    mockFetch({ 'GET /settings/tmphost': { connected: false, serverUrl: 'https://app.tmphost.io' } })
    const { wrapper } = await mountAt(SettingsView, { path: '/settings' })
    expect(wrapper.find('[data-test="tmphost-connect"]').attributes('href')).toBe('/api/v1/settings/tmphost/connect')
    expect(wrapper.text()).toContain('expires after 30 minutes')
    expect(wrapper.find('input').exists()).toBe(false)
  })

  it('shows the account when connected and disconnects after confirming', async () => {
    let connected = true
    mockFetch({
      'GET /settings/tmphost': () => ({ connected, account: 'dev@example.com', serverUrl: 'https://app.tmphost.io', connectedAt: '2026-01-01T00:00:00Z' }),
      'DELETE /settings/tmphost': () => { connected = false; return jsonResponse(null, 204) },
    })
    const { wrapper } = await mountAt(SettingsView, { path: '/settings' })
    expect(wrapper.find('[data-test="tmphost-account"]').text()).toBe('dev@example.com')
    await wrapper.find('[data-test="tmphost-disconnect"]').trigger('click')
    document.querySelector('[data-test="confirm-delete"]').click()
    await flush(); await flush(); await flush()
    expect(wrapper.find('[data-test="tmphost-connect"]').exists()).toBe(true)
  })

  it('toasts the OAuth callback result and cleans the query', async () => {
    mockFetch({ 'GET /settings/tmphost': { connected: true, serverUrl: 'https://app.tmphost.io' } })
    const { router } = await mountAt(SettingsView, { path: '/settings?tmphost=connected' })
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'success', message: 'tmphost.io connected' })
    expect(router.currentRoute.value.query).toEqual({})

    const second = await mountAt(SettingsView, { path: '/settings?tmphost=error&message=access_denied' })
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'error', message: 'access_denied' })
    expect(second.router.currentRoute.value.fullPath).toBe('/settings')
  })
})
