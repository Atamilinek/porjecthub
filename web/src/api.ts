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

export interface HealthCheck {
  id: string
  project_id: string
  kind: 'http' | 'tcp' | 'agent'
  target: string
  interval_seconds: number
  timeout_seconds: number
  failure_threshold: number
  recovery_threshold: number
  enabled: boolean
  state: 'checking' | 'healthy' | 'degraded' | 'offline' | 'unknown' | 'agent_unreachable'
  failure_count: number
  recovery_count: number
  last_checked_at: number | null
}

export interface HealthSample {
  id: string
  check_id: string
  state: HealthCheck['state']
  latency_ms: number | null
  detail: string
  checked_at: number
}
export interface Host { id: string; name: string; agent_id: string; last_seen_at: number | null; created_at: number; updated_at: number }
export interface ServiceTarget { id: string; project_id: string; host_id: string; host_name: string; agent_id: string; agent_target_id: string; kind: 'docker_container' | 'systemd_unit'; display_name: string; created_at: number; state?: 'running' | 'stopped' | 'unknown' }
export interface ActionJob { id: string; request_id: string; actor_id: string; project_id: string; target_id: string; action: 'start' | 'stop' | 'restart'; status: 'queued' | 'running' | 'succeeded' | 'failed' | 'timed_out'; result: string; error_code: string; created_at: number }
export interface ManagedUser { id: string; username: string; role: User['role']; disabled: boolean; createdAt: number }
export interface AuditEntry { id: string; actor_id: string; actor_username: string; request_id: string; event_type: string; target_type: string; target_id: string; action: string; outcome: string; detail: string; created_at: number }
export interface RuntimeSettings { agent_transport_configured: boolean; probe_allow_cidrs: string[]; default_probe_interval_seconds: number; default_probe_timeout_seconds: number; sample_retention_days: number; session_lifetime_hours: number; storage: string }

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
  listHealthChecks() {
    return request<{ items: HealthCheck[]; count: number }>('/health-checks')
  },
  createHealthCheck(input: { project_id: string; kind: HealthCheck['kind']; target: string; enabled: boolean }) {
    return request<HealthCheck>('/health-checks', { method: 'POST', body: JSON.stringify(input) })
  },
  deleteHealthCheck(id: string) {
    return request<void>(`/health-checks/${encodeURIComponent(id)}`, { method: 'DELETE' })
  },
  listHealthSamples(id: string) {
    return request<{ items: HealthSample[]; count: number }>(`/health-checks/${encodeURIComponent(id)}/samples`)
  },
  listHosts() { return request<{ items: Host[]; count: number }>('/hosts') },
  createHost(input: { name: string; agent_id: string }) { return request<Host>('/hosts', { method: 'POST', body: JSON.stringify(input) }) },
  deleteHost(id: string) { return request<void>(`/hosts/${encodeURIComponent(id)}`, { method: 'DELETE' }) },
  listServiceTargets() { return request<{ items: ServiceTarget[]; count: number }>('/service-targets') },
  createServiceTarget(input: { project_id: string; host_id: string; agent_target_id: string; kind: ServiceTarget['kind']; display_name: string }) { return request<ServiceTarget>('/service-targets', { method: 'POST', body: JSON.stringify(input) }) },
  deleteServiceTarget(id: string) { return request<void>(`/service-targets/${encodeURIComponent(id)}`, { method: 'DELETE' }) },
  serviceTargetStatus(id: string) { return request<{ state: ServiceTarget['state'] }>(`/service-targets/${encodeURIComponent(id)}/status`) },
  createAction(id: string, action: ActionJob['action'], idempotencyKey: string) { return request<ActionJob>(`/service-targets/${encodeURIComponent(id)}/actions`, { method: 'POST', headers: { 'Idempotency-Key': idempotencyKey }, body: JSON.stringify({ action }) }) },
  getActionJob(id: string) { return request<ActionJob>(`/action-jobs/${encodeURIComponent(id)}`) },
  listActionJobs() { return request<{ items: ActionJob[]; count: number }>('/action-jobs') },
  listAdminUsers() { return request<{ items: ManagedUser[]; count: number }>('/admin/users') },
  createAdminUser(input: { username: string; password: string; role: ManagedUser['role'] }) { return request<ManagedUser>('/admin/users', { method: 'POST', body: JSON.stringify(input) }) },
  setAdminUserDisabled(id: string, disabled: boolean) { return request<void>(`/admin/users/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify({ disabled }) }) },
  resetAdminUserPassword(id: string, password: string) { return request<void>(`/admin/users/${encodeURIComponent(id)}/password`, { method: 'PUT', body: JSON.stringify({ password }) }) },
  listAudit(query: URLSearchParams) { return request<{ items: AuditEntry[]; count: number; limit: number; offset: number }>(`/admin/audit?${query.toString()}`) },
  getAdminSettings() { return request<RuntimeSettings>('/admin/settings') },
}
