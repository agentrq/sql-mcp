<template>
  <div class="h-full flex flex-col w-full">
    <div class="py-2 mb-6 flex flex-col sm:flex-row sm:items-center justify-between gap-4 px-4">
      <div class="min-w-0">
        <router-link :to="isEdit ? `/databases/${databaseId}` : '/'" class="text-[10px] font-black uppercase tracking-widest text-gray-400 hover:text-gray-900 dark:hover:text-white transition-colors">
          &larr; {{ isEdit ? (original?.name || 'Database') : 'Databases' }}
        </router-link>
        <h1 :class="[PAGE_TITLE, 'mt-1']">{{ isEdit ? 'Edit database' : 'New database' }}</h1>
      </div>
      <button v-if="isEdit && original" type="button" data-test="delete" :class="BTN_DANGER" @click="showDelete = true">Delete</button>
    </div>

    <LoadingState v-if="loading" label="Loading database..." />

    <div v-else-if="loadError" class="mx-4 bg-red-50 dark:bg-red-500/10 border border-red-200 dark:border-red-500/30 text-red-700 dark:text-red-400 px-5 py-3 rounded-lg text-xs font-bold">
      {{ loadError }}
    </div>

    <form v-else @submit.prevent="save" class="px-4 pb-10 w-full space-y-6" data-test="database-form">
      <div :class="[CARD, 'p-5 sm:p-8 space-y-8']">
        <div>
          <label for="db-name" :class="LABEL">Name</label>
          <input id="db-name" v-model="form.name" type="text" required maxlength="100" :class="INPUT" placeholder="e.g. analytics" autocomplete="off" />
          <p :class="HINT">Shown in the sidebar and used as the MCP server name.</p>
        </div>

        <div>
          <span :class="LABEL">Driver</span>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-3" role="radiogroup" aria-label="Driver">
            <button v-for="d in drivers" :key="d.id" type="button" role="radio"
                    :data-test="`driver-${d.id}`"
                    :aria-checked="form.driver === d.id"
                    :disabled="!d.available || isEdit"
                    @click="form.driver = d.id"
                    class="relative flex items-center gap-3 p-4 rounded-xl border text-left transition-colors disabled:cursor-not-allowed"
                    :class="[
                      form.driver === d.id ? 'border-gray-900 dark:border-white bg-gray-50 dark:bg-zinc-800/60' : 'border-gray-200 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-zinc-700',
                      !d.available ? 'opacity-50' : ''
                    ]">
              <div class="w-9 h-9 rounded-lg flex items-center justify-center shrink-0 bg-white border border-gray-200 dark:border-zinc-700 text-gray-600">
                <DriverIcon :driver="d.id" class="w-6 h-6" />
              </div>
              <div class="min-w-0 flex-1">
                <div class="text-sm font-bold text-gray-900 dark:text-zinc-100">{{ d.name }}</div>
                <div class="text-[11px] text-gray-500 dark:text-zinc-400">{{ d.available ? 'Available' : 'Not yet supported' }}</div>
              </div>
              <span v-if="!d.available" data-test="coming-soon" class="shrink-0 px-1.5 py-0.5 rounded text-[9px] font-black uppercase tracking-widest bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-400">Coming soon</span>
              <svg v-else-if="form.driver === d.id" class="w-4 h-4 shrink-0 text-gray-900 dark:text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
            </button>
          </div>
          <p v-if="isEdit" :class="HINT">The driver cannot be changed after the database is created.</p>
        </div>

        <div>
          <label for="db-dsn" :class="LABEL">Connection string (DSN)</label>
          <div class="relative">
            <input id="db-dsn" v-model="form.dsn" :type="showDsn ? 'text' : 'password'" :required="!isEdit"
                   :class="[INPUT, 'font-mono pr-20']" :placeholder="isEdit ? (original?.dsnMasked || '') : dsnPlaceholder"
                   autocomplete="off" spellcheck="false" autocapitalize="off" />
            <button type="button" data-test="toggle-dsn" @click="showDsn = !showDsn" :class="[BTN_GHOST, 'absolute right-2 top-1/2 -translate-y-1/2']">
              {{ showDsn ? 'Hide' : 'Show' }}
            </button>
          </div>
          <p :class="HINT" data-test="dsn-format">{{ dsnFormat }}</p>
          <p v-if="isEdit" :class="HINT" data-test="dsn-keep-hint">Leave blank to keep the current connection string.</p>
          <p v-else :class="HINT">Stored encrypted. Use a read-only database user where possible.</p>

          <div class="mt-4 flex flex-wrap items-center gap-3">
            <button type="button" data-test="test-connection" :class="BTN_SECONDARY" :disabled="testing || !form.dsn.trim() || !selectedDriverAvailable" @click="test">
              {{ testing ? 'Testing...' : 'Test connection' }}
            </button>
            <span v-if="testResult?.ok" data-test="test-ok" class="inline-flex items-center gap-1.5 text-xs font-semibold text-green-700 dark:text-green-400">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
              Connected{{ testResult.serverVersion ? ` · ${testResult.serverVersion}` : '' }}
            </span>
            <span v-else-if="testResult?.error" data-test="test-error" class="text-xs font-semibold text-red-600 dark:text-red-400 break-words min-w-0">{{ testResult.error }}</span>
          </div>
        </div>
      </div>

      <div class="flex items-center justify-end gap-3">
        <router-link :to="isEdit ? `/databases/${databaseId}` : '/'" :class="BTN_SECONDARY">Cancel</router-link>
        <button type="submit" data-test="save" :class="BTN_PRIMARY" :disabled="saving || !canSave">
          {{ saving ? 'Saving...' : isEdit ? 'Save changes' : 'Create database' }}
        </button>
      </div>
    </form>

    <DeleteModal :show="showDelete" title="Delete database" :item-name="original?.name || ''" :busy="deleting"
                 message=""
                 @close="showDelete = false" @confirm="remove" />
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import DeleteModal from '../components/DeleteModal.vue'
import DriverIcon from '../components/DriverIcon.vue'
import LoadingState from '../components/LoadingState.vue'
import { createDatabase, deleteDatabase, fetchDatabase, fetchDrivers, testConnection, updateDatabase } from '../api'
import { useToasts } from '../composables/useToasts'
import { useDatabaseStore } from '../stores/databaseStore'
import { DSN_FORMATS, DSN_PLACEHOLDERS, FALLBACK_DRIVERS } from '../utils/drivers'
import { BTN_DANGER, BTN_GHOST, BTN_PRIMARY, BTN_SECONDARY, CARD, HINT, INPUT, LABEL, PAGE_TITLE } from '../ui'

