<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { APIError, api, type Category, type HealthCheck, type HealthSample, type Project, type ProjectInput, type Session } from './api'

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
const view = ref<'projects' | 'monitor'>('projects')
const healthChecks = ref<HealthCheck[]>([])
const monitorOpen = ref(false)
const monitorKind = ref<HealthCheck['kind']>('http')
const monitorTarget = ref('')
const monitorProject = ref('')
const selectedSamples = ref('')
const samples = ref<HealthSample[]>([])
const eventConnected = ref(false)
let eventSource: EventSource | null = null
let pollTimer: ReturnType<typeof setInterval> | undefined
const editor = reactive({ id: '', name: '', description: '', icon: '', category_id: '', links: [] as { label: string; url: string }[] })
const isAdmin = computed(() => user.value?.role === 'admin')
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
watch(view, (next) => { if (next === 'monitor' && user.value) { void loadMonitor(); startMonitorUpdates() } else closeMonitorUpdates() })
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
  try { healthChecks.value = (await api.listHealthChecks()).items } catch (e) { error.value = message(e) }
}
function startMonitorUpdates() {
  closeMonitorUpdates()
  if (typeof EventSource !== 'undefined') {
    eventSource = new EventSource('/api/v1/events')
    eventSource.onopen = () => { eventConnected.value = true; if (pollTimer) { clearInterval(pollTimer); pollTimer = undefined } }
    const refresh = () => { void loadMonitor() }
    eventSource.addEventListener('health.state', refresh)
    eventSource.addEventListener('health.sample', refresh)
    eventSource.onerror = () => {
      eventConnected.value = false
      if (!pollTimer) { void loadMonitor(); pollTimer = setInterval(() => { void loadMonitor() }, 15000) }
    }
  } else {
    eventConnected.value = false
    pollTimer = setInterval(() => { void loadMonitor() }, 15000)
  }
}
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
      <a class="brand" href="#" @click.prevent="category='';favorites=false"><span class="brand-icon">P</span><span>PROJECT HUB</span></a>
      <div class="workspace-label">WORKSPACE</div>
      <nav class="nav-list" aria-label="项目导航">
        <button class="nav-item" :class="{active:!category&&!favorites}" @click="category='';favorites=false"><span>◫</span>所有项目</button>
        <button class="nav-item" :class="{active:favorites}" @click="favorites=!favorites;category=''"><span>☆</span>我的收藏</button>
        <button class="nav-item" :class="{active:view==='monitor'}" aria-label="运行监控" @click="view='monitor'"><span>◉</span>运行监控</button>
      </nav>
      <div class="category-head"><span>分类</span><button v-if="isAdmin" class="icon-button" aria-label="添加分类" @click="categoryOpen=!categoryOpen">＋</button></div>
      <form v-if="categoryOpen" class="category-form" @submit.prevent="addCategory"><input v-model="categoryName" aria-label="分类名称" placeholder="新分类名称" required maxlength="80" /><button class="button small primary">添加</button></form>
      <nav class="nav-list" aria-label="项目分类">
        <div v-for="item in categories" :key="item.id" class="category-row"><button class="nav-item category-item" :class="{active:category===item.id}" @click="category=item.id;favorites=false"><span>◌</span><span class="category-name">{{ item.name }}</span><span v-if="!search&&!category&&!favorites" class="category-count">{{ projects.filter(p=>p.category_id===item.id).length }}</span></button><button v-if="isAdmin" class="delete-category" :aria-label="'删除分类 '+item.name" @click="deleteCategory(item)">×</button></div>
      </nav>
      <div class="sidebar-bottom"><span class="avatar">{{ user.username.slice(0,1).toUpperCase() }}</span><span class="user-info"><strong>{{ user.username }}</strong><small>{{ user.role }}</small></span><button class="icon-button signout" aria-label="退出登录" @click="logout">↗</button></div>
    </aside>
    <section class="main-area">
      <header class="topbar"><div class="breadcrumb">Workspace <span>/</span> <strong>{{ view==='monitor'?'运行监控':title }}</strong></div><div class="top-actions"><label v-if="view==='projects'" class="search-box"><span>⌕</span><input v-model="search" type="search" placeholder="搜索项目…" aria-label="搜索项目" /></label><span class="role-chip">{{ user.role }}</span></div></header>
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
        <template v-else>
          <div class="page-heading"><div><p class="eyebrow">SERVICE HEALTH</p><h1>运行监控<span class="heading-count">{{ healthChecks.length }}</span></h1><p class="page-subtitle">状态由后台定时探测更新；Agent 检查在 Agent 接入前显示不可达。</p></div><button v-if="isAdmin" class="button primary" aria-label="添加监控" @click="monitorOpen=true;monitorProject=projects[0]?.id||''">＋ 添加监控</button></div>
          <div class="monitor-connection"><span class="connection-dot" :class="{online:eventConnected}"></span>{{ eventConnected?'实时事件已连接':'实时连接中断，使用 15 秒轮询' }}</div>
          <div v-if="healthChecks.length" class="monitor-list"><article v-for="check in healthChecks" :key="check.id" class="monitor-card">
            <div class="monitor-main"><span class="state-badge" :class="'state-'+check.state">{{ stateLabel(check.state) }}</span><div class="monitor-target"><strong>{{ projects.find(p=>p.id===check.project_id)?.name || '未知项目' }}</strong><code>{{ check.target }}</code></div><div class="monitor-meta"><span>{{ check.kind.toUpperCase() }}</span><span>每 {{ check.interval_seconds }} 秒</span><span>{{ check.last_checked_at?new Date(check.last_checked_at*1000).toLocaleTimeString():'尚未检测' }}</span></div><div class="monitor-actions"><button class="button secondary small" @click="toggleSamples(check)">{{ selectedSamples===check.id?'收起记录':'检测记录' }}</button><button v-if="isAdmin" class="icon-button" :aria-label="'删除监控 '+check.target" @click="removeMonitor(check)">×</button></div></div>
            <ul v-if="selectedSamples===check.id" class="sample-list"><li v-for="sample in samples" :key="sample.id"><span>{{ new Date(sample.checked_at*1000).toLocaleString() }}</span><strong>{{ stateLabel(sample.state) }}</strong><span>{{ sample.latency_ms===null?'—':sample.latency_ms+' ms' }}</span><small>{{ sample.detail || '探测成功' }}</small></li><li v-if="!samples.length">暂无检测记录</li></ul>
          </article></div>
          <section v-else class="empty-state"><div class="empty-icon">◉</div><h2>还没有监控目标</h2><p>{{ isAdmin?'添加 HTTP、TCP 或 Agent 目标后，后台会按设定周期进行检测。':'请管理员为项目添加健康检查。' }}</p></section>
          <footer class="page-footer">Project Hub <span>·</span> 健康检查与状态历史</footer>
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
      <form class="editor-form monitor-form" @submit.prevent="addMonitor"><label>关联项目<select v-model="monitorProject" required><option value="" disabled>选择项目</option><option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</option></select></label><label>检查类型<select v-model="monitorKind"><option value="http">HTTP(S)</option><option value="tcp">TCP</option><option value="agent">Agent</option></select></label><label>目标地址<input v-model="monitorTarget" aria-label="目标地址" :type="monitorKind==='http'?'url':'text'" maxlength="2048" placeholder="https://service.example/health" required /></label><p class="form-hint">HTTP/TCP 探测应用目标网络策略；Agent 目标使用已授权的 Agent ID。</p><div class="modal-actions"><button class="button secondary" type="button" @click="monitorOpen=false">取消</button><button class="button primary" :disabled="busy||!projects.length">{{ busy?'保存中…':'创建监控' }}</button></div></form>
    </section></div>
  </div>
</template>
<style>@import './style.css';</style>
