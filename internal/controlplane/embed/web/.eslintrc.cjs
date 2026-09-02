module.exports = {
  root: true,
  env: {
    browser: true,
    es2022: true,
  },
  parserOptions: {
    ecmaVersion: 2022,
    sourceType: 'module',
  },
  rules: {
    // 基本规则
    'no-unused-vars': ['warn', { argsIgnorePattern: '^_' }],
    'no-console': 'off', // 原生前端允许 console
    'no-undef': 'error',
    'no-redeclare': 'error',
    'prefer-const': 'warn',
    'no-empty': ['warn', { allowEmptyCatch: true }],
    'no-dupe-keys': 'error',
    'no-sparse-arrays': 'warn',
    'no-unreachable': 'error',
    'no-dupe-args': 'error',
    'no-irregular-whitespace': 'error',
    // ES Module 规则
    'no-multiple-empty-lines': ['warn', { max: 2 }],
    'no-trailing-spaces': 'warn',
    'eol-last': 'warn',
    // 放宽的规则（原生 JS 不需要太严格）
    'indent': 'off',
    'quotes': 'off',
    'semi': 'off',
    'comma-dangle': 'off',
    'arrow-spacing': 'off',
    'space-before-function-paren': 'off',
  },
};