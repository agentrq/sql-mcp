<script setup>
import { onMounted, ref } from 'vue'
import LoadingState from './LoadingState.vue'
import { fetchQueries } from '../api'
import { formatDateTime, formatDuration } from '../composables/useFormat'
import { BTN_GHOST } from '../ui'

const props = defineProps({ databaseId: { type: String, required: true } })

const STATUS_STYLES = {
  ok: 'bg-green-50 text-green-700 dark:bg-green-500/10 dark:text-green-400',
  rejected: 'bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-400',
  error: 'bg-red-50 text-red-700 dark:bg-red-500/10 dark:text-red-400',
}

const queries = ref([])
const loading = ref(true)
const error = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    queries.value = await fetchQueries(props.databaseId, 50)
  } catch (err) {
    error.value = err?.message || 'Failed to load activity'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="min-w-0">
    <div class="flex items-center justify-between mb-3 px-1">
      <span class="text-[10px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400">Last 50 queries</span>
      <button type="button" :class="BTN_GHOST" :disabled="loading" @click="load" data-test="refresh">Refresh</button>
    </div>

    <LoadingState v-if="loading && !queries.length" label="Loading activity..." />
    <div v-else-if="error" class="p-4 rounded-xl border border-red-200 dark:border-red-500/30 bg-red-50 dark:bg-red-500/10 text-sm text-red-700 dark:text-red-300">{{ error }}</div>
    <div v-else-if="!queries.length" data-test="empty" class="py-16 text-center border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl">
      <p class="text-sm font-bold text-gray-900 dark:text-zinc-100">No queries yet</p>
      <p class="text-xs text-gray-500 dark:text-zinc-400 mt-1">Queries from MCP clients and the console show up here.</p>
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
          <tr v-for="q in queries" :key="q.id" data-test="query-row" class="align-top hover:bg-gray-50 dark:hover:bg-zinc-800/40">
            <td class="px-3 py-2 whitespace-nowrap text-gray-500 dark:text-zinc-400">{{ formatDateTime(q.createdAt) }}</td>
            <td class="px-3 py-2"><span class="px-1.5 py-0.5 rounded text-[9px] font-black uppercase tracking-widest bg-gray-100 dark:bg-zinc-800 text-gray-600 dark:text-zinc-300">{{ q.source }}</span></td>
            <td class="px-3 py-2">
              <span :data-test="`status-${q.status}`" class="px-1.5 py-0.5 rounded text-[9px] font-black uppercase tracking-widest" :class="STATUS_STYLES[q.status] || STATUS_STYLES.error" :title="q.reason || ''">{{ q.status }}</span>
            </td>
            <td class="px-3 py-2 max-w-0 w-full">
              <code class="block font-mono text-gray-800 dark:text-zinc-200 truncate" :title="q.sql">{{ q.sql }}</code>
              <p v-if="q.reason" class="text-[11px] text-gray-500 dark:text-zinc-400 truncate mt-0.5" :title="q.reason">{{ q.reason }}</p>
            </td>
            <td class="px-3 py-2 text-right tabular-nums text-gray-700 dark:text-zinc-300">{{ q.status === 'ok' ? q.rowCount : '' }}</td>
            <td class="px-3 py-2 text-right tabular-nums whitespace-nowrap text-gray-500 dark:text-zinc-400">{{ formatDuration(q.durationMs) }}</td>
            <td class="px-3 py-2 whitespace-nowrap">
              <a v-if="q.resultUrl" :href="q.resultUrl" target="_blank" rel="noopener" class="text-[11px] font-bold underline text-gray-700 dark:text-zinc-200 hover:opacity-70">Open</a>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
