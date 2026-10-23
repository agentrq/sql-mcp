<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'
import LoadingState from './LoadingState.vue'
import QueryDetail from './QueryDetail.vue'
import { STATUS_STYLES } from './queryStatus'
import { fetchQueries, queryStreamUrl, refreshSession } from '../api'
import { formatDateTime, formatDuration } from '../composables/useFormat'
import { createQueryStream } from '../composables/useQueryStream'
import { BTN_GHOST } from '../ui'

const props = defineProps({ databaseId: { type: String, required: true } })

/** How many queries the list keeps as live ones arrive. */
const LIMIT = 50

const STREAM_LABELS = { live: 'Live', connecting: 'Connecting', offline: 'Offline' }
const STREAM_DOTS = { live: 'bg-green-500 animate-pulse', connecting: 'bg-amber-400', offline: 'bg-gray-400 dark:bg-zinc-500' }

const queries = ref([])
const loading = ref(true)
const error = ref('')
const selected = ref(null)

const byNewest = (a, b) => String(b.createdAt).localeCompare(String(a.createdAt))

/** Adds queries the list does not have yet, newest first, keeping LIMIT. */
function merge(incoming) {
  const known = new Set(queries.value.map((q) => q.id))
  const fresh = incoming.filter((q) => !known.has(q.id))
  if (fresh.length) queries.value = [...fresh, ...queries.value].sort(byNewest).slice(0, LIMIT)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const fetched = await fetchQueries(props.databaseId, LIMIT)
    // A query the stream delivered while this was in flight may be missing
    // from the response; it is kept.
    const listed = new Set(fetched.map((q) => q.id))
    const newest = fetched[0]?.createdAt || ''
    const streamed = queries.value.filter((q) => !listed.has(q.id) && String(q.createdAt) > String(newest))
    queries.value = [...streamed, ...fetched].sort(byNewest).slice(0, LIMIT)
  } catch (err) {
    error.value = err?.message || 'Failed to load activity'
  } finally {
    loading.value = false
  }
}

const stream = createQueryStream({
  url: () => queryStreamUrl(props.databaseId),
  onQuery: (q) => merge([q]),
  // Each (re)connect catches up on whatever happened while it was down.
  onOpen: load,
  refresh: refreshSession,
})

function select(q) {
  selected.value = selected.value?.id === q.id ? null : q
}

onMounted(() => {
  load()
  stream.start()
})
onBeforeUnmount(stream.stop)
</script>

<template>
  <div class="min-w-0 flex gap-4 items-start">
    <div class="flex-1 min-w-0">
      <div class="flex items-center justify-between mb-3 px-1 gap-2">
        <div class="flex items-center gap-3">
          <span class="text-[10px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400">Last {{ LIMIT }} queries</span>
          <span data-test="stream-status" :data-status="stream.status.value" class="inline-flex items-center gap-1.5 text-[10px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400"
                :title="stream.status.value === 'live' ? 'New queries appear as they run' : ''">
            <span class="w-1.5 h-1.5 rounded-full" :class="STREAM_DOTS[stream.status.value]"></span>
            {{ STREAM_LABELS[stream.status.value] }}
          </span>
          <button v-if="stream.status.value === 'offline'" type="button" :class="BTN_GHOST" data-test="reconnect" @click="stream.start">Reconnect</button>
        </div>
        <button type="button" :class="BTN_GHOST" :disabled="loading" @click="load" data-test="refresh">Refresh</button>
      </div>

      <LoadingState v-if="loading && !queries.length" label="Loading activity..." />
      <div v-else-if="error" class="p-4 rounded-xl border border-red-200 dark:border-red-500/30 bg-red-50 dark:bg-red-500/10 text-sm text-red-700 dark:text-red-300">{{ error }}</div>
      <div v-else-if="!queries.length" data-test="empty" class="py-16 text-center border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl">
        <p class="text-sm font-bold text-gray-900 dark:text-zinc-100">No queries yet</p>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-1">Queries from MCP clients and the console show up here as they run.</p>
      </div>

      <div v-else class="rounded-xl border border-gray-200 dark:border-zinc-800 overflow-auto">
        <table class="min-w-full text-left text-[12px]">
          <thead class="sticky top-0 bg-gray-50 dark:bg-zinc-800">
            <tr class="text-[10px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400">
              <th class="px-3 py-2 whitespace-nowrap">Time</th>
              <th class="px-3 py-2">Source</th>
              <th class="px-3 py-2">Status</th>
              <th class="px-3 py-2 w-full">SQL</th>
              <th class="px-3 py-2 text-right">Rows</th>
              <th class="px-3 py-2 text-right">Duration</th>
              <th class="px-3 py-2">Result</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-zinc-800">
            <tr v-for="q in queries" :key="q.id" data-test="query-row" tabindex="0"
                :aria-selected="selected?.id === q.id"
                class="align-top cursor-pointer outline-none focus-visible:bg-gray-50 dark:focus-visible:bg-zinc-800/40"
                :class="selected?.id === q.id ? 'bg-gray-100 dark:bg-zinc-800/70' : 'hover:bg-gray-50 dark:hover:bg-zinc-800/40'"
                @click="select(q)" @keydown.enter.prevent="select(q)">
              <td class="px-3 py-2 whitespace-nowrap text-gray-500 dark:text-zinc-400">{{ formatDateTime(q.createdAt) }}</td>
              <td class="px-3 py-2"><span class="px-1.5 py-0.5 rounded text-[9px] font-black uppercase tracking-widest bg-gray-100 dark:bg-zinc-800 text-gray-600 dark:text-zinc-300">{{ q.source }}</span></td>
              <td class="px-3 py-2">
                <span :data-test="`status-${q.status}`" class="px-1.5 py-0.5 rounded text-[9px] font-black uppercase tracking-widest" :class="STATUS_STYLES[q.status] || STATUS_STYLES.error" :title="q.reason || ''">{{ q.status }}</span>
              </td>
              <td class="px-3 py-2 max-w-0 w-full">
                <code class="block font-mono text-gray-800 dark:text-zinc-200 truncate">{{ q.sql }}</code>
                <p v-if="q.reason" class="text-[11px] text-gray-500 dark:text-zinc-400 truncate mt-0.5">{{ q.reason }}</p>
              </td>
              <td class="px-3 py-2 text-right tabular-nums text-gray-700 dark:text-zinc-300">{{ q.status === 'ok' ? q.rowCount : '' }}</td>
              <td class="px-3 py-2 text-right tabular-nums whitespace-nowrap text-gray-500 dark:text-zinc-400">{{ formatDuration(q.durationMs) }}</td>
              <td class="px-3 py-2 whitespace-nowrap">
                <a v-if="q.resultUrl" :href="q.resultUrl" target="_blank" rel="noopener" class="text-[11px] font-bold underline text-gray-700 dark:text-zinc-200 hover:opacity-70" @click.stop>Open</a>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <QueryDetail v-if="selected" :key="selected.id" :query="selected" @close="selected = null" />
  </div>
</template>
