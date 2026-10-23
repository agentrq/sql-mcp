import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { fetchDatabases as apiFetchDatabases } from '../api'

const byName = (a, b) => (a.name || '').localeCompare(b.name || '', undefined, { numeric: true, sensitivity: 'base' })

/**
 * The one copy of the database list: the sidebar and the home page read it,
 * and the form and detail pages write to it after a change, so the sidebar
 * never shows a stale name or a deleted database.
 */
export const useDatabaseStore = defineStore('databases', () => {
  const databases = ref([])
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref('')

  // Reads overlap; only the latest one may write.
  let latestFetch = 0

  async function fetchDatabases() {
    const fetchId = ++latestFetch
    loading.value = true
    try {
      const list = await apiFetchDatabases()
      if (fetchId !== latestFetch) return
      databases.value = [...list].sort(byName)
      error.value = ''
      loaded.value = true
    } catch (err) {
      if (fetchId === latestFetch) error.value = err?.message || 'Failed to load databases'
    } finally {
      if (fetchId === latestFetch) loading.value = false
    }
  }

  const byId = computed(() => new Map(databases.value.map((d) => [String(d.id), d])))
  const getById = (id) => byId.value.get(String(id)) || null

  /** Add or replace one database, keeping the list sorted. */
  function upsert(database) {
    if (!database?.id) return
    const rest = databases.value.filter((d) => String(d.id) !== String(database.id))
    databases.value = [...rest, database].sort(byName)
  }

  function remove(id) {
    databases.value = databases.value.filter((d) => String(d.id) !== String(id))
  }

  function reset() {
    latestFetch++
    databases.value = []
    loaded.value = false
    loading.value = false
    error.value = ''
  }

  return { databases, loading, loaded, error, fetchDatabases, getById, upsert, remove, reset }
})
