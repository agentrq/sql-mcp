/**
 * A fetch that renews an expired session instead of dropping the person at
 * the login screen (ported from agentrq).
 *
 * - One refresh, not one per request: a burst of simultaneous 401s shares a
 *   single `POST /auth/refresh`, so the requests do not rotate the cookie out
 *   from under each other.
 * - Exactly one retry: if the replay also fails, that 401 is passed through.
 */

/**
 * @param {object} deps
 * @param {typeof fetch} deps.fetchImpl
 * @param {string | (() => string)} deps.refreshUrl the endpoint that swaps a refresh cookie for a session
 */
export function createAuthedFetch({ fetchImpl, refreshUrl }) {
  /** The refresh in progress, shared by everyone who hit a 401 at once. */
  let inFlight = null
  const refreshTarget = () => (typeof refreshUrl === 'function' ? refreshUrl() : refreshUrl)

  function refreshOnce() {
    if (!inFlight) {
      inFlight = fetchImpl(refreshTarget(), { method: 'POST', credentials: 'same-origin' })
        .then((res) => Boolean(res?.ok))
        // A refresh that cannot be attempted is a failed refresh: the caller
        // still has its original 401 to return.
        .catch(() => false)
        .finally(() => {
          inFlight = null
        })
    }
    return inFlight
  }

  return async function authedFetch(input, init) {
    const res = await fetchImpl(input, init)
    if (res?.status !== 401) return res

    // Refreshing the refresh call would be a loop with extra steps.
    if (input === refreshTarget()) return res

    const renewed = await refreshOnce()
    if (!renewed) return res

    return fetchImpl(input, init)
  }
}
