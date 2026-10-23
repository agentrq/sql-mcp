/**
 * A database's queries as they happen, over server-sent events.
 *
 * The browser retries a dropped stream by itself. What it cannot do is renew
 * the session: when the server refuses the stream (an expired cookie is a
 * 401), EventSource gives up for good. So a refused stream renews the session
 * once and reconnects, backing off, and after `maxFailures` refusals in a row
 * the stream is `offline` until `start()` again.
 */
import { ref } from 'vue'

/**
 * @param {object} deps
 * @param {() => string} deps.url the stream's URL
 * @param {(query: object) => void} deps.onQuery each query as it is recorded
 * @param {() => void} [deps.onOpen] the stream is (re)connected: catch up on what was missed
 * @param {() => Promise<unknown>} [deps.refresh] renews the session
 * @param {typeof EventSource} [deps.EventSourceImpl]
 * @param {number} [deps.maxFailures]
 * @param {number} [deps.backoffMs] the wait before the nth retry is n times this
 */
export function createQueryStream({
  url,
  onQuery,
  onOpen = () => {},
  refresh = async () => {},
  EventSourceImpl = globalThis.EventSource,
  maxFailures = 3,
  backoffMs = 1000,
}) {
  /** 'connecting' | 'live' | 'offline' */
  const status = ref('connecting')
  let source = null
  let timer = null
  let failures = 0
  let stopped = false

  function close() {
    clearTimeout(timer)
    timer = null
    if (source) source.close()
    source = null
  }

  function connect() {
    close()
    if (!EventSourceImpl) {
      status.value = 'offline'
      return
    }
    status.value = 'connecting'
    const es = new EventSourceImpl(url())
    source = es
    es.onopen = () => {
      failures = 0
      status.value = 'live'
      onOpen()
    }
    es.addEventListener('query', (e) => {
      let query
      try {
        query = JSON.parse(e.data)
      } catch {
        return
      }
      onQuery(query)
    })
    es.onerror = () => {
      if (es !== source) return
      // Still CONNECTING: the browser is retrying a dropped stream itself.
      if (es.readyState !== EventSourceImpl.CLOSED) {
        status.value = 'connecting'
        return
      }
      close()
      failures += 1
      if (failures > maxFailures) {
        status.value = 'offline'
        return
      }
      status.value = 'connecting'
      const wait = failures * backoffMs
      Promise.resolve()
        .then(refresh)
        .catch(() => {})
        .then(() => {
          if (!stopped) timer = setTimeout(connect, wait)
        })
    }
  }

  return {
    status,
    /** Connects, or tries again after the stream went offline. */
    start() {
      stopped = false
      failures = 0
      connect()
    },
    stop() {
      stopped = true
      close()
    },
  }
}
