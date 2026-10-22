<template>
  <div class="flex flex-col md:flex-row h-[100dvh] bg-white md:bg-zinc-100 dark:bg-zinc-950 font-inter overflow-hidden">

    <!-- Mobile menu toggle (bottom floating pill) -->
    <button v-if="!isBarePage"
            @click.stop="isMobileMenuOpen = true"
            class="md:hidden fixed bottom-3 left-1/2 -translate-x-1/2 px-4 py-1.5 bg-black/80 dark:bg-white/80 backdrop-blur-md text-white dark:text-black text-[10px] font-semibold rounded-full shadow-lg z-[60] border border-white/10 dark:border-black/10 transition-all active:scale-95 flex items-center justify-center gap-2">
      <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
        <path stroke-linecap="round" stroke-linejoin="round" d="M4 6h16M4 12h16m-7 6h7" />
      </svg>
      Menu
    </button>

    <div v-if="isMobileMenuOpen" @click="isMobileMenuOpen = false" class="md:hidden fixed inset-0 bg-black/70 backdrop-blur-sm z-[90]"></div>

    <!-- Sidebar -->
    <nav v-if="!isBarePage"
         :class="[
           isMobileMenuOpen ? 'flex' : 'hidden', 'md:flex fixed inset-y-0 left-0 z-[100] transform bg-white md:bg-zinc-100 dark:bg-zinc-950 md:relative md:translate-x-0',
           'text-gray-900 dark:text-zinc-100 shrink-0 flex-col h-full transition-all duration-300 ease-in-out',
           compact ? 'w-16' : 'w-64',
           isMobileMenuOpen ? 'w-[280px] shadow-2xl' : ''
         ]">
      <div :class="[compact ? 'px-2 py-4' : 'p-4']" class="flex flex-col min-h-0 grow">
        <!-- Sidebar header -->
        <div :class="['relative pb-1 flex transition-all duration-300', compact ? 'flex-col items-center gap-2' : 'flex-row items-center gap-1']">
          <div :class="['flex items-center p-1', compact ? 'justify-center w-full' : 'grow min-w-0']">
            <router-link :to="isAdmin ? '/admin' : '/'" class="flex items-center gap-2.5 min-w-0">
              <div class="w-8 h-8 flex items-center justify-center shrink-0">
                <AppLogo class="w-6 h-6 text-gray-700 dark:text-zinc-100" />
              </div>
              <span v-if="!compact" class="text-sm font-bold truncate text-gray-800 dark:text-zinc-200">SQL MCP<span v-if="isAdmin" class="ml-1.5 px-1.5 py-0.5 rounded text-[9px] font-black uppercase tracking-widest bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-400" data-test="admin-badge">Admin</span></span>
            </router-link>
          </div>
          <button @click="toggleCollapsed"
                  class="hidden md:inline-flex items-center justify-center text-gray-500 hover:text-gray-900 dark:hover:text-white size-8 transition-all duration-200 shrink-0 rounded-md hover:bg-gray-200 dark:hover:bg-zinc-800"
                  :title="isCollapsed ? 'Expand sidebar' : 'Collapse sidebar'">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
                 :class="['transition-transform duration-300', isCollapsed ? 'rotate-180' : '']">
              <path d="m15 18-6-6 6-6" />
            </svg>
          </button>
        </div>

        <div class="space-y-0.5 overflow-y-auto flex-1 min-h-0 px-2 no-scrollbar" :class="{ 'mt-6': compact }">
          <div v-if="!compact" class="p-2">
            <span class="text-[11px] font-medium text-gray-500 dark:text-zinc-400">{{ isAdmin ? 'Admin' : 'Overview' }}</span>
          </div>
          <router-link v-for="item in navItems" :key="item.to" :to="item.to"
              @mouseenter="showTooltip($event, item.label)" @mouseleave="hideTooltip"
              class="flex items-center gap-2.5 px-2 py-1.5 text-xs transition-all duration-150 rounded-md"
              :class="[
                compact ? 'justify-center' : '',
                item.active() ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
              ]">
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
              <path stroke-linecap="round" stroke-linejoin="round" :d="item.icon" />
            </svg>
            <span v-if="!compact">{{ item.label }}</span>
          </router-link>

          <template v-if="!isAdmin && databases.length > 0 && !compact">
            <div class="p-2 mt-2">
              <span class="text-[11px] font-medium text-gray-500 dark:text-zinc-400">Databases</span>
            </div>
            <SidebarDatabases :databases="databases" :show-tooltip="showTooltip" :hide-tooltip="hideTooltip" />
          </template>
        </div>

        <!-- Sidebar footer -->
        <div class="mt-auto" :class="compact ? 'px-2 pt-4' : 'px-4 pt-4'">
          <div v-if="!compact" class="px-2 mb-3 flex items-center justify-between gap-2">
            <span class="text-[10px] text-gray-400 dark:text-zinc-600 font-mono">v{{ appVersion }}</span>
            <a :href="docsUrl" target="_blank" rel="noopener noreferrer"
               class="flex items-center gap-1 text-[10px] text-gray-400 dark:text-zinc-600 hover:text-gray-700 dark:hover:text-zinc-300 transition-colors">
              <svg class="w-3.5 h-3.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" :d="DOCS_ICON" /></svg>
              Docs
            </a>
          </div>
          <div v-else class="mb-3 flex flex-col items-center gap-1">
            <a :href="docsUrl" target="_blank" rel="noopener noreferrer"
               @mouseenter="showTooltip($event, 'Docs')" @mouseleave="hideTooltip"
               class="flex items-center justify-center size-8 rounded-md text-gray-400 dark:text-zinc-600 hover:text-gray-900 dark:hover:text-white hover:bg-gray-200 dark:hover:bg-zinc-800 transition-all duration-150">
              <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" :d="DOCS_ICON" /></svg>
            </a>
          </div>

          <!-- User profile -->
          <div v-click-outside="closeUserMenu" class="relative pt-3 border-t border-gray-300/50 dark:border-zinc-600/50 mt-2 overflow-visible">
            <div v-if="isUserMenuOpen"
                 :class="[
                   'absolute bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-lg shadow-2xl p-2 z-[110]',
                   compact ? 'left-full bottom-0 ml-2 min-w-[220px]' : 'bottom-full left-0 right-0 mb-2'
                 ]">
              <div class="px-3 py-2 border-b border-gray-50 dark:border-zinc-800/50 mb-1">
                <p class="text-[10px] font-black text-gray-500 dark:text-zinc-500">Account</p>
                <p class="text-xs font-bold text-gray-700 dark:text-zinc-200 truncate mt-0.5" :title="user?.email">{{ user?.name || user?.email || 'Loading...' }}</p>
                <p v-if="user?.name && user?.email" class="text-[10px] font-medium text-gray-400 dark:text-zinc-500 truncate mt-0.5" :title="user.email">{{ user.email }}</p>
              </div>
              <div class="px-3 py-2 border-b border-gray-50 dark:border-zinc-800/50 mb-1">
                <p class="text-[10px] font-black text-gray-500 dark:text-zinc-500 mb-2">Theme Preference</p>
                <div class="flex items-center gap-1 bg-gray-50 dark:bg-zinc-800/50 p-1 rounded-md border border-gray-100 dark:border-zinc-800">
                  <button v-for="opt in THEME_OPTIONS" :key="opt.id" @click="themeStore.setTheme(opt.id)" :title="opt.label"
                          :data-test="`theme-${opt.id}`"
                          :class="['flex-1 flex justify-center py-1.5 rounded-md transition-all', themeStore.theme === opt.id ? 'bg-white dark:bg-zinc-700 shadow-sm border border-gray-200 dark:border-zinc-600 text-black dark:text-white' : 'text-gray-400 hover:text-gray-600 dark:hover:text-zinc-300']">
                    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" :d="opt.icon" /></svg>
                  </button>
                </div>
              </div>
              <button @click="logout" data-test="logout" class="w-full flex items-center gap-2.5 px-3 py-2 rounded-md text-xs font-bold text-rose-500 hover:bg-rose-50 dark:hover:bg-rose-500/10 transition-colors">
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" />
                </svg>
                Logout
              </button>
            </div>

            <button @click="isUserMenuOpen = !isUserMenuOpen"
                    class="flex items-center gap-3 w-full py-1.5 rounded-md transition-all duration-200 group outline-none"
                    :class="compact ? 'justify-center' : 'px-2 hover:bg-gray-200/60 dark:hover:bg-zinc-800'">
              <div class="rounded-full bg-white dark:bg-zinc-900 shadow-sm flex items-center justify-center text-gray-700 dark:text-zinc-200 font-bold overflow-hidden shrink-0"
                   :class="compact ? 'w-7 h-7 text-[11px]' : 'w-9 h-9 text-sm border border-gray-200 dark:border-zinc-700'">
                <img v-if="user?.picture" :src="user.picture" class="w-full h-full object-cover" alt="Profile" referrerpolicy="no-referrer" />
                <span v-else>{{ (user?.name || user?.email || '?').charAt(0).toUpperCase() }}</span>
              </div>
              <span v-if="!compact" class="text-sm font-semibold text-gray-700 dark:text-zinc-200 truncate min-w-0 flex-1 text-left">
                {{ user?.name || user?.email || 'User' }}
              </span>
            </button>
          </div>
        </div>
      </div>
    </nav>

    <!-- Login -->
    <main v-if="isBarePage" class="grow h-full bg-white dark:bg-zinc-950 flex flex-col overflow-hidden">
      <router-view class="grow flex flex-col" />
    </main>

    <!-- App content -->
    <main v-else class="grow min-w-0 p-0 md:py-4 md:pr-4 h-full min-h-0 flex flex-col relative bg-white md:bg-zinc-100 dark:bg-zinc-950">
      <div class="h-full overflow-y-auto min-w-0 md:rounded-xl page-surface md:border border-gray-200 dark:border-zinc-800 no-scrollbar">
        <div class="px-2 pt-4 pb-6 md:p-4 min-h-full flex flex-col">
          <router-view v-slot="{ Component, route: page }">
            <component :is="Component" :key="pageKey(page)" class="grow flex flex-col min-w-0" />
          </router-view>
        </div>
      </div>
    </main>

    <!-- Global tooltip -->
    <div v-if="tooltipStore.visible"
         class="fixed z-[100] px-3 py-1.5 text-xs font-semibold text-black dark:text-white bg-white dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 rounded-md shadow-lg pointer-events-none whitespace-pre"
         :style="tooltipStore.style">{{ tooltipStore.text }}</div>

    <Toast />
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { pageKey } from './app'
import AppLogo from './components/AppLogo.vue'
import SidebarDatabases from './components/SidebarDatabases.vue'
import Toast from './components/Toast.vue'
import { useAdminStore } from './stores/adminStore'
import { useAuthStore } from './stores/authStore'
import { useDatabaseStore } from './stores/databaseStore'
import { useThemeStore } from './stores/themeStore'
import { useTooltipStore } from './stores/tooltipStore'

