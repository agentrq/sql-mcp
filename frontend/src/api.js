/**
 * Every call the frontend makes to the backend. Same-origin, relative to the
 * base path the backend injects (`window.__SQLMCP_BASE_PATH__`), and routed
 * through `apiFetch`, which renews an expired session once on a 401.
 */
import { createAuthedFetch } from './composables/useAuthedFetch'

function basePath() {
  const base = (typeof window !== 'undefined' && window.__SQLMCP_BASE_PATH__) || ''
  return String(base).replace(/\/$/, '')
}

/** `/api/v1` under the injected base path, read at call time. */
export function apiBase() {
  return `${basePath()}/api/v1`
}

/** `/admin/api/v1`, the setup API the root token opens. */
export function adminBase() {
  return `${basePath()}/admin/api/v1`
}

/** A failed call: the server's `error.message` and the HTTP status. */
export class ApiError extends Error {
  constructor(message, status, code) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code ?? status
  }
}

export const apiFetch = createAuthedFetch({
  fetchImpl: (input, init) => fetch(input, init),
  refreshUrl: () => `${apiBase()}/auth/refresh`,
})

async function readError(res) {
  let message = `Request failed (${res.status})`
  let code
  try {
    const data = await res.json()
    if (data?.error?.message) message = data.error.message
    else if (typeof data?.error === 'string') message = data.error
    code = data?.error?.code
  } catch {
    // not JSON: keep the generic message
  }
  return new ApiError(message, res.status, code)
}

/**
 * One JSON call. Resolves with the parsed body (null for an empty one, like a
 * 204), rejects with an ApiError carrying the server's message.
 */
export function request(path, options) {
  return send(apiFetch, `${apiBase()}${path}`, options)
}

/**
 * A call to the admin API. No refresh on a 401: an admin session is an hour
 * long and ends with signing in again.
 */
export function adminRequest(path, options) {
  return send((input, init) => fetch(input, init), `${adminBase()}${path}`, options)
}

async function send(fetcher, url, { method = 'GET', body } = {}) {
  const init = { method, credentials: 'same-origin', headers: { Accept: 'application/json' } }
  if (body !== undefined) {
    init.headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(body)
  }
  let res
  try {
    res = await fetcher(url, init)
  } catch (err) {
    throw new ApiError(err?.message || 'Network error', 0)
  }
  if (!res.ok) throw await readError(res)
  if (res.status === 204) return null
  const text = await res.text()
  return text ? JSON.parse(text) : null
}

const enc = encodeURIComponent

// --- Auth -------------------------------------------------------------------

export async function fetchAuthConfig() {
  const data = await request('/auth/config')
  return {
    googleLoginEnabled: Boolean(data?.googleLoginEnabled),
    rootLoginEnabled: Boolean(data?.rootLoginEnabled),
    basePath: data?.basePath || '',
  }
}

/** The signed-in person, or null when nobody is. */
export async function fetchUser() {
  try {
    return await request('/auth/user')
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) return null
    throw err
  }
}

export function rootLogin(rootToken) {
  return request('/auth/root/login', { method: 'POST', body: { rootToken } })
}

export function logout() {
  return request('/auth/logout', { method: 'POST' })
}

/** Where the Google button points, carrying the return address when there is one. */
export function googleLoginUrl(redirect) {
  const route = `${apiBase()}/auth/google/login`
  return redirect ? `${route}?redirect_url=${enc(redirect)}` : route
}

// --- Databases --------------------------------------------------------------

export async function fetchDrivers() {
  const data = await request('/drivers')
  return data?.drivers || []
}

export async function fetchDatabases() {
  const data = await request('/databases')
  return data?.databases || []
}

export async function fetchDatabase(id) {
  const data = await request(`/databases/${enc(id)}`)
  return data?.database
}

export async function createDatabase({ name, driver, dsn }) {
  const data = await request('/databases', { method: 'POST', body: { database: { name, driver, dsn } } })
  return data?.database
}

/** An empty `dsn` keeps the stored one. */
export async function updateDatabase(id, { name, dsn = '' }) {
  const data = await request(`/databases/${enc(id)}`, { method: 'PUT', body: { database: { name, dsn } } })
  return data?.database
}

