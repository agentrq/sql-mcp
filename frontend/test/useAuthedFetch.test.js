import { describe, expect, it, vi } from 'vitest'
import { createAuthedFetch } from '../src/composables/useAuthedFetch'

const res = (status) => ({ status, ok: status >= 200 && status < 300 })

describe('createAuthedFetch', () => {
  it('passes through non-401 responses', async () => {
    const fetchImpl = vi.fn(async () => res(200))
    const f = createAuthedFetch({ fetchImpl, refreshUrl: '/r' })
    expect((await f('/x')).status).toBe(200)
    expect(fetchImpl).toHaveBeenCalledTimes(1)
  })

  it('refreshes and replays once on 401', async () => {
    const fetchImpl = vi.fn()
      .mockResolvedValueOnce(res(401))
      .mockResolvedValueOnce(res(200))
      .mockResolvedValueOnce(res(200))
    const f = createAuthedFetch({ fetchImpl, refreshUrl: '/r' })
    expect((await f('/x', { method: 'GET' })).status).toBe(200)
    expect(fetchImpl.mock.calls.map((c) => c[0])).toEqual(['/x', '/r', '/x'])
  })

  it('returns the original 401 when the refresh fails or throws', async () => {
    const fetchImpl = vi.fn().mockResolvedValueOnce(res(401)).mockResolvedValueOnce(res(401))
    const f = createAuthedFetch({ fetchImpl, refreshUrl: '/r' })
    expect((await f('/x')).status).toBe(401)
    const throwing = vi.fn().mockResolvedValueOnce(res(401)).mockRejectedValueOnce(new Error('down'))
    const g = createAuthedFetch({ fetchImpl: throwing, refreshUrl: () => '/r' })
    expect((await g('/x')).status).toBe(401)
  })

  it('does not refresh the refresh call itself', async () => {
    const fetchImpl = vi.fn(async () => res(401))
    const f = createAuthedFetch({ fetchImpl, refreshUrl: () => '/r' })
    expect((await f('/r')).status).toBe(401)
    expect(fetchImpl).toHaveBeenCalledTimes(1)
  })

  it('shares one refresh across simultaneous 401s', async () => {
    let refreshes = 0
    let n = 0
    const fetchImpl = vi.fn(async (url) => {
      if (url === '/r') { refreshes++; await new Promise((r) => setTimeout(r, 5)); return res(200) }
      n++
      return n <= 2 ? res(401) : res(200)
    })
    const f = createAuthedFetch({ fetchImpl, refreshUrl: '/r' })
    const out = await Promise.all([f('/a'), f('/b')])
    expect(out.map((r) => r.status)).toEqual([200, 200])
    expect(refreshes).toBe(1)
  })

  it('treats a missing response as not-401', async () => {
    const f = createAuthedFetch({ fetchImpl: vi.fn(async () => undefined), refreshUrl: '/r' })
    expect(await f('/x')).toBeUndefined()
  })
})
