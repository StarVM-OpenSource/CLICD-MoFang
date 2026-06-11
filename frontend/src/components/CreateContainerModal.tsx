import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { CalendarClock, RefreshCw, X } from 'lucide-react'
import { batchCreate, getIPv6Status, getEnabledImages, getHostInfo, CreateContainerRequest, HostInfo, IPv6Status, Template } from '../services/api'
import { useDialog } from './Dialog'
import { useLanguage, type Language } from '../contexts/LanguageContext'
import { generateSSHPassword, sshPasswordError, sshPublicKeyError, type SSHAuthMode } from '../utils/sshAuth'

interface CreateContainerModalProps {
  isOpen: boolean
  onClose: () => void
  onSuccess: (containers: CreateContainerRequest[]) => void | Promise<void>
  existingNames?: string[]
}

const defaultForm: CreateContainerRequest = {
  name: '',
  virtualization: 'lxc',
  template_id: '',
  vcpu: 1,
  cpu_percent: 100,
  ram_mb: 512,
  disk_gb: 10,
  network_bw_mbps: 0,
  monthly_traffic_gb: 0,
  traffic_mode: 'total',
  traffic_in_gb: 0,
  traffic_out_gb: 0,
  io_speed_mbps: 0,
  extra_ports: [],
  port_mapping_count: 2,
  assign_nat: true,
  snapshot_limit: 1,
  assign_ipv4: false,
  ipv4_count: 1,
  public_ipv4s: [],
  assign_ipv6: false,
  ipv6_count: 1,
  ipv6_addresses: [],
  ssh_auth_mode: 'auto_password',
  ssh_password: '',
  ssh_public_key: '',
  expires_at: '',
}

