import { vi } from 'vitest'

/** A fetch Response with a JSON body (or none for 204). */
export function jsonResponse(body, status = 200) {
  if (status === 204) return new Response(null, { status })
  return new Response(body === undefined ? '' : JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

/**
 * Stub global fetch with a router: `routes` maps "METHOD /path" (path after
 * /api/v1, query included) to a body, a [status, body] pair, or a function.
 */
export function mockFetch(routes = {}) {
  const fn = vi.fn(async (input, init = {}) => {
    const url = new URL(String(input), 'http://localhost')
    const path = url.pathname.replace(/^.*\/api\/v1/, '') + url.search
    const key = `${(init.method || 'GET').toUpperCase()} ${path}`
    let entry = routes[key]
    if (typeof entry === 'function') entry = await entry(init, url)
    if (entry instanceof Response) return entry
    if (entry === undefined) return jsonResponse({ error: { message: `no route ${key}`, code: 404 } }, 404)
    if (Array.isArray(entry)) return jsonResponse(entry[1], entry[0])
    return jsonResponse(entry)
  })
  vi.stubGlobal('fetch', fn)
  return fn
}

export const flush = () => new Promise((r) => setTimeout(r, 0))

import { createRouter, createMemoryHistory } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'

const Blank = defineComponent({ render: () => h('div') })

/** Mount a view at `path` (matching `pattern`) with a memory router and fresh pinia. */
export async function mountAt(component, { path, pattern = path, props = {} } = {}) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: pattern, component },
      { path: '/:rest(.*)*', component: Blank },
    ],
  })
  const pinia = createPinia()
  setActivePinia(pinia)
  await router.push(path)
  await router.isReady()
  const wrapper = mount(component, {
    props,
    attachTo: document.body,
    global: { plugins: [router, pinia] },
  })
  await flush()
  await flush()
  return { wrapper, router, pinia }
}
