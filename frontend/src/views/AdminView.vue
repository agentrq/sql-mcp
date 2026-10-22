<template>
  <div class="h-full flex flex-col w-full">
    <div class="py-2 mb-6 px-4 flex flex-wrap items-center justify-between gap-3">
      <div class="min-w-0">
        <h1 :class="PAGE_TITLE">Setup</h1>
        <p v-if="settings" class="text-xs text-gray-500 dark:text-zinc-400 mt-1" data-test="setup-source">
          <template v-if="settings.source === 'saved'">Saved configuration, updated {{ formatDateTime(settings.updatedAt) }}. It overrides the environment.</template>
          <template v-else>From the environment (.env). Saving here overrides it.</template>
        </p>
      </div>
      <div v-if="settings" class="flex items-center gap-2">
        <button v-if="settings.source === 'saved'" type="button" data-test="setup-reset" :class="BTN_SECONDARY" @click="showReset = true">Reset to environment</button>
        <button type="button" data-test="setup-save" :class="BTN_PRIMARY" :disabled="saving || !dirty" @click="save">
          {{ saving ? 'Saving...' : 'Save' }}
        </button>
      </div>
    </div>

    <LoadingState v-if="loading" label="Loading setup..." class="px-4" />
    <div v-else-if="loadError" class="mx-4 p-3 rounded-lg border border-red-200 dark:border-red-500/30 bg-red-50 dark:bg-red-500/10 text-xs text-red-700 dark:text-red-300">{{ loadError }}</div>

    <div v-else class="px-4 pb-10 w-full grid grid-cols-1 xl:grid-cols-2 gap-6 items-start">
      <!-- Allowlist -->
      <section :class="[CARD, 'p-5 sm:p-6']" data-test="allowlist-card">
        <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100">Allowed emails</h2>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-1 mb-4 leading-relaxed">
          Who may sign in, one entry per line: an exact address, a pattern where
          <code class="font-mono">*</code> matches anything (<code class="font-mono">*@agentrq.com</code>), or a regular
          expression after <code class="font-mono">re:</code>. Matching ignores case and covers the whole address.
          A change applies at once; people no longer allowed are signed out at their next session refresh.
        </p>
        <label :class="LABEL" for="allowed-emails">Entries ({{ entries.length }})</label>
        <textarea id="allowed-emails" v-model="form.allowedEmails" data-test="allowed-emails" rows="12" spellcheck="false"
                  :class="[INPUT, 'font-mono text-xs leading-relaxed resize-y']" placeholder="*@agentrq.com"></textarea>
        <p v-if="entries.length === 0" :class="HINT" class="!text-red-600 dark:!text-red-400">At least one entry is required, or nobody can sign in.</p>

        <div class="mt-6 pt-5 border-t border-gray-100 dark:border-zinc-800">
          <label :class="LABEL" for="check-email">Test an email against these entries</label>
          <form class="flex gap-2" @submit.prevent="check">
            <input id="check-email" v-model="checkEmail" data-test="check-email" type="email" :class="INPUT" placeholder="someone@example.com" />
            <button type="submit" data-test="check-email-submit" :class="BTN_SECONDARY" :disabled="!checkEmail || checking">Test</button>
          </form>
          <p v-if="checkResult" data-test="check-result" :class="HINT">
            <span v-if="checkResult.allowed" class="text-green-700 dark:text-green-400 font-bold">Allowed</span>
            <span v-else class="text-red-600 dark:text-red-400 font-bold">Not allowed</span>
            <span v-if="checkResult.matchedEntry"> by <code class="font-mono">{{ checkResult.matchedEntry }}</code></span>
          </p>
        </div>
      </section>

      <div class="flex flex-col gap-6 min-w-0">
      <!-- Query limits -->
      <section :class="[CARD, 'p-5 sm:p-6']" data-test="limits-card">
        <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100">Queries</h2>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-1 mb-4 leading-relaxed">
          How much a query may read, and how much of it an MCP client gets inline. A larger result is stored whole on the
          owner's tmphost.io and its URL returned with a preview.
        </p>
        <div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
          <div>
            <label :class="LABEL" for="max-rows">Max rows</label>
            <input id="max-rows" v-model.number="form.queryMaxRows" data-test="max-rows" type="number" min="1" max="100000" :class="INPUT" />
          </div>
          <div>
            <label :class="LABEL" for="inline-rows">Inline rows</label>
            <input id="inline-rows" v-model.number="form.queryInlineRows" data-test="inline-rows" type="number" min="1" :class="INPUT" />
          </div>
          <div>
            <label :class="LABEL" for="timeout">Timeout (seconds)</label>
            <input id="timeout" v-model.number="form.queryTimeoutSeconds" data-test="timeout" type="number" min="1" max="600" :class="INPUT" />
          </div>
        </div>
      </section>

      <!-- Server -->
      <section :class="[CARD, 'p-5 sm:p-6']" data-test="server-card">
        <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100">Server</h2>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-1 mb-4">Set in the environment; a change needs a restart.</p>
        <dl class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div v-for="item in serverItems" :key="item.label" class="min-w-0">
            <dt :class="LABEL" class="!ml-0 !mb-1">{{ item.label }}</dt>
            <dd class="text-xs font-mono text-gray-800 dark:text-zinc-200 truncate" :title="item.value">{{ item.value }}</dd>
          </div>
        </dl>
      </section>
      </div>
    </div>

    <DeleteModal :show="showReset" title="Reset to environment" confirm-label="Reset" :busy="saving"
                 message="The saved configuration is deleted and the values from the server's environment (.env) apply again, including its allowed emails."
                 @close="showReset = false" @confirm="reset" />
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import DeleteModal from '../components/DeleteModal.vue'
import LoadingState from '../components/LoadingState.vue'
import { checkAdminEmail, fetchAdminSettings, resetAdminSettings, saveAdminSettings } from '../api'
import { formatDateTime } from '../composables/useFormat'
import { useToasts } from '../composables/useToasts'
import { BTN_PRIMARY, BTN_SECONDARY, CARD, HINT, INPUT, LABEL, PAGE_TITLE } from '../ui'