export function deleteDatabase(id) {
  return request(`/databases/${enc(id)}`, { method: 'DELETE' })
}

/** `{ ok, serverVersion }`; rejects with the connection error. */
export function testConnection({ driver, dsn }) {
  return request('/databases/test', { method: 'POST', body: { database: { driver, dsn } } })
}

export async function fetchSchema(id) {
  const data = await request(`/databases/${enc(id)}/schema`)
  return data?.tables || []
}

export async function fetchPiiColumns(id) {
  const data = await request(`/databases/${enc(id)}/pii`)
  return data?.piiColumns || []
}

/** Replaces the whole set. */
export async function savePiiColumns(id, piiColumns) {
  const data = await request(`/databases/${enc(id)}/pii`, { method: 'PUT', body: { piiColumns } })
  return data?.piiColumns || []
}

/** Lets, or stops, the database's MCP agent marking PII columns; the updated database. */
export async function setAgentMarksPii(id, agentMarksPii) {
  const data = await request(`/databases/${enc(id)}/pii/agent`, { method: 'PUT', body: { agentMarksPii } })
  return data?.database
}

/** `{ columns, rows, rowCount, truncated }`; a query touching PII rejects with status 422. */
export function runQuery(id, sql) {
  return request(`/databases/${enc(id)}/query`, { method: 'POST', body: { sql } })
}

export async function fetchQueries(id, limit = 50) {
  const data = await request(`/databases/${enc(id)}/queries?limit=${enc(limit)}`)
  return data?.queries || []
}

/** The server-sent events stream of a database's queries, one `query` event each. */
export function queryStreamUrl(id) {
  return `${apiBase()}/databases/${enc(id)}/queries/stream`
}

/** Swaps the refresh cookie for a new session; rejects when it cannot. */
export function refreshSession() {
  return request('/auth/refresh', { method: 'POST' })
}

// --- Settings ---------------------------------------------------------------

/** `{ connected, account?, serverUrl, connectedAt? }` */
export function fetchTmphost() {
  return request('/settings/tmphost')
}

/**
 * Where the browser goes to connect tmphost.io. A navigation, not a fetch:
 * the server answers with a 302 to tmphost's OAuth consent page.
 */
export function tmphostConnectUrl() {
  return `${apiBase()}/settings/tmphost/connect`
}

export function disconnectTmphost() {
  return request('/settings/tmphost', { method: 'DELETE' })
}

// --- Admin ------------------------------------------------------------------

/** Whether the server has a root token, which is what enables the admin setup. */
export async function fetchAdminConfig() {
  const data = await adminRequest('/config')
  return { enabled: Boolean(data?.enabled) }
}

/** Signs in with the root token; the admin JWT comes back as an HttpOnly cookie. */
export function adminLogin(rootToken) {
  return adminRequest('/login', { method: 'POST', body: { rootToken } })
}

export function adminLogout() {
  return adminRequest('/logout', { method: 'POST' })
}

/** `{ expiresAt }` of the admin session, or null when there is none. */
export async function fetchAdminSession() {
  try {
    return await adminRequest('/session')
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) return null
    throw err
  }
}

/** `{ allowedEmails, queryMaxRows, queryInlineRows, queryTimeoutSeconds, source, updatedAt?, server }` */
export function fetchAdminSettings() {
  return adminRequest('/settings')
}

export function saveAdminSettings({ allowedEmails, queryMaxRows, queryInlineRows, queryTimeoutSeconds }) {
  return adminRequest('/settings', { method: 'PUT', body: { allowedEmails, queryMaxRows, queryInlineRows, queryTimeoutSeconds } })
}

/** Drops the saved configuration; the environment's is in force again. */
export function resetAdminSettings() {
  return adminRequest('/settings', { method: 'DELETE' })
}

/** `{ allowed, matchedEntry? }` for `email` against a list not saved yet. */
export function checkAdminEmail(email, allowedEmails) {
  return adminRequest('/settings/check-email', { method: 'POST', body: { email, allowedEmails } })
}
