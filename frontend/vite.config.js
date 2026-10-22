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
      '/mcp': apiTarget,
      '/.well-known': apiTarget,
    },
  },
})
