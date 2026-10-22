import { ref } from 'vue'
import { ApiError, runQuery } from '../api'

/**
 * The query console's state. A 422 is a PII rejection — shown as its own
 * callout rather than as a generic error, because it is the guard working.
 */
export function useQueryConsole(databaseId, run = runQuery) {
  const sql = ref('')
  const running = ref(false)
  const result = ref(null)
  const rejection = ref('')
  const error = ref('')

  async function execute() {
    const text = sql.value.trim()
    if (!text || running.value) return
    running.value = true
    rejection.value = ''
    error.value = ''
    try {
      const data = await run(typeof databaseId === 'function' ? databaseId() : databaseId, text)
      result.value = {
        columns: data?.columns || [],
        rows: data?.rows || [],
        rowCount: data?.rowCount ?? (data?.rows || []).length,
        truncated: Boolean(data?.truncated),
        resultUrl: data?.resultUrl || '',
      }
    } catch (err) {
      result.value = null
      if (err instanceof ApiError && err.status === 422) rejection.value = err.message
      else error.value = err?.message || 'Query failed'
    } finally {
      running.value = false
    }
  }

  /** Cmd/Ctrl+Enter runs. */
  function onKeydown(event) {
    if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
      event.preventDefault()
      execute()
    }
  }

  return { sql, running, result, rejection, error, execute, onKeydown }
}
