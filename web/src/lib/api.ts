// src/lib/api.ts — 类型化 API client
import type {
  Analysis,
  AuditEntry,
  BackupFile,
  Connection,
  ConnectionDetail,
  ConnTypeDef,
  DiscoverResult,
  Job,
  JobVersion,
  LogLine,
  Notification,
  NotifyChannel,
  PreviewData,
  RunRecord,
  RunSnapshot,
  Stats,
  TestResult,
  UpcomingItem,
  JobSpec,
} from "./types"

const BASE = "/api"

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(BASE + path, {
    ...init,
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
  })
  const text = await resp.text()
  const body = text ? JSON.parse(text) : {}
  if (!resp.ok) {
    throw new ApiError(body.error ?? `HTTP ${resp.status}`, resp.status, body)
  }
  return body as T
}

export class ApiError extends Error {
  status: number
  constructor(msg: string, status: number, _body: unknown) {
    super(msg)
    this.status = status
  }
}

export const api = {
  /* misc */
  stats: () => req<Stats>("/stats"),
  version: () => req<Record<string, string>>("/version"),

  /* connections */
  listConns: () => req<Connection[]>("/conns"),
  getConn: (name: string) => req<ConnectionDetail>(`/conns/${encodeURIComponent(name)}`),
  connTypes: () => req<ConnTypeDef[]>("/conns/types"),
  createConn: (input: { name: string; type: string; description?: string; tags?: string[]; props: Record<string, unknown> }) =>
    req<Connection>("/conns", { method: "POST", body: JSON.stringify(input) }),
  updateConn: (name: string, input: { name: string; type: string; description?: string; tags?: string[]; props: Record<string, unknown> }) =>
    req<Connection>(`/conns/${encodeURIComponent(name)}`, { method: "PUT", body: JSON.stringify(input) }),
  deleteConn: (name: string) => req<{ deleted: boolean }>(`/conns/${encodeURIComponent(name)}`, { method: "DELETE" }),
  testConn: (input: { name?: string; type?: string; props?: Record<string, unknown> }) =>
    req<TestResult>("/conns/test", { method: "POST", body: JSON.stringify(input) }),
  discover: (name: string, level = "table") =>
    req<DiscoverResult>(`/conns/${encodeURIComponent(name)}/discover?level=${level}`),
  primaryKeys: (name: string, tables: string[]) =>
    req<{ keys: Record<string, string[]> }>(`/conns/${encodeURIComponent(name)}/primary-keys`, {
      method: "POST",
      body: JSON.stringify({ tables }),
    }),
  preview: (name: string, table: string, limit = 50) =>
    req<PreviewData>(`/conns/${encodeURIComponent(name)}/preview?table=${encodeURIComponent(table)}&limit=${limit}`),
  importConns: () => req<{ imported: string[] }>("/conns/import", { method: "POST" }),

  /* jobs */
  listJobs: (q = "") => req<{ jobs: Job[]; total: number }>(`/jobs${q ? `?q=${encodeURIComponent(q)}` : ""}`),
  getJob: (id: string) => req<Job>(`/jobs/${id}`),
  createJob: (input: JobInput) => req<Job>("/jobs", { method: "POST", body: JSON.stringify(input) }),
  updateJob: (id: string, input: JobInput) => req<Job>(`/jobs/${id}`, { method: "PUT", body: JSON.stringify(input) }),
  deleteJob: (id: string) => req<{ deleted: boolean }>(`/jobs/${id}`, { method: "DELETE" }),
  runJob: (id: string) => req<{ queued: boolean }>(`/jobs/${id}/run`, { method: "POST" }),
  duplicateJob: (id: string) => req<Job>(`/jobs/${id}/duplicate`, { method: "POST" }),
  exportJob: (id: string) => req<{ name: string; yaml: string }>(`/jobs/${id}/export`),
  importJob: (yaml: string, name?: string) => req<Job>("/jobs/import", { method: "POST", body: JSON.stringify({ yaml, name }) }),
  jobVersions: (id: string) => req<JobVersion[]>(`/jobs/${id}/versions`),
  rollbackJob: (id: string, version: number) => req<Job>(`/jobs/${id}/rollback`, { method: "POST", body: JSON.stringify({ version }) }),
  nextRuns: (id: string) => req<{ next_runs: string[] }>(`/jobs/${id}/next-runs`),
  setSchedule: (id: string, input: { cron?: string; tz?: string; enabled?: boolean }) =>
    req<Job>(`/jobs/${id}/schedule`, { method: "PATCH", body: JSON.stringify(input) }),
  batchCreate: (input: unknown) => req<{ created: Job[]; count: number }>("/jobs/batch-create", { method: "POST", body: JSON.stringify(input) }),
  upcoming: () => req<UpcomingItem[]>("/schedules/upcoming"),

  /* runs */
  adhocRun: (spec: JobSpec) => req<RunSnapshot>("/runs", { method: "POST", body: JSON.stringify({ spec }) }),
  listRuns: (q = "") => req<{ runs: RunRecord[]; total: number; active: number }>(`/runs${q ? `?${q}` : ""}`),
  getRun: (id: string) => req<Record<string, unknown>>(`/runs/${id}`),
  runLogs: (id: string, offset = 0, limit = 500) => req<{ logs: LogLine[]; count: number }>(`/runs/${id}/logs?offset=${offset}&limit=${limit}`),
  cancelRun: (id: string) => req<{ cancelling: boolean }>(`/runs/${id}/cancel`, { method: "POST" }),

  /* analysis */
  analyzeSpec: (spec: JobSpec) =>
    req<Analysis>("/analyze", { method: "POST", body: JSON.stringify({ spec }) }),
  analyzeJob: (id: string) => req<Analysis>(`/jobs/${id}/analyze`),

  /* notifications */
  notifications: (opts: { unread?: boolean; type?: string; limit?: number } = {}) => {
    const p = new URLSearchParams()
    if (opts.unread) p.set("unread", "1")
    if (opts.type) p.set("type", opts.type)
    if (opts.limit) p.set("limit", String(opts.limit))
    const qs = p.toString()
    return req<{ notifications: Notification[]; unread: number }>(`/notifications${qs ? `?${qs}` : ""}`)
  },
  markRead: (ids?: number[]) => req<{ ok: boolean }>("/notifications/read", { method: "POST", body: JSON.stringify({ ids: ids ?? [] }) }),

  /* notify channels */
  notifyChannels: () => req<{ channels: NotifyChannel[] }>("/notify-channels"),
  createNotifyChannel: (input: ChannelInput) =>
    req<NotifyChannel>("/notify-channels", { method: "POST", body: JSON.stringify(input) }),
  updateNotifyChannel: (id: number, input: ChannelInput) =>
    req<NotifyChannel>(`/notify-channels/${id}`, { method: "PUT", body: JSON.stringify(input) }),
  deleteNotifyChannel: (id: number) =>
    req<{ deleted: boolean }>(`/notify-channels/${id}`, { method: "DELETE" }),
  testNotifyChannel: (id: number) =>
    req<{ ok: boolean; error?: string }>(`/notify-channels/${id}/test`, { method: "POST" }),

  /* audit */
  audit: (q = "") => req<{ entries: AuditEntry[]; total: number }>(`/audit${q ? `?${q}` : ""}`),

  /* backups */
  backups: () => req<{ backups: BackupFile[]; dir: string }>("/backups"),
  createBackup: () => req<BackupFile>("/backups", { method: "POST" }),
  deleteBackup: (name: string) =>
    req<{ deleted: boolean }>(`/backups/${encodeURIComponent(name)}`, { method: "DELETE" }),

  /* settings */
  getSettings: () => req<Record<string, string>>("/settings"),
  putSettings: (settings: Record<string, string>) => req<Record<string, string>>("/settings", { method: "PUT", body: JSON.stringify(settings) }),
}

export interface ChannelInput {
  name: string
  provider: string
  url: string
  secret?: string
  events?: string[]
  enabled?: boolean
}

export interface JobInput {
  name: string
  description?: string
  folder?: string
  tags?: string[]
  spec: JobSpec
  schedule_cron?: string
  schedule_tz?: string
  schedule_enabled?: boolean
  schema_policy?: string
  concurrency_policy?: string
  retry_max?: number
  retry_backoff_sec?: number
  verify_rows?: boolean
  notify_webhooks?: string[]
  is_template?: boolean
}