const route = useRoute()
const router = useRouter()
const store = useDatabaseStore()
const { notifySuccess, notifyError } = useToasts()

const databaseId = computed(() => route.params.id)
const isEdit = computed(() => Boolean(databaseId.value))

const drivers = ref(FALLBACK_DRIVERS)
const original = ref(null)
const form = reactive({ name: '', driver: 'postgres', dsn: '' })
const showDsn = ref(false)
const loading = ref(false)
const loadError = ref('')
const saving = ref(false)
const testing = ref(false)
const testResult = ref(null)
const showDelete = ref(false)
const deleting = ref(false)

const selectedDriverAvailable = computed(() => Boolean(drivers.value.find((d) => d.id === form.driver)?.available))
const dsnPlaceholder = computed(() => DSN_PLACEHOLDERS[form.driver] || DSN_PLACEHOLDERS.postgres)
const dsnFormat = computed(() => DSN_FORMATS[form.driver] || '')
const canSave = computed(() => form.name.trim() !== '' && (isEdit.value || (form.dsn.trim() !== '' && selectedDriverAvailable.value)))

// A test result describes the DSN it was run on; editing either invalidates it.
watch(() => [form.dsn, form.driver], () => { testResult.value = null })

onMounted(async () => {
  loading.value = isEdit.value
  try {
    const list = await fetchDrivers()
    if (list.length) drivers.value = list
  } catch {
    // keep the fallback list
  }
  if (!isEdit.value) {
    const firstAvailable = drivers.value.find((d) => d.available)
    if (firstAvailable) form.driver = firstAvailable.id
    return
  }
  try {
    const db = await fetchDatabase(databaseId.value)
    original.value = db
    form.name = db?.name || ''
    form.driver = db?.driver || 'postgres'
  } catch (err) {
    loadError.value = err?.message || 'Failed to load database'
  } finally {
    loading.value = false
  }
})

async function test() {
  testing.value = true
  testResult.value = null
  try {
    const res = await testConnection({ driver: form.driver, dsn: form.dsn.trim() })
    testResult.value = { ok: true, serverVersion: res?.serverVersion || '' }
  } catch (err) {
    testResult.value = { ok: false, error: err?.message || 'Connection failed' }
  } finally {
    testing.value = false
  }
}

async function save() {
  if (!canSave.value) return
  saving.value = true
  try {
    const saved = isEdit.value
      ? await updateDatabase(databaseId.value, { name: form.name.trim(), dsn: form.dsn.trim() })
      : await createDatabase({ name: form.name.trim(), driver: form.driver, dsn: form.dsn.trim() })
    store.upsert(saved)
    notifySuccess(isEdit.value ? 'Database updated' : 'Database created')
    router.push(`/databases/${saved?.id ?? databaseId.value}`)
  } catch (err) {
    notifyError(err?.message || 'Failed to save database')
  } finally {
    saving.value = false
  }
}

async function remove() {
  deleting.value = true
  try {
    await deleteDatabase(databaseId.value)
    store.remove(databaseId.value)
    showDelete.value = false
    notifySuccess('Database deleted')
    router.push('/')
  } catch (err) {
    notifyError(err?.message || 'Failed to delete database')
  } finally {
    deleting.value = false
  }
}
</script>
