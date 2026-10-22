<script setup>
import { useToasts } from '../composables/useToasts'

const { toasts, removeToast } = useToasts()

const DOTS = {
  error: { tone: 'alert', glyph: '!' },
  success: { tone: 'done', glyph: '✓' },
  info: { tone: 'info', glyph: 'i' },
}
const dotFor = (toast) => DOTS[toast.type] ?? DOTS.info
</script>

<template>
  <div class="toast-container">
    <TransitionGroup name="toast">
      <div v-for="toast in toasts" :key="toast.id" class="toast" :class="toast.type" role="status">
        <div class="toast-row">
          <span class="toast-dot" :class="dotFor(toast).tone" aria-hidden="true">{{ dotFor(toast).glyph }}</span>
          <div class="toast-content">
            <div v-if="toast.title" class="toast-title">{{ toast.title }}</div>
            <div class="toast-message" :class="{ solo: !toast.title }" :title="toast.message">{{ toast.message }}</div>
          </div>
        </div>
        <button @click.stop="removeToast(toast.id)" class="toast-close" aria-label="Dismiss">
          <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8" /></svg>
        </button>
        <div v-if="!toast.persistent" class="toast-progress" :style="{ animationDuration: `${toast.duration}ms` }"></div>
      </div>
    </TransitionGroup>
  </div>
</template>

<style scoped>
.toast-container { position: fixed; bottom: 24px; right: 24px; z-index: 9999; display: flex; flex-direction: column-reverse; gap: 12px; pointer-events: none; }
@media (max-width: 640px) { .toast-container { bottom: 12px; left: 12px; right: 12px; align-items: stretch; } }
.toast { pointer-events: auto; position: relative; overflow: hidden; width: 380px; padding: 12px 14px 14px; background: #fff; color: #131314; border: 1px solid #e8e6dc; border-radius: 8px; box-shadow: 4px 4px 0 rgba(232, 230, 220, 0.7), 0 10px 28px -12px rgba(19, 19, 20, 0.18); }
@media (max-width: 640px) { .toast { width: 100%; } }
.dark .toast { background: #1f1f1e; color: #f4f4f5; border-color: #3a3a36; box-shadow: 4px 4px 0 rgba(0, 0, 0, 0.35), 0 10px 28px -12px rgba(0, 0, 0, 0.7); }
.toast-row { display: flex; align-items: flex-start; gap: 10px; }
.toast-content { flex: 1; min-width: 0; padding-right: 22px; }
.toast-title { font-size: 14px; font-weight: 700; line-height: 20px; }
.toast-dot { flex: none; width: 18px; height: 18px; margin-top: 1px; border-radius: 9999px; border: 1.5px solid #c2c8c0; display: flex; align-items: center; justify-content: center; font-size: 10px; font-weight: 700; line-height: 1; color: #737971; }
.toast-dot.alert { border-color: #fca5a5; color: #dc2626; }
.toast-dot.done { border-color: #86efac; color: #16a34a; }
.toast-dot.info { font-family: Georgia, serif; font-style: italic; }
.dark .toast-dot { border-color: #52524e; color: #a1a1aa; }
.dark .toast-dot.alert { border-color: #7f1d1d; color: #f87171; }
.dark .toast-dot.done { border-color: #166534; color: #4ade80; }
.toast-message { margin-top: 2px; font-size: 13px; line-height: 19px; color: #424842; overflow-wrap: anywhere; display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 4; line-clamp: 4; overflow: hidden; }
.dark .toast-message { color: #a1a1aa; }
.toast-message.solo { margin-top: 0; font-size: 14px; line-height: 20px; color: inherit; }
.toast-close { position: absolute; top: 10px; right: 10px; background: none; border: none; color: #737971; cursor: pointer; padding: 2px; border-radius: 4px; }
.toast-close svg { display: block; width: 16px; height: 16px; fill: none; stroke: currentColor; stroke-width: 1.5; stroke-linecap: round; }
.toast-close:hover { color: #131314; background: #f6f4ec; }
.dark .toast-close { color: #a1a1aa; }
.dark .toast-close:hover { color: #fafafa; background: #3a3a36; }
.toast-progress { position: absolute; bottom: 0; left: 0; height: 2px; width: 100%; background: #3d5a42; opacity: 0.35; animation: progress linear forwards; }
.toast.error .toast-progress { background: #dc2626; }
.dark .toast-progress { background: #9cc3a2; }
@keyframes progress { from { width: 100%; } to { width: 0%; } }
.toast-enter-active { transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1); }
.toast-leave-active { transition: all 0.2s ease; }
.toast-enter-from { opacity: 0; transform: translateY(12px); }
.toast-leave-to { opacity: 0; transform: translateX(24px); }
.toast-move { transition: transform 0.3s ease; }
</style>
