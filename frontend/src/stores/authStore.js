import { defineStore } from 'pinia'
import { ref } from 'vue'
import { fetchUser, logout as apiLogout } from '../api'

/** Who is signed in. Looked up once, by the route guard, and cleared on logout. */
export const useAuthStore = defineStore('auth', () => {
  const user = ref(null)
  let pending = null

  async function loadUser({ force = false } = {}) {
    if (user.value && !force) return user.value
    if (!pending) {
      pending = fetchUser()
        .then((u) => {
          user.value = u || null
          return user.value
        })
        .finally(() => {
          pending = null
        })
    }
    return pending
  }

  async function logout() {
    try {
      await apiLogout()
    } finally {
      // Signed out locally even when the call fails: the next guard check asks again.
      user.value = null
    }
  }

  return { user, loadUser, logout }
})
