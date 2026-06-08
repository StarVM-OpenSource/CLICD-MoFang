import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Check,
  ChevronDown,
  ChevronUp,
  Copy,
  Edit3,
  Key,
  Plus,
  RefreshCw,
  ShieldCheck,
  Trash2,
  X,
} from 'lucide-react'
import api, { APIResponse, Container } from '../services/api'
import { copyToClipboard } from '../utils/clipboard'

interface ApiKeyItem {
  id: string
  name: string
  key?: string
  prefix: string
  ip_whitelist: string
  created_at: string
  last_used: string
  scopes?: string[]
  expires_at?: string
  disabled?: boolean
  container_uuids?: string[]
  last_used_ip?: string
}

interface ApiKeyForm {
  name: string
  ipWhitelist: string
  scopes: string[]
  expiresAt: string
  disabled: boolean
  containerUUIDs: string[]
}

const BASE_URL = window.location.origin

const scopeGroups = [
  {
    title: '总览与只读',
    scopes: [
      ['dashboard:read', '控制面板'],
      ['host:read', '主机资源'],
      ['routing:read', '路由信息'],
      ['ipv6:read', 'IPv6 状态'],
      ['task:read', '任务列表'],
      ['image:read', '镜像列表'],
    ],
  },
  {
    title: '容器',
    scopes: [
      ['container:read', '查看容器'],
      ['container:create', '创建容器'],
      ['container:power', '开关机/重启'],
      ['container:reinstall', '重装系统'],
      ['container:delete', '删除容器'],
      ['container:resize', '资源/到期'],
      ['container:traffic', '流量管理'],
      ['container:network', '端口映射'],
      ['container:password', '重置密码'],
      ['ipv6:assign', '分配 IPv6'],
    ],
  },
  {
    title: '快照与终端',
    scopes: [
      ['snapshot:read', '查看快照'],
      ['snapshot:create', '创建快照'],
      ['snapshot:delete', '删除快照'],
      ['snapshot:restore', '恢复快照'],
      ['snapshot:schedule', '计划/配额'],
      ['terminal:ssh', 'WebSSH 票据'],
      ['terminal:vnc', 'WebVNC 票据'],
    ],
  },
  {
    title: '平台管理',
    scopes: [
      ['image:download', '下载镜像'],
      ['image:delete', '删除镜像'],
      ['image:toggle', '启停镜像'],
      ['security:read', '安全数据'],
      ['security:check', '安全扫描'],
      ['security:settings', '安全设置'],
      ['swap:read', 'Swap 信息'],
      ['swap:manage', 'Swap 管理'],
      ['subuser:read', '子用户列表'],
      ['subuser:create', '创建子用户'],
      ['subuser:update', '更新子用户'],
      ['audit:read', '操作日志'],
      ['loginlog:read', '登录日志'],
      ['apikey:read', 'Key 列表'],
      ['apikey:create', '创建 Key'],
      ['apikey:update', '更新 Key'],
      ['apikey:delete', '删除 Key'],
      ['admin:access', '管理员接口'],
    ],
  },
]

const defaultReadScopes = [
  'dashboard:read',
  'container:read',
  'task:read',
  'image:read',
  'snapshot:read',
  'routing:read',
  'ipv6:read',
  'host:read',
]

