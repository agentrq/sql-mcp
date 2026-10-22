/**
 * The one place the application is assembled: the route table, the auth
 * guard, the `v-click-outside` directive and pinia. `main.js` only mounts it.
 */
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createRouter } from 'vue-router'

import App from './App.vue'
import { useAdminStore } from './stores/adminStore'
import { useAuthStore } from './stores/authStore'

export const routes = [
  { path: '/', component: () => import('./views/DatabasesView.vue') },
  // Ranked above /databases/:id by the router, since a static segment wins.
  { path: '/databases/new', component: () => import('./views/DatabaseFormView.vue') },
  { path: '/databases/:id/edit', component: () => import('./views/DatabaseFormView.vue') },
  { path: '/databases/:id', component: () => import('./views/DatabaseDetailView.vue') },
  { path: '/settings', component: () => import('./views/SettingsView.vue') },
  { path: '/login', component: () => import('./views/LoginView.vue'), meta: { public: true } },
  // The admin setup: the root token's, never a person's session.
  { path: '/admin', component: () => import('./views/AdminView.vue'), meta: { admin: true } },
  { path: '/admin/login', component: () => import('./views/AdminLoginView.vue'), meta: { public: true, admin: true } },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

/** Calls the bound handler when a click lands outside the element. */
export const clickOutside = {
  mounted(el, binding) {
    el._clickOutside = (event) => {
      if (!(el === event.target || el.contains(event.target))) binding.value(event)
    }
    document.body.addEventListener('click', el._clickOutside)
  },
  unmounted(el) {
    document.body.removeEventListener('click', el._clickOutside)
  },
}

/**
 * Every route except `meta.public` ones needs a signed-in person. Anyone else
 * goes to /login, carrying where they were headed as `redirect_url` (the
 * resolved href, so it includes the base path). A failed lookup counts as no
 * person: the login page is the safe destination.
 *
 * @param {() => Promise<object|null>} loadUser
 * @param {(to: object) => string} [hrefOf] the address `to` lives at
 */
export function createAuthGuard(loadUser, hrefOf = (to) => to.fullPath) {
  return async function authGuard(to) {
    if (to.meta?.public) return true
    let user = null
    try {
      user = await loadUser()
    } catch {
      user = null
    }
    if (user) return true
    const query = to.fullPath && to.fullPath !== '/' ? { redirect_url: hrefOf(to) } : {}
    return { path: '/login', query }
  }
}

/**
 * Admin pages need the admin session, not a person's: without one they go to
 * /admin/login. Other routes pass through to the person's guard.
 *
 * @param {() => Promise<object|null>} loadSession
 */
export function createAdminGuard(loadSession) {
  return async function adminGuard(to) {
    if (!to.meta?.admin || to.meta?.public) return true
    let session = null
    try {
      session = await loadSession()
    } catch {
      session = null
    }
    return session ? true : { path: '/admin/login' }
  }
}

/**
 * Pages read their params once, in setup, so a different path is a fresh page
 * (/databases/a -> /databases/b). The query is not part of the key: a tab
 * switch (?tab=) must not throw away unsaved work.
 */
export function pageKey(route) {
  return route.path
}

export function createSqlMcpApp({ history }) {
  const router = createRouter({ history, routes })
  const pinia = createPinia()
  const app = createApp(App)

  app.directive('click-outside', clickOutside)
  app.use(pinia)
  app.use(router)

  const auth = useAuthStore(pinia)
  const admin = useAdminStore(pinia)
  const authGuard = createAuthGuard(() => auth.loadUser(), (to) => router.resolve(to.fullPath).href)
  const adminGuard = createAdminGuard(() => admin.loadSession())
  router.beforeEach((to) => (to.meta?.admin ? adminGuard(to) : authGuard(to)))

  return { app, router, pinia }
}
