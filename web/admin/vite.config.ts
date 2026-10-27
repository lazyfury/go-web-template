import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import UnoCSS from 'unocss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue(), UnoCSS()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  // 单元测试：纯逻辑（store/菜单/路由约束），不需要浏览器；SFC 走同一个 vue 插件。
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
    restoreMocks: true,
  },
  server: {
    port: 5173,
    proxy: {
      // 开发环境将 /api 代理到 Go 后端（见 go-web-template deploy/docker-compose.yml 的 APP_PORT）
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    // 三方库拆分为独立 chunk，避免单文件过大；echarts 已按需引入（见 DashboardView）
    chunkSizeWarningLimit: 1000,
    rollupOptions: {
      output: {
        manualChunks: {
          'vendor-vue': ['vue', 'vue-router', 'pinia'],
          'vendor-element': ['element-plus', '@element-plus/icons-vue'],
          'vendor-axios': ['axios'],
        },
      },
    },
  },
})
