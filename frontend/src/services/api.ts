import axios from 'axios'

const api = axios.create({
  baseURL: '/api',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
})

// Request interceptor to add auth token
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('clicd_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Response interceptor to handle auth errors
api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      localStorage.removeItem('clicd_token')
      localStorage.removeItem('clicd_username')
      window.location.href = '/login'
    }
    return Promise.reject(error)
  }
)

export interface LoginResponse {
  token: string
  username: string
}

export type ContainerIdentifier = number | string

export interface PortMapping {
  container_port: number
  host_port: number
  protocol: string
  description: string
}

export interface Container {
  id: number
  uuid: string
  name: string
  virtualization?: string
  template: string
  vcpu: number
  ram_mb: number
  disk_gb: number
  network_bw_mbps: number
  monthly_traffic_gb: number
  traffic_mode: string
  traffic_in_gb: number
  traffic_out_gb: number
  traffic_used_rx: number
  traffic_used_tx: number
  traffic_reset_date: string
  io_speed_mbps: number
  status: string
  ip: string
  ipv6: string
  ipv6_prefix_len: number
  ipv6_interface: string
  vnc_port: number
  ssh_port: number
  ssh_password: string
  port_mappings: PortMapping[]
  port_mapping_limit: number
  snapshot_limit: number
  created_at: string
  expires_at: string
  snapshot_schedule_enabled: boolean
  snapshot_schedule_interval_hours: number
  snapshot_schedule_time: string
  snapshot_schedule_last_run: string
  snapshot_schedule_next_run: string
  snapshot_schedule_created_by: string
  policy_blocked?: boolean
  policy_blocked_reason?: string
  policy_blocked_at?: string
}

export interface Template {
  id: string
  name: string
  type?: string
  distro: string
  release: string
  arch: string
  variant?: string
  desktop?: string
  description: string
}

export interface CreateContainerRequest {
  name: string
  virtualization: string
  template_id: string
  vcpu: number
  cpu_percent: number
  ram_mb: number
  disk_gb: number
  network_bw_mbps: number
  monthly_traffic_gb: number
  traffic_mode: string
  traffic_in_gb: number
  traffic_out_gb: number
  io_speed_mbps: number
  extra_ports: number[]
  port_mapping_count: number
  snapshot_limit: number
  assign_ipv6: boolean
  expires_at: string
}

export interface IPv6PrefixInfo {
  interface: string
  address: string
  prefix: string
  prefix_len: number
  gateway: string
  is_tunnel?: boolean
  source?: string
}

export interface IPv6Status {
  available: boolean
  reachable: boolean
  reason: string
  prefixes: IPv6PrefixInfo[]
}

export interface DashboardStats {
  total_containers: number
  running: number
  stopped: number
}

export interface HostInfo {
  cpu: { cores: number; usage_pct: number }
  ram: { total_mb: number; used_mb: number; free_mb: number }
  disk: { total_gb: number; used_gb: number; free_gb: number }
  network: {
    rx_bytes: number
    tx_bytes: number
    rx_bps: number
    tx_bps: number
    public_ipv4?: string
    public_ipv4_interface?: string
    public_ipv6?: string
    public_ipv6_interface?: string
    ipv6_prefixes?: IPv6PrefixInfo[]
  }
  disk_io: { read_bytes: number; write_bytes: number; read_bps: number; write_bps: number }
  load: { load1: number; load5: number; load15: number }
}

export interface ContainerUsage {
  memory_usage_bytes: number
  memory_total_bytes?: number
  cpu_usage_usec: number
  cpu_usage_pct: number
  disk_usage_bytes: number
  network_rx_bytes: number
  network_tx_bytes: number
  network_rx_bps: number
  network_tx_bps: number
  disk_read_bytes: number
  disk_write_bytes: number
  disk_read_bps: number
  disk_write_bps: number
  load1: number
  load5: number
  load15: number
  guest_metrics?: boolean
}

export interface APIResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

