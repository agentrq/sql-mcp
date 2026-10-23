<template>
  <div class="h-full flex flex-col w-full">
    <LoadingState v-if="loading" label="Loading database..." />

    <div v-else-if="loadError" class="mx-4 bg-red-50 dark:bg-red-500/10 border border-red-200 dark:border-red-500/30 text-red-700 dark:text-red-400 px-5 py-3 rounded-lg text-xs font-bold">
      {{ loadError }}
    </div>

    <template v-else-if="database">
      <!-- Page header -->
      <div class="py-2 mb-6 flex flex-col lg:flex-row lg:items-start justify-between gap-4 px-4">
        <div class="min-w-0 flex items-start gap-3">
          <div class="w-10 h-10 rounded-xl bg-black dark:bg-white text-white dark:text-black flex items-center justify-center shrink-0 rotate-3">
            <AppLogo class="w-5 h-5" />
          </div>
          <div class="min-w-0">
            <div class="flex items-center gap-2 flex-wrap">
              <h1 :class="PAGE_TITLE" class="truncate">{{ database.name }}</h1>
              <span class="px-1.5 py-0.5 rounded text-[9px] font-black uppercase tracking-widest bg-gray-100 dark:bg-zinc-800 text-gray-600 dark:text-zinc-300">{{ driverName(database.driver) }}</span>
            </div>
            <p class="font-mono text-[11px] text-gray-500 dark:text-zinc-400 truncate mt-1" :title="database.dsnMasked">{{ database.dsnMasked }}</p>
          </div>
        </div>

        <div class="flex items-center gap-2 shrink-0 flex-wrap">
          <!-- Tabs -->
          <div class="flex items-center gap-1 bg-gray-100 dark:bg-zinc-900/90 p-1 border border-gray-200 dark:border-zinc-800 rounded-lg max-w-full overflow-x-auto no-scrollbar" role="tablist">
            <button v-for="tab in TABS" :key="tab.id" type="button" role="tab"
                    :data-test="`tab-${tab.id}`"
                    :aria-selected="activeTab === tab.id"
                    @click="selectTab(tab.id)"
                    class="px-3 py-1.5 text-[10px] font-black uppercase tracking-widest rounded-md transition-all whitespace-nowrap"
                    :class="activeTab === tab.id
                      ? 'bg-white dark:bg-zinc-800 text-black dark:text-zinc-50 shadow-sm border border-gray-200 dark:border-zinc-700'
                      : 'text-gray-500 hover:text-gray-900 dark:text-zinc-400 dark:hover:text-zinc-50 border border-transparent'">
              {{ tab.label }}<span v-if="tab.id === 'pii'" class="ml-1 opacity-60">{{ database.piiColumnCount || 0 }}</span>
            </button>
          </div>
          <router-link :to="`/databases/${database.id}/edit`" :class="BTN_SECONDARY" data-test="edit">Edit</router-link>
        </div>
      </div>

      <div class="flex-1 px-4 pb-6 min-w-0 flex flex-col">
        <ConnectPanel v-if="activeTab === 'connect'" :database="database" />
        <PiiEditor v-else-if="activeTab === 'pii'" :database-id="database.id" @saved="onPiiSaved" />
        <QueryConsole v-else-if="activeTab === 'query'" :database-id="database.id" />
        <ActivityPanel v-else-if="activeTab === 'activity'" :database-id="database.id" :driver="database.driver" />
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import ActivityPanel from '../components/ActivityPanel.vue'
import AppLogo from '../components/AppLogo.vue'
import ConnectPanel from '../components/ConnectPanel.vue'
import LoadingState from '../components/LoadingState.vue'
import PiiEditor from '../components/PiiEditor.vue'
import QueryConsole from '../components/QueryConsole.vue'
import { fetchDatabase } from '../api'
import { useDatabaseStore } from '../stores/databaseStore'
import { driverName } from '../utils/drivers'
import { BTN_SECONDARY, PAGE_TITLE } from '../ui'

const TABS = [
  { id: 'connect', label: 'Connect' },
  { id: 'pii', label: 'PII columns' },
  { id: 'query', label: 'Query console' },
  { id: 'activity', label: 'Activity' },
]

const route = useRoute()
const router = useRouter()
const store = useDatabaseStore()

const databaseId = route.params.id
// Shown from the list straight away when we have it, then refreshed.
const database = ref(store.getById(databaseId))
const loading = ref(!database.value)
const loadError = ref('')

const activeTab = computed(() => (TABS.some((t) => t.id === route.query.tab) ? route.query.tab : 'connect'))
function selectTab(id) {
  router.replace({ query: { ...route.query, tab: id === 'connect' ? undefined : id } })
}

onMounted(async () => {
  try {
    const db = await fetchDatabase(databaseId)
    database.value = db
    store.upsert(db)
  } catch (err) {
    if (!database.value) loadError.value = err?.message || 'Failed to load database'
  } finally {
    loading.value = false
  }
})

function onPiiSaved(count) {
  database.value = { ...database.value, piiColumnCount: count }
  store.upsert(database.value)
}
</script>