export default function CreateContainerModal({ isOpen, onClose, onSuccess, existingNames = [] }: CreateContainerModalProps) {
  const dialog = useDialog()
  const { language } = useLanguage()
  const networkText = createNetworkText[language]
  const [templates, setTemplates] = useState<Template[]>([])
  const [loading, setLoading] = useState(false)
  const [batchCount, setBatchCount] = useState(1)
  const [form, setForm] = useState<CreateContainerRequest>(defaultForm)
  const [hostInfo, setHostInfo] = useState<HostInfo | null>(null)
  const [ipv6Status, setIPv6Status] = useState<IPv6Status | null>(null)
  const [nameError, setNameError] = useState('')

  useEffect(() => {
    if (!isOpen) return

    getEnabledImages(form.virtualization)
      .then((res) => {
        const data = res.data.data || []
        setTemplates(data)
        setForm((prev) => {
          const templateID = data.some((item) => item.id === prev.template_id) ? prev.template_id : (data[0]?.id || '')
          return applyTemplateDefaults({ ...prev, template_id: templateID })
        })
      })
      .catch(console.error)

    getIPv6Status()
      .then((res) => {
        const status = res.data.data || null
        setIPv6Status(status)
        if (!status?.available) {
          setForm((prev) => ({ ...prev, assign_ipv6: false }))
        }
      })
      .catch(() => {
        setIPv6Status({ available: false, reachable: false, reason: 'IPv6 status check failed', prefixes: [] })
        setForm((prev) => ({ ...prev, assign_ipv6: false }))
      })

    getHostInfo()
      .then((res) => setHostInfo(res.data.data || null))
      .catch(() => setHostInfo(null))
  }, [isOpen, form.virtualization])

  const ipv6Available = !!ipv6Status?.available
  const ipv6Prefixes = ipv6Status?.prefixes || []
  const ipv6Prefix = ipv6Prefixes.length > 1 ? `${ipv6Prefixes.length} prefixes configured` : (ipv6Prefixes[0]?.prefix || '')
  const publicIPv4s = hostInfo?.network.public_ipv4_addresses || []
  const ipv4Available = publicIPv4s.length > 0
  const manualIPv4s = form.public_ipv4s || []
  const maxVCPU = hostInfo?.cpu.cores || 64
  const maxRAMMB = hostInfo?.ram.total_mb ? Number(hostInfo.ram.total_mb) : undefined
  const maxDiskGB = hostInfo?.disk.total_gb ? Math.max(1, Math.floor(hostInfo.disk.total_gb)) : undefined
  const resourceErrors = validateResourceInputs(form, maxVCPU, maxRAMMB, maxDiskGB)
  const natEnabled = form.assign_nat !== false
  const natPortCount = natEnabled ? Math.max(2, form.port_mapping_count || 2) : 0
  const linuxTemplate = !isWindowsTemplate(form.template_id)
  const sshAuthMode = (form.ssh_auth_mode || 'auto_password') as SSHAuthMode

  const autoPorts = useMemo(() => {
    if (!natEnabled) return []
    const count = natPortCount
    return Array.from({ length: count - 1 }, (_, index) => 22002 + index)
  }, [natEnabled, natPortCount])

  // SSH port preview (will be allocated sequentially, starting around 22000+)
  const sshPortPreview = 22000

  // Find next available batch index to avoid name conflicts
  const batchStartIndex = useMemo(() => {
    if (batchCount <= 1 || !form.name) return 1
    const prefix = `${form.name}-`
    let maxIdx = 0
    for (const existing of existingNames) {
      if (existing.startsWith(prefix)) {
        const suffix = existing.slice(prefix.length)
        const idx = parseInt(suffix, 10)
        if (!isNaN(idx) && idx > maxIdx) {
          maxIdx = idx
        }
      }
    }
    return maxIdx + 1
  }, [form.name, batchCount, existingNames])

  const handleNameChange = (value: string) => {
    setForm({ ...form, name: value })
    if (/\s/.test(value)) {
      setNameError('容器名称不能包含空格')
    } else if (value && existingNames.includes(value) && batchCount === 1) {
      setNameError('该容器名称已存在')
    } else {
      setNameError('')
    }
  }

  const handleSubmit = async () => {
    if (!form.name || !form.template_id) {
      dialog.alert('提示', '请填写容器名称并选择系统模板')
      return
    }

    if (Object.keys(resourceErrors).length > 0) {
      dialog.alert('资源配置有误', '请按红色提示修改 vCPU、内存或磁盘配置')
      return
    }

    if (!form.assign_ipv4 && !form.assign_ipv6 && form.assign_nat === false) {
      dialog.alert('提示', '请勾选任意一个可用网络')
      return
    }

    const authError = validateSSHAuthInputs(form)
    if (authError) {
      dialog.alert('登录方式有误', authError)
      return
    }

    const boundedForm = normalizeCreateForm(form)
    const wantsNAT = boundedForm.assign_nat !== false

    // Build batch of containers
    const containers: CreateContainerRequest[] = []
    const startIndex = batchStartIndex
    for (let i = 0; i < batchCount; i++) {
      const name = batchCount > 1 ? `${boundedForm.name}-${startIndex + i}` : boundedForm.name
      containers.push({
        ...boundedForm,
        name,
        assign_nat: wantsNAT,
        port_mapping_count: wantsNAT ? Math.max(2, boundedForm.port_mapping_count || 2) : 0,
        snapshot_limit: Math.max(1, boundedForm.snapshot_limit || 3),
        ipv4_count: boundedForm.assign_ipv4 ? Math.max(1, boundedForm.ipv4_count || 1) : 0,
        ipv6_count: boundedForm.assign_ipv6 ? Math.max(1, boundedForm.ipv6_count || 1) : 0,
        extra_ports: [],
      })
    }

    setLoading(true)
    try {
      await batchCreate(containers)
      await onSuccess(containers)
      onClose()
      setBatchCount(1)
      setForm({ ...defaultForm, template_id: templates[0]?.id || '' })
    } catch (err: unknown) {
      const error = err as { response?: { data?: { message?: string } } }
      dialog.alert('创建失败', error.response?.data?.message || '请稍后重试')
    } finally {
      setLoading(false)
    }
  }

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div className="bg-white rounded-lg border border-gray-200 shadow-xl w-full max-w-2xl max-h-[90vh] overflow-y-auto">
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200">
          <h2 className="text-lg font-semibold text-black">创建新容器</h2>
          <button onClick={onClose} className="p-1 hover:bg-gray-100 rounded text-gray-500" title="关闭">
            <X className="w-5 h-5" />
          </button>
        </div>

        <div className="px-6 py-4 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <Field label="容器名称">
              <input
                type="text"
                value={form.name}
                onChange={(event) => handleNameChange(event.target.value)}
                className={`${inputClass} ${nameError ? 'border-red-400 focus:ring-red-400 focus:border-red-400' : ''}`}
                placeholder="my-container"
                required
              />
              {nameError && <p className="text-xs text-red-500 mt-1">{nameError}</p>}
            </Field>
            <Field label="批量创建数量">
              <NumberInput value={batchCount} min={1} max={50} onChange={(value) => setBatchCount(Math.max(1, value || 1))} />
            </Field>
          </div>
          {batchCount > 1 && <p className="text-xs text-gray-400">将创建 {batchCount} 个容器：{form.name}-{batchStartIndex} 至 {form.name}-{batchStartIndex + batchCount - 1}</p>}

          <Field label="虚拟化架构">
            <div className="grid grid-cols-2 gap-2">
              <button
                type="button"
                onClick={() => setForm((prev) => applyTemplateDefaults({ ...prev, virtualization: 'lxc', template_id: '' }))}
                className={`rounded-md border px-3 py-2 text-sm font-medium transition-colors ${form.virtualization === 'lxc' ? 'border-black bg-black text-white' : 'border-gray-300 text-gray-700 hover:bg-gray-50'}`}
              >
                LXC 容器
              </button>
              <button
                type="button"
                onClick={() => setForm((prev) => applyTemplateDefaults({ ...prev, virtualization: 'kvm', template_id: '' }))}
                className={`rounded-md border px-3 py-2 text-sm font-medium transition-colors ${form.virtualization === 'kvm' ? 'border-black bg-black text-white' : 'border-gray-300 text-gray-700 hover:bg-gray-50'}`}
              >
                KVM 虚拟机
              </button>
            </div>
          </Field>

          <Field label="系统模板">
            {templates.length === 0 ? (
              <div className="text-sm text-amber-600 bg-amber-50 border border-amber-200 rounded-md px-3 py-2">
                暂无可用的{form.virtualization === 'kvm' ? ' KVM' : ' LXC'}系统镜像，请先在「镜像管理」中下载镜像模板。
              </div>
            ) : (
            <select
              value={form.template_id}
              onChange={(event) => setForm(applyTemplateDefaults({ ...form, template_id: event.target.value }))}
              className={inputClass}
            >
              {templates.map((template) => (
                <option key={template.id} value={template.id}>
                  {template.name}
                </option>
              ))}
            </select>
            )}

          </Field>

          {linuxTemplate && (
            <div className="rounded-md border border-gray-200 bg-white px-3 py-3 text-sm">
              <div className="mb-2 font-medium text-gray-800">登录方式</div>
              <div className="grid grid-cols-3 gap-2">
                {([
                  ['auto_password', '自动生成密码'],
                  ['password', '自定义密码'],
                  ['key', 'SSH Key'],
                ] as Array<[SSHAuthMode, string]>).map(([mode, label]) => (
                  <button
                    key={mode}
                    type="button"
                    onClick={() => setForm({ ...form, ssh_auth_mode: mode })}
                    className={`rounded-md border px-3 py-2 text-xs font-medium transition-colors ${sshAuthMode === mode ? 'border-black bg-black text-white' : 'border-gray-300 text-gray-700 hover:bg-gray-50'}`}
                  >
                    {label}
                  </button>
                ))}
              </div>
              {sshAuthMode === 'password' && (
                <div className="mt-3 flex gap-2">
                  <input
                    type="text"
                    value={form.ssh_password || ''}
                    onChange={(event) => setForm({ ...form, ssh_password: event.target.value })}
                    className={inputClass}
                    placeholder="RootPass123"
                  />
                  <button
                    type="button"
                    onClick={() => setForm({ ...form, ssh_password: generateSSHPassword() })}
                    className="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-md border border-gray-300 text-gray-600 hover:bg-gray-50"
                    title="生成密码"
                  >
                    <RefreshCw className="h-4 w-4" />
                  </button>
                </div>
              )}
              {sshAuthMode === 'key' && (
                <textarea
                  value={form.ssh_public_key || ''}
                  onChange={(event) => setForm({ ...form, ssh_public_key: event.target.value })}
                  className={`${inputClass} mt-3 min-h-20 resize-y font-mono text-xs`}
                  placeholder="ssh-ed25519 AAAA..."
                />
              )}
            </div>
          )}

          <div className={`rounded-md border px-3 py-2 text-sm ${ipv4Available ? 'border-gray-200 bg-white' : 'border-gray-200 bg-gray-50 text-gray-400'}`}>
            <label className="flex items-start gap-3">
              <input
                type="checkbox"
                checked={!!form.assign_ipv4}
                disabled={!ipv4Available}
                onChange={(event) => setForm({
                  ...form,
                  assign_ipv4: event.target.checked,
                  public_ipv4s: event.target.checked ? form.public_ipv4s : [],
                  ...(event.target.checked ? { assign_nat: false, port_mapping_count: 0, extra_ports: [] } : {}),
                })}
                className="mt-1"
              />
              <span className="min-w-0">
                <span className="block font-medium text-gray-800">{networkText.publicIPv4}</span>
                <span className="block text-xs text-gray-500">
                  {ipv4Available ? formatAllocatableIPv4Count(publicIPv4s.length, language) : networkText.noAllocatableIPv4}
                </span>
              </span>
            </label>
            {form.assign_ipv4 && (
              <div className="mt-3 space-y-3 pl-6">
                <div className="grid grid-cols-2 gap-3">
                  <label className="flex items-center gap-2 text-xs text-gray-600">
                    <input
                      type="radio"
                      checked={manualIPv4s.length === 0}
                      onChange={() => setForm({ ...form, public_ipv4s: [] })}
                    />
                    Auto assign
                  </label>
                  <Field label="IPv4 count">
                    <NumberInput
                      value={form.ipv4_count || 1}
                      min={1}
                      max={Math.max(1, publicIPv4s.length)}
                      onChange={(value) => setForm({ ...form, ipv4_count: Math.max(1, Math.round(value || 1)) })}
                    />
                  </Field>
                </div>
                <div className="space-y-1.5">
                  <label className="flex items-center gap-2 text-xs text-gray-600">
                    <input
                      type="radio"
                      checked={manualIPv4s.length > 0}
                      onChange={() => setForm({ ...form, public_ipv4s: publicIPv4s[0]?.address ? [publicIPv4s[0].address] : [], ipv4_count: 1 })}
                    />
                    Manual select
                  </label>
                  {manualIPv4s.length > 0 && (
                    <div className="grid gap-1.5 sm:grid-cols-2">
                      {publicIPv4s.map((ip) => (
                        <label key={`${ip.interface}-${ip.address}`} className="flex min-w-0 items-center gap-2 rounded border border-gray-200 px-2 py-1.5 text-xs text-gray-700">
                          <input
                            type="checkbox"
                            checked={manualIPv4s.includes(ip.address)}
                            onChange={(event) => {
                              const next = event.target.checked
                                ? [...manualIPv4s, ip.address]
                                : manualIPv4s.filter((value) => value !== ip.address)
                              setForm({ ...form, public_ipv4s: next, ipv4_count: Math.max(1, next.length || 1) })
                            }}
                          />
                          <span className="truncate font-mono">{ip.address}</span>
                          <span className="shrink-0 text-gray-400">{ip.interface}</span>
                          {ip.gateway && <span className="shrink-0 text-gray-400">gw {ip.gateway}</span>}
                        </label>
                      ))}
                    </div>
                  )}
                </div>
              </div>
            )}
          </div>

          <div className={`rounded-md border px-3 py-2 text-sm ${ipv6Available ? 'border-gray-200 bg-white' : 'border-gray-200 bg-gray-50 text-gray-400'}`}>
            <div className="flex items-start justify-between gap-3">
              <label className="flex min-w-0 flex-1 items-start gap-3">
                <input
                  type="checkbox"
                  checked={!!form.assign_ipv6}
                  disabled={!ipv6Available}
                  onChange={(event) => setForm({ ...form, assign_ipv6: event.target.checked })}
                  className="mt-1"
                />
                <span className="min-w-0">
              <span className="block font-medium text-gray-800">{networkText.publicIPv6}</span>
              <span className="block text-xs text-gray-500 truncate">
                    {ipv6Available ? `${networkText.use} ${ipv6Prefix}` : (ipv6Status?.reason || networkText.checkingIPv6Prefix)}
              </span>
                </span>
              </label>
              {form.assign_ipv6 && (
                <span className="block w-24 shrink-0">
                  <NumberInput
                    value={form.ipv6_count || 1}
                    min={1}
                    max={64}
                    onChange={(value) => setForm({ ...form, ipv6_count: Math.max(1, Math.round(value || 1)) })}
                  />
                </span>
              )}
            </div>
          </div>

          <div className="rounded-md border border-gray-200 bg-white px-3 py-2 text-sm">
            <div className="flex items-start justify-between gap-3">
              <label className="flex min-w-0 flex-1 items-start gap-3">
                <input
                  type="checkbox"
                  checked={natEnabled}
                  onChange={(event) => {
                    const checked = event.target.checked
                    setForm({
                      ...form,
                      assign_nat: checked,
                      port_mapping_count: checked ? Math.max(2, form.port_mapping_count || 2) : 0,
                      extra_ports: [],
                      ...(checked ? { assign_ipv4: false, public_ipv4s: [], ipv4_count: 0 } : {}),
                    })
                  }}
                  className="mt-1"
                />
                <span className="min-w-0">
                  <span className="block font-medium text-gray-800">{networkText.publicNAT}</span>
                  <span className="block text-xs text-gray-500">
                    {natEnabled ? formatNATPortCount(natPortCount, language) : networkText.noNATPorts}
                  </span>
                </span>
              </label>
              {natEnabled && (
                <span className="block w-24 shrink-0">
                  <NumberInput
                    value={natPortCount}
                    min={2}
                    max={64}
                    onChange={(value) => setForm({ ...form, port_mapping_count: Math.max(2, value || 2), assign_nat: true })}
                  />
                </span>
              )}
            </div>
            {natEnabled && (
              <div className="mt-2 pl-6">
                <div className="flex flex-wrap gap-1.5">
                  <span className="inline-flex px-2 py-1 bg-emerald-50 text-emerald-700 rounded text-xs font-mono">
                    {isWindowsTemplate(form.template_id) ? 'RDP' : 'SSH'}: {sshPortPreview} -&gt; {isWindowsTemplate(form.template_id) ? 3389 : 22}
                  </span>
                  {autoPorts.map((port) => (
                    <span key={port} className="inline-flex px-2 py-1 bg-gray-100 text-gray-700 rounded text-xs font-mono">
                      {port} -&gt; {port}
                    </span>
                  ))}
                </div>
              </div>
            )}
          </div>

          <div className="grid grid-cols-2 gap-4">
            <Field label="vCPU">
              <NumberInput
                value={form.vcpu}
                min={form.virtualization === 'kvm' ? 1 : 0.25}
                max={maxVCPU}
                step={form.virtualization === 'kvm' ? 1 : 0.25}
                invalid={!!resourceErrors.vcpu}
                onChange={(value) => setForm({ ...form, vcpu: value })}
              />
              {resourceErrors.vcpu && <p className="mt-1 text-xs text-red-500">{resourceErrors.vcpu}</p>}
            </Field>
            <Field label="内存 (MB)">
              <NumberInput
                value={form.ram_mb}
                min={128}
                max={maxRAMMB}
                step={128}
                invalid={!!resourceErrors.ram_mb}
                onChange={(value) => setForm({ ...form, ram_mb: value })}
              />
              {resourceErrors.ram_mb && <p className="mt-1 text-xs text-red-500">{resourceErrors.ram_mb}</p>}
            </Field>
          </div>

          <div className="grid grid-cols-3 gap-3">
            <Field label="磁盘 (GB)">
              <NumberInput
                value={form.disk_gb}
                min={1}
                max={maxDiskGB}
                invalid={!!resourceErrors.disk_gb}
                onChange={(value) => setForm({ ...form, disk_gb: value })}
              />
              {resourceErrors.disk_gb && <p className="mt-1 text-xs text-red-500">{resourceErrors.disk_gb}</p>}
            </Field>
            <Field label="带宽 (Mbps)">
              <NumberInput value={form.network_bw_mbps} min={0} onChange={(value) => setForm({ ...form, network_bw_mbps: value })} />
            </Field>
            <Field label="IO 速度 (MB/s)">
              <NumberInput value={form.io_speed_mbps} min={0} onChange={(value) => setForm({ ...form, io_speed_mbps: value })} />
            </Field>
          </div>

          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            {/* Traffic control */}
            <div>
              <div className="flex items-center gap-3 mb-2">
                <label className="text-sm font-medium text-gray-700">月流量</label>
                <select
                  value={form.traffic_mode}
                  onChange={(e) => setForm({ ...form, traffic_mode: e.target.value })}
                  className="h-8 px-2 border border-gray-300 rounded text-xs text-gray-600 bg-white"
                >
                  <option value="total">双向统计</option>
                  <option value="in_out">入/出分离</option>
                </select>
              </div>
              {form.traffic_mode === 'total' ? (
                <div className="flex items-center gap-2">
                  <NumberInput value={form.monthly_traffic_gb} min={0} onChange={(value) => setForm({ ...form, monthly_traffic_gb: value })} />
                  <span className="text-xs text-gray-400">GB (0=不限制)</span>
                </div>
              ) : (
                <div className="grid grid-cols-2 gap-3">
                  <Field label="入站 (GB)">
                    <NumberInput value={form.traffic_in_gb} min={0} onChange={(value) => setForm({ ...form, traffic_in_gb: value || 0 })} />
                  </Field>
                  <Field label="出站 (GB)">
                    <NumberInput value={form.traffic_out_gb} min={0} onChange={(value) => setForm({ ...form, traffic_out_gb: value || 0 })} />
                  </Field>
                </div>
              )}
            </div>

            <Field label="子用户快照上限">
              <NumberInput
                value={form.snapshot_limit}
                min={1}
                max={999}
                onChange={(value) => setForm({ ...form, snapshot_limit: Math.max(1, Math.round(value || 1)) })}
              />
            </Field>
          </div>

          <Field label="到期时间">
            <div className="relative">
              <CalendarClock className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
              <input
                type="date"
                value={form.expires_at}
                onChange={(event) => setForm({ ...form, expires_at: event.target.value })}
                min={new Date().toISOString().slice(0, 10)}
                className={`${inputClass} pl-10`}
              />
            </div>
            <p className="text-xs text-gray-400 mt-1.5">不选择则长期有效；选择日期后，到期会自动关机。</p>
          </Field>
        </div>

        <div className="flex items-center justify-end gap-3 px-6 py-4 border-t border-gray-200">
          <button onClick={onClose} className="px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 rounded-md transition-colors">
            取消
          </button>
          <button
            onClick={handleSubmit}
            disabled={loading}
            className="px-4 py-2 text-sm bg-black text-white rounded-md hover:bg-gray-800 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {loading ? '创建中...' : '创建容器'}
          </button>
        </div>
      </div>
    </div>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <label className="block text-sm font-medium text-gray-700 mb-1.5">{label}</label>
      {children}
    </div>
  )
}