// Auth
export const login = (username: string, password: string) =>
  api.post<APIResponse<LoginResponse>>('/login', { username, password })

export const checkAuth = () =>
  api.get<APIResponse>('/check-auth')

export const changePassword = (oldPassword: string, newPassword: string) =>
  api.post<APIResponse>('/change-password', { old_password: oldPassword, new_password: newPassword })

export const changeUsername = (newUsername: string, password: string) =>
  api.post<APIResponse>('/change-username', { new_username: newUsername, password })

// Login Logs
export interface LoginLog {
  time: string
  username: string
  ip: string
  user_agent: string
  success: boolean
}

export interface AuditLog {
  time: string
  action: string
  target: string
  detail: string
  user: string
}

export const getLoginLogs = () =>
  api.get<APIResponse<LoginLog[]>>('/login-logs')

// Containers
export const getContainers = () =>
  api.get<APIResponse<Container[]>>('/containers')

export const getContainer = (id: ContainerIdentifier) =>
  api.get<APIResponse<Container>>(`/containers/${id}`)

export const createContainer = (data: CreateContainerRequest) =>
  api.post<APIResponse>('/containers', data)

export const deleteContainer = (id: ContainerIdentifier) =>
  api.delete<APIResponse>(`/containers/${id}/delete`)

export const startContainer = (id: ContainerIdentifier) =>
  api.post<APIResponse>(`/containers/${id}/start`)

export const stopContainer = (id: ContainerIdentifier) =>
  api.post<APIResponse>(`/containers/${id}/stop`)

export const restartContainer = (id: ContainerIdentifier) =>
  api.post<APIResponse>(`/containers/${id}/restart`)

export const reinstallContainer = (id: ContainerIdentifier, templateId: string) =>
  api.post<APIResponse>(`/containers/${id}/reinstall`, { template_id: templateId })

export const resetSSHPassword = (id: ContainerIdentifier, password?: string) =>
  api.post<APIResponse<{ password: string }>>(`/containers/${id}/reset-password`, password ? { password } : {})

export const getContainerUsage = (id: ContainerIdentifier) =>
  api.get<APIResponse<ContainerUsage>>(`/containers/${id}/usage`)

export interface TrafficInfo {
  total_used_bytes: number
  rx_used_bytes: number
  tx_used_bytes: number
  mode: string
  limit_gb: number
  in_limit_gb: number
  out_limit_gb: number
  used_pct: number
  reset_date: string
}

export const getTrafficInfo = (id: ContainerIdentifier) =>
  api.get<APIResponse<TrafficInfo>>(`/containers/${id}/traffic`)

export const resetTraffic = (id: ContainerIdentifier) =>
  api.post<APIResponse>(`/containers/${id}/traffic-reset`)

export const updateTrafficLimit = (id: ContainerIdentifier, data: {
  traffic_mode: string
  monthly_traffic_gb: number
  traffic_in_gb: number
  traffic_out_gb: number
}) =>
  api.put<APIResponse>(`/containers/${id}/traffic-limit`, data)

export const updateResourceLimit = (id: ContainerIdentifier, data: {
  vcpu: number
  ram_mb: number
  io_speed_mbps: number
  network_bw_mbps: number
}) =>
  api.put<APIResponse>(`/containers/${id}/resource-limit`, data)

export const addPortMapping = (id: ContainerIdentifier, data: PortMapping) =>
  api.post<APIResponse<PortMapping[]>>(`/containers/${id}/port-mappings`, data)

export const updatePortMapping = (id: ContainerIdentifier, index: number, data: PortMapping) =>
  api.put<APIResponse<PortMapping[]>>(`/containers/${id}/port-mappings/${index}`, data)

export const deletePortMapping = (id: ContainerIdentifier, index: number) =>
  api.delete<APIResponse<PortMapping[]>>(`/containers/${id}/port-mappings/${index}`)

export const updateContainerExpiry = (id: ContainerIdentifier, expiresAt: string) =>
  api.put<APIResponse>(`/containers/${id}/expiry`, { expires_at: expiresAt })

