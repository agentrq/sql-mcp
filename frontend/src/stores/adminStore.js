import { defineStore } from 'pinia'
import { ref } from 'vue'
import { adminLogin, adminLogout, fetchAdminSession } from '../api'

/** The admin session: the root token's, separate from any person's. */
export const useAdminStore = defineStore('admin', () => {
  const session = ref(null)

  async function loadSession({ force = false } = {}) {
    if (session.value && !force) return session.value
    session.value = (await fetchAdminSession()) || null
    return session.value
  }

  async function login(rootToken) {
    const data = await adminLogin(rootToken)
    session.value = { expiresAt: data?.expiresAt }
    return session.value
  }

  async function logout() {
    try {
      await adminLogout()
    } finally {
      session.value = null
    }
  }

  return { session, loadSession, login, logout }
})
