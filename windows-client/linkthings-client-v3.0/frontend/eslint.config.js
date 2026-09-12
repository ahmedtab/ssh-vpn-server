import tseslint from 'typescript-eslint'
import pluginVue from 'eslint-plugin-vue'

export default tseslint.config(
  {
    ignores: ['dist/**', 'bindings/**'],
  },
  ...tseslint.configs.recommended,
  // 'essential' (Vue's Priority A rules) catches real bugs (duplicate keys,
  // side effects in computed, etc.) without imposing the stricter tiers'
  // template formatting/attribute-ordering style rules on this codebase's
  // existing markup.
  ...pluginVue.configs['flat/essential'],
  {
    files: ['**/*.vue'],
    languageOptions: {
      parserOptions: {
        parser: tseslint.parser,
      },
    },
  },
  {
    rules: {
      // Wails bindings/composables lean on inferred CancellablePromise/event
      // types rather than spelling every return type out by hand.
      '@typescript-eslint/explicit-function-return-type': 'off',
      'vue/multi-word-component-names': ['error', { ignores: ['App'] }],
    },
  },
)
