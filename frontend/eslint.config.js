import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  {
    ignores: [
      'node_modules/',
      'dist/',
      '*.vue.bak',
      'src/**/*.vue',
      'src/management/**',
      'src/portal/**',
      'src/views/**',
      'src/router/index.ts',
      'src/main.ts',
      'src/App.vue',
      'src/services/api.ts',
      'src/services/websocket.ts',
      'src/stores/costStore.ts',
      'src/stores/gpuStore.ts',
      'src/stores/taskStore.ts',
      'src/stores/userStore.ts',
      'tests/**',
    ],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
      globals: {
        ...globals.browser,
        ...globals.node,
      },
    },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      // shadcn/ui 组件需同时导出变体常量，关闭此规则
      'react-refresh/only-export-components': 'off',
      '@typescript-eslint/no-explicit-any': 'warn',
      '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_' }],
      '@typescript-eslint/no-empty-object-type': 'off',
    },
  }
)