function NumberInput({
  value,
  min,
  max,
  step,
  invalid,
  onChange,
}: {
  value: number
  min?: number
  max?: number
  step?: number
  invalid?: boolean
  onChange: (value: number) => void
}) {
  const [draft, setDraft] = useState(Number.isFinite(value) ? String(value) : '')
  const [focused, setFocused] = useState(false)

  useEffect(() => {
    if (!focused) {
      setDraft(Number.isFinite(value) ? String(value) : '')
    }
  }, [focused, value])

  return (
    <input
      type="text"
      inputMode={step && !Number.isInteger(step) ? 'decimal' : 'numeric'}
      value={draft}
      onFocus={() => setFocused(true)}
      onBlur={() => {
        setFocused(false)
        setDraft(Number.isFinite(value) ? String(value) : '')
      }}
      onChange={(event) => {
        const raw = event.target.value
        setDraft(raw)
        const next = step && !Number.isInteger(step) ? parseFloat(raw) : parseInt(raw, 10)
        onChange(next)
      }}
      aria-invalid={invalid || undefined}
      data-min={min}
      data-max={max}
      data-step={step}
      className={`${inputClass} ${invalid ? 'border-red-400 focus:border-red-400 focus:ring-red-400' : ''}`}
    />
  )
}

function validateResourceInputs(form: CreateContainerRequest, maxVCPU: number, maxRAMMB?: number, maxDiskGB?: number) {
  const errors: Partial<Record<'vcpu' | 'ram_mb' | 'disk_gb', string>> = {}
  const windows = isWindowsTemplate(form.template_id)
  const minVCPU = windows ? 2 : (form.virtualization === 'kvm' ? 1 : 0.25)
  const minRAMMB = windows ? 2048 : 128
  const minDiskGB = windows ? 30 : 1

  if (!Number.isFinite(form.vcpu)) {
    errors.vcpu = '请输入 vCPU'
  } else if (form.vcpu < minVCPU) {
    errors.vcpu = `不能小于 ${minVCPU} 核`
  } else if (form.vcpu > maxVCPU) {
    errors.vcpu = `不能大于 ${maxVCPU} 核`
  } else if (form.virtualization === 'kvm' && form.vcpu !== Math.round(form.vcpu)) {
    errors.vcpu = 'KVM vCPU 必须是整数'
  }

  if (!Number.isFinite(form.ram_mb)) {
    errors.ram_mb = '请输入内存'
  } else if (form.ram_mb < minRAMMB) {
    errors.ram_mb = `不能小于 ${minRAMMB} MB`
  } else if (maxRAMMB && form.ram_mb > maxRAMMB) {
    errors.ram_mb = `不能大于 ${maxRAMMB} MB`
  }

  if (!Number.isFinite(form.disk_gb)) {
    errors.disk_gb = '请输入磁盘'
  } else if (form.disk_gb < minDiskGB) {
    errors.disk_gb = `不能小于 ${minDiskGB} GB`
  } else if (maxDiskGB && form.disk_gb > maxDiskGB) {
    errors.disk_gb = `不能大于 ${maxDiskGB} GB`
  }

  return errors
}

