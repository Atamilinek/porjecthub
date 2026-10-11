import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import App from './Hub.vue'

function response(body: unknown, status = 200) {
  return new Response(status === 204 ? null : JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('Project Hub application', () => {
  beforeEach(() => vi.stubGlobal('fetch', vi.fn()))

  it('shows the real login screen when there is no authenticated session', async () => {
    vi.mocked(fetch).mockResolvedValue(response('authentication required', 401))
    const wrapper = mount(App)
    await flushPromises()
    expect(wrapper.get('h1').text()).toBe('欢迎回来')
    expect(wrapper.find('input#username').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('尚未连接到 API')
    expect(wrapper.text()).not.toContain('Demo project')
  })

  it('logs in and renders projects returned by the authenticated API', async () => {
    const project = { id: 'p-1', name: 'Compiler', description: 'Build service', icon: '', category_id: '', sort_order: 0, favorite: false, created_at: 1, updated_at: 1, links: [] }
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input)
      if (url.endsWith('/session')) return response('authentication required', 401)
      if (url.endsWith('/login')) return response({ user: { id: 'u-1', username: 'admin', role: 'admin', disabled: false, createdAt: 1 }, csrfToken: 'csrf-secret' })
      if (url.endsWith('/categories')) return response({ items: [], count: 0 })
      if (url.includes('/projects')) return response({ items: [project], count: 1 })
      throw new Error(`Unexpected request ${url} ${init?.method ?? 'GET'}`)
    })
    const wrapper = mount(App)
    await flushPromises()
    await wrapper.get('input#username').setValue('admin')
    await wrapper.get('input#password').setValue('correct horse battery staple')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Compiler')
    expect(wrapper.text()).toContain('Build service')
    expect(wrapper.findAll('.project-card')).toHaveLength(1)
    expect(vi.mocked(fetch)).toHaveBeenCalledWith('/api/v1/projects?sort=sort_order', expect.objectContaining({ credentials: 'same-origin' }))
    expect(vi.mocked(fetch)).toHaveBeenCalledWith('/api/v1/login', expect.objectContaining({ method: 'POST' }))
  })

  it('shows empty state and omits management controls for a viewer', async () => {
    vi.mocked(fetch).mockImplementation(async (input) => {
      const url = String(input)
      if (url.endsWith('/session')) return response({ user: { id: 'u-2', username: 'viewer', role: 'viewer', disabled: false, createdAt: 1 }, csrfToken: 'csrf-viewer' })
      if (url.endsWith('/categories')) return response({ items: [], count: 0 })
      if (url.includes('/projects')) return response({ items: [], count: 0 })
      throw new Error(`Unexpected request ${url}`)
    })
    const wrapper = mount(App)
    await flushPromises()
    expect(wrapper.get('.empty-state').text()).toContain('项目管理员可以添加项目')
    expect(wrapper.find('button').text()).not.toContain('新建项目')
    expect(wrapper.find('[aria-label="添加分类"]').exists()).toBe(false)
  })

  it('preserves all project links when an administrator edits a project', async () => {
    const project = {
      id: 'p-1', name: 'Compiler', description: 'Build service', icon: '', category_id: '', sort_order: 0, favorite: false, created_at: 1, updated_at: 1,
      links: [
        { id: 'l-1', label: 'Repository', url: 'https://example.com/repo', position: 0, created_at: 1 },
        { id: 'l-2', label: 'Docs', url: 'https://example.com/docs', position: 1, created_at: 1 },
      ],
    }
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input)
      if (url.endsWith('/session')) return response({ user: { id: 'u-1', username: 'admin', role: 'admin', disabled: false, createdAt: 1 }, csrfToken: 'csrf-secret' })
      if (url.endsWith('/categories')) return response({ items: [], count: 0 })
      if (url.endsWith('/projects/p-1') && init?.method === 'PUT') return response(project)
      if (url.includes('/projects')) return response({ items: [project], count: 1 })
      throw new Error(`Unexpected request ${url}`)
    })
    const wrapper = mount(App)
    await flushPromises()
    await wrapper.get('.project-title').trigger('click')
    await wrapper.get('.detail-actions button:nth-child(2)').trigger('click')
    await wrapper.get('.editor-form').trigger('submit')
    await flushPromises()

    const update = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/api/v1/projects/p-1' && init?.method === 'PUT')
    expect(update).toBeDefined()
    expect(JSON.parse(String(update?.[1]?.body)).links).toEqual([
      { label: 'Repository', url: 'https://example.com/repo' },
      { label: 'Docs', url: 'https://example.com/docs' },
    ])
    expect(new Headers(update?.[1]?.headers).get('X-CSRF-Token')).toBe('csrf-secret')
  })

  it('loads persisted health checks from the API and submits a monitor configuration', async () => {
    const project = { id: 'p-1', name: 'Compiler', description: '', icon: '', category_id: '', sort_order: 0, favorite: false, created_at: 1, updated_at: 1, links: [] }
    const check = { id: 'hc-1', project_id: 'p-1', kind: 'http', target: 'https://status.example.com/health', interval_seconds: 30, timeout_seconds: 5, failure_threshold: 3, recovery_threshold: 2, enabled: true, state: 'healthy', failure_count: 0, recovery_count: 0, last_checked_at: 1 }
    vi.stubGlobal('EventSource', class { onopen: (() => void) | null = null; onerror: (() => void) | null = null; addEventListener() {}; close() {} })
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input)
      if (url.endsWith('/session')) return response({ user: { id: 'u-1', username: 'admin', role: 'admin', disabled: false, createdAt: 1 }, csrfToken: 'csrf-secret' })
      if (url.endsWith('/categories')) return response({ items: [], count: 0 })
      if (url.includes('/projects')) return response({ items: [project], count: 1 })
      if (url.endsWith('/health-checks') && init?.method === 'POST') return response(check, 201)
      if (url.endsWith('/health-checks')) return response({ items: [check], count: 1 })
      if (url.endsWith('/events')) return response('')
      throw new Error(`Unexpected request ${url} ${init?.method ?? 'GET'}`)
    })
    const wrapper = mount(App)
    await flushPromises()
    await wrapper.get('[aria-label="运行监控"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('status.example.com/health')
    expect(wrapper.text()).toContain('运行正常')
    await wrapper.get('[aria-label="添加监控"]').trigger('click')
    await wrapper.get('input[aria-label="目标地址"]').setValue('https://status.example.com/health')
    await wrapper.get('.monitor-form').trigger('submit')
    await flushPromises()
    const createCall = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/api/v1/health-checks' && init?.method === 'POST')
    expect(JSON.parse(String(createCall?.[1]?.body))).toMatchObject({ project_id: 'p-1', kind: 'http', target: 'https://status.example.com/health', enabled: true })
    expect(new Headers(createCall?.[1]?.headers).get('X-CSRF-Token')).toBe('csrf-secret')
    wrapper.unmount()
  })
})
