<template>
  <div class="h-full flex flex-col w-full">
    <div class="py-2 mb-6 px-4">
      <h1 :class="PAGE_TITLE">Settings</h1>
    </div>

    <div class="px-4 pb-10 w-full space-y-6">
      <!-- tmphost.io -->
      <section :class="[CARD, 'p-5 sm:p-6']" data-test="tmphost-card">
        <div class="flex items-start justify-between gap-4 mb-4">
          <div class="min-w-0">
            <div class="flex items-center gap-2">
              <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100">tmphost.io</h2>
              <span v-if="tmphost?.connected" data-test="tmphost-connected" class="px-1.5 py-0.5 rounded text-[9px] font-black uppercase tracking-widest bg-green-50 text-green-700 dark:bg-green-500/10 dark:text-green-400">Connected</span>
              <span v-else-if="tmphost" class="px-1.5 py-0.5 rounded text-[9px] font-black uppercase tracking-widest bg-gray-100 text-gray-500 dark:bg-zinc-800 dark:text-zinc-400">Not connected</span>
            </div>
            <p class="text-xs text-gray-500 dark:text-zinc-400 mt-1 leading-relaxed">
              Query results too large to return inline are stored on
              <a :href="serverUrl" target="_blank" rel="noopener" class="underline">tmphost.io</a>
              in a folder that expires after 30 minutes.
            </p>
          </div>
        </div>

        <LoadingState v-if="loading" label="Loading..." />
        <div v-else-if="loadError" class="p-3 rounded-lg border border-red-200 dark:border-red-500/30 bg-red-50 dark:bg-red-500/10 text-xs text-red-700 dark:text-red-300">{{ loadError }}</div>

        <template v-else-if="tmphost?.connected">
          <dl class="grid grid-cols-1 sm:grid-cols-3 gap-4 p-4 rounded-lg bg-gray-50 dark:bg-zinc-800/40 border border-gray-100 dark:border-zinc-800 mb-4">
            <div class="min-w-0">
              <dt :class="LABEL" class="!ml-0 !mb-1">Account</dt>
              <dd class="text-xs font-semibold text-gray-900 dark:text-zinc-100 truncate" data-test="tmphost-account">{{ tmphost.account || '—' }}</dd>
            </div>
            <div class="min-w-0">
              <dt :class="LABEL" class="!ml-0 !mb-1">Server</dt>
              <dd class="text-xs font-mono text-gray-700 dark:text-zinc-300 truncate">{{ tmphost.serverUrl }}</dd>
            </div>
            <div class="min-w-0">
              <dt :class="LABEL" class="!ml-0 !mb-1">Connected</dt>
              <dd class="text-xs text-gray-700 dark:text-zinc-300">{{ formatDateTime(tmphost.connectedAt) || '—' }}</dd>
            </div>
          </dl>
          <div class="flex justify-end">
            <button type="button" data-test="tmphost-disconnect" :class="BTN_DANGER" @click="showDisconnect = true">Disconnect</button>
          </div>
        </template>

        <div v-else class="flex justify-end">
          <a :href="connectUrl" data-test="tmphost-connect" :class="BTN_PRIMARY">Connect tmphost</a>
        </div>
      </section>

      <!-- Account -->
      <section :class="[CARD, 'p-5 sm:p-6']" data-test="account-card">
        <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100 mb-4">Account</h2>
        <div class="flex items-center gap-3">
          <div class="w-10 h-10 rounded-full border border-gray-200 dark:border-zinc-700 bg-white dark:bg-zinc-900 flex items-center justify-center overflow-hidden text-sm font-bold text-gray-700 dark:text-zinc-200 shrink-0">
            <img v-if="auth.user?.picture" :src="auth.user.picture" alt="" class="w-full h-full object-cover" referrerpolicy="no-referrer" />
            <span v-else>{{ (auth.user?.name || auth.user?.email || '?').charAt(0).toUpperCase() }}</span>
          </div>
          <div class="min-w-0">
            <p class="text-sm font-semibold text-gray-900 dark:text-zinc-100 truncate">{{ auth.user?.name || auth.user?.email }}</p>
            <p v-if="auth.user?.name" class="text-xs text-gray-500 dark:text-zinc-400 truncate">{{ auth.user?.email }}</p>
          </div>
        </div>
      </section>
    </div>

    <DeleteModal :show="showDisconnect" title="Disconnect tmphost" confirm-label="Disconnect" :busy="disconnecting"
                 message="Large query results will no longer be stored on tmphost.io; they are truncated instead. Files already uploaded expire on their own."
                 @close="showDisconnect = false" @confirm="disconnect" />
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import DeleteModal from '../components/DeleteModal.vue'
import LoadingState from '../components/LoadingState.vue'
import { disconnectTmphost, fetchTmphost, tmphostConnectUrl } from '../api'
import { formatDateTime } from '../composables/useFormat'
import { useToasts } from '../composables/useToasts'
import { useAuthStore } from '../stores/authStore'
import { BTN_DANGER, BTN_PRIMARY, CARD, LABEL, PAGE_TITLE } from '../ui'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { notifySuccess, notifyError } = useToasts()

const tmphost = ref(null)
const loading = ref(true)
const loadError = ref('')
const showDisconnect = ref(false)
const disconnecting = ref(false)
const connectUrl = tmphostConnectUrl()
const serverUrl = computed(() => tmphost.value?.serverUrl || 'https://app.tmphost.io')

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    tmphost.value = await fetchTmphost()
  } catch (err) {
    loadError.value = err?.message || 'Failed to load tmphost settings'
  } finally {
    loading.value = false
  }
}

/** The OAuth callback lands here with ?tmphost=connected|error; say so once, then drop it from the URL. */
function consumeCallbackResult() {
  const result = route.query.tmphost
  if (!result) return
  if (result === 'connected') notifySuccess('tmphost.io connected')
  else notifyError(route.query.message || 'Could not connect tmphost.io', 'tmphost')
  const { tmphost: _t, message: _m, ...rest } = route.query
  router.replace({ query: rest })
}

async function disconnect() {
  disconnecting.value = true
  try {
    await disconnectTmphost()
    showDisconnect.value = false
    notifySuccess('tmphost.io disconnected')
    await load()
  } catch (err) {
    notifyError(err?.message || 'Failed to disconnect')
  } finally {
    disconnecting.value = false
  }
}

onMounted(() => {
  consumeCallbackResult()
  load()
})
</script>
