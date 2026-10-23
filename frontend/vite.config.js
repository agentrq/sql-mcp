import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { readFileSync } from 'node:fs'

const { version } = JSON.parse(readFileSync('./package.json', 'utf-8'))

// The backend the dev server proxies to.
const apiTarget = process.env.SQLMCP_DEV_API_TARGET || 'http://localhost:3000'

export default defineConfig({
  base: '',
  define: {
    __APP_VERSION__: JSON.stringify(version),
  },
  plugins: [vue(), tailwindcss()],
  build: {
    outDir: 'dist',
  },
  server: {
    port: 5173,
    proxy: {
      '/api': apiTarget,
      // Only the admin API: /admin and /admin/login are the web app's pages.
      '/admin/api': apiTarget,
      '/mcp': apiTarget,
      '/.well-known': apiTarget,
    },
  },
})
