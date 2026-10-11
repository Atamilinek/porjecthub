<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { APIError, api, type ActionJob, type AuditEntry, type Category, type HealthCheck, type HealthSample, type Host, type ManagedUser, type Project, type ProjectInput, type RuntimeSettings, type ServiceTarget, type Session } from './api'

const user = ref<Session['user'] | null>(null)
const projects = ref<Project[]>([])
const categories = ref<Category[]>([])
const loading = ref(true)
const busy = ref(false)
const error = ref('')
const loginError = ref('')
const username = ref('')
const password = ref('')
const search = ref('')
const category = ref('')
const favorites = ref(false)
const sort = ref('sort_order')
const editorOpen = ref(false)
const detail = ref<Project | null>(null)
const categoryOpen = ref(false)
const categoryName = ref('')
const view = ref<'projects' | 'monitor' | 'services' | 'admin'>('projects')
const healthChecks = ref<HealthCheck[]>([])
const agentTargets = ref<ServiceTarget[]>([])
const monitorOpen = ref(false)
const monitorKind = ref<HealthCheck['kind']>('http')
const monitorTarget = ref('')
const monitorProject = ref('')
const selectedSamples = ref('')
const samples = ref<HealthSample[]>([])
const eventConnected = ref(false)
const hosts = ref<Host[]>([])
const serviceTargets = ref<ServiceTarget[]>([])
const actionJobs = ref<ActionJob[]>([])
const hostOpen = ref(false)
const targetOpen = ref(false)
const hostForm = reactive({ name: '', agent_id: '' })
const targetForm = reactive({ project_id: '', host_id: '', agent_target_id: '', kind: 'docker_container' as ServiceTarget['kind'], display_name: '' })
const actionBusy = ref('')
const managedUsers = ref<ManagedUser[]>([])
const auditEntries = ref<AuditEntry[]>([])
const auditTotal = ref(0)
const auditOffset = ref(0)
const runtime = ref<RuntimeSettings | null>(null)
const auditQuery = ref('')
const newUser = reactive({ username: '', password: '', role: 'viewer' as ManagedUser['role'] })
let eventSource: EventSource | null = null
let pollTimer: ReturnType<typeof setInterval> | undefined
const editor = reactive({ id: '', name: '', description: '', icon: '', category_id: '', links: [] as { label: string; url: string }[] })
const isAdmin = computed(() => user.value?.role === 'admin')
const canOperate = computed(() => user.value?.role === 'admin' || user.value?.role === 'operator')
const title = computed(() => favorites.value ? '我的收藏' : categories.value.find((item) => item.id === category.value)?.name || '所有项目')

onMounted(async () => {
  try {
    const session = await api.getSession()
    user.value = session.user
    await load()
  } catch (e) {
    if (!(e instanceof APIError) || e.status !== 401) error.value = message(e)
  } finally { loading.value = false }
})
onUnmounted(closeMonitorUpdates)
watch(view, (next) => {
  closeMonitorUpdates()
  if (!user.value) return
  if (next === 'monitor') { void loadMonitor(); startMonitorUpdates() }
  else if (next === 'services') { void loadServices(); startMonitorUpdates() }
  else if (next === 'admin') void loadAdmin()
})
watch(monitorKind, (kind) => { if (kind === 'agent' && monitorOpen.value) void loadAgentTargets() })
watch([search, category, favorites, sort], () => { if (user.value) void loadProjects() })

