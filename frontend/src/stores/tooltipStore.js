import { defineStore } from 'pinia'

export const useTooltipStore = defineStore('tooltip', {
  state: () => ({
    visible: false,
    text: '',
    style: { top: '0px', left: '0px' },
  }),
  actions: {
    show(event, text) {
      const rect = event.currentTarget.getBoundingClientRect()
      this.visible = true
      this.text = text
      this.style = {
        top: `${rect.top + rect.height / 2}px`,
        left: `${rect.right + 12}px`,
        transform: 'translateY(-50%)',
      }
    },
    hide() {
      this.visible = false
    },
  },
})
