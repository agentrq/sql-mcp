<script setup>
defineProps({ label: { type: String, default: 'Loading…' } })
</script>

<template>
  <div class="flex items-center justify-center gap-3 py-10 text-gray-400 dark:text-zinc-500">
    <svg class="animate-spin h-4 w-4" fill="none" viewBox="0 0 24 24" aria-hidden="true">
      <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
      <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
    </svg>
    <span class="text-[11px] font-black uppercase tracking-widest">{{ label }}</span>
  </div>
</template>
