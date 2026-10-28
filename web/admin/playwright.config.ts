import { defineConfig, devices } from '@playwright/test'

// E2E 冒烟：真实浏览器 + 真实后端 + 真实 PG。默认只跑 login → CRUD → logout 一条主链路，
// 目的是钉住「前端按需引入 / 路由守卫 / 401 语义」这类单测覆盖不到的集成行为。
//
// 前置（缺一不可）：
//   make db-up && make migrate-up
//   EMAIL=admin@example.com APP_SEED_PASSWORD=admin-password make seed-admin
//   make dev                       # 后端 :8080
//   pnpm exec playwright install chromium
// 跑法：pnpm test:e2e（Playwright 自己拉起 vite dev :5173，复用已存在的实例）
export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  fullyParallel: false,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL: process.env.E2E_BASE_URL ?? 'http://127.0.0.1:5173',
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: process.env.E2E_BASE_URL
    ? undefined
    : {
        command: 'pnpm dev --host 127.0.0.1 --port 5173',
        url: 'http://127.0.0.1:5173',
        reuseExistingServer: true,
        timeout: 60_000,
      },
})
