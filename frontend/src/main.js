import { createWebHistory } from 'vue-router'

import './style.css'
import '@fontsource-variable/inter'

import { createSqlMcpApp } from './app'

// The backend injects the base path the app is served under.
const { app } = createSqlMcpApp({
  history: createWebHistory(window.__SQLMCP_BASE_PATH__ || '/'),
})

app.mount('#app')