const endpointGroups = [
  {
    title: '总览',
    endpoints: [
      ['GET', '/api/v1/dashboard', '控制面板统计'],
      ['GET', '/api/v1/host-info', '主机资源'],
      ['GET', '/api/v1/routing', 'NAT/IPv6 路由'],
      ['GET', '/api/v1/ipv6/status', 'IPv6 状态'],
      ['GET', '/api/v1/tasks', '任务队列'],
      ['DELETE', '/api/v1/tasks/{task_id}', '删除任务'],
    ],
  },
  {
    title: '容器',
    endpoints: [
      ['GET', '/api/v1/containers', '容器列表'],
      ['POST', '/api/v1/containers', '创建容器'],
      ['GET', '/api/v1/containers/{id|uuid|name}', '容器详情'],
      ['POST', '/api/v1/containers/{id}/start', '开机'],
      ['POST', '/api/v1/containers/{id}/stop', '关机'],
      ['POST', '/api/v1/containers/{id}/restart', '重启'],
      ['POST', '/api/v1/containers/{id}/reinstall', '重装'],
      ['DELETE', '/api/v1/containers/{id}/delete', '删除'],
      ['GET', '/api/v1/containers/{id}/usage', '资源用量'],
      ['GET', '/api/v1/containers/{id}/traffic', '流量统计'],
      ['POST', '/api/v1/containers/{id}/traffic-reset', '重置流量'],
      ['PUT', '/api/v1/containers/{id}/traffic-limit', '调整流量限制'],
      ['PUT', '/api/v1/containers/{id}/resource-limit', '调整资源限制'],
      ['PUT', '/api/v1/containers/{id}/expiry', '调整到期时间'],
      ['POST', '/api/v1/containers/{id}/reset-password', '重置 SSH 密码'],
      ['POST', '/api/v1/containers/{id}/ipv6', '分配 IPv6'],
    ],
  },
  {
    title: '端口与快照',
    endpoints: [
      ['GET', '/api/v1/containers/{id}/random-port', '随机可用端口'],
      ['POST', '/api/v1/containers/{id}/port-mappings', '添加端口映射'],
      ['PUT', '/api/v1/containers/{id}/port-mappings/{index}', '更新端口映射'],
      ['DELETE', '/api/v1/containers/{id}/port-mappings/{index}', '删除端口映射'],
      ['GET', '/api/v1/snapshots', '快照总览'],
      ['GET', '/api/v1/containers/{id}/snapshots', '容器快照'],
      ['POST', '/api/v1/containers/{id}/snapshots', '创建快照'],
      ['DELETE', '/api/v1/containers/{id}/snapshots/{snapshot_id}', '删除快照'],
      ['POST', '/api/v1/containers/{id}/snapshots/{snapshot_id}/restore', '恢复快照'],
      ['POST', '/api/v1/containers/{id}/snapshots/schedule', '计划快照'],
      ['PUT', '/api/v1/containers/{id}/snapshots/quota', '快照配额'],
    ],
  },
  {
    title: '平台管理',
    endpoints: [
      ['GET', '/api/v1/templates', '模板列表'],
      ['GET', '/api/v1/images', '镜像管理列表'],
      ['POST', '/api/v1/images/download', '下载镜像'],
      ['POST', '/api/v1/images/cancel', '取消镜像下载'],
      ['DELETE', '/api/v1/images/delete', '删除镜像缓存'],
      ['PUT', '/api/v1/images/toggle', '启用/禁用镜像'],
      ['GET', '/api/v1/security/alerts', '安全告警'],
      ['POST', '/api/v1/security/check', '立即安全检查'],
      ['GET', '/api/v1/security/logs?container={name}', '安全连接日志'],
      ['GET', '/api/v1/security/summary', '安全汇总'],
      ['GET', '/api/v1/security/settings', '安全设置'],
      ['PUT', '/api/v1/security/settings', '更新安全设置'],
      ['GET', '/api/v1/swap', 'Swap 信息'],
      ['POST', '/api/v1/swap', '调整 Swap'],
      ['POST', '/api/v1/batch-create', '批量创建容器'],
      ['POST', '/api/v1/batch-action', '批量开关机/删除/重装'],
      ['POST', '/api/v1/ssh-ticket', '创建 WebSSH 票据'],
      ['POST', '/api/v1/vnc-ticket', '创建 WebVNC 票据'],
    ],
  },
  {
    title: '账号与日志',
    endpoints: [
      ['POST', '/api/v1/sub-user/create', '创建子用户链接'],
      ['GET', '/api/v1/sub-users', '子用户列表'],
      ['POST', '/api/v1/sub-users/{id}/rotate-password', '轮换子用户密码'],
      ['GET', '/api/v1/sub-users/{id}/audit-logs', '子用户操作日志'],
      ['GET', '/api/v1/sub-users/{id}/login-logs', '子用户登录日志'],
      ['GET', '/api/v1/audit-logs', '操作日志'],
      ['GET', '/api/v1/login-logs', '登录日志'],
      ['GET', '/api/v1/api-keys', 'API Key 列表'],
      ['POST', '/api/v1/api-keys', '创建 API Key'],
      ['PATCH', '/api/v1/api-keys/{id}', '更新 API Key'],
      ['DELETE', '/api/v1/api-keys/{id}', '删除 API Key'],
    ],
  },
]

