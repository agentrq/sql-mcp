<script setup>
import { computed, onMounted, ref } from 'vue'
import LoadingState from './LoadingState.vue'
import { fetchPiiColumns, fetchSchema, savePiiColumns, setAgentMarksPii } from '../api'
import { tableKey, tableLabel, usePiiSelection } from '../composables/usePiiSelection'
import { useToasts } from '../composables/useToasts'
import { BTN_GHOST, BTN_PRIMARY, BTN_SECONDARY, CARD } from '../ui'

const props = defineProps({
  databaseId: { type: String, required: true },
  agentMarksPii: { type: Boolean, default: false },
})
const emit = defineEmits(['saved', 'agent-changed'])

const { notifySuccess, notifyError } = useToasts()
const pii = usePiiSelection()

const loading = ref(true)
const loadError = ref('')
const saving = ref(false)

// Which tables are open. With a search, every match is open.
const expanded = ref(new Set())
const searching = computed(() => pii.query.value.trim() !== '')
const isOpen = (t) => searching.value || expanded.value.has(tableKey(t))
function toggleOpen(t) {
  const next = new Set(expanded.value)
  const k = tableKey(t)
  next.has(k) ? next.delete(k) : next.add(k)
  expanded.value = next
}
const expandAll = () => { expanded.value = new Set(pii.tables.value.map(tableKey)) }
const collapseAll = () => { expanded.value = new Set() }

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const [tables, saved] = await Promise.all([
      fetchSchema(props.databaseId),
      fetchPiiColumns(props.databaseId).catch(() => null),
    ])
    pii.load(tables, saved)
    // A short schema opens fully; a long one opens where there is something to look at.
    const open = tables.length <= 8
      ? tables
      : tables.filter((t) => (t.columns || []).some((c) => c.pii || c.suggestedPii))
    expanded.value = new Set(open.map(tableKey))
  } catch (err) {
    loadError.value = err?.message || 'Failed to load the schema'
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  try {
    // The agent may have marked columns since the editor loaded: apply this
    // editor's changes to the current set rather than replacing it.
    const latest = await fetchPiiColumns(props.databaseId)
    const saved = await savePiiColumns(props.databaseId, pii.toPayloadOnto(latest))
    pii.markSaved(saved)
    emit('saved', pii.selectedCount.value)
    notifySuccess(`${pii.selectedCount.value} PII ${pii.selectedCount.value === 1 ? 'column' : 'columns'} saved`)
  } catch (err) {
    notifyError(err?.message || 'Failed to save PII columns')
  } finally {
    saving.value = false
  }
}

const switching = ref(false)
async function toggleAgent() {
  switching.value = true
  try {
    const db = await setAgentMarksPii(props.databaseId, !props.agentMarksPii)
    emit('agent-changed', db)
    notifySuccess(db.agentMarksPii ? 'The agent can now mark PII columns' : 'The agent can no longer mark PII columns')
  } catch (err) {
    notifyError(err?.message || 'Failed to change the setting')
  } finally {
    switching.value = false
  }
}

const stateClass = (t) => ({
  all: 'bg-black border-black dark:bg-white dark:border-white text-white dark:text-black',
  some: 'bg-gray-400 border-gray-400 dark:bg-zinc-500 dark:border-zinc-500 text-white',
  none: 'border-gray-300 dark:border-zinc-600',
}[pii.tableState(t)])

onMounted(load)
</script>