function normalizeCreateForm(form: CreateContainerRequest): CreateContainerRequest {
  const normalized = applyTemplateDefaults(form)
  const wantsIPv4 = !!normalized.assign_ipv4
  const wantsIPv6 = !!normalized.assign_ipv6
  // IPv4 and NAT are mutually exclusive
  const wantsNAT = wantsIPv4 ? false : normalized.assign_nat !== false
  const linuxTemplate = !isWindowsTemplate(normalized.template_id)
  const sshAuthMode = linuxTemplate ? (normalized.ssh_auth_mode || 'auto_password') : 'auto_password'
  return {
    ...normalized,
    vcpu: normalized.virtualization === 'kvm' ? Math.round(normalized.vcpu) : normalizeLXCvCPU(normalized.vcpu),
    ram_mb: Math.round(normalized.ram_mb),
    disk_gb: Math.round(normalized.disk_gb),
    assign_nat: wantsNAT,
    port_mapping_count: wantsNAT ? clampInt(normalized.port_mapping_count, 2, 64, 2) : 0,
    assign_ipv4: wantsIPv4,
    ipv4_count: wantsIPv4 ? clampInt(normalized.ipv4_count || 1, 1, 64, 1) : 0,
    public_ipv4s: wantsIPv4 ? (normalized.public_ipv4s || []) : [],
    assign_ipv6: wantsIPv6,
    ipv6_count: wantsIPv6 ? clampInt(normalized.ipv6_count || 1, 1, 64, 1) : 0,
    ipv6_addresses: wantsIPv6 ? (normalized.ipv6_addresses || []) : [],
    ssh_auth_mode: sshAuthMode,
    ssh_password: linuxTemplate && sshAuthMode === 'password' ? (normalized.ssh_password || '').trim() : '',
    ssh_public_key: linuxTemplate && sshAuthMode === 'key' ? (normalized.ssh_public_key || '').trim() : '',
    snapshot_limit: clampInt(normalized.snapshot_limit, 1, undefined, 3),
  }
}

