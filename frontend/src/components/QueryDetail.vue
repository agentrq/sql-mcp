<!-- One logged query in full: the right-hand sidebar of the Activity tab. -->
<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { copyToClipboard } from '../composables/useClipboard'
import { formatDuration, formatFullDateTime } from '../composables/useFormat'
import { formatSql } from '../composables/useSqlFormat'
import { highlightSql } from '../composables/useSqlHighlight'
import { BTN_GHOST, SECTION_LABEL } from '../ui'
import { STATUS_STYLES } from './queryStatus'

const props = defineProps({ query: { type: Object, required: true }, driver: { type: String, default: '' } })
const emit = defineEmits(['close'])

/** Off shows the SQL exactly as it was logged. */
const formatted = ref(true)
const shownSql = computed(() => (formatted.value ? formatSql(props.query.sql, props.driver) : props.query.sql))
const highlighted = computed(() => highlightSql(shownSql.value, props.driver))

const copy = () => copyToClipboard(shownSql.value, 'Query copied')

function onKeydown(e) {
  if (e.key === 'Escape') emit('close')
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <aside data-test="query-detail" aria-label="Query details"
         class="fixed inset-y-0 right-0 z-40 w-full sm:w-[28rem] lg:z-auto lg:w-[28rem] lg:shrink-0 lg:self-start lg:sticky lg:top-4 lg:max-h-[calc(100vh-2rem)] flex flex-col bg-white dark:bg-zinc-900 border-l lg:border border-gray-200 dark:border-zinc-800 lg:rounded-xl shadow-2xl lg:shadow-none">
    <div class="flex items-center justify-between gap-2 px-4 py-3 border-b border-gray-200 dark:border-zinc-800">
      <div class="flex items-center gap-2 min-w-0">
        <span :class="SECTION_LABEL">Query</span>
        <span :data-test="`detail-status-${query.status}`" class="px-1.5 py-0.5 rounded text-[9px] font-black uppercase tracking-widest" :class="STATUS_STYLES[query.status] || STATUS_STYLES.error">{{ query.status }}</span>
      </div>
      <button type="button" :class="BTN_GHOST" data-test="close-detail" aria-label="Close" @click="emit('close')">
        <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
      </button>
    </div>

    <div class="flex-1 overflow-y-auto p-4 space-y-4">
      <dl class="grid grid-cols-2 gap-x-4 gap-y-3 text-[12px]">
        <div class="col-span-2">
          <dt :class="SECTION_LABEL">Time</dt>
          <dd class="mt-0.5 text-gray-800 dark:text-zinc-200">{{ formatFullDateTime(query.createdAt) }}</dd>
        </div>
        <div>
          <dt :class="SECTION_LABEL">Source</dt>
          <dd class="mt-0.5 text-gray-800 dark:text-zinc-200 uppercase">{{ query.source }}</dd>
        </div>
        <div>
          <dt :class="SECTION_LABEL">Duration</dt>
          <dd class="mt-0.5 tabular-nums text-gray-800 dark:text-zinc-200">{{ formatDuration(query.durationMs) || '—' }}</dd>
        </div>
        <div v-if="query.status === 'ok'">
          <dt :class="SECTION_LABEL">Rows</dt>
          <dd data-test="detail-rows" class="mt-0.5 tabular-nums text-gray-800 dark:text-zinc-200">{{ query.rowCount }}</dd>
        </div>
        <div v-if="query.resultUrl">
          <dt :class="SECTION_LABEL">Result</dt>
          <dd class="mt-0.5"><a :href="query.resultUrl" target="_blank" rel="noopener" data-test="detail-result" class="font-bold underline text-gray-700 dark:text-zinc-200 hover:opacity-70">Open on tmphost</a></dd>
        </div>
      </dl>

      <div v-if="query.reason" data-test="detail-reason" role="note"
           class="p-3 rounded-lg border text-[12px] break-words"
           :class="query.status === 'rejected'
             ? 'border-amber-200 dark:border-amber-500/30 bg-amber-50 dark:bg-amber-500/10 text-amber-800 dark:text-amber-300'
             : 'border-red-200 dark:border-red-500/30 bg-red-50 dark:bg-red-500/10 text-red-700 dark:text-red-300'">
        {{ query.reason }}
      </div>

      <div>
        <div class="flex items-center justify-between mb-1.5">
          <span :class="SECTION_LABEL">SQL</span>
          <div class="flex items-center gap-1">
            <button type="button" :class="BTN_GHOST" data-test="toggle-format" @click="formatted = !formatted">{{ formatted ? 'Raw' : 'Formatted' }}</button>
            <button type="button" :class="BTN_GHOST" data-test="copy-sql" @click="copy">Copy</button>
          </div>
        </div>
        <!-- highlightSql escapes the query text; only its own spans are markup. -->
        <pre data-test="detail-sql" class="sql-code p-3 rounded-lg border border-gray-200 dark:border-zinc-800 bg-gray-50 dark:bg-zinc-800/50 text-[12px] font-mono text-gray-800 dark:text-zinc-200 whitespace-pre-wrap break-words" v-html="highlighted" />
      </div>
    </div>
  </aside>
</template>

<style>
/* Token colours for highlightSql: Tailwind's 700 shades in light mode, 400 in dark. */
.sql-code .token.keyword { color: #6d28d9; font-weight: 600; }
.sql-code .token.function { color: #0369a1; }
.sql-code .token.string { color: #047857; }
.sql-code .token.number,
.sql-code .token.boolean { color: #b45309; }
.sql-code .token.variable { color: #be185d; }
.sql-code .token.identifier { color: #1d4ed8; }
.sql-code .token.operator,
.sql-code .token.punctuation { color: #6b7280; }
.sql-code .token.comment { color: #9ca3af; font-style: italic; }

.dark .sql-code .token.keyword { color: #a78bfa; }
.dark .sql-code .token.function { color: #38bdf8; }
.dark .sql-code .token.string { color: #34d399; }
.dark .sql-code .token.number,
.dark .sql-code .token.boolean { color: #fbbf24; }
.dark .sql-code .token.variable { color: #f472b6; }
.dark .sql-code .token.identifier { color: #60a5fa; }
.dark .sql-code .token.operator,
.dark .sql-code .token.punctuation { color: #a1a1aa; }
.dark .sql-code .token.comment { color: #71717a; }
</style>
