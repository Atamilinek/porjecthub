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

test('opens persisted health monitoring and creates a protected check', async ({ page }) => {
  let created = false
  const project = { id: 'p-1', name: 'Compiler', description: '', icon: '', category_id: '', sort_order: 0, favorite: false, created_at: 1, updated_at: 1, links: [] }
  const check = { id: 'hc-1', project_id: 'p-1', kind: 'http', target: 'https://status.example.com/health', interval_seconds: 30, timeout_seconds: 5, failure_threshold: 3, recovery_threshold: 2, enabled: true, state: 'healthy', failure_count: 0, recovery_count: 0, last_checked_at: 1 }
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (path.endsWith('/session')) await route.fulfill({ json: { user: { id: 'u-1', username: 'admin', role: 'admin', disabled: false, createdAt: 1 }, csrfToken: 'csrf-e2e' } })
    else if (path.endsWith('/categories')) await route.fulfill({ json: { items: [], count: 0 } })
    else if (path.endsWith('/projects')) await route.fulfill({ json: { items: [project], count: 1 } })
    else if (path.endsWith('/events')) await route.fulfill({ status: 200, contentType: 'text/event-stream', body: 'id: 1\nevent: health.state\ndata: {"check_id":"hc-1","state":"healthy"}\n\n' })
    else if (path.endsWith('/health-checks') && request.method() === 'POST') {
      expect(request.headers()['x-csrf-token']).toBe('csrf-e2e')
      expect(request.postDataJSON()).toMatchObject({ project_id: 'p-1', kind: 'http', enabled: true })
      created = true
      await route.fulfill({ status: 201, json: check })
    } else if (path.endsWith('/health-checks')) await route.fulfill({ json: { items: [check], count: 1 } })
    else await route.fulfill({ status: 404, body: 'not found' })
  })
  await page.goto('/')
  await page.getByRole('button', { name: '运行监控' }).click()
  await expect(page.getByText('status.example.com/health')).toBeVisible()
  await expect(page.getByText('运行正常')).toBeVisible()
  await page.getByRole('button', { name: '添加监控' }).click()
  await page.getByLabel('目标地址').fill('https://status.example.com/health')
  await page.getByRole('button', { name: '创建监控' }).click()
  await expect.poll(() => created).toBe(true)
})

test('registers an Agent host, binds a policy target, and submits an idempotent action', async ({ page }) => {
  let hostCreated = false
  let targetCreated = false
  let actionCreated = false
  const host = { id: 'h-1', name: 'lab-agent', agent_id: 'agent-lab', last_seen_at: 1, created_at: 1, updated_at: 1 }
  const target = { id: 't-1', project_id: 'p-1', host_id: 'h-1', host_name: 'lab-agent', agent_id: 'agent-lab', agent_target_id: 'allowlisted-web', kind: 'docker_container', display_name: 'Web worker', created_at: 1, state: 'running' }
  const job = { id: 'j-1', request_id: 'r-1', actor_id: 'u-1', project_id: 'p-1', target_id: 't-1', action: 'restart', status: actionCreated ? 'succeeded' : 'queued', result: '', error_code: '', created_at: 1 }
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (path.endsWith('/session')) await route.fulfill({ json: { user: { id: 'u-1', username: 'admin', role: 'admin', disabled: false, createdAt: 1 }, csrfToken: 'csrf-e2e' } })
    else if (path.endsWith('/categories')) await route.fulfill({ json: { items: [], count: 0 } })
    else if (path.endsWith('/projects')) await route.fulfill({ json: { items: [{ id: 'p-1', name: 'Compiler', description: '', icon: '', category_id: '', sort_order: 0, favorite: false, created_at: 1, updated_at: 1, links: [] }], count: 1 } })
    else if (path.endsWith('/events')) await route.fulfill({ status: 200, contentType: 'text/event-stream', body: ': connected\n\n' })
    else if (path.endsWith('/hosts') && request.method() === 'POST') { expect(request.headers()['x-csrf-token']).toBe('csrf-e2e'); hostCreated = true; await route.fulfill({ status: 201, json: host }) }
    else if (path.endsWith('/hosts')) await route.fulfill({ json: { items: hostCreated ? [host] : [], count: hostCreated ? 1 : 0 } })
    else if (path.endsWith('/service-targets') && request.method() === 'POST') { expect(request.headers()['x-csrf-token']).toBe('csrf-e2e'); expect(request.postDataJSON()).toMatchObject({ project_id: 'p-1', host_id: 'h-1', agent_target_id: 'allowlisted-web' }); targetCreated = true; await route.fulfill({ status: 201, json: target }) }
    else if (path.endsWith('/service-targets/t-1/status')) await route.fulfill({ json: { state: 'running' } })
    else if (path.endsWith('/service-targets') && request.method() === 'GET') await route.fulfill({ json: { items: targetCreated ? [target] : [], count: targetCreated ? 1 : 0 } })
    else if (path.endsWith('/actions') && request.method() === 'POST') { expect(request.headers()['x-csrf-token']).toBe('csrf-e2e'); expect(request.headers()['idempotency-key']).toBeTruthy(); actionCreated = true; await route.fulfill({ status: 202, json: job }) }
    else if (path.endsWith('/action-jobs/j-1')) await route.fulfill({ json: { ...job, status: 'succeeded' } })
    else if (path.endsWith('/action-jobs')) await route.fulfill({ json: { items: actionCreated ? [{ ...job, status: 'succeeded' }] : [], count: actionCreated ? 1 : 0 } })
    else await route.fulfill({ status: 404, body: 'not found' })
  })
  await page.goto('/')
  await page.getByRole('button', { name: '服务控制' }).click()
  await page.getByRole('button', { name: '添加主机' }).click()
  await page.getByLabel('主机名称').fill('lab-agent')
  await page.getByLabel('Agent ID').fill('agent-lab')
  await page.getByRole('button', { name: '保存主机' }).click()
  await page.getByRole('button', { name: '绑定服务目标' }).click()
  await page.getByLabel('Agent 策略目标 ID').fill('allowlisted-web')
  await page.getByLabel('显示名称').fill('Web worker')
  await page.getByRole('button', { name: '绑定目标' }).click()
  await expect(page.getByText('Web worker · Compiler')).toBeVisible()
  await expect(page.getByText('运行中')).toBeVisible()
  await page.getByRole('button', { name: '重启' }).click()
  await expect.poll(() => actionCreated).toBe(true)
  await expect(page.getByText('restart · succeeded')).toBeVisible()
})
