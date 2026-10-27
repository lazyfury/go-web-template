// ESLint flat config（ESLint 10 / eslint-plugin-vue 10 / typescript-eslint 8）。
// 只做「正确性 + 明显坏味道」：格式交给编辑器，类型交给 vue-tsc。
import js from '@eslint/js'
import pluginVue from 'eslint-plugin-vue'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  {
    ignores: ['dist/**', 'node_modules/**', 'src/types/openapi.ts'],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...pluginVue.configs['flat/essential'],
  {
    // TypeScript 自己就会报未定义标识符（并知道 DOM 全局），eslint 的 no-undef
    // 在 TS 文件里只会误报 window/localStorage/HTMLDivElement。
    files: ['**/*.{ts,vue}'],
    rules: { 'no-undef': 'off' },
  },
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
      // 单文件组件按需命名（view 组件名与文件名一致即可），强制多词会逼出无意义后缀。
      'vue/multi-word-component-names': 'off',
      // 后端返回的类型由 openapi-typescript 生成，未使用参数用 _ 前缀表达。
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
    },
  },
)
