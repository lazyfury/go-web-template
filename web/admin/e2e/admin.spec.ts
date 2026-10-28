import { expect, test } from '@playwright/test'

// 一条主链路：登录 → 新建用户 → 删除用户 → 退出登录。
// 只依赖管理员账号（E2E_EMAIL / E2E_PASSWORD）与后端的真实 API，不做数据 mock：
// 这正是「按需引入后组件还能渲染、401 语义还对」的集成证据。

const adminEmail = process.env.E2E_EMAIL ?? 'admin@example.com'
const adminPassword = process.env.E2E_PASSWORD ?? 'admin-password'

test('login → create user → delete user → logout', async ({ page }) => {
  const email = `e2e-${Date.now()}@example.com`
  const name = `E2E ${Date.now()}`

  // 1. 登录（账号来自 make seed-admin）
  await page.goto('/login')
  await page.getByPlaceholder('admin@example.com').fill(adminEmail)
  await page.getByPlaceholder('请输入密码').fill(adminPassword)
  await page.getByRole('button', { name: /登\s*录/ }).click()
  await expect(page).toHaveURL(/\/dashboard/, { timeout: 15_000 })
  await expect(page.getByText('用户管理').first()).toBeVisible()

  // 2. 新建用户
  await page.goto('/users')
  await page.getByRole('button', { name: /新建用户/ }).click()
  const dialog = page.locator('.el-dialog').filter({ hasText: '新建用户' })
  await dialog.getByPlaceholder('2-64 个字符').fill(name)
  await dialog.getByPlaceholder('user@example.com').fill(email)
  await dialog.getByPlaceholder('8-72 位，交付后请让对方自行修改').fill('e2e-password-123')
  await dialog.getByRole('button', { name: '保存' }).click()
  await expect(page.getByRole('cell', { name: email })).toBeVisible({ timeout: 15_000 })

  // 3. 删除刚建的用户（ElMessageBox 确认）
  const row = page.getByRole('row').filter({ hasText: email })
  await row.getByRole('button', { name: '删除' }).click()
  await page.locator('.el-message-box').getByRole('button', { name: '删除' }).click()
  await expect(page.getByRole('cell', { name: email })).toHaveCount(0, { timeout: 15_000 })

  // 4. 退出登录：先等后端吊销 + 清本地态，再落到登录页（历史 bug：只 fire-and-forget）
  await page.getByRole('banner').locator('.el-dropdown').click()
  await page.getByText('退出登录').click()
  await expect(page).toHaveURL(/\/login/, { timeout: 15_000 })
  // 退出后受保护页面不能再进入（守卫会弹回登录页）
  await page.goto('/users')
  await expect(page).toHaveURL(/\/login/)
})
