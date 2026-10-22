import { ref } from 'vue'

const toasts = ref([])

const DURATION = 4000
const ERROR_DURATION = 10000
const MAX_VISIBLE = 4

export function useToasts() {
  const removeToast = (id) => {
    toasts.value = toasts.value.filter((t) => t.id !== id)
  }

  const addToast = (message, type = 'info', title = null, duration = null) => {
    if (duration === null) duration = type === 'error' ? ERROR_DURATION : DURATION
    const id = Date.now() + Math.random()
    toasts.value = [...toasts.value, { id, message, type, title, duration, persistent: duration <= 0 }].slice(-MAX_VISIBLE)
    if (duration > 0) setTimeout(() => removeToast(id), duration)
    return id
  }

  const notifyError = (message, title = null) => addToast(message, 'error', title)
  const notifySuccess = (message, title = null) => addToast(message, 'success', title)
  const notifyInfo = (message, title = null) => addToast(message, 'info', title)

  return { toasts, addToast, removeToast, notifyError, notifySuccess, notifyInfo }
}