const appVersion = __APP_VERSION__
const docsUrl = 'https://github.com/agentrq/sql-mcp#readme'
const DOCS_ICON = 'M12 6.042A8.967 8.967 0 006 3.75c-1.052 0-2.062.18-3 .512v14.25A8.987 8.987 0 016 18c2.305 0 4.408.867 6 2.292m0-14.25a8.966 8.966 0 016-2.292c1.052 0 2.062.18 3 .512v14.25A8.987 8.987 0 0018 18a8.967 8.967 0 00-6 2.292m0-14.25v14.25'

const THEME_OPTIONS = [
  { id: 'light', label: 'Light Mode', icon: 'M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z' },
  { id: 'dark', label: 'Dark Mode', icon: 'M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z' },
  { id: 'system', label: 'System Default', icon: 'M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z' },
]

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const admin = useAdminStore()
const databaseStore = useDatabaseStore()
const themeStore = useThemeStore()
const tooltipStore = useTooltipStore()

// The sign-in pages have no sidebar; the admin pages have their own.
const isBarePage = computed(() => route.path === '/login' || route.path === '/admin/login')
const isAdmin = computed(() => Boolean(route.meta?.admin))
const ADMIN_USER = { name: 'Administrator', email: 'Root token session' }
const user = computed(() => (isAdmin.value ? ADMIN_USER : auth.user))
const databases = computed(() => databaseStore.databases)