export const getIPv6Status = () =>
  api.get<APIResponse<IPv6Status>>('/ipv6/status')

export const assignIPv6 = (id: ContainerIdentifier) =>
  api.post<APIResponse<Container>>(`/containers/${id}/ipv6`)

export interface RouteCapacity {
  used: number
  remaining: string
  total: string
}

export interface NAT4Route {
  container_id: number
  container_name: string
  lxc_name: string
  status: string
  ip: string
  host_port: number
  container_port: number
  protocol: string
  description: string
}

export interface IPv6Route {
  container_id: number
  container_name: string
  lxc_name: string
  status: string
  address: string
  prefix_len: number
  interface: string
}

export interface RoutingInfo {
  nat4: RouteCapacity
  ipv6: RouteCapacity
  nat4_mappings: NAT4Route[]
  ipv6_assignments: IPv6Route[]
  ipv6_prefixes: IPv6PrefixInfo[]
}

export const getRoutingInfo = () =>
  api.get<APIResponse<RoutingInfo>>('/routing')

// Templates
export const getTemplates = () =>
  api.get<APIResponse<Template[]>>('/templates')

// Images (template download/enable management)
export interface ImageInfo {
  id: string
  name: string
  type: string
  distro: string
  release: string
  arch: string
  description: string
  downloaded: boolean
  enabled: boolean
  downloading: boolean
  progress: number
  downloaded_bytes: number
  total_bytes: number
  stage?: string
  error?: string
  size_bytes: number
  manual_path?: string
  desktop?: string
}

export const getImages = () =>
  api.get<APIResponse<ImageInfo[]>>('/images')

export const downloadImage = (templateId: string) =>
  api.post<APIResponse>('/images/download', { template_id: templateId })

export const cancelImageDownload = (templateId: string) =>
  api.post<APIResponse>('/images/cancel', { template_id: templateId })

export const deleteImage = (templateId: string) =>
  api.delete<APIResponse>('/images/delete', { data: { template_id: templateId } })

export const toggleImage = (templateId: string, enabled: boolean) =>
  api.put<APIResponse>('/images/toggle', { template_id: templateId, enabled })

export const getEnabledImages = (virtualization = 'lxc') =>
  api.get<APIResponse<Template[]>>('/images/enabled', { params: { type: virtualization } })

// Dashboard
export const getDashboard = () =>
  api.get<APIResponse<DashboardStats>>('/dashboard')

export const getHostInfo = () =>
  api.get<APIResponse<HostInfo>>('/host-info')

// Snapshots
export interface Snapshot {
  id: string
  container_id: number
  container_name: string
  lxc_name: string
  created_at: string
  created_by: string
  scheduled: boolean
  path: string
  size_bytes: number
}

export interface SnapshotSchedule {
  enabled: boolean
  interval_hours: number
  time: string
  last_run: string
  next_run: string
  created_by: string
}

export interface ContainerSnapshotsResponse {
  snapshots: Snapshot[]
  quota: number
  schedule: SnapshotSchedule
}

export const getSnapshots = () =>
  api.get<APIResponse<Snapshot[]>>('/snapshots')

export const getContainerSnapshots = (id: ContainerIdentifier) =>
  api.get<APIResponse<ContainerSnapshotsResponse>>(`/containers/${id}/snapshots`)

export const createContainerSnapshot = (id: ContainerIdentifier) =>
  api.post<APIResponse<Snapshot>>(`/containers/${id}/snapshots`, {}, { timeout: 600000 })

export const deleteContainerSnapshot = (id: ContainerIdentifier, snapshotId: string) =>
  api.delete<APIResponse>(`/containers/${id}/snapshots/${snapshotId}`, { timeout: 600000 })

export const restoreContainerSnapshot = (id: ContainerIdentifier, snapshotId: string) =>
  api.post<APIResponse>(`/containers/${id}/snapshots/${snapshotId}/restore`, {}, { timeout: 600000 })

