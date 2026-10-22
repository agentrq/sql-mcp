<script setup>
import { computed } from 'vue'
import CopyField from './CopyField.vue'
import { claudeCodeCommand, mcpJsonConfig } from '../utils/mcpSnippets'
import { CARD, SECTION_LABEL } from '../ui'

const props = defineProps({ database: { type: Object, required: true } })

const claudeCmd = computed(() => claudeCodeCommand(props.database))
const jsonConfig = computed(() => mcpJsonConfig(props.database))
</script>

<template>
  <div class="space-y-6 w-full">
    <section :class="[CARD, 'p-5 sm:p-6']">
      <h2 :class="SECTION_LABEL" class="mb-3">MCP server URL</h2>
      <CopyField :value="database.mcpUrl" label="MCP URL copied" data-test="mcp-url" />
      <div class="mt-4 flex items-start gap-3 p-3 rounded-lg bg-gray-50 dark:bg-zinc-800/40 border border-gray-100 dark:border-zinc-800">
        <svg class="w-4 h-4 shrink-0 mt-0.5 text-gray-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" /></svg>
        <p class="text-xs text-gray-600 dark:text-zinc-300 leading-relaxed">
          The server is protected with OAuth. When a client connects for the first time it opens a consent screen in your browser;
          sign in with an allow-listed account and approve access to <span class="font-semibold">{{ database.name }}</span>.
          Queries that touch a column marked as PII are rejected.
        </p>
      </div>
    </section>

    <section :class="[CARD, 'p-5 sm:p-6']">
      <h2 :class="SECTION_LABEL" class="mb-1">Claude Code</h2>
      <p class="text-xs text-gray-500 dark:text-zinc-400 mb-3">Run in your terminal:</p>
      <CopyField :value="claudeCmd" label="Command copied" multiline data-test="claude-snippet" />
    </section>

    <section :class="[CARD, 'p-5 sm:p-6']">
      <h2 :class="SECTION_LABEL" class="mb-1">Other MCP clients</h2>
      <p class="text-xs text-gray-500 dark:text-zinc-400 mb-3">Add to the client's MCP configuration (for example <code class="font-mono">.mcp.json</code>):</p>
      <CopyField :value="jsonConfig" label="Configuration copied" multiline data-test="json-snippet" />
    </section>
  </div>
</template>
