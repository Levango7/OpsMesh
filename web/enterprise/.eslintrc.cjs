/* eslint-env node */
module.exports = {
  root: true,
  env: {
    browser: true,
    es2022: true,
    node: true
  },
  extends: [
    'eslint:recommended',
    'plugin:vue/vue3-recommended'
  ],
  parserOptions: {
    ecmaVersion: 'latest',
    sourceType: 'module'
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
  },
  ignorePatterns: ['dist/', 'node_modules/']
}