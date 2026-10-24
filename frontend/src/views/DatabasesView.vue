<template>
  <div class="h-full flex flex-col w-full">
    <div class="py-2 mb-6 flex flex-col sm:flex-row sm:items-center justify-between gap-4 px-4">
      <div>
        <h1 :class="PAGE_TITLE">Databases</h1>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-1">Each database is its own MCP server. Queries touching a PII column are rejected.</p>
      </div>
      <router-link to="/databases/new" :class="BTN_PRIMARY" data-test="new-database">
        <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M12 4v16m8-8H4" /></svg>
        New database
      </router-link>
    </div>

    <div class="flex-1 px-4 pb-10">
      <div v-if="store.error && !store.databases.length" class="bg-red-50 dark:bg-red-500/10 border border-red-200 dark:border-red-500/30 text-red-700 dark:text-red-400 px-5 py-3 rounded-lg text-xs font-bold">
        {{ store.error }}
      </div>

      <LoadingState v-else-if="!store.loaded && store.loading" label="Loading databases..." />

      <!-- Empty state -->
      <div v-else-if="store.loaded && store.databases.length === 0" data-test="empty"
           class="flex flex-col items-center justify-center text-center py-20 border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl">
        <div class="w-12 h-12 rounded-xl bg-gray-100 dark:bg-zinc-800 flex items-center justify-center mb-4">
          <AppLogo class="w-6 h-6 text-gray-500 dark:text-zinc-400" />
        </div>
        <h3 class="text-sm font-bold text-gray-900 dark:text-zinc-100">No databases yet</h3>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-1 mb-6 max-w-sm">Register a PostgreSQL or MySQL database to get an MCP server URL your agents can connect to.</p>
        <router-link to="/databases/new" :class="BTN_PRIMARY">New database</router-link>
      </div>

      <div v-else class="grid grid-cols-1 lg:grid-cols-2 2xl:grid-cols-3 gap-4">
        <router-link v-for="db in store.databases" :key="db.id" :to="`/databases/${db.id}`" data-test="database-card"
                     :class="[CARD, 'block p-5 hover:border-gray-300 dark:hover:border-zinc-700 transition-colors min-w-0']">
          <div class="flex items-start justify-between gap-3 mb-4">
            <div class="flex items-center gap-3 min-w-0">
              <div class="w-9 h-9 rounded-lg bg-white border border-gray-200 dark:border-zinc-700 flex items-center justify-center shrink-0 text-gray-700">
                <DriverIcon :driver="db.driver" class="w-6 h-6" />
              </div>
              <div class="min-w-0">
                <h3 class="text-sm font-bold text-gray-900 dark:text-zinc-100 truncate">{{ db.name }}</h3>
                <span class="inline-block mt-0.5 px-1.5 py-0.5 rounded text-[9px] font-black uppercase tracking-widest bg-gray-100 dark:bg-zinc-800 text-gray-600 dark:text-zinc-300">{{ driverName(db.driver) }}</span>
              </div>
            </div>
            <span class="shrink-0 inline-flex items-center gap-1.5 px-2 py-1 rounded-md text-[10px] font-black uppercase tracking-widest"
                  :class="db.piiColumnCount > 0 ? 'bg-green-50 text-green-700 dark:bg-green-500/10 dark:text-green-400' : 'bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-400'">
              {{ db.piiColumnCount || 0 }} PII
            </span>
          </div>
          <p class="font-mono text-[11px] text-gray-500 dark:text-zinc-400 truncate mb-3" :title="db.dsnMasked">{{ db.dsnMasked }}</p>
          <div>
            <span class="block text-[9px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest mb-1">MCP URL</span>
            <CopyField :value="db.mcpUrl" label="MCP URL copied" compact />
          </div>
        </router-link>
      </div>
    </div>
  </div>
</template>

<script setup>
import { onMounted } from 'vue'
import AppLogo from '../components/AppLogo.vue'
import CopyField from '../components/CopyField.vue'
import DriverIcon from '../components/DriverIcon.vue'
import LoadingState from '../components/LoadingState.vue'
import { useDatabaseStore } from '../stores/databaseStore'
import { driverName } from '../utils/drivers'
import { BTN_PRIMARY, CARD, PAGE_TITLE } from '../ui'

const store = useDatabaseStore()
onMounted(() => store.fetchDatabases())
</script>
