import { expect, test } from '@playwright/test'

test('admin signs in, creates a project with links, favorites it, and uses the mobile layout', async ({ page }) => {
  let authenticated = false
  const projects: Array<Record<string, unknown>> = []
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (path.endsWith('/session')) {
      await route.fulfill({ status: authenticated ? 200 : 401, json: authenticated ? { user: { id: 'u-1', username: 'admin', role: 'admin', disabled: false, createdAt: 1 }, csrfToken: 'csrf-e2e' } : { error: 'authentication required' } })
      return
    }
    if (path.endsWith('/login') && request.method() === 'POST') {
      authenticated = true
      await route.fulfill({ json: { user: { id: 'u-1', username: 'admin', role: 'admin', disabled: false, createdAt: 1 }, csrfToken: 'csrf-e2e' } })
      return
    }
    if (path.endsWith('/categories')) {
      await route.fulfill({ json: { items: [], count: 0 } })
      return
    }
    if (path.endsWith('/projects') && request.method() === 'POST') {
      const payload = request.postDataJSON()
      expect(request.headers()['x-csrf-token']).toBe('csrf-e2e')
      projects.push({ ...payload, id: 'p-1', favorite: false, created_at: 1, updated_at: 1, links: payload.links.map((link: object, i: number) => ({ ...link, id: `l-${i}`, position: i, created_at: 1 })) })
      await route.fulfill({ status: 201, json: projects[0] })
      return
    }
    if (path.endsWith('/projects/p-1') && request.method() === 'PUT') {
      const payload = request.postDataJSON()
      projects[0] = { ...projects[0], ...payload, links: payload.links.map((link: object, i: number) => ({ ...link, id: `l-${i}`, position: i, created_at: 1 })) }
      await route.fulfill({ json: projects[0] })
      return
    }
    if (path.endsWith('/projects/p-1') && request.method() === 'DELETE') {
      projects.splice(0)
      await route.fulfill({ status: 204, body: '' })
      return
    }
    if (path.endsWith('/favorite') && request.method() === 'PUT') {
      expect(request.headers()['x-csrf-token']).toBe('csrf-e2e')
      const project = projects[0]
      project.favorite = request.postDataJSON().favorite
      await route.fulfill({ status: 204, body: '' })
      return
    }
    if (path.endsWith('/projects')) {
      await route.fulfill({ json: { items: projects, count: projects.length } })
      return
    }
    await route.fulfill({ status: 404, body: 'not found' })
  })

  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/')
  await page.getByLabel('用户名').fill('admin')
  await page.getByLabel('密码').fill('correct horse battery staple')
  await page.getByRole('button', { name: '登录 Project Hub' }).click()
  await page.getByRole('button', { name: '新建项目' }).click()
  await page.getByLabel('项目名称').fill('Compiler')
  await page.getByLabel('项目简介').fill('Build service')
  await page.getByRole('button', { name: '＋ 添加入口' }).click()
  await page.getByLabel('名称', { exact: true }).fill('Repository')
  await page.getByLabel('地址', { exact: true }).fill('https://example.com/compiler')
  await page.getByRole('button', { name: '保存项目' }).click()
  await expect(page.getByRole('button', { name: 'Compiler' })).toBeVisible()
  await page.getByRole('button', { name: '收藏项目' }).click()
  await expect(page.getByRole('button', { name: '取消收藏' })).toBeVisible()
  await page.getByRole('button', { name: 'Compiler' }).click()
  await page.getByRole('button', { name: '编辑' }).click()
  await page.getByLabel('项目简介').fill('Build and release service')
  await page.getByRole('button', { name: '保存项目' }).click()
  await expect(page.getByText('Build and release service')).toBeVisible()
  await page.getByRole('button', { name: 'Compiler' }).click()
  page.once('dialog', (dialog) => dialog.accept())
  await page.getByRole('button', { name: '删除' }).click()
  await expect(page.getByRole('heading', { name: '还没有项目' })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
})

test('shows the API permission denial instead of pretending a project save succeeded', async ({ page }) => {
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (path.endsWith('/session')) {
      await route.fulfill({ json: { user: { id: 'u-1', username: 'admin', role: 'admin', disabled: false, createdAt: 1 }, csrfToken: 'csrf-e2e' } })
    } else if (path.endsWith('/categories')) {
      await route.fulfill({ json: { items: [], count: 0 } })
    } else if (path.endsWith('/projects') && request.method() === 'GET') {
      await route.fulfill({ json: { items: [], count: 0 } })
    } else if (path.endsWith('/projects') && request.method() === 'POST') {
      await route.fulfill({ status: 403, body: 'forbidden' })
    } else {
      await route.fulfill({ status: 404, body: 'not found' })
    }
  })
  await page.goto('/')
  await page.getByRole('button', { name: '新建项目' }).click()
  await page.getByLabel('项目名称').fill('Restricted project')
  await page.getByRole('button', { name: '保存项目' }).click()
  await expect(page.getByRole('alert')).toContainText('服务器拒绝了此操作')
  await expect(page.getByText('Restricted project')).toHaveCount(0)
})
