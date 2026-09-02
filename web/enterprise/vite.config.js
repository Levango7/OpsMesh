import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'
import viteCompression from 'vite-plugin-compression'

// Vite 配置：企业版前端独立构建产物，由控制面以 /enterprise/ 前缀分发
export default defineConfig({
  plugins: [
    vue(),
    viteCompression({
      algorithm: 'brotliCompress',
      ext: '.br',
      threshold: 1024
    }),
    viteCompression({
      algorithm: 'gzip',
      ext: '.gz',
      threshold: 1024
    })
  ],
  base: '/enterprise/',
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    }
  },
  server: {
    port: 5174,
    proxy: {
      // 后端 REST API 代理到本地控制面（可用 VITE_API_PROXY_TARGET 覆盖，方便联调不同后端地址）
      '/api': {
        target: process.env.VITE_API_PROXY_TARGET || 'http://localhost:8080',
        changeOrigin: true
      }
    }
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    target: 'esnext',
    minify: 'terser',
    terserOptions: {
      compress: {
        drop_console: true,
        drop_debugger: true,
        pure_funcs: ['console.log', 'console.info', 'console.debug']
      },
      format: {
        comments: false
      }
    },
    cssCodeSplit: true,
    chunkSizeWarningLimit: 500,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules')) {
            if (id.includes('vue') || id.includes('pinia') || id.includes('vue-router')) {
              return 'vendor'
            }
            if (id.includes('axios')) {
              return 'vendor-http'
            }
          }
          // i18n chunk：只包含 i18n 核心模块（index.js）和 common.json（静态 import）
          // 动态 import 的域 json（通过 import.meta.glob）由 Vite 自动拆分为单独 chunk，按需加载
          if (id.includes('/src/i18n/index.js')) {
            return 'i18n'
          }
          if (id.includes('/src/i18n/locales/') && id.includes('common.json')) {
            return 'i18n'
          }
          if (id.includes('/src/components/') && !id.includes('/src/views/')) {
            // 异步加载的组件（defineAsyncComponent 动态 import）单独 chunk，不打入 components
            if (
              id.includes('RelationGraph.vue') ||
              id.includes('ProgressRing.vue') ||
              id.includes('PromptModal.vue')
            ) {
              return // undefined：让 Vite 自动拆分为单独 chunk，按需加载
            }
            return 'components'
          }
        },
        chunkFileNames: 'assets/js/[name]-[hash].js',
        entryFileNames: 'assets/js/[name]-[hash].js',
        assetFileNames: (assetInfo) => {
          const name = assetInfo.name || ''
          if (/\.(png|jpe?g|gif|svg|webp|avif)$/i.test(name)) {
            return 'assets/images/[name]-[hash][extname]'
          }
          if (/\.(woff2?|eot|ttf|otf)$/i.test(name)) {
            return 'assets/fonts/[name]-[hash][extname]'
          }
          if (/\.css$/i.test(name)) {
            return 'assets/css/[name]-[hash][extname]'
          }
          return 'assets/[name]-[hash][extname]'
        }
      }
    }
  },
  preview: {
    port: 4174,
    proxy: {
      '/api': {
        target: process.env.VITE_API_PROXY_TARGET || 'http://localhost:8080',
        changeOrigin: true
      }
    }
  }
})