const emptyForm = (): ApiKeyForm => ({
  name: '',
  ipWhitelist: '',
  scopes: [...defaultReadScopes],
  expiresAt: '',
  disabled: false,
  containerUUIDs: [],
})

export default function ApiIntegration() {
  const [keys, setKeys] = useState<ApiKeyItem[]>([])
  const [containers, setContainers] = useState<Container[]>([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [editingKey, setEditingKey] = useState<ApiKeyItem | null>(null)
  const [form, setForm] = useState<ApiKeyForm>(emptyForm)
  const [saving, setSaving] = useState(false)
  const [newKey, setNewKey] = useState('')
  const [copiedKey, setCopiedKey] = useState(false)
  const [showDocs, setShowDocs] = useState(true)

  const containerNameByUUID = useMemo(() => {
    const map = new Map<string, string>()
    containers.forEach(c => map.set(c.uuid, c.name))
    return map
  }, [containers])

  const fetchData = useCallback(async () => {
    setLoading(true)
    try {
      const [keyRes, containerRes] = await Promise.all([
        api.get<APIResponse<ApiKeyItem[]>>('/api-keys'),
        api.get<APIResponse<Container[]>>('/containers'),
      ])
      setKeys(keyRes.data.data || [])
      setContainers(containerRes.data.data || [])
    } catch {
      // keep the page usable if one request fails
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  const openCreate = () => {
    setEditingKey(null)
    setForm(emptyForm())
    setShowForm(true)
  }

  const openEdit = (item: ApiKeyItem) => {
    setEditingKey(item)
    setForm({
      name: item.name,
      ipWhitelist: item.ip_whitelist || '',
      scopes: item.scopes?.length ? item.scopes : ['*'],
      expiresAt: toDateTimeLocal(item.expires_at || ''),
      disabled: Boolean(item.disabled),
      containerUUIDs: item.container_uuids || [],
    })
    setShowForm(true)
  }

  const saveKey = async () => {
    if (!form.name.trim()) return
    setSaving(true)
    const payload = {
      name: form.name.trim(),
      ip_whitelist: form.ipWhitelist.trim(),
      scopes: form.scopes,
      expires_at: fromDateTimeLocal(form.expiresAt),
      disabled: form.disabled,
      container_uuids: form.containerUUIDs,
    }
    try {
      if (editingKey) {
        const res = await api.patch<APIResponse<ApiKeyItem>>(`/api-keys/${editingKey.id}`, payload)
        if (res.data.data) {
          setKeys(prev => prev.map(k => (k.id === editingKey.id ? res.data.data! : k)))
        }
      } else {
        const res = await api.post<APIResponse<ApiKeyItem>>('/api-keys', payload)
        if (res.data.data) {
          setKeys(prev => [res.data.data!, ...prev])
          if (res.data.data.key) setNewKey(res.data.data.key)
        }
      }
      setShowForm(false)
    } catch {
      // axios interceptor handles auth; form stays open
    } finally {
      setSaving(false)
    }
  }

  const deleteKey = async (id: string) => {
    if (!window.confirm('确定删除这个 API Key 吗？')) return
    try {
      await api.delete(`/api-keys/${id}`)
      setKeys(prev => prev.filter(k => k.id !== id))
    } catch {
      // ignore
    }
  }

  const copyKey = async () => {
    const copied = await copyToClipboard(newKey)
    if (copied) {
      setCopiedKey(true)
      setTimeout(() => setCopiedKey(false), 1600)
    }
  }

  const toggleScope = (scope: string) => {
    setForm(prev => {
      if (scope === '*') {
        return { ...prev, scopes: prev.scopes.includes('*') ? [...defaultReadScopes] : ['*'] }
      }
      const withoutAll = prev.scopes.filter(s => s !== '*')
      const scopes = withoutAll.includes(scope)
        ? withoutAll.filter(s => s !== scope)
        : [...withoutAll, scope]
      return { ...prev, scopes: scopes.length ? scopes : [...defaultReadScopes] }
    })
  }

  const toggleContainer = (uuid: string) => {
    setForm(prev => ({
      ...prev,
      containerUUIDs: prev.containerUUIDs.includes(uuid)
        ? prev.containerUUIDs.filter(item => item !== uuid)
        : [...prev.containerUUIDs, uuid],
    }))
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold text-black">API 集成</h1>
          <p className="mt-1 text-sm text-gray-500">管理外部调用凭据、权限范围与平台 API 文档</p>
        </div>
        <button
          onClick={openCreate}
          className="inline-flex items-center gap-1.5 rounded-md bg-black px-3 py-2 text-sm text-white hover:bg-gray-800"
        >
          <Plus className="h-4 w-4" />
          创建 Key
        </button>
      </div>

      {newKey && (
        <div className="rounded-lg border border-amber-200 bg-amber-50 p-4">
          <div className="mb-3 flex items-center justify-between gap-3">
            <div className="text-sm font-semibold text-amber-800">新的 API Key 已生成</div>
            <button onClick={() => setNewKey('')} className="rounded p-1 text-amber-700 hover:bg-amber-100" title="关闭">
              <X className="h-4 w-4" />
            </button>
          </div>
          <div className="flex flex-col gap-2 sm:flex-row">
            <code className="min-w-0 flex-1 break-all rounded border border-amber-300 bg-white px-3 py-2 font-mono text-xs text-gray-800">
              {newKey}
            </code>
            <button
              onClick={copyKey}
              className="inline-flex items-center justify-center gap-1.5 rounded-md bg-amber-600 px-3 py-2 text-xs text-white hover:bg-amber-700"
            >
              {copiedKey ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              {copiedKey ? '已复制' : '复制'}
            </button>
          </div>
        </div>
      )}

      <div className="rounded-lg border border-gray-200 bg-white">
        <div className="flex items-center justify-between gap-3 border-b border-gray-200 px-5 py-4">
          <h2 className="flex items-center gap-2 text-sm font-semibold text-black">
            <Key className="h-4 w-4" />
            API Keys
          </h2>
          <button onClick={fetchData} className="rounded p-1.5 text-gray-400 hover:text-black" title="刷新">
            <RefreshCw className="h-4 w-4" />
          </button>
        </div>

        {loading ? (
          <div className="py-10 text-center text-sm text-gray-400">加载中...</div>
        ) : keys.length === 0 ? (
          <div className="py-10 text-center text-sm text-gray-400">暂无 API Key</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-100 text-left text-xs font-medium text-gray-500">
                  <th className="px-4 py-3">名称</th>
                  <th className="px-4 py-3">权限</th>
                  <th className="px-4 py-3">绑定容器</th>
                  <th className="px-4 py-3">限制</th>
                  <th className="px-4 py-3">最后使用</th>
                  <th className="px-4 py-3 text-right">操作</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {keys.map(item => (
                  <tr key={item.id} className="hover:bg-gray-50">
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-gray-900">{item.name}</span>
                        {item.disabled && (
                          <span className="rounded bg-red-50 px-1.5 py-0.5 text-[10px] font-medium text-red-600">已禁用</span>
                        )}
                      </div>
                      <div className="mt-1 font-mono text-xs text-gray-400">{item.prefix}</div>
                    </td>
                    <td className="px-4 py-3">
                      <ScopeSummary scopes={item.scopes || ['*']} />
                    </td>
                    <td className="px-4 py-3 text-xs text-gray-500">
                      {item.container_uuids?.length
                        ? item.container_uuids.map(uuid => containerNameByUUID.get(uuid) || uuid).join('、')
                        : '全部容器'}
                    </td>
                    <td className="px-4 py-3 text-xs text-gray-500">
                      <div>{item.ip_whitelist ? 'IP 白名单' : '不限 IP'}</div>
                      <div>{item.expires_at ? `到期 ${item.expires_at}` : '长期有效'}</div>
                    </td>
                    <td className="px-4 py-3 text-xs text-gray-500">
                      <div>{item.last_used || '从未使用'}</div>
                      {item.last_used_ip && <div className="font-mono text-[11px] text-gray-400">{item.last_used_ip}</div>}
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex justify-end gap-1">
                        <button onClick={() => openEdit(item)} className="rounded p-1.5 text-gray-400 hover:text-black" title="编辑">
                          <Edit3 className="h-3.5 w-3.5" />
                        </button>
                        <button onClick={() => deleteKey(item.id)} className="rounded p-1.5 text-gray-400 hover:text-red-600" title="删除">
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <div className="rounded-lg border border-gray-200 bg-white">
        <button
          onClick={() => setShowDocs(value => !value)}
          className="flex w-full items-center justify-between gap-3 border-b border-gray-200 px-5 py-4 text-left"
        >
          <h2 className="flex items-center gap-2 text-sm font-semibold text-black">
            <ShieldCheck className="h-4 w-4" />
            API 文档
          </h2>
          {showDocs ? <ChevronUp className="h-4 w-4 text-gray-400" /> : <ChevronDown className="h-4 w-4 text-gray-400" />}
        </button>

        {showDocs && (
          <div className="space-y-6 p-5">
            <div className="rounded-lg bg-gray-900 p-4 font-mono text-xs text-gray-100">
              <div>curl -H "X-API-Key: clicd_sk_xxxx" {BASE_URL}/api/v1/containers</div>
              <div className="mt-2 text-gray-400">curl -H "Authorization: Bearer clicd_sk_xxxx" {BASE_URL}/api/v1/dashboard</div>
            </div>

            {endpointGroups.map(group => (
              <section key={group.title}>
                <h3 className="mb-2 text-sm font-semibold text-black">{group.title}</h3>
                <div className="overflow-hidden rounded-lg border border-gray-200">
                  {group.endpoints.map(([method, path, desc]) => (
                    <div key={`${method}-${path}`} className="grid gap-2 border-b border-gray-100 px-3 py-2 text-xs last:border-b-0 md:grid-cols-[72px_minmax(280px,1fr)_180px]">
                      <span className="w-fit rounded border border-blue-200 bg-blue-50 px-1.5 py-0.5 font-mono font-bold text-blue-700">{method}</span>
                      <code className="min-w-0 break-all font-mono text-gray-800">{path}</code>
                      <span className="text-gray-500">{desc}</span>
                    </div>
                  ))}
                </div>
              </section>
            ))}
          </div>
        )}
      </div>

      {showForm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
          <div className="absolute inset-0 bg-black/50" onClick={() => setShowForm(false)} />
          <div className="relative flex max-h-[90vh] w-full max-w-4xl flex-col overflow-hidden rounded-lg border border-gray-200 bg-white shadow-xl">
            <div className="flex items-center justify-between gap-3 border-b border-gray-200 px-5 py-4">
              <h3 className="text-base font-semibold text-black">{editingKey ? '编辑 API Key' : '创建 API Key'}</h3>
              <button onClick={() => setShowForm(false)} className="rounded p-1 text-gray-400 hover:text-black" title="关闭">
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto p-5">
              <div className="grid gap-5 lg:grid-cols-[1fr_1.2fr]">
                <div className="space-y-4">
                  <label className="block">
                    <span className="mb-1 block text-xs text-gray-500">名称</span>
                    <input
                      value={form.name}
                      onChange={e => setForm(prev => ({ ...prev, name: e.target.value }))}
                      className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
                      placeholder="CI/CD、计费系统、自动化脚本"
                    />
                  </label>

                  <label className="block">
                    <span className="mb-1 block text-xs text-gray-500">IP 白名单</span>
                    <textarea
                      value={form.ipWhitelist}
                      onChange={e => setForm(prev => ({ ...prev, ipWhitelist: e.target.value }))}
                      rows={4}
                      className="w-full resize-none rounded-md border border-gray-300 px-3 py-2 font-mono text-sm"
                      placeholder={`1.2.3.4\n10.0.0.0/24`}
                    />
                  </label>

                  <label className="block">
                    <span className="mb-1 block text-xs text-gray-500">过期时间</span>
                    <input
                      type="datetime-local"
                      value={form.expiresAt}
                      onChange={e => setForm(prev => ({ ...prev, expiresAt: e.target.value }))}
                      className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
                    />
                  </label>

                  <label className="flex items-center gap-2 text-sm text-gray-700">
                    <input
                      type="checkbox"
                      checked={form.disabled}
                      onChange={e => setForm(prev => ({ ...prev, disabled: e.target.checked }))}
                      className="h-4 w-4 accent-black"
                    />
                    禁用这个 Key
                  </label>

                  <div>
                    <div className="mb-2 text-xs text-gray-500">绑定容器</div>
                    <div className="max-h-48 space-y-1 overflow-y-auto rounded-md border border-gray-200 p-2">
                      <label className="flex items-center gap-2 rounded px-2 py-1.5 text-sm text-gray-700 hover:bg-gray-50">
                        <input
                          type="checkbox"
                          checked={form.containerUUIDs.length === 0}
                          onChange={() => setForm(prev => ({ ...prev, containerUUIDs: [] }))}
                          className="h-4 w-4 accent-black"
                        />
                        全部容器
                      </label>
                      {containers.map(container => (
                        <label key={container.uuid} className="flex items-center gap-2 rounded px-2 py-1.5 text-sm text-gray-700 hover:bg-gray-50">
                          <input
                            type="checkbox"
                            checked={form.containerUUIDs.includes(container.uuid)}
                            onChange={() => toggleContainer(container.uuid)}
                            className="h-4 w-4 accent-black"
                          />
                          <span className="min-w-0 truncate">{container.name}</span>
                          <span className="shrink-0 font-mono text-[10px] text-gray-400">{container.uuid.slice(0, 8)}</span>
                        </label>
                      ))}
                    </div>
                  </div>
                </div>

                <div>
                  <div className="mb-2 flex items-center justify-between gap-3">
                    <div className="text-xs text-gray-500">权限范围</div>
                    <button onClick={() => toggleScope('*')} className="rounded border border-gray-200 px-2 py-1 text-xs text-gray-600 hover:bg-gray-50">
                      {form.scopes.includes('*') ? '取消全权限' : '全权限'}
                    </button>
                  </div>
                  <div className="space-y-4">
                    {scopeGroups.map(group => (
                      <div key={group.title}>
                        <div className="mb-2 text-xs font-medium text-gray-700">{group.title}</div>
                        <div className="grid gap-2 sm:grid-cols-2">
                          {group.scopes.map(([scope, label]) => (
                            <label key={scope} className="flex items-center gap-2 rounded border border-gray-200 px-2 py-2 text-xs text-gray-700 hover:bg-gray-50">
                              <input
                                type="checkbox"
                                checked={form.scopes.includes('*') || form.scopes.includes(scope)}
                                disabled={form.scopes.includes('*')}
                                onChange={() => toggleScope(scope)}
                                className="h-4 w-4 accent-black"
                              />
                              <span className="min-w-0 flex-1 truncate">{label}</span>
                              <code className="hidden shrink-0 font-mono text-[10px] text-gray-400 sm:block">{scope}</code>
                            </label>
                          ))}
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </div>
            <div className="flex justify-end gap-2 border-t border-gray-200 px-5 py-4">
              <button onClick={() => setShowForm(false)} className="rounded-md border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50">
                取消
              </button>
              <button
                onClick={saveKey}
                disabled={saving || !form.name.trim()}
                className="rounded-md bg-black px-4 py-2 text-sm text-white hover:bg-gray-800 disabled:opacity-50"
              >
                {saving ? '保存中...' : '保存'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function ScopeSummary({ scopes }: { scopes: string[] }) {
  if (scopes.includes('*')) {
    return <span className="rounded bg-red-50 px-2 py-1 text-xs font-medium text-red-600">全权限</span>
  }
  const visible = scopes.slice(0, 3)
  return (
    <div className="flex max-w-xs flex-wrap gap-1">
      {visible.map(scope => (
        <span key={scope} className="rounded bg-gray-100 px-1.5 py-0.5 font-mono text-[10px] text-gray-600">
          {scope}
        </span>
      ))}
      {scopes.length > visible.length && (
        <span className="rounded bg-gray-100 px-1.5 py-0.5 text-[10px] text-gray-500">+{scopes.length - visible.length}</span>
      )}
    </div>
  )
}

function toDateTimeLocal(value: string) {
  if (!value) return ''
  return value.replace(' ', 'T').slice(0, 16)
}

function fromDateTimeLocal(value: string) {
  if (!value) return ''
  return `${value.replace('T', ' ')}:00`
}
