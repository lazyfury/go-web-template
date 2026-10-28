import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import UnoCSS from 'unocss/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import AutoImport from 'unplugin-auto-import/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    vue(),
    UnoCSS(),
    // 模板里的 <el-*> 按需引入（含 v-loading 指令），不再 app.use(ElementPlus) 全量注册：
    // Element Plus 的 JS 体积随使用面收缩，未使用的组件不进 bundle。
    // 样式仍走全量 element-plus/dist/index.css（main.ts）：服务式组件（ElMessage /
    // ElMessageBox）不经过模板解析，按需样式容易漏，模板优先选可预测。
    Components({
      resolvers: [ElementPlusResolver({ importStyle: false })],
      directives: true,
      dts: 'src/components.d.ts',
    }),
    // 服务式组件（ElMessage / ElMessageBox）在模板之外调用，组件解析器管不到：
    // auto-import 把它们从 element-plus/es 的子路径引入，避免整个 barrel 进 bundle。
    AutoImport({
      resolvers: [ElementPlusResolver({ importStyle: false })],
      dts: 'src/auto-imports.d.ts',
    }),
  ],
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
    // 三方库拆分为独立 chunk，避免单文件过大；echarts 已按需引入（见 DashboardView）。
    // 注意：manualChunks 写成对象（{ 'vendor-element': ['element-plus'] }）会把整个包
    // 及其全部模块拉进 chunk，按需引入白做（实测 943KB vs 按模块 388KB）；用函数只
    // 归类「实际进入模块图」的模块。
    chunkSizeWarningLimit: 1000,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) return undefined
          // 不要按包名把 element-plus 整包归堆：实测会把未使用的模块一起塞进
          // chunk（943KB vs 按需 ~490KB）。只给稳定大依赖分组。
          if (id.includes('/echarts/') || id.includes('/zrender/')) return 'vendor-echarts'
          if (id.includes('/axios/')) return 'vendor-axios'
          if (id.includes('/@vue/') || id.includes('/vue/') || id.includes('/vue-router/') || id.includes('/pinia/')) {
            return 'vendor-vue'
          }
          return undefined
        },
      },
    },
  },
})