function validateSSHAuthInputs(form: CreateContainerRequest) {
  if (isWindowsTemplate(form.template_id)) return ''
  const mode = form.ssh_auth_mode || 'auto_password'
  if (mode === 'password') return sshPasswordError((form.ssh_password || '').trim())
  if (mode === 'key') return sshPublicKeyError(form.ssh_public_key || '')
  if (mode !== 'auto_password') return '请选择登录方式'
  return ''
}

function applyTemplateDefaults(form: CreateContainerRequest): CreateContainerRequest {
  if (!isWindowsTemplate(form.template_id)) return form
  return {
    ...form,
    virtualization: 'kvm',
    vcpu: Math.max(2, Math.round(Number.isFinite(form.vcpu) ? form.vcpu : 2)),
    ram_mb: Math.max(2048, Math.round(Number.isFinite(form.ram_mb) ? form.ram_mb : 2048)),
    disk_gb: Math.max(30, Math.round(Number.isFinite(form.disk_gb) ? form.disk_gb : 30)),
  }
}

function isWindowsTemplate(templateID: string) {
  return templateID.toLowerCase().includes('windows')
}

function normalizeLXCvCPU(value: number) {
  const rounded = Math.round((Number.isFinite(value) ? value : 1) * 4) / 4
  return Number(rounded.toFixed(2))
}