export const updateSnapshotSchedule = (id: ContainerIdentifier, enabled: boolean, intervalHours: number, time: string) =>
  api.post<APIResponse<{ container: Container; snapshot?: Snapshot }>>(
    `/containers/${id}/snapshots/schedule`,
    { enabled, interval_hours: intervalHours, time },
    { timeout: 600000 }
  )

export const updateSnapshotQuota = (id: ContainerIdentifier, snapshotLimit: number) =>
  api.put<APIResponse<{ container: Container; quota: number }>>(
    `/containers/${id}/snapshots/quota`,
    { snapshot_limit: snapshotLimit }
  )

// WebSSH URL generator
export const getWebSSHUrl = (containerName: string) => {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const params = new URLSearchParams({ container: containerName })
  return `${protocol}//${window.location.host}/api/ssh?${params.toString()}`
}

export const getWebVNCUrl = (containerName: string) => {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const params = new URLSearchParams({ container: containerName })
  return `${protocol}//${window.location.host}/api/vnc?${params.toString()}`
}

// Task Queue
export interface Task {
  id: string
  type: string
  container_id?: number
  container_name: string
  status: string
  error?: string
  created_at: string
  template_id?: string
  config?: CreateContainerRequest
}

export const getTasks = () =>
  api.get<APIResponse<Task[]>>('/tasks')

export const deleteTask = (taskId: string) =>
  api.delete<APIResponse>(`/tasks/${taskId}`)

export const batchCreate = (containers: CreateContainerRequest[]) =>
  api.post<APIResponse<string[]>>('/batch-create', { containers })

export const batchAction = (action: string, containers: number[], templateId?: string) =>
  api.post<APIResponse>('/batch-action', { action, containers, template_id: templateId })

// Sub Users
export interface SubUser {
  id: string
  username: string
  password?: string
  container_names: string[]
  container_uuids?: string[]
  access_code: string
  created_at: string
}

export const createSubUser = (containerId: ContainerIdentifier) =>
  api.post<APIResponse<SubUser>>('/sub-user/create', { container_name: String(containerId) })

// Audit Logs
export interface AuditLog {
  time: string
  action: string
  target: string
  detail: string
  user: string
}

export const getAuditLogs = () =>
  api.get<APIResponse<AuditLog[]>>('/audit-logs')

// Security
export interface SecurityAlert {
  id: string
  container_name: string
  type: string
  severity: string
  source_ip: string
  target_ip: string
  target_port: number
  detail: string
  log_line: string
  timestamp: string
  count: number
}

export interface SecuritySummary {
  total_alerts: number
  critical: number
  high: number
  medium: number
  low: number
}

export interface SecuritySettings {
  auto_shutdown: boolean
}

export interface SecurityLog {
  src_ip: string
  dst_ip: string
  src_port: number
  dst_port: number
  protocol: string
  state: string
}

export const getSecurityAlerts = () =>
  api.get<APIResponse<SecurityAlert[]>>('/security/alerts')

export const checkContainerSecurity = (containerName: string) =>
  api.post<APIResponse>('/security/check', { container_name: containerName })

export const getSecurityLogs = (containerName: string) =>
  api.get<APIResponse<SecurityLog[]>>('/security/logs', { params: { container: containerName } })

export const getSecuritySummary = () =>
  api.get<APIResponse<SecuritySummary>>('/security/summary')

export const getSecuritySettings = () =>
  api.get<APIResponse<SecuritySettings>>('/security/settings')

export const updateSecuritySettings = (data: SecuritySettings) =>
  api.put<APIResponse<SecuritySettings>>('/security/settings', data)

export const createWebSSHTicket = (containerName: string) =>
  api.post<APIResponse<{ ticket: string }>>('/ssh-ticket', { container_name: containerName })

export const createVNCTicket = (containerName: string) =>
  api.post<APIResponse<{ ticket: string }>>('/vnc-ticket', { container_name: containerName })

// Version
export const getVersion = () =>
  api.get<APIResponse<{ version: string }>>('/version')

export default api
