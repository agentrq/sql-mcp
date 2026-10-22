<script setup>
defineProps({
  show: Boolean,
  title: { type: String, default: 'Delete' },
  itemName: { type: String, default: '' },
  message: { type: String, default: '' },
  confirmLabel: { type: String, default: 'Delete' },
  busy: Boolean,
})

const emit = defineEmits(['close', 'confirm'])
</script>

<template>
  <Teleport to="body">
    <Transition name="fade">
      <div v-if="show" class="fixed inset-0 z-[100] overflow-y-auto" aria-labelledby="modal-title" role="dialog" aria-modal="true">
        <div class="flex items-center justify-center min-h-screen p-4">
          <div class="fixed inset-0 bg-gray-900/60 backdrop-blur-sm" aria-hidden="true" @click="emit('close')"></div>
          <div class="relative z-[110] w-full max-w-md bg-white dark:bg-zinc-900 rounded-xl text-left overflow-hidden shadow-2xl border border-gray-100 dark:border-zinc-800">
            <div class="px-6 pt-7 pb-6 sm:p-8 sm:pb-7 sm:flex sm:items-start">
              <div class="shrink-0 flex items-center justify-center h-10 w-10 rounded-lg bg-red-50 dark:bg-red-500/10 border border-red-100 dark:border-red-500/20 mb-4 sm:mb-0">
                <svg class="h-5 w-5 text-red-600 dark:text-red-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                </svg>
              </div>
              <div class="sm:ml-4">
                <h3 id="modal-title" class="text-xl leading-8 font-bold text-gray-900 dark:text-zinc-50 tracking-tight">{{ title }}</h3>
                <p v-if="message" class="mt-2 text-[14px] leading-relaxed text-gray-500 dark:text-zinc-400 font-medium">{{ message }}</p>
                <p v-else class="mt-2 text-[14px] leading-relaxed text-gray-500 dark:text-zinc-400 font-medium">
                  Are you sure you want to delete <span class="text-black dark:text-white font-semibold">{{ itemName }}</span>? This action cannot be undone.
                </p>
              </div>
            </div>
            <div class="bg-gray-50/50 dark:bg-zinc-800/50 px-6 py-5 sm:px-8 flex flex-col-reverse sm:flex-row sm:justify-end gap-3 border-t border-gray-100 dark:border-zinc-800">
              <button type="button" @click="emit('close')"
                class="inline-flex justify-center rounded-lg border border-gray-200 dark:border-zinc-700 px-5 py-2.5 bg-white dark:bg-zinc-900 text-[11px] font-black uppercase tracking-widest text-gray-700 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800 transition-colors">
                Cancel
              </button>
              <button type="button" data-test="confirm-delete" :disabled="busy" @click="emit('confirm')"
                class="inline-flex justify-center rounded-lg px-5 py-2.5 bg-red-600 text-[11px] font-black uppercase tracking-widest text-white hover:bg-red-700 transition-colors disabled:opacity-50">
                {{ busy ? 'Working…' : confirmLabel }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.fade-enter-active, .fade-leave-active { transition: opacity 0.2s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
