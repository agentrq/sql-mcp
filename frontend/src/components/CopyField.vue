<!-- A read-only value in mono with a copy button: MCP URLs and snippets. -->
<script setup>
import { copyToClipboard } from '../composables/useClipboard'

const props = defineProps({
  value: { type: String, default: '' },
  label: { type: String, default: 'Copied to clipboard' },
  multiline: Boolean,
  compact: Boolean,
})

const copy = () => copyToClipboard(props.value, props.label)
</script>

<template>
  <div class="flex items-stretch gap-0 min-w-0 rounded-lg border border-gray-200 dark:border-zinc-800 bg-gray-50 dark:bg-zinc-800/50 overflow-hidden">
    <pre v-if="multiline" class="flex-1 min-w-0 px-3 py-2.5 text-[12px] font-mono text-gray-800 dark:text-zinc-200 overflow-x-auto whitespace-pre">{{ value }}</pre>
    <code v-else class="flex-1 min-w-0 truncate font-mono text-gray-800 dark:text-zinc-200" :class="compact ? 'px-2.5 py-1.5 text-[11px]' : 'px-3 py-2.5 text-[12px]'" :title="value">{{ value }}</code>
    <button type="button" data-test="copy" @click.stop.prevent="copy" :aria-label="`Copy ${value}`" title="Copy"
            class="shrink-0 flex items-start justify-center border-l border-gray-200 dark:border-zinc-800 text-gray-400 hover:text-gray-900 dark:hover:text-white hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors"
            :class="compact ? 'px-2 py-1.5' : 'px-3 py-2.5'">
      <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" /></svg>
    </button>
  </div>
</template>