<template>
  <div class="flex flex-col min-w-0 grow">
    <LoadingState v-if="loading" label="Reading schema..." />
    <div v-else-if="loadError" class="p-4 rounded-xl border border-red-200 dark:border-red-500/30 bg-red-50 dark:bg-red-500/10 text-sm text-red-700 dark:text-red-300 flex items-center justify-between gap-3">
      <span class="break-words min-w-0">{{ loadError }}</span>
      <button type="button" :class="BTN_SECONDARY" @click="load">Retry</button>
    </div>
    <div v-else-if="!pii.tables.value.length" class="py-16 text-center border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl">
      <p class="text-sm font-bold text-gray-900 dark:text-zinc-100">No tables found</p>
      <p class="text-xs text-gray-500 dark:text-zinc-400 mt-1">The connected user cannot see any tables in this database.</p>
    </div>

    <template v-else>
      <p class="text-xs text-gray-500 dark:text-zinc-400 mb-4 max-w-3xl">
        Mark every column that holds personal data. Any query that reads, filters or joins on a marked column is rejected,
        for MCP clients and the console alike.
      </p>

      <!-- Delegating to the agent -->
      <div :class="CARD" class="flex items-start gap-3 px-4 py-3 mb-4">
        <button type="button" role="switch" data-test="agent-marks-pii" :aria-checked="agentMarksPii" :disabled="switching"
                aria-labelledby="agent-marks-pii-label" @click="toggleAgent"
                class="relative mt-0.5 w-8 h-[18px] rounded-full shrink-0 transition-colors disabled:opacity-40"
                :class="agentMarksPii ? 'bg-black dark:bg-white' : 'bg-gray-200 dark:bg-zinc-700'">
          <span class="absolute top-[3px] left-[3px] w-3 h-3 rounded-full bg-white dark:bg-zinc-900 transition-transform"
                :class="agentMarksPii ? 'translate-x-[14px]' : ''"></span>
        </button>
        <div class="min-w-0">
          <p id="agent-marks-pii-label" class="text-xs font-bold text-gray-900 dark:text-zinc-100">Let the agent mark PII columns</p>
          <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-0.5 max-w-3xl">
            MCP clients can then mark columns they find holding personal data. They can only add marks, never remove one: only you can uncheck a column, here.
          </p>
        </div>
      </div>

      <!-- Toolbar -->
      <div class="flex flex-col md:flex-row md:items-center gap-2 mb-3">
        <div class="relative flex-1">
          <svg class="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" /></svg>
          <input v-model="pii.query.value" data-test="pii-search" type="search" aria-label="Search tables and columns"
                 :placeholder="`Search ${pii.tables.value.length} tables, ${pii.columnCount.value} columns...`"
                 class="w-full pl-9 pr-4 py-2 bg-gray-50 dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-lg text-[12px] outline-none font-medium text-gray-800 dark:text-zinc-200 placeholder:text-gray-500 focus:border-gray-900 dark:focus:border-white transition-all" />
        </div>
        <div class="flex items-center gap-1 flex-wrap">
          <button type="button" data-test="select-suggested" :class="BTN_SECONDARY" :disabled="!pii.pendingSuggestedCount.value" @click="pii.selectSuggested()">
            Select suggested<span v-if="pii.pendingSuggestedCount.value" class="opacity-60">({{ pii.pendingSuggestedCount.value }})</span>
          </button>
          <button type="button" :class="BTN_GHOST" @click="expandAll" :disabled="searching">Expand all</button>
          <button type="button" :class="BTN_GHOST" @click="collapseAll" :disabled="searching">Collapse all</button>
        </div>
      </div>

      <div v-if="searching && !pii.filteredTables.value.length" class="py-10 text-center text-xs text-gray-500 dark:text-zinc-400">
        Nothing matches "{{ pii.query.value }}".
      </div>

      <!-- Tables -->
      <div class="rounded-xl border border-gray-200 dark:border-zinc-800 divide-y divide-gray-100 dark:divide-zinc-800 overflow-hidden">
        <section v-for="t in pii.filteredTables.value" :key="tableKey(t)" data-test="pii-table">
          <div class="flex items-center gap-2 px-3 py-2 bg-white dark:bg-zinc-900 hover:bg-gray-50 dark:hover:bg-zinc-800/50">
            <button type="button" @click="toggleOpen(t)" :aria-expanded="isOpen(t)" class="flex items-center gap-2 min-w-0 flex-1 text-left">
              <svg class="w-3 h-3 shrink-0 text-gray-400 transition-transform" :class="isOpen(t) ? '' : '-rotate-90'" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" /></svg>
              <span class="w-3.5 h-3.5 rounded border shrink-0 flex items-center justify-center" :class="stateClass(t)" aria-hidden="true">
                <svg v-if="pii.tableState(t) === 'all'" class="w-2.5 h-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="4"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
                <svg v-else-if="pii.tableState(t) === 'some'" class="w-2.5 h-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="4"><path stroke-linecap="round" d="M6 12h12" /></svg>
              </span>
              <span class="font-mono text-[12px] font-semibold text-gray-900 dark:text-zinc-100 truncate">{{ tableLabel(t) }}</span>
              <span class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 shrink-0 tabular-nums">
                {{ pii.tableSelectedCount(t) }}/{{ (t.columns || []).length }}<template v-if="t.partial"> · filtered</template>
              </span>
            </button>
            <button type="button" data-test="mark-all" :class="BTN_GHOST" @click="pii.setTable(t, true)" :disabled="pii.tableState(t) === 'all'">Mark all</button>
            <button type="button" data-test="clear-table" :class="BTN_GHOST" @click="pii.setTable(t, false)" :disabled="pii.tableState(t) === 'none'">Clear</button>
          </div>

          <ul v-if="isOpen(t)" class="bg-gray-50/50 dark:bg-zinc-950/40 py-1">
            <li v-for="c in t.columns" :key="c.name">
              <label class="flex items-center gap-3 pl-10 pr-3 py-1.5 cursor-pointer hover:bg-gray-100 dark:hover:bg-zinc-800/60">
                <input type="checkbox" data-test="pii-column" :checked="pii.isSelected(t, c)" @change="pii.setColumn(t, c, $event.target.checked)"
                       class="w-3.5 h-3.5 rounded accent-black dark:accent-white shrink-0" />
                <span class="font-mono text-[12px] text-gray-800 dark:text-zinc-200 truncate">{{ c.name }}</span>
                <span class="font-mono text-[11px] text-gray-400 dark:text-zinc-500 truncate">{{ c.dataType }}</span>
                <span v-if="c.suggestedPii" data-test="suggested" class="ml-auto shrink-0 px-1.5 py-0.5 rounded text-[9px] font-black uppercase tracking-widest bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-400">Suggested</span>
              </label>
            </li>
          </ul>
        </section>
      </div>

      <!-- Sticky save bar -->
      <div class="sticky bottom-0 -mx-4 px-4 pt-3 pb-1 mt-4 bg-gradient-to-t from-white via-white dark:from-zinc-900 dark:via-zinc-900 to-transparent">
        <div class="flex items-center justify-between gap-3 px-4 py-3 rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 shadow-sm">
          <div class="flex items-center gap-3 min-w-0 flex-wrap">
            <span data-test="selected-count" class="text-xs font-bold text-gray-900 dark:text-zinc-100">
              {{ pii.selectedCount.value }} {{ pii.selectedCount.value === 1 ? 'column' : 'columns' }} selected
            </span>
            <span v-if="pii.dirty.value" data-test="unsaved" class="inline-flex items-center gap-1.5 text-[10px] font-black uppercase tracking-widest text-amber-600 dark:text-amber-400">
              <span class="w-1.5 h-1.5 rounded-full bg-amber-500"></span>
              Unsaved<template v-if="pii.addedCount.value || pii.removedCount.value"> (+{{ pii.addedCount.value }} / −{{ pii.removedCount.value }})</template>
            </span>
          </div>
          <div class="flex items-center gap-2 shrink-0">
            <button v-if="pii.dirty.value" type="button" :class="BTN_GHOST" @click="pii.revert()">Discard</button>
            <button type="button" data-test="save-pii" :class="BTN_PRIMARY" :disabled="saving || !pii.dirty.value" @click="save">
              {{ saving ? 'Saving...' : 'Save' }}
            </button>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>
