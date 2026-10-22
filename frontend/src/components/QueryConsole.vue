<script setup>
import { useQueryConsole } from '../composables/useQueryConsole'
import { formatCell } from '../composables/useFormat'
import { BTN_PRIMARY } from '../ui'

const props = defineProps({ databaseId: { type: String, required: true } })

const { sql, running, result, rejection, error, execute, onKeydown } = useQueryConsole(() => props.databaseId)
const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent || '')
</script>

<template>
  <div class="flex flex-col gap-4 min-w-0">
    <div class="rounded-xl border border-gray-200 dark:border-zinc-800 overflow-hidden bg-gray-50 dark:bg-zinc-800/40 focus-within:border-gray-900 dark:focus-within:border-white transition-colors">
      <textarea v-model="sql" data-test="sql-input" @keydown="onKeydown" rows="7" spellcheck="false" autocapitalize="off"
                aria-label="SQL query"
                placeholder="select id, created_at from orders order by created_at desc limit 20"
                class="block w-full bg-transparent px-4 py-3 font-mono text-[13px] text-gray-900 dark:text-zinc-100 outline-none resize-y placeholder:text-gray-400 dark:placeholder:text-zinc-500"></textarea>
      <div class="flex items-center justify-between gap-3 px-3 py-2 border-t border-gray-200 dark:border-zinc-800 bg-white/60 dark:bg-zinc-900/60">
        <span class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest">{{ isMac ? '⌘' : 'Ctrl' }} + Enter to run</span>
        <button type="button" data-test="run" :class="BTN_PRIMARY" :disabled="running || !sql.trim()" @click="execute">
          <svg v-if="!running" class="w-3 h-3" fill="currentColor" viewBox="0 0 24 24"><path d="M6 4l14 8-14 8z" /></svg>
          {{ running ? 'Running...' : 'Run' }}
        </button>
      </div>
    </div>

    <!-- A PII rejection: the guard working, said plainly. -->
    <div v-if="rejection" data-test="rejection" role="alert"
         class="flex items-start gap-3 p-4 rounded-xl border border-red-200 dark:border-red-500/30 bg-red-50 dark:bg-red-500/10">
      <svg class="w-5 h-5 shrink-0 text-red-600 dark:text-red-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636" /></svg>
      <div class="min-w-0">
        <p class="text-[11px] font-black uppercase tracking-widest text-red-700 dark:text-red-400">Query rejected</p>
        <p class="text-sm text-red-700 dark:text-red-300 mt-1 break-words">{{ rejection }}</p>
      </div>
    </div>

    <div v-if="error" data-test="query-error" class="p-4 rounded-xl border border-red-200 dark:border-red-500/30 bg-red-50 dark:bg-red-500/10 text-sm text-red-700 dark:text-red-300 font-mono whitespace-pre-wrap break-words">{{ error }}</div>

    <div v-if="result" data-test="result" class="min-w-0">
      <div class="flex items-center justify-between gap-3 mb-2 px-1 flex-wrap">
        <span class="text-[10px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400">
          {{ result.rowCount }} {{ result.rowCount === 1 ? 'row' : 'rows' }}
        </span>
        <span v-if="result.truncated" data-test="truncated" class="text-[11px] font-semibold text-amber-700 dark:text-amber-400">
          Result truncated, showing the first {{ result.rows.length }} rows<template v-if="result.resultUrl"> · <a :href="result.resultUrl" target="_blank" rel="noopener" class="underline">full result</a></template>
        </span>
      </div>
      <div v-if="result.columns.length" class="rounded-xl border border-gray-200 dark:border-zinc-800 overflow-auto max-h-[60vh]">
        <table class="min-w-full text-left text-[12px]">
          <thead class="sticky top-0 z-10 bg-gray-50 dark:bg-zinc-800">
            <tr>
              <th v-for="(c, i) in result.columns" :key="i" class="px-3 py-2 font-bold text-gray-700 dark:text-zinc-200 whitespace-nowrap border-b border-gray-200 dark:border-zinc-700">{{ c }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-zinc-800">
            <tr v-for="(row, r) in result.rows" :key="r" class="hover:bg-gray-50 dark:hover:bg-zinc-800/40">
              <td v-for="(cell, i) in row" :key="i" class="px-3 py-1.5 font-mono whitespace-nowrap max-w-[28rem] truncate"
                  :class="cell === null || cell === undefined ? 'text-gray-400 dark:text-zinc-500 italic' : 'text-gray-800 dark:text-zinc-200'"
                  :title="formatCell(cell)">{{ formatCell(cell) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="text-xs text-gray-500 dark:text-zinc-400 px-1">The statement returned no columns.</p>
    </div>
  </div>
</template>
