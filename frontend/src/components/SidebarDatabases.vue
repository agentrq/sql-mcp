<script setup>
import { useRoute } from 'vue-router'
import { toKebabCase } from '../composables/useFormat'

defineProps({
  databases: { type: Array, default: () => [] },
  showTooltip: { type: Function, default: () => {} },
  hideTooltip: { type: Function, default: () => {} },
})

const route = useRoute()
const isActive = (db) => route.path === `/databases/${db.id}` || route.path.startsWith(`/databases/${db.id}/`)
</script>

<template>
  <div class="space-y-0.5">
    <router-link v-for="db in databases" :key="db.id" :to="`/databases/${db.id}`"
        data-test="sidebar-database"
        @mouseenter="showTooltip($event, db.name)" @mouseleave="hideTooltip"
        class="flex items-center gap-2.5 py-1.5 pl-2 pr-2 text-xs transition-all duration-150 rounded-md"
        :class="isActive(db) ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white font-semibold' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'">
      <div class="w-1.5 h-1.5 rounded-full shrink-0"
           :class="db.piiColumnCount > 0 ? 'bg-green-500 dark:bg-green-400' : 'bg-gray-300 dark:bg-zinc-600'"
           :title="db.piiColumnCount > 0 ? 'PII columns protected' : 'No PII columns marked'"></div>
      <span class="truncate flex-1">{{ toKebabCase(db.name) }}</span>
    </router-link>
  </div>
</template>
