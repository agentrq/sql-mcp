/**
 * Where the login page sends a person once they are signed in: the
 * `redirect_url` an MCP client's authorize step put on it. Only an address on
 * this same origin is kept (absolute or a path); anything else is null, so the
 * page cannot be used to bounce someone to another site.
 *
 * @param {unknown} raw the `redirect_url` query value
 * @param {string} origin this page's origin
 * @returns {string|null} an absolute same-origin URL
 */
export function loginRedirect(raw, origin) {
  if (typeof raw !== 'string' || raw.trim() === '') return null
  // `//evil.com` is a path to the browser's eye but another host to URL().
  let target
  try {
    target = new URL(raw, origin)
  } catch {
    return null
  }
  if (target.protocol !== 'http:' && target.protocol !== 'https:') return null
  return target.origin === origin ? target.href : null
}