const SETUP_ICON = 'M10.5 6h9.75M10.5 6a1.5 1.5 0 11-3 0m3 0a1.5 1.5 0 10-3 0M3.75 6H7.5m3 12h9.75m-9.75 0a1.5 1.5 0 01-3 0m3 0a1.5 1.5 0 00-3 0m-3.75 0H7.5m9-6h3.75m-3.75 0a1.5 1.5 0 01-3 0m3 0a1.5 1.5 0 00-3 0m-9.75 0h9.75'

const adminNavItems = [
  { to: '/admin', label: 'Setup', icon: SETUP_ICON, active: () => route.path === '/admin' },
]

const userNavItems = [
  {
    to: '/',
    label: 'Databases',
    icon: 'M20.25 6.375c0 2.278-3.694 4.125-8.25 4.125S3.75 8.653 3.75 6.375m16.5 0c0-2.278-3.694-4.125-8.25-4.125S3.75 4.097 3.75 6.375m16.5 0v11.25c0 2.278-3.694 4.125-8.25 4.125s-8.25-1.847-8.25-4.125V6.375m16.5 5.625c0 2.278-3.694 4.125-8.25 4.125s-8.25-1.847-8.25-4.125',
    active: () => route.path === '/' || route.path === '/databases/new',
  },
  {
    to: '/settings',
    label: 'Settings',
    icon: 'M10.343 3.94c.09-.542.56-.94 1.11-.94h1.093c.55 0 1.02.398 1.11.94l.149.894c.07.424.384.764.78.93.398.164.855.142 1.205-.108l.737-.527a1.125 1.125 0 011.45.12l.773.774c.39.389.44 1.002.12 1.45l-.527.737c-.25.35-.272.806-.107 1.204.165.397.505.71.93.78l.893.15c.543.09.94.559.94 1.109v1.094c0 .55-.397 1.02-.94 1.11l-.894.149c-.424.07-.764.383-.929.78-.165.398-.143.854.107 1.204l.527.738c.32.447.269 1.06-.12 1.45l-.774.773a1.125 1.125 0 01-1.449.12l-.738-.527c-.35-.25-.806-.272-1.203-.107-.398.165-.71.505-.781.929l-.149.894c-.09.542-.56.94-1.11.94h-1.094c-.55 0-1.019-.398-1.11-.94l-.148-.894c-.071-.424-.384-.764-.781-.93-.398-.164-.854-.142-1.204.108l-.738.527c-.447.32-1.06.269-1.45-.12l-.773-.774a1.125 1.125 0 01-.12-1.45l.527-.737c.25-.35.272-.806.108-1.204-.165-.397-.506-.71-.93-.78l-.894-.15c-.542-.09-.94-.56-.94-1.109v-1.094c0-.55.398-1.02.94-1.11l.894-.149c.424-.07.765-.383.93-.78.165-.398.143-.854-.108-1.204l-.526-.738a1.125 1.125 0 01.12-1.45l.773-.773a1.125 1.125 0 011.45-.12l.737.527c.35.25.807.272 1.204.107.397-.165.71-.505.78-.929l.15-.894zM15 12a3 3 0 11-6 0 3 3 0 016 0z',
    active: () => route.path.startsWith('/settings'),
  },
]

