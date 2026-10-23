import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useDatabaseStore } from '../src/stores/databaseStore'
import { useAuthStore } from '../src/stores/authStore'
import { useTooltipStore } from '../src/stores/tooltipStore'
import { jsonResponse, mockFetch } from './helpers'

describe('databaseStore', () => {
  beforeEach(() => setActivePinia(createPinia()))
  afterEach(() => vi.unstubAllGlobals())

  it('loads and sorts by name', async () => {
    mockFetch({ 'GET /databases': { databases: [{ id: '2', name: 'zeta' }, { id: '1', name: 'Alpha' }, { id: '3', name: 'beta10' }, { id: '4', name: 'beta9' }] } })
    const s = useDatabaseStore()
    await s.fetchDatabases()
    expect(s.databases.map((d) => d.name)).toEqual(['Alpha', 'beta9', 'beta10', 'zeta'])
    expect(s.loaded).toBe(true)
    expect(s.loading).toBe(false)
    expect(s.getById('1').name).toBe('Alpha')
    expect(s.getById(2).name).toBe('zeta')
    expect(s.getById('nope')).toBeNull()
  })

  it('records errors', async () => {
    mockFetch({ 'GET /databases': [500, { error: { message: 'db down' } }] })
    const s = useDatabaseStore()
    await s.fetchDatabases()
    expect(s.error).toBe('db down')
    expect(s.loaded).toBe(false)
    vi.stubGlobal('fetch', vi.fn(async () => { throw {} }))
    await s.fetchDatabases()
    expect(s.error).toBe('Network error')
  })

  it('lets only the latest overlapping fetch write', async () => {
    let resolveFirst
    let call = 0
    vi.stubGlobal('fetch', vi.fn(() => {
      call++
      if (call === 1) return new Promise((r) => { resolveFirst = () => r(jsonResponse({ databases: [{ id: 'old', name: 'old' }] })) })
      return Promise.resolve(jsonResponse({ databases: [{ id: 'new', name: 'new' }] }))
    }))
    const s = useDatabaseStore()
    const first = s.fetchDatabases()
    await s.fetchDatabases()
    resolveFirst()
    await first
    expect(s.databases.map((d) => d.id)).toEqual(['new'])
  })

  it('ignores a stale failure', async () => {
    let rejectFirst
    let call = 0
    vi.stubGlobal('fetch', vi.fn(() => {
      call++
      if (call === 1) return new Promise((_r, rej) => { rejectFirst = () => rej(new Error('late')) })
      return Promise.resolve(jsonResponse({ databases: [] }))
    }))
    const s = useDatabaseStore()
    const first = s.fetchDatabases()
    await s.fetchDatabases()
    rejectFirst()
    await first
    expect(s.error).toBe('')
  })

  it('upserts, removes and resets', () => {
    const s = useDatabaseStore()
    s.upsert({ id: 'b', name: 'b' })
    s.upsert({ id: 'a', name: 'a' })
    s.upsert({ id: 'b', name: 'c' })
    s.upsert(null)
    s.upsert({ name: 'no id' })
    expect(s.databases.map((d) => d.name)).toEqual(['a', 'c'])
    s.upsert({ id: 'x' })
    expect(s.databases).toHaveLength(3)
    s.remove('b')
    expect(s.databases.map((d) => d.id).sort()).toEqual(['a', 'x'])
    s.reset()
    expect(s.databases).toEqual([])
    expect(s.loaded).toBe(false)
  })
})

describe('authStore', () => {
  beforeEach(() => setActivePinia(createPinia()))
  afterEach(() => vi.unstubAllGlobals())

  it('loads the user once and shares the lookup', async () => {
    const f = mockFetch({ 'GET /auth/user': { id: 'u', email: 'a@b.c' } })
    const s = useAuthStore()
    const [a, b] = await Promise.all([s.loadUser(), s.loadUser()])
    expect(a).toEqual(b)
    expect(await s.loadUser()).toEqual({ id: 'u', email: 'a@b.c' })
    expect(f).toHaveBeenCalledTimes(1)
    await s.loadUser({ force: true })
    expect(f).toHaveBeenCalledTimes(2)
  })

  it('is null when signed out', async () => {
    mockFetch({ 'GET /auth/user': [401, {}], 'POST /auth/refresh': [401, {}] })
    const s = useAuthStore()
    expect(await s.loadUser()).toBeNull()
    expect(s.user).toBeNull()
  })

  it('logs out, clearing the user even when the call fails', async () => {
    mockFetch({ 'GET /auth/user': { id: 'u' }, 'POST /auth/logout': jsonResponse(null, 204) })
    const s = useAuthStore()
    await s.loadUser()
    await s.logout()
    expect(s.user).toBeNull()
    mockFetch({ 'GET /auth/user': { id: 'u' }, 'POST /auth/logout': [500, {}] })
    await s.loadUser()
    await expect(s.logout()).rejects.toThrow()
    expect(s.user).toBeNull()
  })
})

describe('tooltipStore', () => {
  beforeEach(() => setActivePinia(createPinia()))
  it('positions beside the target and hides', () => {
    const s = useTooltipStore()
    s.show({ currentTarget: { getBoundingClientRect: () => ({ top: 10, height: 20, right: 50 }) } }, 'Hi')
    expect(s.visible).toBe(true)
    expect(s.text).toBe('Hi')
    expect(s.style).toEqual({ top: '20px', left: '62px', transform: 'translateY(-50%)' })
    s.hide()
    expect(s.visible).toBe(false)
  })
})