function clampInt(value: number, min: number, max?: number, fallback = min) {
  const next = Math.round(Number.isFinite(value) ? value : fallback)
  return Math.min(Math.max(next, min), max ?? next)
}

const createNetworkText = {
  zh: {
    publicIPv4: '公网 IPv4',
    noAllocatableIPv4: '未检测到可分配公网 IPv4',
    publicIPv6: '公网 IPv6',
    use: '使用',
    checkingIPv6Prefix: '正在检测 IPv6 前缀...',
    publicNAT: '公网 NAT',
    noNATPorts: '不分配 NAT 端口',
  },
  en: {
    publicIPv4: 'Public IPv4',
    noAllocatableIPv4: 'No allocatable public IPv4 detected',
    publicIPv6: 'Public IPv6',
    use: 'Use',
    checkingIPv6Prefix: 'Checking IPv6 prefix...',
    publicNAT: 'Public NAT',
    noNATPorts: 'No NAT ports will be assigned',
  },
} as const

function formatAllocatableIPv4Count(count: number, language: Language) {
  return language === 'en'
    ? `${count} allocatable address${count === 1 ? '' : 'es'} detected`
    : `检测到 ${count} 个可分配地址`
}

function formatNATPortCount(count: number, language: Language) {
  return language === 'en'
    ? `${count} NAT ports will be assigned`
    : `将分配 ${count} 个 NAT 端口`
}

const inputClass =
  'w-full px-3 py-2 border border-gray-300 rounded-md text-sm text-black bg-white focus:outline-none focus:ring-2 focus:ring-black focus:border-black'
