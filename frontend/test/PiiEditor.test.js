import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PiiEditor from '../src/components/PiiEditor.vue'
import { useToasts } from '../src/composables/useToasts'
import { flush, mockFetch } from './helpers'

const SCHEMA = {
  tables: [
    { schema: 'public', name: 'users', columns: [
      { name: 'id', dataType: 'bigint', pii: false, suggestedPii: false },
      { name: 'email', dataType: 'text', pii: true, suggestedPii: true },
      { name: 'phone', dataType: 'text', pii: false, suggestedPii: true },
    ] },
    { schema: 'public', name: 'orders', columns: [{ name: 'id', dataType: 'bigint' }] },
  ],
}

describe('PiiEditor', () => {
  afterEach(() => { vi.unstubAllGlobals(); useToasts().toasts.value = [] })

  async function mountEditor(extra = {}, props = {}) {
    setActivePinia(createPinia())
    const f = mockFetch({ 'GET /databases/d1/schema': SCHEMA, 'GET /databases/d1/pii': { piiColumns: [] }, ...extra })
    const w = mount(PiiEditor, { props: { databaseId: 'd1', ...props } })
    await flush(); await flush()
    return { w, f }
  }

  it('lists tables, suggestions, and saves the full set', async () => {
    const { w, f } = await mountEditor({ 'PUT /databases/d1/pii': (init) => ({ piiColumns: JSON.parse(init.body).piiColumns }) })
    expect(w.findAll('[data-test="pii-table"]')).toHaveLength(2)
    expect(w.findAll('[data-test="suggested"]')).toHaveLength(2)
    expect(w.find('[data-test="selected-count"]').text()).toBe('1 column selected')
    expect(w.find('[data-test="unsaved"]').exists()).toBe(false)
    expect(w.find('[data-test="save-pii"]').attributes('disabled')).toBeDefined()

    await w.find('[data-test="select-suggested"]').trigger('click')
    expect(w.find('[data-test="selected-count"]').text()).toBe('2 columns selected')
    expect(w.find('[data-test="unsaved"]').exists()).toBe(true)

    await w.find('[data-test="save-pii"]').trigger('click')
    await flush(); await flush()
    const put = f.mock.calls.find((c) => c[1]?.method === 'PUT')
    expect(JSON.parse(put[1].body)).toEqual({ piiColumns: [
      { schema: 'public', table: 'users', column: 'email' },
      { schema: 'public', table: 'users', column: 'phone' },
    ] })
    expect(w.find('[data-test="unsaved"]').exists()).toBe(false)
    expect(w.emitted('saved')[0]).toEqual([2])
  })

  it('marks a whole table and filters by search', async () => {
    const { w } = await mountEditor()
    await w.findAll('[data-test="mark-all"]')[1].trigger('click')
    expect(w.find('[data-test="selected-count"]').text()).toBe('2 columns selected')
    await w.find('[data-test="pii-search"]').setValue('phone')
    expect(w.findAll('[data-test="pii-table"]')).toHaveLength(1)
    expect(w.findAll('[data-test="pii-column"]')).toHaveLength(1)
    await w.find('[data-test="pii-column"]').setValue(true)
    expect(w.find('[data-test="selected-count"]').text()).toBe('3 columns selected')
  })

  it('keeps columns the agent marked after the editor loaded', async () => {
    let calls = 0
    const { w, f } = await mountEditor({
      // By the save, the agent has marked orders.id.
      'GET /databases/d1/pii': () => ({ piiColumns: [
        { schema: 'public', table: 'users', column: 'email' },
        ...(calls++ ? [{ schema: 'public', table: 'orders', column: 'id' }] : []),
      ] }),
      'PUT /databases/d1/pii': (init) => ({ piiColumns: JSON.parse(init.body).piiColumns }),
    }, { agentMarksPii: true })
    await w.find('[data-test="select-suggested"]').trigger('click')
    await w.find('[data-test="save-pii"]').trigger('click')
    await flush(); await flush()
    const put = f.mock.calls.find((c) => c[1]?.method === 'PUT')
    expect(JSON.parse(put[1].body).piiColumns).toEqual([
      { schema: 'public', table: 'orders', column: 'id' },
      { schema: 'public', table: 'users', column: 'email' },
      { schema: 'public', table: 'users', column: 'phone' },
    ])
    expect(w.emitted('saved')[0]).toEqual([3])
  })

  it('turns the agent marking PII on and off', async () => {
    const { w, f } = await mountEditor({
      'PUT /databases/d1/pii/agent': (init) => ({ database: { id: 'd1', ...JSON.parse(init.body) } }),
    })
    const sw = w.find('[data-test="agent-marks-pii"]')
    expect(sw.attributes('aria-checked')).toBe('false')
    await sw.trigger('click')
    await flush(); await flush()
    expect(JSON.parse(f.mock.calls.at(-1)[1].body)).toEqual({ agentMarksPii: true })
    expect(w.emitted('agent-changed')[0]).toEqual([{ id: 'd1', agentMarksPii: true }])
    expect(useToasts().toasts.value.at(-1).message).toContain('can now mark')

    await w.setProps({ agentMarksPii: true })
    expect(sw.attributes('aria-checked')).toBe('true')
    await sw.trigger('click')
    await flush(); await flush()
    expect(JSON.parse(f.mock.calls.at(-1)[1].body)).toEqual({ agentMarksPii: false })
    expect(useToasts().toasts.value.at(-1).message).toContain('no longer')
  })

  it('reports a failed switch', async () => {
    const { w } = await mountEditor({ 'PUT /databases/d1/pii/agent': [500, { error: { message: 'boom' } }] })
    await w.find('[data-test="agent-marks-pii"]').trigger('click')
    await flush(); await flush()
    expect(w.emitted('agent-changed')).toBeUndefined()
    expect(useToasts().toasts.value.at(-1).message).toBe('boom')
  })

  it('shows a schema load error', async () => {
    const { w } = await mountEditor({ 'GET /databases/d1/schema': [500, { error: { message: 'permission denied' } }] })
    expect(w.text()).toContain('permission denied')
  })
})
