import { afterEach, describe, expect, it, vi } from 'vitest'
import DatabaseFormView from '../src/views/DatabaseFormView.vue'
import { useDatabaseStore } from '../src/stores/databaseStore'
import { useToasts } from '../src/composables/useToasts'
import { flush, jsonResponse, mockFetch, mountAt } from './helpers'

const DRIVERS = { drivers: [{ id: 'postgres', name: 'PostgreSQL', available: true }, { id: 'mysql', name: 'MySQL', available: true }] }
const DB = { id: 'd1', name: 'analytics', driver: 'postgres', dsnMasked: 'postgres://app:****@h/db', piiColumnCount: 0, mcpUrl: 'https://x/mcp/d1' }

describe('DatabaseFormView', () => {
  afterEach(() => { vi.unstubAllGlobals(); document.body.innerHTML = ''; useToasts().toasts.value = [] })

  it('disables a driver that is not available yet, with a Coming soon badge', async () => {
    mockFetch({ 'GET /drivers': { drivers: [...DRIVERS.drivers, { id: 'oracle', name: 'Oracle', available: false }] } })
    const { wrapper } = await mountAt(DatabaseFormView, { path: '/databases/new' })
    const pg = wrapper.find('[data-test="driver-postgres"]')
    const ora = wrapper.find('[data-test="driver-oracle"]')
    expect(pg.attributes('disabled')).toBeUndefined()
    expect(pg.attributes('aria-checked')).toBe('true')
    expect(ora.attributes('disabled')).toBeDefined()
    expect(ora.find('[data-test="coming-soon"]').text()).toBe('Coming soon')
    // Each engine shows its own logo; one without falls back to the cylinder.
    expect(pg.find('[data-test="driver-icon"]').attributes('alt')).toBe('PostgreSQL')
    expect(wrapper.find('[data-test="driver-mysql"] [data-test="driver-icon"]').attributes('alt')).toBe('MySQL')
    expect(ora.find('[data-test="driver-icon"]').exists()).toBe(false)
    expect(ora.find('svg').exists()).toBe(true)
    await ora.trigger('click')
    expect(pg.attributes('aria-checked')).toBe('true')
    expect(wrapper.find('[data-test="dsn-format"]').text()).toContain('postgres://')
  })

  it('falls back to the built-in driver list', async () => {
    mockFetch({ 'GET /drivers': [500, {}] })
    const { wrapper } = await mountAt(DatabaseFormView, { path: '/databases/new' })
    expect(wrapper.find('[data-test="driver-postgres"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[data-test="driver-mysql"]').attributes('disabled')).toBeUndefined()
  })

  it('creates a MySQL database from its DSN', async () => {
    const f = mockFetch({ 'GET /drivers': DRIVERS, 'POST /databases/test': { ok: true, serverVersion: 'MySQL 8.4.3' }, 'POST /databases': [201, { database: { ...DB, driver: 'mysql' } }] })
    const { wrapper } = await mountAt(DatabaseFormView, { path: '/databases/new' })
    const my = wrapper.find('[data-test="driver-mysql"]')
    expect(my.find('[data-test="coming-soon"]').exists()).toBe(false)
    await my.trigger('click')
    expect(my.attributes('aria-checked')).toBe('true')
    expect(wrapper.find('#db-dsn').attributes('placeholder')).toBe('mysql://user:pass@host:3306/db?tls=true')
    expect(wrapper.find('[data-test="dsn-format"]').text()).toContain('user:pass@tcp(host:3306)/db')

    await wrapper.find('#db-name').setValue('shop')
    await wrapper.find('#db-dsn').setValue('app:p@tcp(db:3306)/shop')
    await wrapper.find('[data-test="test-connection"]').trigger('click')
    await flush()
    expect(wrapper.find('[data-test="test-ok"]').text()).toContain('MySQL 8.4.3')
    expect(JSON.parse(f.mock.calls.at(-1)[1].body)).toEqual({ database: { driver: 'mysql', dsn: 'app:p@tcp(db:3306)/shop' } })

    await wrapper.find('form').trigger('submit')
    await flush(); await flush()
    expect(JSON.parse(f.mock.calls.at(-1)[1].body)).toEqual({ database: { name: 'shop', driver: 'mysql', dsn: 'app:p@tcp(db:3306)/shop' } })
  })

  it('keeps browsers and password managers from filling the form', async () => {
    mockFetch({ 'GET /drivers': DRIVERS })
    const { wrapper } = await mountAt(DatabaseFormView, { path: '/databases/new' })
    expect(wrapper.find('form').attributes('autocomplete')).toBe('off')
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    for (const id of ['#db-name', '#db-dsn']) {
      const input = wrapper.find(id)
      expect(input.attributes('autocomplete')).toBe('off')
      expect(input.attributes('data-1p-ignore')).toBeDefined()
      expect(input.attributes('data-lpignore')).toBe('true')
      expect(input.attributes('data-bwignore')).toBeDefined()
      expect(input.attributes('data-form-type')).toBe('other')
    }
  })

  it('toggles DSN visibility and tests the connection', async () => {
    const f = mockFetch({ 'GET /drivers': DRIVERS, 'POST /databases/test': { ok: true, serverVersion: 'PostgreSQL 17.2' } })
    const { wrapper } = await mountAt(DatabaseFormView, { path: '/databases/new' })
    const dsn = wrapper.find('#db-dsn')
    expect(dsn.attributes('type')).toBe('text')
    expect(dsn.attributes('data-masked')).toBe('true')
    expect(dsn.classes()).toContain('[-webkit-text-security:disc]')
    expect(dsn.attributes('placeholder')).toBe('postgres://user:pass@host:5432/db?sslmode=require')
    await wrapper.find('[data-test="toggle-dsn"]').trigger('click')
    expect(dsn.attributes('data-masked')).toBe('false')
    expect(dsn.classes()).not.toContain('[-webkit-text-security:disc]')
    expect(wrapper.find('[data-test="test-connection"]').attributes('disabled')).toBeDefined()
    await dsn.setValue('postgres://u:p@h/db')
    await wrapper.find('[data-test="test-connection"]').trigger('click')
    await flush()
    expect(wrapper.find('[data-test="test-ok"]').text()).toContain('PostgreSQL 17.2')
    expect(JSON.parse(f.mock.calls.at(-1)[1].body)).toEqual({ database: { driver: 'postgres', dsn: 'postgres://u:p@h/db' } })
    await dsn.setValue('postgres://u:p@h/db2')
    expect(wrapper.find('[data-test="test-ok"]').exists()).toBe(false)
  })

  it('shows a failed connection test', async () => {
    mockFetch({ 'GET /drivers': DRIVERS, 'POST /databases/test': [400, { error: { message: 'password authentication failed' } }] })
    const { wrapper } = await mountAt(DatabaseFormView, { path: '/databases/new' })
    await wrapper.find('#db-dsn').setValue('postgres://bad')
    await wrapper.find('[data-test="test-connection"]').trigger('click')
    await flush()
    expect(wrapper.find('[data-test="test-error"]').text()).toBe('password authentication failed')
  })

  it('creates a database, updates the store and opens it', async () => {
    const f = mockFetch({ 'GET /drivers': DRIVERS, 'POST /databases': [201, { database: DB }] })
    const { wrapper, router } = await mountAt(DatabaseFormView, { path: '/databases/new' })
    expect(wrapper.find('[data-test="save"]').attributes('disabled')).toBeDefined()
    await wrapper.find('#db-name').setValue(' analytics ')
    await wrapper.find('#db-dsn').setValue('postgres://u:p@h/db')
    await wrapper.find('form').trigger('submit')
    await flush(); await flush()
    expect(JSON.parse(f.mock.calls.at(-1)[1].body)).toEqual({ database: { name: 'analytics', driver: 'postgres', dsn: 'postgres://u:p@h/db' } })
    expect(useDatabaseStore().getById('d1').name).toBe('analytics')
    expect(router.currentRoute.value.path).toBe('/databases/d1')
  })

  it('toasts a failed save', async () => {
    mockFetch({ 'GET /drivers': DRIVERS, 'POST /databases': [400, { error: { message: 'could not connect' } }] })
    const { wrapper } = await mountAt(DatabaseFormView, { path: '/databases/new' })
    await wrapper.find('#db-name').setValue('a')
    await wrapper.find('#db-dsn').setValue('postgres://x')
    await wrapper.find('form').trigger('submit')
    await flush(); await flush()
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'error', message: 'could not connect' })
  })

  it('edits with a blank DSN meaning keep, and locks the driver', async () => {
    const f = mockFetch({ 'GET /drivers': DRIVERS, 'GET /databases/d1': { database: DB }, 'PUT /databases/d1': { database: { ...DB, name: 'renamed' } } })
    const { wrapper, router } = await mountAt(DatabaseFormView, { path: '/databases/d1/edit', pattern: '/databases/:id/edit' })
    expect(wrapper.find('#db-name').element.value).toBe('analytics')
    expect(wrapper.find('[data-test="dsn-keep-hint"]').exists()).toBe(true)
    expect(wrapper.find('#db-dsn').attributes('placeholder')).toBe(DB.dsnMasked)
    expect(wrapper.find('[data-test="driver-postgres"]').attributes('disabled')).toBeDefined()
    await wrapper.find('#db-name').setValue('renamed')
    await wrapper.find('form').trigger('submit')
    await flush(); await flush()
    expect(JSON.parse(f.mock.calls.at(-1)[1].body)).toEqual({ database: { name: 'renamed', dsn: '' } })
    expect(router.currentRoute.value.path).toBe('/databases/d1')
  })

  it('shows a load error on edit', async () => {
    mockFetch({ 'GET /drivers': DRIVERS, 'GET /databases/d1': [404, { error: { message: 'not found' } }] })
    const { wrapper } = await mountAt(DatabaseFormView, { path: '/databases/d1/edit', pattern: '/databases/:id/edit' })
    expect(wrapper.text()).toContain('not found')
  })

  it('deletes through the modal', async () => {
    mockFetch({ 'GET /drivers': DRIVERS, 'GET /databases/d1': { database: DB }, 'DELETE /databases/d1': jsonResponse(null, 204) })
    const { wrapper, router } = await mountAt(DatabaseFormView, { path: '/databases/d1/edit', pattern: '/databases/:id/edit' })
    useDatabaseStore().upsert(DB)
    await wrapper.find('[data-test="delete"]').trigger('click')
    document.querySelector('[data-test="confirm-delete"]').click()
    await flush(); await flush()
    expect(useDatabaseStore().databases).toEqual([])
    expect(router.currentRoute.value.path).toBe('/')
  })
})
