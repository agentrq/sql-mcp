import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import * as api from '../src/api'
import { jsonResponse, mockFetch } from './helpers'

const bodyOf = (fetchMock, i = 0) => JSON.parse(fetchMock.mock.calls[i][1].body)

describe('api', () => {
  beforeEach(() => { delete window.__SQLMCP_BASE_PATH__ })
  afterEach(() => { vi.unstubAllGlobals(); delete window.__SQLMCP_BASE_PATH__ })

  it('builds the base from the injected base path', () => {
    expect(api.apiBase()).toBe('/api/v1')
    window.__SQLMCP_BASE_PATH__ = '/sql/'
    expect(api.apiBase()).toBe('/sql/api/v1')
    expect(api.googleLoginUrl('http://localhost/x?y=1')).toBe('/sql/api/v1/auth/google/login?redirect_url=http%3A%2F%2Flocalhost%2Fx%3Fy%3D1')
    expect(api.googleLoginUrl(null)).toBe('/sql/api/v1/auth/google/login')
    expect(api.tmphostConnectUrl()).toBe('/sql/api/v1/settings/tmphost/connect')
  })

  it('reads the auth config with defaults', async () => {
    mockFetch({ 'GET /auth/config': { googleLoginEnabled: true } })
    expect(await api.fetchAuthConfig()).toEqual({ googleLoginEnabled: true, rootLoginEnabled: false, basePath: '' })
    mockFetch({ 'GET /auth/config': jsonResponse(undefined, 200) })
    expect(await api.fetchAuthConfig()).toEqual({ googleLoginEnabled: false, rootLoginEnabled: false, basePath: '' })
    mockFetch({ 'GET /auth/config': { googleLoginEnabled: false, rootLoginEnabled: true, basePath: '/b' } })
    expect((await api.fetchAuthConfig()).basePath).toBe('/b')
  })

  it('fetchUser returns the user, null on 401, and throws otherwise', async () => {
    mockFetch({ 'GET /auth/user': { id: '1', email: 'a@b.c' } })
    expect(await api.fetchUser()).toEqual({ id: '1', email: 'a@b.c' })
    mockFetch({ 'GET /auth/user': [401, { error: { message: 'no', code: 401 } }], 'POST /auth/refresh': [401, {}] })
    expect(await api.fetchUser()).toBeNull()
    mockFetch({ 'GET /auth/user': [500, { error: { message: 'boom', code: 500 } }] })
    await expect(api.fetchUser()).rejects.toThrow('boom')
  })

  it('posts the root token and logs out', async () => {
    const f = mockFetch({ 'POST /auth/root/login': { status: 'ok' }, 'POST /auth/logout': jsonResponse(null, 204) })
    expect(await api.rootLogin('tok')).toEqual({ status: 'ok' })
    expect(bodyOf(f)).toEqual({ rootToken: 'tok' })
    expect(f.mock.calls[0][1].headers['Content-Type']).toBe('application/json')
    expect(await api.logout()).toBeNull()
  })

  it('turns error bodies into ApiError with status and message', async () => {
    mockFetch({ 'POST /auth/root/login': [403, { error: { message: 'Invalid token', code: 403 } }] })
    const err = await api.rootLogin('x').catch((e) => e)
    expect(err).toBeInstanceOf(api.ApiError)
    expect(err.status).toBe(403)
    expect(err.code).toBe(403)
    expect(err.message).toBe('Invalid token')
  })

  it('handles string errors, non-JSON errors and network failures', async () => {
    mockFetch({ 'GET /drivers': [400, { error: 'plain' }] })
    await expect(api.fetchDrivers()).rejects.toThrow('plain')
    mockFetch({ 'GET /drivers': new Response('<html>', { status: 502 }) })
    const err = await api.fetchDrivers().catch((e) => e)
    expect(err.message).toBe('Request failed (502)')
    expect(err.code).toBe(502)
    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('offline') }))
    const net = await api.fetchDrivers().catch((e) => e)
    expect(net.status).toBe(0)
    expect(net.message).toBe('offline')
    vi.stubGlobal('fetch', vi.fn(async () => { throw {} }))
    expect((await api.fetchDrivers().catch((e) => e)).message).toBe('Network error')
  })

  it('refreshes once on 401 and replays', async () => {
    let calls = 0
    const f = mockFetch({
      'GET /databases': () => (++calls === 1 ? [401, {}] : { databases: [{ id: 'a' }] }),
      'POST /auth/refresh': { status: 'ok' },
    })
    expect(await api.fetchDatabases()).toEqual([{ id: 'a' }])
    expect(f.mock.calls.map((c) => c[0])).toEqual(['/api/v1/databases', '/api/v1/auth/refresh', '/api/v1/databases'])
  })

  it('covers the database endpoints', async () => {
    const db = { id: 'x1', name: 'a' }
    const f = mockFetch({
      'GET /drivers': { drivers: [{ id: 'postgres' }] },
      'GET /databases': {},
      'GET /databases/x1': { database: db },
      'POST /databases': [201, { database: db }],
      'PUT /databases/x1': { database: db },
      'DELETE /databases/x1': jsonResponse(null, 204),
      'POST /databases/test': { ok: true, serverVersion: 'PostgreSQL 17.2' },
      'GET /databases/x1/schema': { tables: [{ name: 't' }] },
      'GET /databases/x1/pii': { piiColumns: [{ schema: 'public', table: 't', column: 'c' }] },
      'PUT /databases/x1/pii': (init) => ({ piiColumns: JSON.parse(init.body).piiColumns }),
      'POST /databases/x1/query': { columns: ['id'], rows: [[1]], rowCount: 1, truncated: false },
      'GET /databases/x1/queries?limit=10': { queries: [{ id: 'q' }] },
    })
    expect(await api.fetchDrivers()).toEqual([{ id: 'postgres' }])
    expect(await api.fetchDatabases()).toEqual([])
    expect(await api.fetchDatabase('x1')).toEqual(db)
    expect(await api.createDatabase({ name: 'a', driver: 'postgres', dsn: 'd' })).toEqual(db)
    expect(bodyOf(f, 3)).toEqual({ database: { name: 'a', driver: 'postgres', dsn: 'd' } })
    expect(await api.updateDatabase('x1', { name: 'a' })).toEqual(db)
    expect(bodyOf(f, 4)).toEqual({ database: { name: 'a', dsn: '' } })
    expect(await api.deleteDatabase('x1')).toBeNull()
    expect(await api.testConnection({ driver: 'postgres', dsn: 'd' })).toEqual({ ok: true, serverVersion: 'PostgreSQL 17.2' })
    expect(bodyOf(f, 6)).toEqual({ database: { driver: 'postgres', dsn: 'd' } })
    expect(await api.fetchSchema('x1')).toEqual([{ name: 't' }])
    expect(await api.fetchPiiColumns('x1')).toHaveLength(1)
    const cols = [{ schema: 'public', table: 't', column: 'email' }]
    expect(await api.savePiiColumns('x1', cols)).toEqual(cols)
    expect(bodyOf(f, 9)).toEqual({ piiColumns: cols })
    expect((await api.runQuery('x1', 'select 1')).rowCount).toBe(1)
    expect(bodyOf(f, 10)).toEqual({ sql: 'select 1' })
    expect(await api.fetchQueries('x1', 10)).toEqual([{ id: 'q' }])
  })

  it('defaults missing lists to empty arrays', async () => {
    mockFetch({
      'GET /drivers': {},
      'GET /databases/a/schema': {},
      'GET /databases/a/pii': {},
      'PUT /databases/a/pii': {},
      'GET /databases/a/queries?limit=50': {},
    })
    expect(await api.fetchDrivers()).toEqual([])
    expect(await api.fetchSchema('a')).toEqual([])
    expect(await api.fetchPiiColumns('a')).toEqual([])
    expect(await api.savePiiColumns('a', [])).toEqual([])
    expect(await api.fetchQueries('a')).toEqual([])
  })

  it('points the query stream at the database and renews the session', async () => {
    expect(api.queryStreamUrl('a/b')).toBe('/api/v1/databases/a%2Fb/queries/stream')
    const f = mockFetch({ 'POST /auth/refresh': { status: 'ok' } })
    await api.refreshSession()
    expect(f.mock.calls[0][1].method).toBe('POST')
    mockFetch({})
    await expect(api.refreshSession()).rejects.toBeInstanceOf(api.ApiError)
  })

  it('escapes ids in paths', async () => {
    const f = mockFetch({})
    await api.fetchDatabase('a/b').catch(() => {})
    expect(f.mock.calls[0][0]).toBe('/api/v1/databases/a%2Fb')
  })

  it('rejects a query touching PII with status 422', async () => {
    mockFetch({ 'POST /databases/a/query': [422, { error: { message: 'column users.email is PII', code: 422 } }] })
    const err = await api.runQuery('a', 'select email from users').catch((e) => e)
    expect(err.status).toBe(422)
    expect(err.message).toBe('column users.email is PII')
  })

  it('covers tmphost settings', async () => {
    const state = { connected: true, account: 'a@b.c', serverUrl: 'https://app.tmphost.io', connectedAt: '2026-01-01T00:00:00Z' }
    mockFetch({ 'GET /settings/tmphost': state, 'DELETE /settings/tmphost': jsonResponse(null, 204) })
    expect(await api.fetchTmphost()).toEqual(state)
    expect(await api.disconnectTmphost()).toBeNull()
  })
})
