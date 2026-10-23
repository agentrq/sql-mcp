import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ActivityPanel from '../src/components/ActivityPanel.vue'
import { useToasts } from '../src/composables/useToasts'
import { flush, mockFetch } from './helpers'

class FakeEventSource {
  static CLOSED = 2
  static instances = []
  constructor(url) {
    this.url = url
    this.listeners = {}
    FakeEventSource.instances.push(this)
  }
  addEventListener(type, fn) { this.listeners[type] = fn }
  close() { this.closed = true }
}

const q = (id, createdAt, extra = {}) => ({
  id, createdAt, source: 'mcp', sql: `select ${id} from orders`, status: 'ok', reason: '', rowCount: 3, durationMs: 12, resultUrl: '', ...extra,
})

const stream = () => FakeEventSource.instances.at(-1)

describe('ActivityPanel', () => {
  beforeEach(() => {
    FakeEventSource.instances = []
    vi.stubGlobal('EventSource', FakeEventSource)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    useToasts().toasts.value = []
  })

  async function mountPanel(routes) {
    const f = mockFetch(routes)
    const w = mount(ActivityPanel, { props: { databaseId: 'd1' }, attachTo: document.body })
    await flush(); await flush()
    return { w, f }
  }

  it('adds queries as they arrive on the stream', async () => {
    const { w } = await mountPanel({ 'GET /databases/d1/queries?limit=50': { queries: [q('a', '2026-10-23T04:00:00Z')] } })
    expect(stream().url).toBe('/api/v1/databases/d1/queries/stream')
    expect(w.find('[data-test="stream-status"]').attributes('data-status')).toBe('connecting')

    stream().onopen()
    await flush(); await flush()
    expect(w.find('[data-test="stream-status"]').text()).toBe('Live')

    stream().listeners.query({ data: JSON.stringify(q('b', '2026-10-23T04:01:00Z')) })
    stream().listeners.query({ data: JSON.stringify(q('b', '2026-10-23T04:01:00Z')) }) // a repeat is ignored
    await flush()
    const rows = w.findAll('[data-test="query-row"]')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('select b from orders')
    w.unmount()
    expect(stream().closed).toBe(true)
  })

  it('keeps a streamed query the catch-up load missed', async () => {
    let resolve
    const { w } = await mountPanel({ 'GET /databases/d1/queries?limit=50': () => new Promise((r) => { resolve = r }) })
    stream().listeners.query({ data: JSON.stringify(q('new', '2026-10-23T04:05:00Z')) })
    resolve({ queries: [q('old', '2026-10-23T04:00:00Z')] })
    await flush(); await flush()
    expect(w.findAll('[data-test="query-row"]').map((r) => r.text().includes('select new'))).toEqual([true, false])
  })

  it('shows the whole query in the sidebar when a row is clicked', async () => {
    const long = `select ${'col, '.repeat(200)}id from orders where note = 'x'`
    const { w } = await mountPanel({
      'GET /databases/d1/queries?limit=50': { queries: [
        q('a', '2026-10-23T04:00:00Z', { sql: long, resultUrl: 'https://tmphost.io/r/1' }),
        q('b', '2026-10-23T03:00:00Z', { status: 'rejected', reason: 'query reads PII column public.users.email', sql: 'select email from users' }),
      ] },
    })
    expect(w.find('[data-test="query-detail"]').exists()).toBe(false)

    await w.findAll('[data-test="query-row"]')[0].trigger('click')
    expect(w.find('[data-test="detail-sql"]').text()).toBe(long)
    expect(w.find('[data-test="detail-rows"]').text()).toBe('3')
    expect(w.find('[data-test="detail-result"]').attributes('href')).toBe('https://tmphost.io/r/1')
    expect(w.findAll('[data-test="query-row"]')[0].attributes('aria-selected')).toBe('true')

    const writeText = vi.fn(async () => {})
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    await w.find('[data-test="copy-sql"]').trigger('click')
    expect(writeText).toHaveBeenCalledWith(long)

    await w.findAll('[data-test="query-row"]')[1].trigger('keydown', { key: 'Enter' })
    expect(w.find('[data-test="detail-sql"]').text()).toBe('select email from users')
    expect(w.find('[data-test="detail-reason"]').text()).toContain('public.users.email')
    expect(w.find('[data-test="detail-status-rejected"]').exists()).toBe(true)
    expect(w.find('[data-test="detail-rows"]').exists()).toBe(false)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'a' }))
    await flush()
    expect(w.find('[data-test="query-detail"]').exists()).toBe(true)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flush()
    expect(w.find('[data-test="query-detail"]').exists()).toBe(false)

    await w.findAll('[data-test="query-row"]')[0].trigger('click')
    await w.find('[data-test="close-detail"]').trigger('click')
    expect(w.find('[data-test="query-detail"]').exists()).toBe(false)
    await w.findAll('[data-test="query-row"]')[0].trigger('click')
    await w.findAll('[data-test="query-row"]')[0].trigger('click')
    expect(w.find('[data-test="query-detail"]').exists()).toBe(false) // a second click closes it
    w.unmount()
  })

  it('offers to reconnect once the stream gives up', async () => {
    vi.useFakeTimers()
    try {
      mockFetch({ 'GET /databases/d1/queries?limit=50': { queries: [] }, 'POST /auth/refresh': [401, {}] })
      const w = mount(ActivityPanel, { props: { databaseId: 'd1' } })
      for (let i = 0; i < 4; i++) {
        stream().readyState = FakeEventSource.CLOSED
        stream().onerror()
        await vi.advanceTimersByTimeAsync(5000)
      }
      expect(w.find('[data-test="stream-status"]').text()).toBe('Offline')
      expect(w.find('[data-test="empty"]').exists()).toBe(true)
      const before = FakeEventSource.instances.length
      await w.find('[data-test="reconnect"]').trigger('click')
      expect(FakeEventSource.instances).toHaveLength(before + 1)
      w.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('shows a failed load', async () => {
    const { w } = await mountPanel({ 'GET /databases/d1/queries?limit=50': [500, { error: { message: 'boom' } }] })
    expect(w.text()).toContain('boom')
  })
})
