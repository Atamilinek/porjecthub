export interface User {
  id: string
  username: string
  role: 'viewer' | 'operator' | 'admin'
  disabled: boolean
  createdAt: number
}

export interface ProjectLink {
  id: string
  label: string
  url: string
  position: number
  created_at: number
}

export interface Project {
  id: string
  name: string
  description: string
  icon: string
  category_id: string
  sort_order: number
  favorite: boolean
  created_at: number
  updated_at: number
  links: ProjectLink[]
}

export interface Category {
  id: string
  name: string
  icon: string
  sort_order: number
  created_at: number
  updated_at: number
}

export interface ProjectInput {
  name: string
  description: string
  icon: string
  category_id: string
  sort_order: number
  links: { label: string; url: string }[]
}

export interface Session {
  user: User
  csrfToken: string
  expiresAt?: number
}

let csrfToken = ''

export class APIError extends Error {
  constructor(public status: number, message: string) {
    super(message)
    this.name = 'APIError'
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body) headers.set('Content-Type', 'application/json')
  if (init.method && !['GET', 'HEAD', 'OPTIONS'].includes(init.method.toUpperCase())) {
    headers.set('X-CSRF-Token', csrfToken)
  }
  const response = await fetch(`/api/v1${path}`, { ...init, headers, credentials: 'same-origin' })
  if (!response.ok) {
    const message = (await response.text()).trim() || `Request failed (${response.status})`
    throw new APIError(response.status, message)
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

export const api = {
  async login(username: string, password: string) {
    const session = await request<Session>('/login', { method: 'POST', body: JSON.stringify({ username, password }) })
    csrfToken = session.csrfToken
    return session
  },
  async getSession() {
    const session = await request<Session>('/session')
    csrfToken = session.csrfToken
    return session
  },
  async logout() {
    await request<void>('/logout', { method: 'POST' })
    csrfToken = ''
  },
  listProjects(params: URLSearchParams) {
    const query = params.toString()
    return request<{ items: Project[]; count: number }>(`/projects${query ? `?${query}` : ''}`)
  },
  listCategories() {
    return request<{ items: Category[]; count: number }>('/categories')
  },
  getProject(id: string) {
    return request<Project>(`/projects/${encodeURIComponent(id)}`)
  },
  createProject(input: ProjectInput) {
    return request<Project>('/projects', { method: 'POST', body: JSON.stringify(input) })
  },
  updateProject(id: string, input: ProjectInput) {
    return request<Project>(`/projects/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(input) })
  },
  deleteProject(id: string) {
    return request<void>(`/projects/${encodeURIComponent(id)}`, { method: 'DELETE' })
  },
  setFavorite(id: string, favorite: boolean) {
    return request<void>(`/projects/${encodeURIComponent(id)}/favorite`, { method: 'PUT', body: JSON.stringify({ favorite }) })
  },
  createCategory(input: { name: string; icon: string; sort_order: number }) {
    return request<Category>('/categories', { method: 'POST', body: JSON.stringify(input) })
  },
  deleteCategory(id: string) {
    return request<void>(`/categories/${encodeURIComponent(id)}`, { method: 'DELETE' })
  },
}
