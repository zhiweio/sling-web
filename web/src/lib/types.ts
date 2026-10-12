// src/lib/types.ts — API type definitions (mirrors the server package models one-to-one)
export type ExecStatus =
  | "created"
  | "queued"
  | "running"
  | "success"
  | "warning"
  | "error"
  | "cancelled"
  | "terminated"
  | "interrupted"
  | "skipped"

export interface PropDef {
  name: string
  type: string // text|integer|boolean|dropdown|password
  title?: string
  description?: string
  examples?: string[]
  default?: unknown
  options?: string[]
  secret?: boolean
  required?: boolean
}

export interface ConnTypeDef {
  type: string
  title: string
  kind: "database" | "file" | "api" | string
  required?: string[]
  url_template?: string
  docs?: string
  properties: PropDef[]
}

export interface Connection {
  name: string
  type: string
  description?: string
  tags?: string[] | null
  last_test_status?: string
  last_test_at?: string | null
  last_test_ms?: number
  last_test_err?: string
  created_at: string
  updated_at: string
}

export interface ConnectionDetail extends Connection {
  props: Record<string, unknown>
  secret_fields?: string[]
  used_by?: string[]
}

export interface TestResult {
  success: boolean
  error?: string
  ms: number
}

// One connection candidate parsed from an import payload (file/paste or
// environment scan). Secret values are never included — field names only.
export interface ImportEntry {
  name: string
  type: string
  source?: string
  exists: boolean
  valid: boolean
  error?: string
  secret_fields?: string[]
  masked_secret?: boolean
}

export interface ImportCommitResult {
  imported: string[]
  skipped: string[]
}

export interface ExportResult {
  name: string
  body: string
  count: number
}

export interface StreamSpec {
  name?: string
  source_object?: string
  target_object?: string
  mode?: string // ""|replace|truncate|append|upsert|snapshot
  primary_key?: string[]
  update_key?: string
  select?: string[]
  disabled?: boolean
  options?: StreamOptions | null
}

export interface StreamOptions {
  add_new_columns?: boolean
  adjust_column_type?: boolean
  column_casing?: string
  file_format?: string
  pre_sql?: string
  post_sql?: string
  where?: string
  limit?: number
  chunk_size?: number
}

export interface JobSpec {
  source_conn: string
  target_conn: string
  defaults: StreamSpec
  streams: StreamSpec[]
  metadata?: {
    loaded_at?: boolean
    synced_at?: boolean
    row_num?: boolean
    row_id?: boolean
    exec_id?: boolean
    stream_url?: boolean
  }
}

export interface Job {
  id: string
  name: string
  description?: string
  folder?: string
  tags?: string[] | null
  spec_json?: string
  schedule_cron?: string
  schedule_tz?: string
  schedule_enabled: boolean
  next_run_at?: string | null
  schema_policy?: string
  concurrency_policy?: string
  retry_max?: number
  retry_backoff_sec?: number
  verify_rows?: boolean
  notify_webhooks?: string
  last_run_at?: string | null
  consecutive_failures?: number
  auto_paused?: boolean
  version?: number
  is_template?: boolean
  created_at: string
  updated_at: string
}

export interface JobVersion {
  id: number
  job_id: string
  version: number
  spec: string
  comment?: string
  created_at: string
}

export interface StreamStatus {
  stream: string
  object?: string
  status: ExecStatus
  rows?: number
  error?: string
}

export interface RunSnapshot {
  run_id: string
  job_id?: string
  job_name?: string
  type: string
  trigger: string
  attempt?: number
  status: ExecStatus
  rows: number
  bytes: number
  rate: number
  progress?: string
  streams?: StreamStatus[]
  start_time: string
  end_time?: string | null
  duration_ms: number
  error?: string
}

export interface RunRecord {
  id: string
  job_id?: string
  job_name?: string
  type: string
  trigger: string
  attempt?: number
  status: ExecStatus
  error?: string
  rows?: number
  bytes?: number
  duration_ms?: number
  stream_summary?: StreamStatus[] | null
  start_time: string
  end_time?: string | null
}

export interface LogLine {
  seq: number
  ts: string
  level: number
  text: string
}

export interface Notification {
  id: number
  type: string
  level: string
  title: string
  body?: string
  meta_json?: string
  read: boolean
  created_at: string
}

// Notification channel (WeCom/Feishu/DingTalk/Slack/generic webhook); url is a masked value
export interface NotifyChannel {
  id: number
  name: string
  provider: string // wecom|feishu|dingtalk|slack|generic
  url: string
  has_secret: boolean
  events: string[] // empty array = subscribed to all events
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface AuditEntry {
  id: number
  action: string
  entity_type: string
  entity_id?: string
  detail?: string
  created_at: string
}

export interface BackupFile {
  name: string
  size: number
  created_at: string
  kind: string // auto|manual
}

export interface UpcomingItem {
  job_id: string
  job_name: string
  at: string
}

export interface DayTrend {
  day: string
  runs: number
  success: number
  failed: number
  rows: number
}

export interface Stats {
  jobs: number
  connections: number
  active_runs: number
  upcoming: UpcomingItem[]
  trend: DayTrend[]
}

export interface DiscoverResult {
  kind: string
  schemas?: { name: string; tables: { name: string; is_view?: boolean; columns?: { name: string; type: string }[] }[] }[]
  nodes?: { path: string; name: string; is_dir?: boolean; size?: number; updated_at?: string }[]
  endpoints?: string[]
}

export interface PreviewData {
  columns: { name: string; type: string }[] | null
  rows: Record<string, unknown>[] | null
}

/* ---------------------------- pre-run analysis ---------------------------- */

export interface ColBrief {
  name: string
  type?: string
}

export interface StreamAnalysis {
  source_object: string
  target_object?: string
  kind: string // table|file
  columns?: ColBrief[]
  column_count: number
  rows?: number | null
  size_bytes?: number | null
  target_exists?: boolean
  target_rows?: number | null
  warnings?: string[]
  error?: string
}

export interface AnalysisTotals {
  tables: number
  columns: number
  rows: number
  rows_known: boolean
  size_bytes: number
  size_known: boolean
  new_targets: number
  estimated_seconds: number
  load: "low" | "medium" | "high" | string
}

export interface Analysis {
  source_conn: string
  target_conn: string
  streams: StreamAnalysis[]
  totals: AnalysisTotals
  analyzed_at: string
}
