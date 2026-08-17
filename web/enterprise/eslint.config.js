// ESLint 9 flat config — 替代 .eslintrc.cjs
// 详见 https://eslint.org/docs/latest/use/configure/configuration-files
import js from '@eslint/js'
import pluginVue from 'eslint-plugin-vue'
import globals from 'globals'

export default [
  // 全局忽略
  { ignores: ['dist/**', 'node_modules/**'] },

  // JS 基础规则
  js.configs.recommended,

  // Vue 3 推荐规则（含 .vue 文件处理器）
  ...pluginVue.configs['flat/recommended'],

  // 项目通用配置
  {
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: {
        ...globals.browser,
        ...globals.es2022,
        ...globals.node
      }
    },
    rules: {
      'vue/multi-word-component-names': 'off',
      'vue/no-v-html': 'off',
      'no-unused-vars': ['warn', { argsIgnorePattern: '^_' }],
      'vue/attributes-order': 'off',
      'vue/order-in-components': 'off',
      'vue/require-default-prop': 'off',
      // task 97：以下 5 条为 vue3-recommended 的纯排版类格式规则，与项目既有紧凑模板风格冲突
      // （attributes-order 已关闭表明团队偏好）。关闭后避免 1400+ 行格式化 churn。
      'vue/max-attributes-per-line': 'off',
      'vue/singleline-html-element-content-newline': 'off',
      'vue/multiline-html-element-content-newline': 'off',
      'vue/html-self-closing': 'off',
      'vue/html-quotes': 'off'
    }
  }
]