const { notifySuccess, notifyError } = useToasts()

const settings = ref(null)
const loading = ref(true)
const loadError = ref('')
const saving = ref(false)
const showReset = ref(false)
const form = reactive({ allowedEmails: '', queryMaxRows: 0, queryInlineRows: 0, queryTimeoutSeconds: 0 })
const checkEmail = ref('')
const checking = ref(false)
const checkResult = ref(null)

/** The textarea's entries: one per line or comma, blanks dropped. */
const entries = computed(() => form.allowedEmails.split(/[\n,]/).map((e) => e.trim()).filter(Boolean))

const dirty = computed(() => {
  const s = settings.value
  if (!s) return false
  return entries.value.join('\n') !== s.allowedEmails.join('\n')
    || form.queryMaxRows !== s.queryMaxRows
    || form.queryInlineRows !== s.queryInlineRows
    || form.queryTimeoutSeconds !== s.queryTimeoutSeconds
})

const serverItems = computed(() => {
  const s = settings.value?.server || {}
  const yes = (v) => (v ? 'enabled' : 'disabled')
  return [
    { label: 'Base URL', value: s.baseUrl || '—' },
    { label: "HTTPS (Let's Encrypt)", value: s.sslEnabled ? `${s.domain}:${s.httpsPort}` : 'disabled' },
    { label: 'Google sign-in', value: yes(s.googleLoginEnabled) },
    { label: 'Root token sign-in', value: yes(s.rootLoginEnabled) },
    { label: 'App database', value: s.appDbDriver || '—' },
    { label: 'tmphost', value: s.tmphostApiUrl || '—' },
  ]
})

function apply(s) {
  settings.value = s
  form.allowedEmails = s.allowedEmails.join('\n')
  form.queryMaxRows = s.queryMaxRows
  form.queryInlineRows = s.queryInlineRows
  form.queryTimeoutSeconds = s.queryTimeoutSeconds
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    apply(await fetchAdminSettings())
  } catch (err) {
    loadError.value = err?.message || 'Failed to load the setup'
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  try {
    apply(await saveAdminSettings({
      allowedEmails: entries.value,
      queryMaxRows: form.queryMaxRows,
      queryInlineRows: form.queryInlineRows,
      queryTimeoutSeconds: form.queryTimeoutSeconds,
    }))
    notifySuccess('Setup saved')
  } catch (err) {
    notifyError(err?.message || 'Failed to save', 'Setup')
  } finally {
    saving.value = false
  }
}

async function reset() {
  saving.value = true
  try {
    apply(await resetAdminSettings())
    showReset.value = false
    notifySuccess('Back to the environment configuration')
  } catch (err) {
    notifyError(err?.message || 'Failed to reset', 'Setup')
  } finally {
    saving.value = false
  }
}

async function check() {
  checking.value = true
  checkResult.value = null
  try {
    checkResult.value = await checkAdminEmail(checkEmail.value, entries.value)
  } catch (err) {
    notifyError(err?.message || 'Could not test the email', 'Setup')
  } finally {
    checking.value = false
  }
}

onMounted(load)
</script>
