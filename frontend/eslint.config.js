import js from '@eslint/js'
import pluginVue from 'eslint-plugin-vue'
import globals from 'globals'

export default [
  { ignores: ['dist/', 'coverage/', 'node_modules/'] },
  js.configs.recommended,
  // essential: the correctness rules only. The stylistic tiers would reflow
  // every template for no change in behavior.
  ...pluginVue.configs['flat/essential'],
  {
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: { ...globals.browser, __APP_VERSION__: 'readonly' },
    },
    // Destructuring a key away to keep the rest is how a query param is dropped.
    rules: { 'no-unused-vars': ['error', { ignoreRestSiblings: true }] },
  },
  {
    files: ['test/**/*.js', '*.config.js'],
    languageOptions: { globals: { ...globals.node } },
  },
]
