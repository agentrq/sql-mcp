import { describe, expect, it, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import { clickOutside, createAuthGuard, createSqlMcpApp, pageKey, routes } from '../src/bootstrap'

describe('auth guard', () => {
  it('lets public routes through without a lookup', async () => {
    const load = vi.fn()
    expect(await createAuthGuard(load)({ meta: { public: true } })).toBe(true)
    expect(load).not.toHaveBeenCalled()
  })

  it('allows a signed-in person', async () => {
    expect(await createAuthGuard(async () => ({ id: 1 }))({ meta: {}, fullPath: '/x' })).toBe(true)
  })

  it('sends anyone else to login with redirect_url', async () => {
    const guard = createAuthGuard(async () => null, (to) => `/base${to.fullPath}`)
    expect(await guard({ meta: {}, fullPath: '/databases/1?tab=pii' })).toEqual({ path: '/login', query: { redirect_url: '/base/databases/1?tab=pii' } })
    expect(await guard({ meta: {}, fullPath: '/' })).toEqual({ path: '/login', query: {} })
    const throwing = createAuthGuard(async () => { throw new Error('x') })
    expect(await throwing({ fullPath: '/settings' })).toEqual({ path: '/login', query: { redirect_url: '/settings' } })
  })
})

describe('bootstrap', () => {
  it('keys pages by path', () => {
    expect(pageKey({ path: '/databases/1', fullPath: '/databases/1?tab=pii' })).toBe('/databases/1')
  })

  it('has the expected routes', () => {
    const paths = routes.map((r) => r.path)
    expect(paths).toEqual(expect.arrayContaining(['/', '/databases/new', '/databases/:id', '/databases/:id/edit', '/settings', '/login']))
    expect(routes.find((r) => r.path === '/login').meta.public).toBe(true)
  })

  it('assembles and guards navigation', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 401 })))
    const { app, router } = createSqlMcpApp({ history: createMemoryHistory() })
    expect(app.directive('click-outside')).toBe(clickOutside)
    await router.push('/settings')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect_url).toBe('/settings')
    await router.push('/nowhere')
    expect(router.currentRoute.value.path).toBe('/login')
    vi.unstubAllGlobals()
  })

  it('click-outside fires only for outside clicks', () => {
    const el = document.createElement('div')
    const inner = document.createElement('span')
    el.appendChild(inner)
    document.body.appendChild(el)
    const handler = vi.fn()
    clickOutside.mounted(el, { value: handler })
    inner.click()
    el.click()
    expect(handler).not.toHaveBeenCalled()
    document.body.click()
    expect(handler).toHaveBeenCalledTimes(1)
    clickOutside.unmounted(el)
    document.body.click()
    expect(handler).toHaveBeenCalledTimes(1)
  })
})
