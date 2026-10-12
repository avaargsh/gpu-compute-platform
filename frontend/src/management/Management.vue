<template>
  <div class="management-shell">
    <header class="management-header">
      <div><small>GPU COMPUTE · ISOLATED MANAGEMENT</small><h1>计算资源管理 · 本机试用</h1></div>
      <span class="management-warning">LOCALHOST ONLY · NOT FOR PUBLIC DEPLOYMENT</span>
    </header>
    <p class="management-note">此工作台独立于原只读 Portal。浏览器使用 OAuth S256 PKCE 获取访问令牌；服务器重新校验令牌与 PostgreSQL 项目授权。这里只证明 Desired 状态操作，不代表 Kueue 准入或 GPU 执行成功。</p>

    <section class="management-card">
      <h2>1. 登录与身份</h2>
      <p>身份提供商、公开客户端 ID 由部署时配置；Token 仅保留在页面内存中，刷新后需重新登录。禁止将机密客户端 Secret 写入浏览器。</p>
      <div class="management-status" v-if="!configured">尚未配置 OIDC：请设置 <code>VITE_MANAGEMENT_OIDC_ISSUER</code> 和 <code>VITE_MANAGEMENT_OIDC_CLIENT_ID</code> 后重新启动本机前端。</div>
      <div class="management-status" v-else>Issuer: <code>{{ issuer }}</code> · {{ authenticated ? '已取得短期访问令牌（服务端仍逐次验证）' : '尚未登录' }}</div>
      <div class="management-actions">
        <button type="button" :disabled="!configured || busy" @click="signIn">通过 OIDC 登录</button>
        <button type="button" :disabled="!authenticated" @click="signOut">清除本页令牌</button>
      </div>
    </section>

    <section class="management-card">
      <h2>2. 项目与资源</h2>
      <div class="management-actions">
        <button type="button" :disabled="!authenticated || busy" @click="loadProjects">查询当前账号的项目</button>
      </div>
      <p>下列项目与角色来自服务端当前 OIDC 身份、PostgreSQL 授权和项目绑定；点击选择，不依赖浏览器声明角色。</p>
      <div v-if="availableProjects.length" class="management-actions">
        <button v-for="item in availableProjects" :key="item.projectId + '/' + item.clusterId"
          type="button" :disabled="busy" @click="selectProject(item)">
          使用 {{ item.projectId }} ({{ item.clusterId }}) · {{ item.role }}
        </button>
      </div>
      <p v-if="projectListTruncated">只显示前 100 个项目，列表不完整。请联系管理员或手动输入授权项目。</p>
      <div class="management-fields">
        <label>Cluster ID<input v-model.trim="cluster" autocomplete="off" placeholder="cluster-a" /></label>
        <label>Project ID<input v-model.trim="project" autocomplete="off" placeholder="research" /></label>
        <label>类型<select v-model="kind"><option value="ComputePool">ComputePool</option><option value="Workload">Workload</option></select></label>
        <label>资源 ID<input v-model.trim="resource" autocomplete="off" placeholder="pool-a" /></label>
      </div>
      <div class="management-actions">
        <button type="button" :disabled="!authenticated || busy" @click="read">从 PostgreSQL 读取</button>
      </div>
      <div v-if="desired" class="management-result">
        <div>Generation: <strong>{{ desired.generation }}</strong> · 删除请求: {{ desired.deleting ? '已发起' : '未发起' }}</div>
        <pre>{{ JSON.stringify(desired, null, 2) }}</pre>
      </div>
      <p>读取的是已授权项目的单个 Desired 资源，不包含独立 Observed / Ready 证明。</p>
    </section>

    <section class="management-card">
      <h2>3. 创建或修改 Desired 资源</h2>
      <label>JSON 请求体（必须包含 expectedGeneration 和 spec；Workload 还需 poolId）
        <textarea v-model="requestJSON" rows="12" spellcheck="false"></textarea>
      </label>
      <p>新建时 expectedGeneration=0；更新已有 ComputePool 时使用读取到的 Generation。已有 Workload 不允许原地更新。</p>
      <div class="management-actions">
        <button type="button" :disabled="!authenticated || busy" @click="write">确认并提交 PUT</button>
        <button type="button" class="management-danger" :disabled="!authenticated || busy || !desired || desired.deleting" @click="remove">确认并发起删除请求</button>
      </div>
      <p>删除操作使用当前选中资源的 Generation 和 If-Match；服务端会检查项目角色、资源依赖与 Finalizer，HTTP 202 不等于实际删除完成。</p>
    </section>

    <section class="management-card">
      <h2>4. 操作回执与恢复</h2>
      <div v-if="recentOperation" class="management-status">
        最近一次操作（仅当前浏览器标签）：
        <code>{{ recentOperation.method }} {{ recentOperation.project }}/{{ recentOperation.kind }}/{{ recentOperation.resource }}</code>
        · <code>{{ recentOperation.operationId }}</code>
        <div class="management-actions">
          <button type="button" :disabled="busy" @click="restoreOperation">恢复操作范围与 ID</button>
          <button type="button" :disabled="busy" @click="discardOperation">清除此操作记录</button>
        </div>
      </div>
      <label>操作 ID（128 位随机十六进制；断连时必须使用原 ID）<input v-model.trim="operationId" autocomplete="off" spellcheck="false" placeholder="原始 Idempotency-Key" /></label>
      <div class="management-actions">
        <button type="button" :disabled="!authenticated || busy" @click="receipt">查询已提交回执</button>
        <button type="button" :disabled="!operationId" @click="copyOperationId">复制操作 ID</button>
      </div>
      <p>发送写请求前会将最近一次操作 ID 与项目/资源范围保存到本标签 sessionStorage，最多保留 24 小时，绝不保存 Token、JSON 请求体或密码。刷新后重新登录，再恢复操作范围查询回执。仅保留最近一条，关闭标签、退出登录、存储失效或新操作都可能使其丢失；请另外安全保存关键操作 ID。</p>
      <p>不会自动重试；断网或 HTTP 503 后先查询回执。HTTP 404 不能证明不存在正在提交的事务。只有能恢复完全相同的原请求体、操作 ID 和 CAS 条件时，才能人工重发；不要利用新的 ID 猜测提交结果。</p>
      <pre v-if="receiptData">{{ JSON.stringify(receiptData, null, 2) }}</pre>
    </section>
    <div v-if="message" class="management-feedback" role="status">{{ message }}</div>
    <div v-if="error" class="management-error" role="alert">{{ error }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { beginLogin, clearPendingLogin, discover, finishLogin, type OAuthConfig, type AccessSession } from './auth'
import { readResource, writeResource, deleteResource, readReceipt, listProjects, type Kind, type DesiredView, type Receipt, type ProjectAccess, desiredMatchesScope, createOperationId } from './api'
import { rememberOperation, recoverOperation, forgetOperation, type OperationPointer } from './recovery'

const issuer = import.meta.env.VITE_MANAGEMENT_OIDC_ISSUER || ''
const clientId = import.meta.env.VITE_MANAGEMENT_OIDC_CLIENT_ID || ''
const config: OAuthConfig = { issuer, clientId }
const configured = Boolean(issuer && clientId)
const session = ref<AccessSession | null>(null)
const authenticated = computed(() => !!session.value && session.value.expiresAt > Date.now())
const cluster = ref('')
const project = ref('')
const resource = ref('')
const kind = ref<Kind>('ComputePool')
const requestJSON = ref(JSON.stringify({
  expectedGeneration: 0,
  spec: {
    accelerators: [{ class: 'h100', quota: 2 }],
    acceleratorBindings: [{ class: 'h100', resourceName: 'nvidia.com/gpu', flavor: 'h100' }],
    scheduling: { mode: 'kueue' },
  },
}, null, 2))
const desired = ref<DesiredView | null>(null)
const availableProjects = ref<ProjectAccess[]>([])
const projectListTruncated = ref(false)
const receiptData = ref<Receipt | null>(null)
const operationId = ref('')
const recentOperation = ref<OperationPointer | null>(null)
const busy = ref(false)
const message = ref('')
const error = ref('')

function redirectUri() { return window.location.origin + '/management.html' }
function token() {
  if (!authenticated.value || !session.value) throw new Error('需要重新登录')
  return session.value.accessToken
}
function scope() { return { cluster: cluster.value, project: project.value, kind: kind.value, resource: resource.value } }
function clearResult() { error.value = ''; message.value = ''; receiptData.value = null }
function describeError(e: unknown) { error.value = e instanceof Error ? e.message : '未知请求失败' }

onMounted(async () => {
  // Recovery pointer is metadata only; a browser reload always discards
  // the bearer and demands a new OIDC sign-in for receipt queries.
  recentOperation.value = recoverOperation(sessionStorage)
  const params = new URLSearchParams(window.location.search)
  if (!params.has('code') && !params.has('error')) return
  // Strip authorization code from location *before any exchange*.
  const callback = window.location.search
  window.history.replaceState({}, '', window.location.pathname)
  if (!configured) { error.value = '回调失败：未配置身份提供商'; return }
  busy.value = true
  try {
    const discovery = await discover(config)
    session.value = await finishLogin(config, discovery, callback, redirectUri(), sessionStorage)
    message.value = '身份提供商返回 Access Token；管理 API 仍将执行令牌与项目权限验证'
  } catch (e) {
    clearPendingLogin(sessionStorage)
    describeError(e)
  } finally { busy.value = false }
})

async function signIn() {
  clearResult()
  busy.value = true
  try {
    const discovery = await discover(config)
    const url = await beginLogin(config, discovery, redirectUri(), sessionStorage)
    window.location.assign(url)
  } catch (e) { describeError(e); busy.value = false }
}

function signOut() {
  session.value = null
  clearPendingLogin(sessionStorage)
  forgetOperation(sessionStorage)
  recentOperation.value = null
  operationId.value = ''
  desired.value = null
  receiptData.value = null
  availableProjects.value = []
  projectListTruncated.value = false
  message.value = '本页访问令牌已清除；身份提供商 SSO 会话未注销'
}
async function loadProjects() {
  clearResult()
  busy.value = true
  try {
    const result = await listProjects(token())
    availableProjects.value = result.value?.projects ?? []
    projectListTruncated.value = result.value?.truncated ?? false
    message.value = '服务端返回 ' + availableProjects.value.length + ' 个当前授权项目'
  } catch (e) {
    availableProjects.value = []
    projectListTruncated.value = false
    describeError(e)
  } finally { busy.value = false }
}

function selectProject(item: ProjectAccess) {
  cluster.value = item.clusterId
  project.value = item.projectId
  // A scope change must invalidate any previously read Generation and
  // the prior operation receipt ID, even if the resource name is identical.
  resource.value = ''
  kind.value = 'ComputePool'
  desired.value = null
  receiptData.value = null
  operationId.value = ''
  message.value = '已选择项目 ' + item.projectId + '（' + item.role + '），请重新选择资源'
  error.value = ''
}

function restoreOperation() {
  const pointer = recoverOperation(sessionStorage)
  if (!pointer) {
    recentOperation.value = null
    error.value = '操作记录已过期，请使用外部保存的原始操作 ID'
    return
  }
  cluster.value = pointer.cluster
  project.value = pointer.project
  kind.value = pointer.kind
  resource.value = pointer.resource
  operationId.value = pointer.operationId
  desired.value = null
  receiptData.value = null
  recentOperation.value = pointer
  message.value = '已恢复操作范围和 ID；请重新登录后查询服务端回执'
  error.value = ''
}

function discardOperation() {
  forgetOperation(sessionStorage)
  recentOperation.value = null
  operationId.value = ''
  message.value = '本页操作记录已清除，服务端审计不受影响'
}

async function read() {
  clearResult(); busy.value = true
  try {
    const result = await readResource(scope(), token())
    desired.value = result.value
    message.value = '读取到授权项目内的 Desired 资源'
  } catch (e) { desired.value = null; describeError(e) }
  finally { busy.value = false }
}
async function write() {
  clearResult()
  try {
    JSON.parse(requestJSON.value)
    token()
    if (!window.confirm('确认向项目 ' + project.value + ' 提交 ' + kind.value + '/' + resource.value + ' Desired 变更？')) return
    const original = createOperationId()
    const target = scope()
    // If tab-local persistence is unavailable, DO NOT send an operation
    // whose outcome would be unknowable after a network disconnect.
    recentOperation.value = rememberOperation(sessionStorage, target, 'PUT', original)
    operationId.value = original
    busy.value = true
    // Never reuse an earlier operation ID for a *different* write.
    const result = await writeResource(target, token(), requestJSON.value, original)
    desired.value = null
    message.value = 'Desired 事务已提交（HTTP ' + result.status + '）。操作 ID: ' + original + '；可查询回执，但尚无执行成功证明'
  } catch (e) { describeError(e) }
  finally { busy.value = false }
}
async function remove() {
  clearResult()
  try {
    const current = desired.value
    if (!current || !desiredMatchesScope(current, scope())) throw new Error('资源范围已切换，请先重新读取当前资源和 Generation')
    token()
    if (!window.confirm('确认发起删除 ' + current.kind + '/' + current.id + '（Generation ' + current.generation + '）？此操作不会立即删除真实 GPU 资源。')) return
    const original = createOperationId()
    const target = scope()
    recentOperation.value = rememberOperation(sessionStorage, target, 'DELETE', original)
    operationId.value = original
    busy.value = true
    const result = await deleteResource(target, token(), current.generation, original)
    desired.value = null
    message.value = '删除请求已提交（HTTP ' + result.status + '）。请查询操作回执及后续 Agent 观察'
  } catch (e) { describeError(e) }
  finally { busy.value = false }
}
async function receipt() {
  clearResult(); busy.value = true
  try {
    const result = await readReceipt(cluster.value, project.value, operationId.value, token())
    receiptData.value = result.value
    message.value = '仅证明 Desired 写入和操作审计已提交，非 Workload 执行证明'
  } catch (e) { describeError(e) }
  finally { busy.value = false }
}
async function copyOperationId() {
  try { await navigator.clipboard.writeText(operationId.value); message.value = '操作 ID 已复制' }
  catch { error.value = '浏览器不允许复制，请手动保存操作 ID' }
}
</script>

<style scoped>
.management-shell{max-width:1040px;margin:30px auto 70px;padding:0 22px;font:14px/1.6 system-ui,-apple-system,sans-serif;color:#202a38}
.management-header{display:flex;align-items:center;justify-content:space-between;gap:16px;flex-wrap:wrap}.management-header h1{font-size:27px;margin:2px 0 14px}.management-header small{font-size:11px;letter-spacing:.1em;color:#51668d}.management-warning{font-size:11px;color:#a35610;font-weight:650}
.management-note,.management-card{border:1px solid #dde4ed;border-radius:12px;background:#fff;padding:18px 22px;margin:15px 0}.management-note{background:#fff8e9;color:#6b501e}
.management-card h2{font-size:17px;margin:0 0 10px}.management-card p{color:#607087;font-size:12px}.management-fields{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px}
label{display:block;font-size:12px;font-weight:650;color:#47566b;margin:10px 0}input,textarea,select{width:100%;border:1px solid #bbc7d6;border-radius:7px;padding:10px 12px;display:block;margin-top:6px;font:inherit;color:#202a38}textarea,pre{font-family:ui-monospace,monospace;font-size:12px}
.management-actions{display:flex;flex-wrap:wrap;gap:10px;margin:17px 0 6px}
button{background:#1956bd;color:white;border:0;border-radius:7px;padding:10px 15px;cursor:pointer}button:disabled{opacity:.4;cursor:not-allowed}.management-danger{background:#a12828}
pre{white-space:pre-wrap;overflow-wrap:anywhere;background:#f4f6f9;border-radius:8px;padding:14px;max-height:370px;overflow:auto}
.management-feedback,.management-error,.management-status{border-radius:8px;padding:10px 13px;margin:13px 0;background:#eaf5ea;color:#214c31;overflow-wrap:anywhere}
.management-error{background:#fff0f0;color:#a12121}.management-status{background:#f1f4f9;color:#34486b}
@media(max-width:700px){.management-fields{grid-template-columns:1fr}}
</style>
