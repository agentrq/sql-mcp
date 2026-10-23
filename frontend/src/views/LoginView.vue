<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center p-4 bg-gray-50/80 dark:bg-zinc-950/80 backdrop-blur-sm">
    <div class="w-full max-w-md p-8 bg-white dark:bg-zinc-900 rounded-3xl shadow-2xl border border-gray-100 dark:border-zinc-800">
      <div class="mb-10 text-center">
        <AppLogo tile />
        <h2 class="text-3xl font-black text-gray-900 dark:text-zinc-50 tracking-tight mb-2">SQL MCP</h2>
        <p class="text-gray-500 dark:text-zinc-400 text-sm leading-relaxed">
          Your databases as MCP servers, with PII kept out
        </p>
      </div>

      <div class="space-y-4">
        <div v-if="callbackError" data-test="login-error" role="alert"
             class="p-4 bg-red-50 dark:bg-red-900/20 border border-red-100 dark:border-red-900/30 rounded-2xl">
          <p class="text-sm text-red-600 dark:text-red-400 font-medium text-center">{{ callbackError }}</p>
        </div>

        <div v-if="loadingConfig" class="flex justify-center py-6">
          <svg class="animate-spin h-6 w-6 text-gray-500" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
          </svg>
        </div>

        <template v-else>
          <a v-if="config.googleLoginEnabled" data-test="google-login" :href="googleLoginUrl(redirect)"
            class="w-full py-4 px-6 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-200 font-bold rounded-2xl border border-gray-200 dark:border-zinc-700 hover:bg-gray-50 dark:hover:bg-zinc-700 flex items-center justify-center gap-3 transform active:scale-[0.98] transition-all shadow-sm">
            <svg width="20" height="20" viewBox="0 0 24 24" aria-hidden="true">
              <path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z" fill="#4285F4"/>
              <path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853"/>
              <path d="M5.84 14.1c-.22-.66-.35-1.36-.35-2.1s.13-1.44.35-2.1V7.06H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.94l3.66-2.84z" fill="#FBBC05"/>
              <path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.06l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335"/>
            </svg>
            Continue with Google
          </a>

          <div v-if="config.googleLoginEnabled && config.rootLoginEnabled" class="relative py-4">
            <div class="absolute inset-0 flex items-center">
              <div class="w-full border-t border-gray-100 dark:border-zinc-800"></div>
            </div>
            <div class="relative flex justify-center text-xs text-gray-500">
              <span class="bg-white dark:bg-zinc-900 px-2 font-bold">or use token</span>
            </div>
          </div>

          <form v-if="config.rootLoginEnabled" data-test="root-login" @submit.prevent="submitRootLogin" class="space-y-4">
            <input
              v-model="rootToken"
              type="password"
              autocomplete="current-password"
              aria-label="Root token"
              placeholder="Paste root token..."
              class="w-full px-4 py-4 bg-gray-50 dark:bg-zinc-800/50 text-gray-900 dark:text-zinc-50 border border-gray-100 dark:border-zinc-700/50 rounded-2xl outline-none focus:ring-4 focus:ring-black/5 dark:focus:ring-white/10 focus:border-black dark:focus:border-white transition-all text-center placeholder:text-gray-500"
              required
            />
            <div v-if="errorMsg" data-test="root-error" class="p-4 bg-red-50 dark:bg-red-900/20 border border-red-100 dark:border-red-900/30 rounded-2xl">
              <p class="text-sm text-red-600 dark:text-red-400 font-medium text-center">{{ errorMsg }}</p>
            </div>
            <button type="submit" :disabled="loggingIn"
              class="w-full py-4 bg-gray-900 dark:bg-zinc-800 text-white dark:text-zinc-200 font-bold rounded-2xl hover:bg-black dark:hover:bg-zinc-700 transform active:scale-[0.98] transition-all shadow-sm disabled:opacity-50 disabled:cursor-not-allowed">
              {{ loggingIn ? 'Verifying...' : 'Sign in' }}
            </button>
          </form>

          <p v-if="!config.googleLoginEnabled && !config.rootLoginEnabled" data-test="no-login"
             class="text-sm text-center text-gray-500 dark:text-zinc-400">
            No sign-in method is enabled on this server.
          </p>
        </template>

        <div class="mt-8 flex items-center justify-between pt-6 border-t border-gray-100 dark:border-zinc-800 text-[11px] font-bold text-gray-500 dark:text-zinc-500">
          <span>Only allow-listed emails can sign in</span>
          <span class="opacity-40">SQL MCP</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLogo from '../components/AppLogo.vue'
import { fetchAuthConfig, googleLoginUrl, rootLogin } from '../api'
import { useAuthStore } from '../stores/authStore'
import { loginRedirect } from '../utils/loginRedirect'

const ERROR_MESSAGES = {
  email_not_allowed: 'This email is not on the allow list',
}

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

// Set when the route guard or an MCP client's authorize step sent the person here.
const redirect = computed(() => loginRedirect(route.query.redirect_url, window.location.origin))

const callbackError = computed(() => {
  const code = route.query.error
  if (!code || typeof code !== 'string') return ''
  return ERROR_MESSAGES[code] || `Sign-in failed (${code.replace(/_/g, ' ')})`
})

const config = reactive({ googleLoginEnabled: false, rootLoginEnabled: false })
const loadingConfig = ref(true)
const rootToken = ref('')
const loggingIn = ref(false)
const errorMsg = ref('')

onMounted(async () => {
  try {
    Object.assign(config, await fetchAuthConfig())
  } catch (err) {
    console.warn('Failed to fetch auth config:', err)
  } finally {
    loadingConfig.value = false
  }
})

async function submitRootLogin() {
  if (!rootToken.value) return
  loggingIn.value = true
  errorMsg.value = ''
  try {
    await rootLogin(rootToken.value)
    if (redirect.value) {
      window.location.assign(redirect.value)
      return
    }
    await auth.loadUser({ force: true })
    router.push('/')
  } catch (err) {
    errorMsg.value = err?.status === 0 ? 'Connection error' : err?.message || 'Invalid token'
  } finally {
    loggingIn.value = false
  }
}
</script>
