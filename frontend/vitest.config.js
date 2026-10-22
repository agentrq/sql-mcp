import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { readFileSync } from 'node:fs'

const { version } = JSON.parse(readFileSync('./package.json', 'utf-8'))

export default defineConfig({
  // Not the app's vite.config.js: Tailwind has no place in a test run.
  plugins: [vue()],
  define: {
    __APP_VERSION__: JSON.stringify(version),
  },
  test: {
    environment: 'jsdom',
    include: ['test/**/*.test.js'],
    css: false,
    coverage: {
      provider: 'v8',
      include: ['src/api.js', 'src/app.js', 'src/composables/**/*.js', 'src/stores/**/*.js', 'src/utils/**/*.js'],
      reporter: ['text', 'lcov'],
      thresholds: {
        lines: 95,
        functions: 95,
        branches: 90,
        statements: 95,
      },
    },
  },
})
