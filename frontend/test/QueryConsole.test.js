import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import QueryConsole from '../src/components/QueryConsole.vue'
import { flush, mockFetch } from './helpers'

describe('QueryConsole', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('renders a PII rejection as a red callout', async () => {
    mockFetch({ 'POST /databases/d1/query': [422, { error: { message: 'query reads PII column public.users.email', code: 422 } }] })
    const w = mount(QueryConsole, { props: { databaseId: 'd1' } })
    await w.find('[data-test="sql-input"]').setValue('select email from users')
    await w.find('[data-test="run"]').trigger('click')
    await flush(); await flush()
    const callout = w.find('[data-test="rejection"]')
    expect(callout.exists()).toBe(true)
    expect(callout.text()).toContain('Query rejected')
    expect(callout.text()).toContain('public.users.email')
    expect(callout.attributes('role')).toBe('alert')
    expect(w.find('[data-test="result"]').exists()).toBe(false)
  })

  it('renders results with NULLs and the truncated notice', async () => {
    mockFetch({ 'POST /databases/d1/query': { columns: ['id', 'note'], rows: [[1, null], [2, 'x']], rowCount: 2, truncated: true } })
    const w = mount(QueryConsole, { props: { databaseId: 'd1' } })
    await w.find('[data-test="sql-input"]').setValue('select id, note from t')
    await w.find('[data-test="sql-input"]').trigger('keydown', { key: 'Enter', ctrlKey: true })
    await flush(); await flush()
    expect(w.findAll('th').map((t) => t.text())).toEqual(['id', 'note'])
    expect(w.findAll('tbody tr')).toHaveLength(2)
    expect(w.find('tbody').text()).toContain('NULL')
    expect(w.find('[data-test="truncated"]').exists()).toBe(true)
    expect(w.text()).toContain('2 rows')
  })

  it('shows other errors as plain errors', async () => {
    mockFetch({ 'POST /databases/d1/query': [400, { error: { message: 'syntax error at or near "selec"' } }] })
    const w = mount(QueryConsole, { props: { databaseId: 'd1' } })
    await w.find('[data-test="sql-input"]').setValue('selec 1')
    await w.find('[data-test="run"]').trigger('click')
    await flush(); await flush()
    expect(w.find('[data-test="query-error"]').text()).toContain('syntax error')
    expect(w.find('[data-test="rejection"]').exists()).toBe(false)
  })
})
