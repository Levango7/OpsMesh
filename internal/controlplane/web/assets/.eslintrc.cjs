module.exports = {
  env: { browser: true, es2021: true },
  parserOptions: { ecmaVersion: 2022, sourceType: 'module' },
  extends: ['eslint:recommended'],
  rules: {
    'no-unused-vars': 'warn',
    'no-undef': 'error',
    'prefer-const': 'error',
    'no-var': 'error',
    'eqeqeq': 'error',
  },
};