function params() {
  const q = new URLSearchParams()
  if (search.value.trim()) q.set('q', search.value.trim())
  if (category.value) q.set('category_id', category.value)
  if (favorites.value) q.set('favorites', 'true')
  q.set('sort', sort.value)
  return q
}
async function load() {
  error.value = ''
  try {
    const [c, p] = await Promise.all([api.listCategories(), api.listProjects(params())])
    categories.value = c.items
    projects.value = p.items
  } catch (e) { error.value = message(e) }
}
async function loadProjects() {
  try { projects.value = (await api.listProjects(params())).items } catch (e) { error.value = message(e) }
}
async function loadMonitor() {
  try { healthChecks.value = (await api.listHealthChecks()).items; if (healthChecks.value.some((item) => item.kind === 'agent')) agentTargets.value = (await api.listServiceTargets()).items } catch (e) { error.value = message(e) }
}
async function loadAgentTargets() { try { agentTargets.value = (await api.listServiceTargets()).items; if (!monitorTarget.value) monitorTarget.value = agentTargets.value[0]?.id ?? '' } catch (e) { error.value = message(e) } }
async function openMonitor() { monitorOpen.value = true; monitorProject.value = projects.value[0]?.id || ''; if (monitorKind.value === 'agent') await loadAgentTargets() }
async function loadServices() {
  try {
    const [h, t, j] = await Promise.all([api.listHosts(), api.listServiceTargets(), api.listActionJobs()])
    hosts.value = h.items; serviceTargets.value = t.items; actionJobs.value = j.items
    await Promise.all(serviceTargets.value.map(async (target) => { try { target.state = (await api.serviceTargetStatus(target.id)).state ?? 'unknown' } catch { target.state = 'unknown' } }))
  } catch (e) { error.value = message(e) }
}
async function loadAdmin() {
  try {
    const query = new URLSearchParams({ q: auditQuery.value.trim(), limit: '50', offset: String(auditOffset.value) })
    const [users, audit, settings] = await Promise.all([api.listAdminUsers(), api.listAudit(query), api.getAdminSettings()])
    managedUsers.value = users.items; auditEntries.value = audit.items; auditTotal.value = audit.count; runtime.value = settings
  } catch (e) { error.value = message(e) }
}
async function createUser() {
  busy.value = true
  try { await api.createAdminUser({ ...newUser }); Object.assign(newUser,{username:'',password:'',role:'viewer'}); await loadAdmin() }
  catch (e) { error.value = message(e) } finally { busy.value = false }
}
async function toggleManagedUser(item: ManagedUser) {
  try { await api.setAdminUserDisabled(item.id,!item.disabled); await loadAdmin() } catch (e) { error.value = message(e) }
}
async function resetManagedUserPassword(item: ManagedUser) {
  const password = window.prompt(`为 ${item.username} 设置新密码（至少 12 个字符）`)
  if (password === null) return
  try { await api.resetAdminUserPassword(item.id,password); error.value = '密码已重置，该用户的现有会话已撤销。'; await loadAdmin() } catch (e) { error.value = message(e) }
}
async function addHost() {
  busy.value = true
  try { hosts.value.push(await api.createHost({ name: hostForm.name.trim(), agent_id: hostForm.agent_id.trim() })); hostForm.name = ''; hostForm.agent_id = ''; hostOpen.value = false }
  catch (e) { error.value = message(e) } finally { busy.value = false }
}
async function addServiceTarget() {
  busy.value = true
  try { await api.createServiceTarget({ ...targetForm, display_name: targetForm.display_name.trim(), agent_target_id: targetForm.agent_target_id.trim() }); targetOpen.value = false; Object.assign(targetForm, { project_id: '', host_id: '', agent_target_id: '', kind: 'docker_container', display_name: '' }); await loadServices() }
  catch (e) { error.value = message(e) } finally { busy.value = false }
}
async function removeHost(host: Host) {
  if (!window.confirm(`删除主机映射“${host.name}”？`)) return
  try { await api.deleteHost(host.id); await loadServices() } catch (e) { error.value = message(e) }
}
async function removeServiceTarget(target: ServiceTarget) {
  if (!window.confirm(`删除服务映射“${target.display_name}”？`)) return
  try { await api.deleteServiceTarget(target.id); await loadServices() } catch (e) { error.value = message(e) }
}
async function runAction(target: ServiceTarget, action: ActionJob['action']) {
  actionBusy.value = target.id
  try {
    const key = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random()}`
    let job = await api.createAction(target.id, action, key)
    for (let attempt = 0; attempt < 70 && (job.status === 'queued' || job.status === 'running'); attempt++) {
      actionJobs.value = [job, ...actionJobs.value.filter((item) => item.id !== job.id)]
      await new Promise((resolve) => setTimeout(resolve, 500))
      job = await api.getActionJob(job.id)
    }
    actionJobs.value = [job, ...actionJobs.value.filter((item) => item.id !== job.id)]
    if (job.status !== 'succeeded') error.value = job.error_code === 'agent_unavailable' ? 'Agent 当前不可达，未执行该操作。' : `操作未完成：${job.error_code || job.status}`
    await loadServices()
  } catch (e) { error.value = message(e) } finally { actionBusy.value = '' }
}
function startMonitorUpdates() {
  closeMonitorUpdates()
  if (typeof EventSource !== 'undefined') {
    eventSource = new EventSource('/api/v1/events')
    eventSource.onopen = () => { eventConnected.value = true; if (pollTimer) { clearInterval(pollTimer); pollTimer = undefined } }
    const refreshMonitor = () => { if (view.value === 'monitor') void loadMonitor() }
    const refreshJobs = () => { if (view.value === 'services') void loadServices() }
    eventSource.addEventListener('health.state', refreshMonitor)
    eventSource.addEventListener('health.sample', refreshMonitor)
    eventSource.addEventListener('job.state', refreshJobs)
    eventSource.onerror = () => {
      eventConnected.value = false
      if (!pollTimer) { void refreshCurrentView(); pollTimer = setInterval(() => { void refreshCurrentView() }, 15000) }
    }
  } else {
    eventConnected.value = false
    pollTimer = setInterval(() => { void refreshCurrentView() }, 15000)
  }
}
function refreshCurrentView() { if (view.value === 'monitor') return loadMonitor(); if (view.value === 'services') return loadServices() }
function closeMonitorUpdates() {
  eventSource?.close(); eventSource = null
  if (pollTimer) clearInterval(pollTimer)
  pollTimer = undefined; eventConnected.value = false
}
async function addMonitor() {
  if (!monitorProject.value || !monitorTarget.value.trim()) return
  busy.value = true
  try { await api.createHealthCheck({ project_id: monitorProject.value, kind: monitorKind.value, target: monitorTarget.value.trim(), enabled: true }); monitorOpen.value = false; monitorTarget.value = ''; await loadMonitor() }
  catch (e) { error.value = message(e) } finally { busy.value = false }
}
async function removeMonitor(check: HealthCheck) {
  if (!window.confirm(`删除此监控配置？`)) return
  try { await api.deleteHealthCheck(check.id); healthChecks.value = healthChecks.value.filter((item) => item.id !== check.id) }
  catch (e) { error.value = message(e) }
}
async function toggleSamples(check: HealthCheck) {
  if (selectedSamples.value === check.id) { selectedSamples.value = ''; return }
  try { samples.value = (await api.listHealthSamples(check.id)).items; selectedSamples.value = check.id }
  catch (e) { error.value = message(e) }
}
function stateLabel(state: HealthCheck['state']) {
  return ({ healthy: '运行正常', checking: '检测中', degraded: '状态异常', offline: '离线', agent_unreachable: 'Agent 不可达', unknown: '等待首次检测' } as const)[state]
}
function serviceStateLabel(state?: ServiceTarget['state']) { return ({ running: '运行中', stopped: '已停止', unknown: 'Agent 不可达' } as const)[state ?? 'unknown'] }
async function login() {
  busy.value = true; loginError.value = ''
  try { const s = await api.login(username.value, password.value); user.value = s.user; password.value = ''; await load() }
  catch (e) { loginError.value = message(e) } finally { busy.value = false }
}
async function logout() {
  try { await api.logout(); closeMonitorUpdates(); user.value = null; projects.value = []; categories.value = []; healthChecks.value = []; detail.value = null }
  catch (e) { error.value = message(e) }
}
async function favorite(project: Project) {
  try { await api.setFavorite(project.id, !project.favorite); await loadProjects(); if (detail.value?.id === project.id) detail.value = projects.value.find(p => p.id === project.id) ?? null }
  catch (e) { error.value = message(e) }
}
function createProject() { Object.assign(editor, { id: '', name: '', description: '', icon: '', category_id: category.value, links: [] }); editorOpen.value = true }
function editProject(p: Project) { Object.assign(editor, { id: p.id, name: p.name, description: p.description, icon: p.icon, category_id: p.category_id, links: p.links.map(({ label, url }) => ({ label, url })) }); detail.value = null; editorOpen.value = true }
function addLink() { if (editor.links.length < 20) editor.links.push({ label: '', url: '' }) }
async function saveProject() {
  busy.value = true
  const input: ProjectInput = { name: editor.name.trim(), description: editor.description.trim(), icon: editor.icon.trim(), category_id: editor.category_id, sort_order: 0, links: editor.links.map((link) => ({ label: link.label.trim(), url: link.url.trim() })) }
  try { if (editor.id) await api.updateProject(editor.id, input); else await api.createProject(input); editorOpen.value = false; detail.value = null; await load() }
  catch (e) { error.value = message(e) } finally { busy.value = false }
}
async function deleteProject(p: Project) {
  if (!window.confirm(`确定删除“${p.name}”吗？此操作不可撤销。`)) return
  try { await api.deleteProject(p.id); detail.value = null; await load() } catch (e) { error.value = message(e) }
}
async function addCategory() {
  if (!categoryName.value.trim()) return
  busy.value = true
  try { const c = await api.createCategory({ name: categoryName.value.trim(), icon: '', sort_order: categories.value.length }); categories.value.push(c); category.value = c.id; favorites.value = false; categoryName.value = ''; categoryOpen.value = false }
  catch (e) { error.value = message(e) } finally { busy.value = false }
}
async function deleteCategory(c: Category) {
  if (!window.confirm(`删除分类“${c.name}”？现有项目会保留为未分类。`)) return
  try { await api.deleteCategory(c.id); categories.value = categories.value.filter(x => x.id !== c.id); if (category.value === c.id) category.value = ''; await loadProjects() }
  catch (e) { error.value = message(e) }
}
function message(e: unknown) {
  if (e instanceof APIError) {
    if (e.status === 401) { closeMonitorUpdates(); user.value = null; return '登录状态已过期，请重新登录。' }
    return e.status === 403 ? '服务器拒绝了此操作，请确认权限。' : e.message
  }
  return e instanceof Error ? e.message : '请求失败，请检查网络后重试。'
}
</script>

<template>
  <main v-if="loading" class="loading-screen" role="status">正在连接 Project Hub…</main>
  <section v-else-if="!user" class="login-page">
    <form class="login-card" @submit.prevent="login">
      <div class="brand"><span class="brand-icon">P</span><span>PROJECT HUB</span></div>
      <p class="eyebrow">SECURE WORKSPACE</p><h1>欢迎回来</h1><p class="login-copy">登录以查看团队项目和资源入口。</p>
      <label for="username">用户名</label><input id="username" v-model="username" autocomplete="username" required />
      <label for="password">密码</label><input id="password" v-model="password" type="password" autocomplete="current-password" required />
      <p v-if="loginError" class="notice error" role="alert">{{ loginError }}</p>
      <button class="button primary full" :disabled="busy">{{ busy ? '正在验证…' : '登录 Project Hub' }}</button>
      <p class="login-foot">使用管理员提供的账户登录</p>
    </form>
  </section>
  <div v-else class="app-layout">
    <aside class="sidebar">
      <a class="brand" href="#" @click.prevent="view='projects';category='';favorites=false"><span class="brand-icon">P</span><span>PROJECT HUB</span></a>
      <div class="workspace-label">WORKSPACE</div>
      <nav class="nav-list" aria-label="项目导航">
        <button class="nav-item" :class="{active:view==='projects'&&!category&&!favorites}" @click="view='projects';category='';favorites=false"><span>◫</span>所有项目</button>
        <button class="nav-item" :class="{active:view==='projects'&&favorites}" @click="view='projects';favorites=!favorites;category=''"><span>☆</span>我的收藏</button>
        <button class="nav-item" :class="{active:view==='monitor'}" aria-label="运行监控" @click="view='monitor'"><span>◉</span>运行监控</button>
        <button class="nav-item" :class="{active:view==='services'}" aria-label="服务控制" @click="view='services'"><span>⏻</span>服务控制</button>
        <button v-if="isAdmin" class="nav-item" :class="{active:view==='admin'}" aria-label="管理中心" @click="view='admin'"><span>⚙</span>管理中心</button>
      </nav>
      <div class="category-head"><span>分类</span><button v-if="isAdmin" class="icon-button" aria-label="添加分类" @click="categoryOpen=!categoryOpen">＋</button></div>
      <form v-if="categoryOpen" class="category-form" @submit.prevent="addCategory"><input v-model="categoryName" aria-label="分类名称" placeholder="新分类名称" required maxlength="80" /><button class="button small primary">添加</button></form>
      <nav class="nav-list" aria-label="项目分类">
        <div v-for="item in categories" :key="item.id" class="category-row"><button class="nav-item category-item" :class="{active:view==='projects'&&category===item.id}" @click="view='projects';category=item.id;favorites=false"><span>◌</span><span class="category-name">{{ item.name }}</span><span v-if="!search&&!category&&!favorites" class="category-count">{{ projects.filter(p=>p.category_id===item.id).length }}</span></button><button v-if="isAdmin" class="delete-category" :aria-label="'删除分类 '+item.name" @click="deleteCategory(item)">×</button></div>
      </nav>
      <div class="sidebar-bottom"><span class="avatar">{{ user.username.slice(0,1).toUpperCase() }}</span><span class="user-info"><strong>{{ user.username }}</strong><small>{{ user.role }}</small></span><button class="icon-button signout" aria-label="退出登录" @click="logout">↗</button></div>
    </aside>
    <section class="main-area">
      <header class="topbar"><div class="breadcrumb">Workspace <span>/</span> <strong>{{ view==='monitor'?'运行监控':view==='services'?'服务控制':view==='admin'?'管理中心':title }}</strong></div><div class="top-actions"><label v-if="view==='projects'" class="search-box"><span>⌕</span><input v-model="search" type="search" placeholder="搜索项目…" aria-label="搜索项目" /></label><span class="role-chip">{{ user.role }}</span></div></header>
      <main class="content">
        <div v-if="error" class="notice error" role="alert"><span>{{ error }}</span><button class="icon-button" aria-label="关闭提示" @click="error=''">×</button></div>
        <template v-if="view==='projects'">
        <div class="page-heading"><div><p class="eyebrow">PROJECT DIRECTORY</p><h1>{{ title }}<span class="heading-count">{{ projects.length }}</span></h1><p class="page-subtitle">团队项目、文档和常用资源的统一入口。</p></div><button v-if="isAdmin" class="button primary" @click="createProject">＋ 新建项目</button></div>
        <div class="toolbar"><div class="filter-tabs"><button :class="{selected:!favorites}" @click="favorites=false">全部项目</button><button :class="{selected:favorites}" @click="favorites=!favorites">已收藏</button></div><label class="sort-control">排序<select v-model="sort" aria-label="排序项目"><option value="sort_order">自定义顺序</option><option value="name">名称</option><option value="created_at">创建时间</option><option value="updated_at">最近更新</option></select></label></div>
        <div v-if="projects.length" class="project-grid">
          <article v-for="p in projects" :key="p.id" class="project-card"><div class="card-top"><div class="project-icon">{{ p.icon || p.name.slice(0,1).toUpperCase() }}</div><button class="favorite-button" :class="{favored:p.favorite}" :aria-label="p.favorite?'取消收藏':'收藏项目'" @click="favorite(p)">{{ p.favorite?'★':'☆' }}</button></div>
            <button class="project-title" @click="detail=p">{{ p.name }}</button><p class="project-description">{{ p.description || '该项目暂无描述。' }}</p><div class="card-bottom"><span class="category-pill">{{ categories.find(c=>c.id===p.category_id)?.name || '未分类' }}</span><button v-if="p.links.length" class="link-count" @click="detail=p">{{ p.links.length }} 个入口 ↗</button></div>
          </article>
        </div>
        <section v-else class="empty-state"><div class="empty-icon">⌕</div><h2>{{ search||category||favorites?'没有找到匹配的项目':'还没有项目' }}</h2><p>{{ isAdmin?'添加项目后，团队可以在这里找到文档、代码仓库和常用入口。':'项目管理员可以添加项目和资源入口。' }}</p><button v-if="isAdmin&&!search" class="button secondary" @click="createProject">＋ 创建第一个项目</button></section>
        <footer class="page-footer">Project Hub <span>·</span> 私有部署 · 项目目录</footer>
        </template>
        <template v-else-if="view==='monitor'">
          <div class="page-heading"><div><p class="eyebrow">SERVICE HEALTH</p><h1>运行监控<span class="heading-count">{{ healthChecks.length }}</span></h1><p class="page-subtitle">状态由后台定时探测更新；Agent 检查关联已登记的服务目标。</p></div><button v-if="isAdmin" class="button primary" aria-label="添加监控" @click="openMonitor">＋ 添加监控</button></div>
          <div class="monitor-connection"><span class="connection-dot" :class="{online:eventConnected}"></span>{{ eventConnected?'实时事件已连接':'实时连接中断，使用 15 秒轮询' }}</div>
          <div v-if="healthChecks.length" class="monitor-list"><article v-for="check in healthChecks" :key="check.id" class="monitor-card">
            <div class="monitor-main"><span class="state-badge" :class="'state-'+check.state">{{ stateLabel(check.state) }}</span><div class="monitor-target"><strong>{{ projects.find(p=>p.id===check.project_id)?.name || '未知项目' }}</strong><code>{{ check.kind==='agent'?(agentTargets.find(t=>t.id===check.target)?.display_name||check.target):check.target }}</code></div><div class="monitor-meta"><span>{{ check.kind.toUpperCase() }}</span><span>每 {{ check.interval_seconds }} 秒</span><span>{{ check.last_checked_at?new Date(check.last_checked_at*1000).toLocaleTimeString():'尚未检测' }}</span></div><div class="monitor-actions"><button class="button secondary small" @click="toggleSamples(check)">{{ selectedSamples===check.id?'收起记录':'检测记录' }}</button><button v-if="isAdmin" class="icon-button" :aria-label="'删除监控 '+check.target" @click="removeMonitor(check)">×</button></div></div>
            <ul v-if="selectedSamples===check.id" class="sample-list"><li v-for="sample in samples" :key="sample.id"><span>{{ new Date(sample.checked_at*1000).toLocaleString() }}</span><strong>{{ stateLabel(sample.state) }}</strong><span>{{ sample.latency_ms===null?'—':sample.latency_ms+' ms' }}</span><small>{{ sample.detail || '探测成功' }}</small></li><li v-if="!samples.length">暂无检测记录</li></ul>
          </article></div>
          <section v-else class="empty-state"><div class="empty-icon">◉</div><h2>还没有监控目标</h2><p>{{ isAdmin?'添加 HTTP、TCP 或 Agent 目标后，后台会按设定周期进行检测。':'请管理员为项目添加健康检查。' }}</p></section>
          <footer class="page-footer">Project Hub <span>·</span> 健康检查与状态历史</footer>
        </template>
        <template v-else-if="view==='services'">
          <div class="page-heading"><div><p class="eyebrow">AUTHORIZED SERVICES</p><h1>服务控制<span class="heading-count">{{ serviceTargets.length }}</span></h1><p class="page-subtitle">只有主机 Agent 策略明确授权的目标才能执行操作；映射本身不会授予宿主机权限。</p></div><div v-if="isAdmin" class="service-config-actions"><button class="button secondary" aria-label="添加主机" @click="hostOpen=true">＋ 添加主机</button><button class="button primary" aria-label="绑定服务目标" :disabled="!hosts.length||!projects.length" @click="targetOpen=true;targetForm.project_id=projects[0]?.id||'';targetForm.host_id=hosts[0]?.id||''">＋ 绑定目标</button></div></div>
          <div class="monitor-connection"><span class="connection-dot" :class="{online:eventConnected}"></span>{{ eventConnected?'实时事件已连接':'Agent 状态不可达时显示未知' }}</div>
          <section v-if="isAdmin" class="host-list"><h2>已登记主机</h2><article v-for="host in hosts" :key="host.id" class="host-row"><div><strong>{{ host.name }}</strong><small>Agent {{ host.agent_id }} · {{ host.last_seen_at?`最近连接 ${new Date(host.last_seen_at*1000).toLocaleString()}`:'尚未连接' }}</small></div><button class="icon-button" :aria-label="'删除主机 '+host.name" @click="removeHost(host)">×</button></article><p v-if="!hosts.length" class="host-empty">先登记主机 Agent ID，再绑定由主机管理员配置的策略目标。</p></section>
          <div v-if="serviceTargets.length" class="monitor-list"><article v-for="target in serviceTargets" :key="target.id" class="monitor-card service-card"><div class="monitor-main"><span class="state-badge" :class="{'state-healthy':target.state==='running','state-offline':target.state==='stopped','state-agent_unreachable':!target.state||target.state==='unknown'}">{{ serviceStateLabel(target.state) }}</span><div class="monitor-target"><strong>{{ target.display_name }} · {{ projects.find(p=>p.id===target.project_id)?.name||'未知项目' }}</strong><code>{{ target.host_name }} / {{ target.agent_target_id }}</code></div><div class="monitor-meta"><span>{{ target.kind==='docker_container'?'Docker 容器':'systemd 单元' }}</span><span>Agent {{ target.agent_id }}</span></div><div class="monitor-actions"><template v-if="canOperate"><button class="button secondary small" :disabled="actionBusy===target.id" @click="runAction(target,'start')">启动</button><button class="button secondary small" :disabled="actionBusy===target.id" @click="runAction(target,'stop')">停止</button><button class="button secondary small" :disabled="actionBusy===target.id" @click="runAction(target,'restart')">重启</button></template><button v-if="isAdmin" class="icon-button" :aria-label="'删除服务映射 '+target.display_name" @click="removeServiceTarget(target)">×</button></div></div></article></div>
          <section v-else class="empty-state"><div class="empty-icon">⏻</div><h2>还没有登记服务目标</h2><p>{{ isAdmin?'登记 Agent 主机并绑定其策略文件中已经授权的服务目标。':'请管理员登记由 Agent 策略授权的服务目标。' }}</p></section>
          <section v-if="actionJobs.length" class="job-list"><h2>最近操作</h2><article v-for="job in actionJobs.slice(0,10)" :key="job.id"><span>{{ serviceTargets.find(t=>t.id===job.target_id)?.display_name||job.target_id }}</span><strong>{{ job.action }} · {{ job.status }}</strong><small>{{ job.error_code||new Date(job.created_at*1000).toLocaleString() }}</small></article></section>
          <footer class="page-footer">Project Hub <span>·</span> 主机策略授权的服务控制</footer>
        </template>
        <template v-else>
          <div class="page-heading"><div><p class="eyebrow">ADMINISTRATION</p><h1>管理中心</h1><p class="page-subtitle">用户账户、操作审计和当前运行配置。</p></div><button class="button secondary" @click="loadAdmin">刷新数据</button></div>
          <section class="admin-section"><div class="section-heading"><div><h2>用户账户 <span class="heading-count">{{ managedUsers.length }}</span></h2><p>停用会撤销该用户的全部现有会话；重置密码也会撤销会话。</p></div></div>
            <form class="admin-user-form" @submit.prevent="createUser"><label>用户名<input v-model="newUser.username" minlength="3" maxlength="64" pattern="[A-Za-z0-9][A-Za-z0-9._-]*" autocomplete="off" required /></label><label>初始密码<input v-model="newUser.password" type="password" minlength="12" maxlength="1024" autocomplete="new-password" required /></label><label>角色<select v-model="newUser.role"><option value="viewer">只读</option><option value="operator">操作员</option><option value="admin">管理员</option></select></label><button class="button primary" :disabled="busy">创建用户</button></form>
            <div class="admin-user-list"><article v-for="item in managedUsers" :key="item.id" class="admin-user-row"><div><strong>{{ item.username }}</strong><small>{{ item.role }} · {{ item.disabled?'已停用':'启用中' }} · 创建于 {{ new Date(item.createdAt*1000).toLocaleDateString() }}</small></div><div class="monitor-actions"><button class="button secondary small" @click="resetManagedUserPassword(item)">重置密码</button><button class="button secondary small" :disabled="item.id===user?.id" @click="toggleManagedUser(item)">{{ item.disabled?'启用':'停用' }}</button></div></article><p v-if="!managedUsers.length" class="host-empty">暂无用户</p></div>
          </section>
          <section class="admin-section"><div class="section-heading"><div><h2>操作审计 <span class="heading-count">{{ auditTotal }}</span></h2><p>记录用户、资源和服务操作的操作者、结果与请求编号。</p></div></div><form class="audit-search" @submit.prevent="auditOffset=0;loadAdmin()"><input v-model="auditQuery" maxlength="100" aria-label="筛选审计记录" placeholder="按用户名、操作或结果筛选" /><button class="button secondary">筛选</button></form>
            <div class="audit-list"><article v-for="entry in auditEntries" :key="entry.id" class="audit-row"><div><strong>{{ entry.action }} · {{ entry.outcome }}</strong><small>{{ entry.actor_username||'系统/已删除用户' }} · {{ entry.event_type }} · {{ new Date(entry.created_at*1000).toLocaleString() }}</small></div><code>{{ entry.target_type }} {{ entry.target_id }} · {{ entry.request_id }}</code></article><p v-if="!auditEntries.length" class="host-empty">暂无匹配的审计记录</p></div><div class="audit-pagination"><button class="button secondary small" :disabled="auditOffset===0" @click="auditOffset=Math.max(0,auditOffset-50);loadAdmin()">较新</button><span>{{ auditTotal?`${auditOffset+1}–${Math.min(auditOffset+auditEntries.length,auditTotal)} / ${auditTotal}`:'0 条记录' }}</span><button class="button secondary small" :disabled="auditOffset+50>=auditTotal" @click="auditOffset+=50;loadAdmin()">更早</button></div>
          </section>
          <section class="admin-section"><div class="section-heading"><div><h2>运行配置</h2><p>这些值来自当前进程和部署环境；修改部署变量后需重启 API。</p></div></div><div v-if="runtime" class="runtime-grid"><article><small>Agent 传输</small><strong>{{ runtime.agent_transport_configured?'已配置本地 Unix socket':'未配置' }}</strong></article><article><small>探测私网白名单</small><strong>{{ runtime.probe_allow_cidrs.length?runtime.probe_allow_cidrs.join(', '):'未开放私网探测' }}</strong></article><article><small>健康检查默认值</small><strong>每 {{ runtime.default_probe_interval_seconds }} 秒 · 超时 {{ runtime.default_probe_timeout_seconds }} 秒</strong></article><article><small>样本保留</small><strong>{{ runtime.sample_retention_days }} 天</strong></article><article><small>登录会话</small><strong>{{ runtime.session_lifetime_hours }} 小时</strong></article><article><small>数据库</small><strong>{{ runtime.storage }}</strong></article></div></section>
        </template>
      </main>
    </section>

    <div v-if="editorOpen" class="modal-backdrop" @click.self="editorOpen=false"><section class="modal" role="dialog" aria-modal="true" aria-labelledby="editor-title">
      <header class="modal-header"><div><p class="eyebrow">PROJECT SETTINGS</p><h2 id="editor-title">{{ editor.id?'编辑项目':'新建项目' }}</h2></div><button class="icon-button" aria-label="关闭" @click="editorOpen=false">×</button></header>
      <form class="editor-form" @submit.prevent="saveProject"><label>项目名称<input v-model="editor.name" maxlength="120" required /></label><label>项目简介<textarea v-model="editor.description" rows="3" maxlength="2000" /></label>
        <div class="form-row"><label>图标 / Emoji<input v-model="editor.icon" maxlength="64" placeholder="📦" /></label><label>分类<select v-model="editor.category_id"><option value="">未分类</option><option v-for="c in categories" :key="c.id" :value="c.id">{{ c.name }}</option></select></label></div>
        <div class="link-editor-heading"><strong>项目入口</strong><button class="button secondary small" type="button" :disabled="editor.links.length >= 20" @click="addLink">＋ 添加入口</button></div>
        <div v-for="(link, index) in editor.links" :key="index" class="link-editor-row"><label>名称<input v-model="link.label" maxlength="80" placeholder="代码仓库" required /></label><label>地址<input v-model="link.url" type="url" maxlength="2048" placeholder="https://…" required /></label><button class="icon-button remove-link" type="button" :aria-label="'移除入口 ' + (index + 1)" @click="editor.links.splice(index, 1)">×</button></div>
        <p class="form-hint">支持 HTTP、HTTPS 和 SSH 项目链接。</p><div class="modal-actions"><button class="button secondary" type="button" @click="editorOpen=false">取消</button><button class="button primary" :disabled="busy">{{ busy?'保存中…':'保存项目' }}</button></div>
      </form>
    </section></div>

    <div v-if="detail" class="modal-backdrop" @click.self="detail=null"><section class="modal detail-modal" role="dialog" aria-modal="true" aria-labelledby="detail-title">
      <header class="modal-header"><div><p class="eyebrow">PROJECT DETAILS</p><h2 id="detail-title">{{ detail.name }}</h2></div><button class="icon-button" aria-label="关闭" @click="detail=null">×</button></header>
      <p class="detail-description">{{ detail.description||'该项目暂无描述。' }}</p><div class="detail-meta"><span>{{ categories.find(c=>c.id===detail?.category_id)?.name||'未分类' }}</span><span>更新于 {{ new Date(detail.updated_at*1000).toLocaleDateString() }}</span></div>
      <h3>项目入口</h3><ul class="link-list"><li v-for="link in detail.links" :key="link.id"><a :href="link.url" target="_blank" rel="noopener noreferrer">{{ link.label }} ↗</a><small>{{ link.url }}</small></li><li v-if="!detail.links.length" class="no-links">暂无可用入口</li></ul>
      <div class="modal-actions detail-actions"><button class="button secondary" @click="favorite(detail!)">{{ detail.favorite?'★ 已收藏':'☆ 收藏' }}</button><template v-if="isAdmin"><button class="button secondary" @click="editProject(detail!)">编辑</button><button class="button danger" @click="deleteProject(detail!)">删除</button></template></div>
    </section></div>
    <div v-if="monitorOpen" class="modal-backdrop" @click.self="monitorOpen=false"><section class="modal" role="dialog" aria-modal="true" aria-labelledby="monitor-title"><header class="modal-header"><div><p class="eyebrow">HEALTH CHECK</p><h2 id="monitor-title">添加监控目标</h2></div><button class="icon-button" aria-label="关闭" @click="monitorOpen=false">×</button></header>
      <form class="editor-form monitor-form" @submit.prevent="addMonitor"><label>关联项目<select v-model="monitorProject" required><option value="" disabled>选择项目</option><option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</option></select></label><label>检查类型<select v-model="monitorKind"><option value="http">HTTP(S)</option><option value="tcp">TCP</option><option value="agent">Agent 服务状态</option></select></label><label v-if="monitorKind==='agent'">Agent 服务目标<select v-model="monitorTarget" aria-label="Agent 服务目标" required><option v-for="target in agentTargets" :key="target.id" :value="target.id">{{ target.display_name }} · {{ target.host_name }}</option></select></label><label v-else>目标地址<input v-model="monitorTarget" aria-label="目标地址" :type="monitorKind==='http'?'url':'text'" maxlength="2048" :placeholder="monitorKind==='http'?'https://service.example/health':'service.example:443'" required /></label><p class="form-hint">HTTP/TCP 探测受网络策略约束；Agent 检查读取已登记服务目标的实际运行状态。</p><div class="modal-actions"><button class="button secondary" type="button" @click="monitorOpen=false">取消</button><button class="button primary" :disabled="busy||!projects.length||(monitorKind==='agent'&&!agentTargets.length)">{{ busy?'保存中…':'创建监控' }}</button></div></form>
    </section></div>
    <div v-if="hostOpen" class="modal-backdrop" @click.self="hostOpen=false"><section class="modal" role="dialog" aria-modal="true" aria-labelledby="host-title"><header class="modal-header"><div><p class="eyebrow">HOST REGISTRY</p><h2 id="host-title">登记 Agent 主机</h2></div><button class="icon-button" aria-label="关闭" @click="hostOpen=false">×</button></header><form class="editor-form" @submit.prevent="addHost"><label>主机名称<input v-model="hostForm.name" maxlength="100" required /></label><label>Agent ID<input v-model="hostForm.agent_id" maxlength="128" pattern="[A-Za-z0-9][A-Za-z0-9._:-]*" required /></label><p class="form-hint">Agent ID 必须与该主机 root 管理的策略文件一致。登记主机不会自动发现或接管服务。</p><div class="modal-actions"><button class="button secondary" type="button" @click="hostOpen=false">取消</button><button class="button primary" :disabled="busy">保存主机</button></div></form></section></div>
    <div v-if="targetOpen" class="modal-backdrop" @click.self="targetOpen=false"><section class="modal" role="dialog" aria-modal="true" aria-labelledby="target-title"><header class="modal-header"><div><p class="eyebrow">AUTHORIZED TARGET</p><h2 id="target-title">绑定服务目标</h2></div><button class="icon-button" aria-label="关闭" @click="targetOpen=false">×</button></header><form class="editor-form" @submit.prevent="addServiceTarget"><label>关联项目<select v-model="targetForm.project_id" required><option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</option></select></label><label>Agent 主机<select v-model="targetForm.host_id" required><option v-for="host in hosts" :key="host.id" :value="host.id">{{ host.name }}</option></select></label><label>目标类型<select v-model="targetForm.kind"><option value="docker_container">已存在的 Docker 容器</option><option value="systemd_unit">systemd 单元</option></select></label><label>Agent 策略目标 ID<input v-model="targetForm.agent_target_id" maxlength="128" required /></label><label>显示名称<input v-model="targetForm.display_name" maxlength="120" required /></label><p class="form-hint">此处只创建 Project Hub 映射。Agent 仅按主机策略文件中的目标 ID、身份和动作列表授权。</p><div class="modal-actions"><button class="button secondary" type="button" @click="targetOpen=false">取消</button><button class="button primary" :disabled="busy">绑定目标</button></div></form></section></div>
  </div>
</template>
<style>@import './style.css';</style>