const navItems = computed(() => (isAdmin.value ? adminNavItems : userNavItems))

// Collapsed is remembered per browser, like agentrq's sidebar.
const COLLAPSED_KEY = 'sqlmcp:sidebarCollapsed'
const isCollapsed = ref((() => {
  try { return localStorage.getItem(COLLAPSED_KEY) === 'true' } catch { return false }
})())
const isMobileMenuOpen = ref(false)
const isUserMenuOpen = ref(false)
/** Icon-only: collapsed, unless the phone menu is open (always full width). */
const compact = computed(() => isCollapsed.value && !isMobileMenuOpen.value)

function toggleCollapsed() {
  isCollapsed.value = !isCollapsed.value
  tooltipStore.hide()
  try { localStorage.setItem(COLLAPSED_KEY, String(isCollapsed.value)) } catch { /* not remembered */ }
}

const closeUserMenu = () => { isUserMenuOpen.value = false }

const showTooltip = (event, text) => {
  if (!isCollapsed.value || window.innerWidth < 1024) return
  tooltipStore.show(event, text)
}
const hideTooltip = () => tooltipStore.hide()

async function logout() {
  isUserMenuOpen.value = false
  if (isAdmin.value) {
    try {
      await admin.logout()
    } catch {
      // signed out locally regardless
    }
    router.push('/admin/login')
    return
  }
  try {
    await auth.logout()
  } catch {
    // signed out locally regardless
  }
  databaseStore.reset()
  router.push('/login')
}

// Close the phone menu and popovers on navigation; load the list once signed in.
watch(() => route.fullPath, () => {
  isMobileMenuOpen.value = false
  isUserMenuOpen.value = false
  tooltipStore.hide()
})

watch(() => [isBarePage.value || isAdmin.value, auth.user], ([login, u]) => {
  if (!login && u && !databaseStore.loaded && !databaseStore.loading) databaseStore.fetchDatabases()
}, { immediate: true })

onMounted(() => {
  themeStore.init()
})
</script>
