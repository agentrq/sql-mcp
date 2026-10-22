<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center p-4 bg-gray-50/80 dark:bg-zinc-950/80 backdrop-blur-sm">
    <div class="w-full max-w-md p-8 bg-white dark:bg-zinc-900 rounded-3xl shadow-2xl border border-gray-100 dark:border-zinc-800">
      <div class="mb-10 text-center">
        <AppLogo tile />
        <h2 class="text-3xl font-black text-gray-900 dark:text-zinc-50 tracking-tight mb-2">SQL MCP Admin</h2>
        <p class="text-gray-500 dark:text-zinc-400 text-sm leading-relaxed">
          Server setup: who can sign in, and how queries run
        </p>
      </div>

      <div class="space-y-4">
        <div v-if="loadingConfig" class="flex justify-center py-6">
          <svg class="animate-spin h-6 w-6 text-gray-500" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
          </svg>
        </div>

        <p v-else-if="!enabled" data-test="admin-disabled" class="p-4 rounded-2xl bg-gray-50 dark:bg-zinc-800/50 border border-gray-100 dark:border-zinc-800 text-sm text-center text-gray-600 dark:text-zinc-300">
          The admin setup is disabled. Set <code class="font-mono text-xs">ROOT_TOKEN</code> on the server to enable it.
        </p>

        <form v-else data-test="admin-login" @submit.prevent="submit" class="space-y-4">
          <input
            v-model="rootToken"
            type="password"
            autocomplete="current-password"
            aria-label="Root token"
            placeholder="Paste root token..."
            class="w-full px-4 py-4 bg-gray-50 dark:bg-zinc-800/50 text-gray-900 dark:text-zinc-50 border border-gray-100 dark:border-zinc-700/50 rounded-2xl outline-none focus:ring-4 focus:ring-black/5 dark:focus:ring-white/10 focus:border-black dark:focus:border-white transition-all text-center placeholder:text-gray-500"
            required
          />
          <div v-if="errorMsg" data-test="admin-error" class="p-4 bg-red-50 dark:bg-red-900/20 border border-red-100 dark:border-red-900/30 rounded-2xl">
            <p class="text-sm text-red-600 dark:text-red-400 font-medium text-center">{{ errorMsg }}</p>
          </div>
          <button type="submit" :disabled="loggingIn"
            class="w-full py-4 bg-gray-900 dark:bg-zinc-800 text-white dark:text-zinc-200 font-bold rounded-2xl hover:bg-black dark:hover:bg-zinc-700 transform active:scale-[0.98] transition-all shadow-sm disabled:opacity-50 disabled:cursor-not-allowed">
            {{ loggingIn ? 'Verifying...' : 'Sign in as admin' }}
          </button>
        </form>

        <div class="mt-8 flex items-center justify-between pt-6 border-t border-gray-100 dark:border-zinc-800 text-[11px] font-bold text-gray-500 dark:text-zinc-500">
          <span>Admin sessions last one hour</span>
          <router-link to="/login" class="hover:text-gray-900 dark:hover:text-white transition-colors">User sign-in</router-link>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import AppLogo from '../components/AppLogo.vue'
import { fetchAdminConfig } from '../api'
import { useAdminStore } from '../stores/adminStore'

const router = useRouter()
const admin = useAdminStore()

const enabled = ref(false)
const loadingConfig = ref(true)
const rootToken = ref('')
const loggingIn = ref(false)
const errorMsg = ref('')

onMounted(async () => {
  try {
    enabled.value = (await fetchAdminConfig()).enabled
  } catch (err) {
    console.warn('Failed to fetch admin config:', err)
  } finally {
    loadingConfig.value = false
  }
})

async function submit() {
  if (!rootToken.value) return
  loggingIn.value = true
  errorMsg.value = ''
  try {
    await admin.login(rootToken.value)
    router.push('/admin')
  } catch (err) {
    errorMsg.value = err?.status === 0 ? 'Connection error' : err?.message || 'Invalid token'
  } finally {
    loggingIn.value = false
  }
}
</script>
