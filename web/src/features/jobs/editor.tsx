// Job 编辑器：5 步向导（选源 → 目标 → 同步方式 → 高级 → 调度保存）
// 大白话模式卡片 + 主键/更新键自动推导 + YAML 预览 + 破坏性操作守卫
// 视觉规范：卡片 rounded-xl / 内嵌芯片 rounded-lg / 徽标 rounded-full；
// 交互反馈统一 200ms + active:scale；步骤切换 300ms 淡入上移（尊重系统减弱动效）。
import { Fragment, useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate, useParams } from "react-router-dom"
import { toast } from "sonner"
import {
  ArrowLeft,
  ArrowRight,
  BarChart3,
  Camera,
  Check,
  Clock,
  Eraser,
  Eye,
  FileCode2,
  Info,
  Loader2,
  Merge,
  MousePointerClick,
  Play,
  Plus,
  RefreshCw,
  Save,
  Search,
  ShieldCheck,
  TableProperties,
  Tags,
  TriangleAlert,
  X,
} from "lucide-react"
import { api, type JobInput } from "@/lib/api"
import type { Connection, JobSpec, StreamSpec } from "@/lib/types"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import { Switch } from "@/components/ui/switch"
import { Skeleton } from "@/components/ui/skeleton"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog"
import { AnalysisDialog } from "@/features/jobs/analysis-dialog"
import { ConnPicker } from "@/features/jobs/conn-picker"
import { TableTreePicker, type PickerSchema } from "@/features/jobs/table-tree-picker"
import { MappingTable } from "@/features/jobs/mapping-table"
import { effectiveTarget, hasWildcard, isQuerySrc, mappingIssues } from "@/features/jobs/spec-helpers"
import { cn } from "@/lib/utils"
import { cronToHuman } from "@/lib/format"

type ModeKey = "replace" | "truncate" | "append" | "upsert" | "snapshot"

type ModeDef = {
  key: ModeKey
  icon: typeof RefreshCw
  chipCls: string
  badge?: { key: string; cls: string }
}

const MODES: ModeDef[] = [
  {
    key: "replace",
    icon: RefreshCw,
    chipCls: "bg-rose-500/10 text-rose-600 dark:text-rose-400",
    badge: { key: "modeBadgeDestructive", cls: "border-rose-500/30 bg-rose-500/10 text-rose-600 dark:text-rose-400" },
  },
  {
    key: "truncate",
    icon: Eraser,
    chipCls: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  },
  {
    key: "append",
    icon: Plus,
    chipCls: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
    badge: { key: "modeBadgeNeedKey", cls: "border-border text-muted-foreground" },
  },
  {
    key: "upsert",
    icon: Merge,
    chipCls: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
    badge: { key: "modeBadgeNeedPk", cls: "border-border text-muted-foreground" },
  },
  {
    key: "snapshot",
    icon: Camera,
    chipCls: "bg-violet-500/10 text-violet-600 dark:text-violet-400",
  },
]

export default function JobEditor() {
  const { t } = useTranslation()
  const nav = useNavigate()
  const { id } = useParams()
  const editing = !!id && id !== "new"
  const qc = useQueryClient()

  const [step, setStep] = useState(1)
  const [name, setName] = useState("")
  const [spec, setSpec] = useState<JobSpec>({
    source_conn: "",
    target_conn: "",
    defaults: { mode: "replace", primary_key: [], update_key: "" } as StreamSpec,
    streams: [],
  })
  const [schedule, setSchedule] = useState<{ enabled: boolean; cron: string; preset: string }>({
    enabled: false,
    cron: "0 9 * * *",
    preset: "daily9",
  })
  const [advanced, setAdvanced] = useState({
    add_new_columns: true,
    adjust_column_type: false,
    retry_max: 0,
    verify_rows: false,
    loaded_at: false,
  })
  const [yamlOpen, setYamlOpen] = useState(false)
  const [analysisOpen, setAnalysisOpen] = useState(false)
  const [runConfirm, setRunConfirm] = useState(false)

  const { data: conns } = useQuery({ queryKey: ["conns"], queryFn: api.listConns })
  const { data: job, isLoading } = useQuery({
    queryKey: ["job", id],
    queryFn: () => api.getJob(id!),
    enabled: editing,
  })

  // 载入编辑
  useEffect(() => {
    if (job) {
      setName(job.name)
      setSpec(JSON.parse(job.spec_json ?? "{}"))
      setSchedule({
        enabled: job.schedule_enabled,
        cron: job.schedule_cron || "0 9 * * *",
        preset: "custom",
      })
      try {
        const adv = JSON.parse(job.spec_json ?? "{}")
        setAdvanced({
          add_new_columns: adv?.defaults?.options?.add_new_columns ?? true,
          adjust_column_type: adv?.defaults?.options?.adjust_column_type ?? false,
          retry_max: job.retry_max ?? 0,
          verify_rows: job.verify_rows ?? false,
          loaded_at: adv?.metadata?.loaded_at ?? false,
        })
      } catch { /* noop */ }
    }
  }, [job])

  const connsAll = conns ?? []
  const sourceIsDb = isDbConn(spec.source_conn, conns)

  /* ---------- 派生 ---------- */
  const canNext = [
    () => spec.source_conn && spec.streams.length > 0,
    () => spec.target_conn && mappingIssues(spec).length === 0,
    () => validMode(),
    () => true,
    () => name.trim() !== "",
  ][step - 1]()
  function validMode() {
    const mode = spec.defaults.mode
    if (mode === "upsert") {
      // defaults 主键兜底；批量场景允许逐表主键（全部流都有键即可）
      if ((spec.defaults.primary_key ?? []).length) return true
      return streamsOf(spec).every((s) => (s.primary_key ?? []).length > 0)
    }
    if (mode === "append") {
      if (spec.defaults.update_key) return true
      return streamsOf(spec).every((s) => !!s.update_key)
    }
    return true
  }
  const blockingHint = String(t(`jobs.wizard.hint${step}`))

  const save = useMutation({
    mutationFn: async (thenRun: boolean) => {
      const finalSpec: JobSpec = {
        ...spec,
        defaults: {
          ...spec.defaults,
          options: {
            add_new_columns: advanced.add_new_columns,
            adjust_column_type: advanced.adjust_column_type,
          },
        },
        metadata: advanced.loaded_at ? { loaded_at: true } : undefined,
      }
      const input: JobInput = {
        name: name.trim(),
        spec: finalSpec,
        schedule_enabled: schedule.enabled,
        schedule_cron: schedule.enabled ? schedule.cron : "",
        retry_max: advanced.retry_max,
        verify_rows: advanced.verify_rows,
      }
      const saved = editing ? await api.updateJob(id!, input) : await api.createJob(input)
      if (thenRun) await api.runJob(saved.id)
      return saved
    },
    onSuccess: (_saved, thenRun) => {
      toast.success(t("common.saved"))
      qc.invalidateQueries({ queryKey: ["jobs"] })
      qc.invalidateQueries({ queryKey: ["stats"] })
      if (thenRun) nav("/runs")
      else nav("/jobs")
    },
    onError: (e) => toast.error(e.message),
  })

  return (
    <div className="mx-auto max-w-3xl pb-10">
      {/* 页头：返回 + 标题 + 步骤进度 + 上下文工具 */}
      <header className="flex items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <Button variant="ghost" size="icon" onClick={() => nav("/jobs")} aria-label="back">
            <ArrowLeft className="size-4" />
          </Button>
          <div className="min-w-0">
            <h1 className="truncate text-lg font-bold tracking-tight">
              {editing ? t("jobs.wizard.titleEdit") : t("jobs.wizard.titleNew")}
            </h1>
            <p className="text-xs text-muted-foreground">{t("jobs.wizard.stepOf", { cur: step, total: 5 })}</p>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
          <Button variant="ghost" size="sm" onClick={() => setYamlOpen(true)}>
            <FileCode2 className="size-4" /> <span className="hidden sm:inline">YAML</span>
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setAnalysisOpen(true)}
            disabled={spec.streams.length === 0 || !spec.source_conn || !spec.target_conn}
          >
            <BarChart3 className="size-4" /> <span className="hidden sm:inline">{t("analysis.title")}</span>
          </Button>
        </div>
      </header>

      {/* 步骤条：编号节点 + 连接线，已完成步骤可点击回跳 */}
      <Stepper step={step} onJump={setStep} />

      {isLoading && editing ? (
        <Skeleton className="mt-8 h-96" />
      ) : (
        <>
          {/* 步骤内容：切换时淡入上移 */}
          <div key={step} className="mt-8 animate-in fade-in slide-in-from-bottom-4 duration-300 motion-reduce:animate-none">
            {step === 1 && (
              <StepSource spec={spec} setSpec={setSpec} conns={connsAll} sourceIsDb={sourceIsDb} />
            )}
            {step === 2 && <StepTarget spec={spec} setSpec={setSpec} conns={connsAll} />}
            {step === 3 && <StepMode spec={spec} setSpec={setSpec} sourceIsDb={sourceIsDb} />}
            {step === 4 && <StepAdvanced advanced={advanced} setAdvanced={setAdvanced} />}
            {step === 5 && (
              <StepFinal
                name={name}
                setName={setName}
                schedule={schedule}
                setSchedule={setSchedule}
                onShowYaml={() => setYamlOpen(true)}
              />
            )}
          </div>

          {/* 底部导航：上一步 | 缺失提示 + 下一步/保存 */}
          <footer className="mt-4 flex items-center justify-between gap-3 border-t pt-4">
            <Button
              variant="ghost"
              onClick={() => setStep((s) => Math.max(1, s - 1))}
              disabled={step === 1}
            >
              <ArrowLeft className="size-4" /> {t("app.actions.prev")}
            </Button>
            <div className="flex items-center gap-3">
              {!canNext && blockingHint && (
                <span className="hidden text-xs text-muted-foreground md:inline">{blockingHint}</span>
              )}
              {step < 5 ? (
                <Button onClick={() => setStep((s) => s + 1)} disabled={!canNext} className="min-w-24">
                  {t("app.actions.next")} <ArrowRight className="size-4" />
                </Button>
              ) : (
                <>
                  <Button
                    variant="outline"
                    onClick={() => save.mutate(false)}
                    disabled={!canNext || save.isPending}
                  >
                    {save.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
                    {t("app.actions.save")}
                  </Button>
                  <Button
                    onClick={() => {
                      if (spec.defaults.mode === "replace" && streamsOf(spec).some((s) => !hasWildcard(s.source_object ?? ""))) {
                        setRunConfirm(true)
                      } else {
                        save.mutate(true)
                      }
                    }}
                    disabled={!canNext || save.isPending}
                  >
                    <Play className="size-4" /> {t("app.actions.save")} + {t("app.actions.run")}
                  </Button>
                </>
              )}
            </div>
          </footer>
        </>
      )}

      {analysisOpen && (
        <AnalysisDialog open={analysisOpen} onOpenChange={setAnalysisOpen} spec={spec} />
      )}

      {yamlOpen && <YamlPreview spec={spec} onClose={() => setYamlOpen(false)} />}

      <AlertDialog open={runConfirm} onOpenChange={setRunConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle className="flex items-center gap-2">
              <ShieldCheck className="size-5 text-amber-500" /> {t("jobs.runConfirmTitle")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t("jobs.runConfirmBodyDestructive", {
                table: confirmTablesText(spec, t),
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("app.actions.cancelEdit")}</AlertDialogCancel>
            <AlertDialogAction onClick={() => save.mutate(true)}>{t("app.actions.confirm")}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

/* ------------------------------- 向导外壳组件 ------------------------------ */

function Stepper({ step, onJump }: { step: number; onJump: (n: number) => void }) {
  const { t } = useTranslation()
  const labels = [1, 2, 3, 4, 5].map((n) => t(`jobs.wizard.step${n}`))
  return (
    <nav aria-label="wizard progress" className="mt-7 flex items-start">
      {labels.map((s, i) => {
        const n = i + 1
        const done = step > n
        const active = step === n
        const clickable = done
        return (
          <Fragment key={s}>
            {i > 0 && (
              <div
                className={cn(
                  "mt-3.5 h-px min-w-4 flex-1 transition-colors duration-300",
                  i < step ? "bg-primary/60" : "bg-border",
                )}
              />
            )}
            <button
              type="button"
              disabled={!clickable}
              onClick={() => clickable && onJump(n)}
              className={cn("group flex flex-col items-center gap-1.5 outline-none", clickable && "cursor-pointer")}
            >
              <span
                className={cn(
                  "flex size-7 shrink-0 items-center justify-center rounded-full border text-xs font-semibold transition-all duration-200",
                  done && "border-primary bg-primary text-primary-foreground group-hover:scale-110",
                  active && "border-primary bg-primary/10 text-primary ring-4 ring-primary/10",
                  !done && !active && "border-border bg-background text-muted-foreground",
                )}
              >
                {done ? <Check className="size-3.5" strokeWidth={3} /> : n}
              </span>
              <span
                className={cn(
                  "hidden max-w-24 truncate text-center text-xs sm:block",
                  active ? "font-medium text-foreground" : "text-muted-foreground",
                )}
              >
                {s}
              </span>
            </button>
          </Fragment>
        )
      })}
    </nav>
  )
}

function StepIntro({ n }: { n: number }) {
  const { t } = useTranslation()
  return (
    <div>
      <h2 className="text-lg font-semibold tracking-tight">{t(`jobs.wizard.step${n}`)}</h2>
      <p className="mt-1 text-sm leading-relaxed text-muted-foreground">{t(`jobs.wizard.step${n}Desc`)}</p>
    </div>
  )
}

/** 表单分组：标题 + 可选说明 + 内容，统一向导内部分节节奏 */
function FieldGroup({
  label,
  desc,
  children,
}: {
  label: string
  desc?: string
  children: React.ReactNode
}) {
  return (
    <section>
      <div className="mb-2.5 flex items-baseline justify-between gap-3">
        <h3 className="text-sm font-semibold">{label}</h3>
        {desc && <span className="shrink-0 text-xs text-muted-foreground">{desc}</span>}
      </div>
      {children}
    </section>
  )
}



/* ------------------------------- Step 1: 源 ------------------------------ */

function StepSource({
  spec,
  setSpec,
  conns,
  sourceIsDb,
}: {
  spec: JobSpec
  setSpec: (s: JobSpec) => void
  conns: Connection[]
  sourceIsDb: boolean
}) {
  const { t } = useTranslation()
  const [manual, setManual] = useState("")
  const [q, setQ] = useState("")

  const { data: discover, isFetching } = useQuery({
    queryKey: ["wizard-discover", spec.source_conn],
    queryFn: () => api.discover(spec.source_conn, "table"),
    enabled: !!spec.source_conn,
    staleTime: 60_000,
  })

  // schema 分组树（DB 源）与扁平文件列表共用一份发现数据
  const grouped = useMemo<PickerSchema[]>(() => {
    const out: PickerSchema[] = []
    for (const schema of discover?.schemas ?? []) {
      const tables = (schema.tables ?? []).map((tb) => ({
        full: `${schema.name}.${tb.name}`,
        name: tb.name,
        isView: tb.is_view,
      }))
      out.push({ name: schema.name, tables })
    }
    return out
  }, [discover])

  const allTables = useMemo(() => grouped.flatMap((s) => s.tables), [grouped])
  const allFiles = useMemo(() => (discover?.nodes ?? []).filter((n) => !n.is_dir), [discover])

  const needle = q.trim().toLowerCase()
  const files = useMemo(
    () => (needle ? allFiles.filter((f) => f.path.toLowerCase().includes(needle)) : allFiles),
    [allFiles, needle],
  )
  const total = allTables.length || allFiles.length

  function toggleStream(full: string) {
    const streams = [...spec.streams]
    const idx = streams.findIndex((s) => s.source_object === full)
    if (idx >= 0) streams.splice(idx, 1)
    else streams.push({ source_object: full })
    setSpec({ ...spec, streams })
  }

  function setMany(fulls: string[], selected: boolean) {
    const set = new Set(spec.streams.map((s) => s.source_object ?? ""))
    for (const f of fulls) {
      if (selected) set.add(f)
      else set.delete(f)
    }
    // 保持树的展示顺序
    const order = new Map(allTables.map((tb, i) => [tb.full, i]))
    const streams = [...set]
      .sort((a, b) => (order.get(a) ?? 1e9) - (order.get(b) ?? 1e9))
      .map((f) => spec.streams.find((s) => s.source_object === f) ?? { source_object: f })
    setSpec({ ...spec, streams })
  }

  const selectedSet = useMemo(() => new Set(spec.streams.map((s) => s.source_object ?? "")), [spec.streams])

  return (
    <div className="space-y-6">
      <StepIntro n={1} />
      <Card>
        <CardContent className="space-y-7 p-6">
          <FieldGroup label={t("jobs.wizard.sourceConn")}>
            <ConnPicker
              value={spec.source_conn}
              onSelect={(name) => setSpec({ ...spec, source_conn: name, streams: [] })}
              conns={conns}
            />
          </FieldGroup>

          {spec.source_conn && (
            <FieldGroup
              label={t("jobs.wizard.sourceTable")}
              desc={total > 0 && !isFetching ? `${sourceIsDb ? allTables.length : files.length}/${total}` : undefined}
            >
              {isFetching ? (
                <div className="space-y-2">
                  {[0, 1, 2].map((i) => (
                    <Skeleton key={i} className="h-9 w-full" />
                  ))}
                </div>
              ) : sourceIsDb ? (
                grouped.length > 0 ? (
                  <TableTreePicker
                    schemas={grouped}
                    selected={selectedSet}
                    onToggle={toggleStream}
                    onSetMany={setMany}
                  />
                ) : null
              ) : files.length > 0 ? (
                <div className="overflow-hidden rounded-xl border">
                  <div className="flex items-center gap-2 border-b bg-muted/40 px-3 py-2">
                    <Search className="size-3.5 shrink-0 text-muted-foreground" />
                    <input
                      value={q}
                      onChange={(e) => setQ(e.target.value)}
                      placeholder={t("jobs.wizard.searchTables")}
                      className="h-5 w-full bg-transparent text-xs outline-none placeholder:text-muted-foreground"
                    />
                  </div>
                  <div className="scrollbar-thin max-h-72 overflow-y-auto">
                    {files.map((it) => {
                      const isSel = spec.streams.some((s) => s.source_object === it.path)
                      return (
                        <label
                          key={it.path}
                          className={cn(
                            "flex cursor-pointer items-center gap-2.5 border-b px-3 py-2 text-sm transition-colors last:border-b-0 hover:bg-muted/50",
                            isSel && "bg-primary/[0.04]",
                          )}
                        >
                          <Checkbox
                            checked={isSel}
                            onCheckedChange={() => toggleStream(it.path)}
                          />
                          <span className="truncate font-mono text-xs">{it.path}</span>
                        </label>
                      )
                    })}
                    {files.length === 0 && (
                      <div className="px-3 py-6 text-center text-xs text-muted-foreground">-</div>
                    )}
                  </div>
                </div>
              ) : null}

              <div className="mt-3 flex gap-2">
                <Input
                  value={manual}
                  onChange={(e) => setManual(e.target.value)}
                  placeholder={t("jobs.wizard.sourceTablePlaceholder")}
                  className="font-mono text-xs"
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && manual.trim()) {
                      toggleStream(manual.trim())
                      setManual("")
                    }
                  }}
                />
                <Button
                  variant="secondary"
                  disabled={!manual.trim()}
                  onClick={() => {
                    if (manual.trim()) {
                      toggleStream(manual.trim())
                      setManual("")
                    }
                  }}
                >
                  <Check className="size-4" />
                </Button>
              </div>
              <p className="mt-1.5 text-xs text-muted-foreground">{t("jobs.wizard.wildcardHint")}</p>
            </FieldGroup>
          )}

          {spec.streams.length > 0 && (
            <div className="flex items-center gap-2.5 rounded-xl border border-primary/25 bg-primary/[0.05] px-3.5 py-2.5 text-sm">
              <Check className="size-4 shrink-0 text-primary" />
              <span>
                {t("jobs.wizard.selectedStreams", { count: spec.streams.length })}
                {spec.streams.length > 1 && (
                  <span className="ml-1 text-xs text-muted-foreground">
                    {t("jobs.wizard.selectedStreamsBatch")}
                  </span>
                )}
              </span>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

/* ------------------------------- Step 2: 目标 ----------------------------- */

function StepTarget({ spec, setSpec, conns }: { spec: JobSpec; setSpec: (s: JobSpec) => void; conns: Connection[] }) {
  const { t } = useTranslation()
  return (
    <div className="space-y-6">
      <StepIntro n={2} />
      <Card>
        <CardContent className="space-y-7 p-6">
          <FieldGroup label={t("jobs.wizard.targetConn")}>
            <ConnPicker value={spec.target_conn} onSelect={(name) => setSpec({ ...spec, target_conn: name })} conns={conns} />
          </FieldGroup>

          {spec.target_conn && (
            <FieldGroup label={t("jobs.wizard.targetTable")}>
              <MappingTable spec={spec} setSpec={setSpec} conns={conns} />
              <div className="mt-2.5 flex items-center gap-2 rounded-lg bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
                <Info className="size-3.5 shrink-0" />
                {t("jobs.wizard.targetAutoCreate")}
              </div>
            </FieldGroup>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

/* ---------------------------- Step 3: 同步方式 ---------------------------- */

function StepMode({
  spec,
  setSpec,
  sourceIsDb,
}: {
  spec: JobSpec
  setSpec: (s: JobSpec) => void
  sourceIsDb: boolean
}) {
  const { t } = useTranslation()
  const mode = (spec.defaults.mode || "replace") as ModeKey
  const streams = streamsOf(spec)
  const firstStream = streams[0]
  const multi = streams.length > 1
  const needsKeys = mode === "upsert" || mode === "append"

  // 全库列级发现（一次查询）：defaults 推导用第一张表，批量时供逐表更新键启发式
  const { data: colsByTable } = useQuery({
    queryKey: ["wizard-cols", spec.source_conn],
    queryFn: () => api.discover(spec.source_conn, "column"),
    enabled: !!spec.source_conn && sourceIsDb && streams.length > 0 && needsKeys,
    staleTime: 120_000,
    select: (d) => {
      const m = new Map<string, { name: string; type: string }[]>()
      for (const schema of d.schemas ?? []) {
        for (const tb of schema.tables ?? []) {
          if (tb.columns?.length) m.set(`${schema.name}.${tb.name}`.toLowerCase(), tb.columns)
        }
      }
      return m
    },
  })

  const firstCols = useMemo(() => {
    if (!firstStream?.source_object) return []
    return colsByTable?.get(firstStream.source_object.toLowerCase()) ?? []
  }, [colsByTable, firstStream?.source_object])

  const columns = firstCols
  const suggestedPk = useMemo(
    () => columns.find((c) => /(^|_)(id|uuid|key)$/i.test(c.name))?.name ?? columns[0]?.name ?? "",
    [columns],
  )
  const suggestedUk = useMemo(
    () => columns.find((c) => /(updated|modified|create)?_?at$|timestamp|updated/i.test(c.name))?.name ?? columns[0]?.name ?? "",
    [columns],
  )

  // defaults 自动填充建议（单表与既有行为一致）
  useEffect(() => {
    if (mode === "upsert" && !(spec.defaults.primary_key ?? []).length && suggestedPk) {
      setSpec({ ...spec, defaults: { ...spec.defaults, primary_key: [suggestedPk] } })
    }
    if (mode === "append" && !spec.defaults.update_key && suggestedUk) {
      setSpec({ ...spec, defaults: { ...spec.defaults, update_key: suggestedUk } })
    }
  }, [mode, suggestedPk, suggestedUk])

  /* ---------- 批量：逐表主键（源库真实主键元数据） ---------- */
  const concreteTables = useMemo(
    () => streams.map((s) => s.source_object ?? "").filter((o) => o && !hasWildcard(o) && !isQuerySrc(o)),
    [streams],
  )
  const pkQueryKey = useMemo(() => [...concreteTables].sort(), [concreteTables])
  const { data: pks } = useQuery({
    queryKey: ["wizard-pks", spec.source_conn, pkQueryKey],
    queryFn: () => api.primaryKeys(spec.source_conn, pkQueryKey),
    enabled: !!spec.source_conn && sourceIsDb && multi && mode === "upsert" && pkQueryKey.length > 0,
    staleTime: 120_000,
  })

  // 自动填充：仅在流缺少主键时写入源库真实主键，绝不覆盖用户输入
  useEffect(() => {
    if (!pks?.keys || mode !== "upsert") return
    let changed = false
    const streamsNext = spec.streams.map((s) => {
      const src = s.source_object ?? ""
      if (s.disabled || s.primary_key?.length || !src) return s
      const found = pks.keys[src]
      if (found?.length) {
        changed = true
        return { ...s, primary_key: found }
      }
      // 无元数据时退回列名启发式
      const cols = colsByTable?.get(src.toLowerCase()) ?? []
      const guess = cols.find((c) => /(^|_)(id|uuid|key)$/i.test(c.name))?.name
      if (guess) {
        changed = true
        return { ...s, primary_key: [guess] }
      }
      return s
    })
    if (changed) setSpec({ ...spec, streams: streamsNext })
  }, [pks, mode, colsByTable])

  function setStream(idx: number, patch: Partial<StreamSpec>) {
    const next = [...spec.streams]
    next[idx] = { ...next[idx], ...patch }
    setSpec({ ...spec, streams: next })
  }

  function guessUk(src: string): string {
    const cols = colsByTable?.get(src.toLowerCase()) ?? []
    return cols.find((c) => /(updated|modified|create)?_?at$|timestamp|updated/i.test(c.name))?.name ?? ""
  }

  return (
    <div className="space-y-6">
      <StepIntro n={3} />
      <Card>
        <CardContent className="space-y-7 p-6">
          <FieldGroup label={t("jobs.wizard.modeTitle")}>
            <div className="grid gap-2.5 sm:grid-cols-2">
              {MODES.map((m) => {
                const selected = mode === m.key
                return (
                  <button
                    key={m.key}
                    type="button"
                    onClick={() => setSpec({ ...spec, defaults: { ...spec.defaults, mode: m.key } })}
                    className={cn(
                      "relative rounded-xl border p-4 text-left transition-all duration-200 active:scale-[0.99]",
                      selected
                        ? "border-primary/60 bg-primary/[0.03] shadow-sm"
                        : "border-border hover:border-primary/30 hover:bg-muted/30",
                    )}
                  >
                    <span
                      className={cn(
                        "absolute right-3.5 top-3.5 flex size-5 items-center justify-center rounded-full border transition-all",
                        selected
                          ? "border-primary bg-primary text-primary-foreground"
                          : "border-muted-foreground/30 text-transparent",
                      )}
                    >
                      <Check className="size-3" strokeWidth={3} />
                    </span>
                    <span className={cn("flex size-9 items-center justify-center rounded-lg", m.chipCls)}>
                      <m.icon className="size-4.5" strokeWidth={2} />
                    </span>
                    <div className="mt-3 pr-6 text-sm font-semibold">{t(`jobs.wizard.mode${cap(m.key)}`)}</div>
                    <p className="mt-1 min-h-8 text-xs leading-relaxed text-muted-foreground">
                      {t(`jobs.wizard.mode${cap(m.key)}Desc`)}
                    </p>
                    {m.badge && (
                      <span
                        className={cn(
                          "mt-1.5 inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-medium",
                          m.badge.cls,
                        )}
                      >
                        {t(`jobs.wizard.${m.badge.key}`)}
                      </span>
                    )}
                  </button>
                )
              })}
            </div>
          </FieldGroup>

          {/* defaults 级键（全部流的兜底） */}
          {needsKeys && sourceIsDb && (
            <div className="grid gap-5 sm:grid-cols-2">
              {mode === "upsert" && (
                <div>
                  <Label>{t("jobs.wizard.primaryKey")}</Label>
                  <ColumnPicker
                    columns={columns}
                    multi
                    value={spec.defaults.primary_key ?? []}
                    onChange={(v) => setSpec({ ...spec, defaults: { ...spec.defaults, primary_key: v } })}
                    placeholder={t("jobs.wizard.primaryKeyHint")}
                  />
                  {!(spec.defaults.primary_key ?? []).length && (
                    <p className="mt-1 text-xs text-destructive">{t("jobs.wizard.pkRequired")}</p>
                  )}
                </div>
              )}
              <div>
                <Label>{t("jobs.wizard.updateKey")}</Label>
                <ColumnPicker
                  columns={columns}
                  value={spec.defaults.update_key ? [spec.defaults.update_key] : []}
                  onChange={(v) => setSpec({ ...spec, defaults: { ...spec.defaults, update_key: v[0] ?? "" } })}
                  placeholder={t("jobs.wizard.updateKeyHint")}
                />
                {mode === "append" && !spec.defaults.update_key && (
                  <p className="mt-1 text-xs text-destructive">{t("jobs.wizard.ukRequired")}</p>
                )}
              </div>
            </div>
          )}

          {needsKeys && !sourceIsDb && (
            <div className="grid gap-5 sm:grid-cols-2">
              {mode === "upsert" && (
                <div>
                  <Label>{t("jobs.wizard.primaryKey")}</Label>
                  <Input
                    value={(spec.defaults.primary_key ?? []).join(",")}
                    onChange={(e) => setSpec({ ...spec, defaults: { ...spec.defaults, primary_key: e.target.value.split(",").map((s) => s.trim()).filter(Boolean) } })}
                    placeholder="id, name"
                    className="mt-1.5"
                  />
                  <p className="mt-1 text-xs text-muted-foreground">{t("jobs.wizard.pkManualHint")}</p>
                </div>
              )}
              <div>
                <Label>{t("jobs.wizard.updateKey")}</Label>
                <Input
                  value={spec.defaults.update_key}
                  onChange={(e) => setSpec({ ...spec, defaults: { ...spec.defaults, update_key: e.target.value } })}
                  placeholder="updated_at"
                  className="mt-1.5"
                />
              </div>
            </div>
          )}

          {/* 批量逐表键：源库真实主键预填 + 逐行覆盖（Airbyte 式 per-stream keys） */}
          {needsKeys && sourceIsDb && multi && (
            <FieldGroup
              label={t("jobs.keys.perTableTitle")}
              desc={t("jobs.keys.perTableDesc")}
            >
              <div className="overflow-hidden rounded-xl border">
                <div className="scrollbar-thin max-h-72 overflow-y-auto">
                  {spec.streams.map((s, idx) => {
                    if (s.disabled) return null
                    const src = s.source_object ?? ""
                    const wildcard = hasWildcard(src)
                    const query = isQuerySrc(src)
                    const hasPk = (s.primary_key ?? []).length > 0 || (spec.defaults.primary_key ?? []).length > 0
                    const hasUk = !!s.update_key || !!spec.defaults.update_key
                    const ok = mode === "upsert" ? hasPk : hasUk
                    const detected = pks?.keys?.[src]
                    return (
                      <div
                        key={src}
                        className="flex flex-wrap items-center gap-2 border-b px-3 py-2 transition-colors last:border-b-0 hover:bg-muted/30"
                      >
                        <span className="min-w-0 flex-1 truncate font-mono text-xs">{src}</span>
                        {mode === "upsert" && (
                          <Input
                            value={(s.primary_key ?? []).join(",")}
                            placeholder={
                              wildcard || query
                                ? t("jobs.keys.manualPlaceholder")
                                : (spec.defaults.primary_key ?? []).length
                                  ? t("jobs.keys.inheritPlaceholder", { value: (spec.defaults.primary_key ?? []).join(",") })
                                  : detected?.length
                                    ? t("jobs.keys.detectedPlaceholder")
                                    : t("jobs.keys.pkPlaceholder")
                            }
                            disabled={wildcard || query}
                            className="h-8 w-44 font-mono text-xs"
                            onChange={(e) =>
                              setStream(idx, { primary_key: e.target.value.split(",").map((x) => x.trim()).filter(Boolean) })
                            }
                          />
                        )}
                        {mode === "append" && (
                          <Input
                            value={s.update_key ?? ""}
                            placeholder={
                              spec.defaults.update_key
                                ? t("jobs.keys.inheritPlaceholder", { value: spec.defaults.update_key })
                                : wildcard || query
                                  ? t("jobs.keys.manualPlaceholder")
                                  : guessUk(src) || t("jobs.keys.ukPlaceholder")
                            }
                            disabled={wildcard || query}
                            className="h-8 w-40 font-mono text-xs"
                            onChange={(e) => setStream(idx, { update_key: e.target.value })}
                          />
                        )}
                        {ok ? (
                          <Badge variant="outline" className="shrink-0 border-emerald-500/40 bg-emerald-500/10 text-[10px] text-emerald-600 dark:text-emerald-400">
                            <Check className="size-3" />
                          </Badge>
                        ) : (
                          <Badge variant="outline" className="shrink-0 gap-1 border-amber-500/40 bg-amber-500/10 text-[10px] text-amber-600 dark:text-amber-400">
                            <TriangleAlert className="size-3" /> {t("jobs.keys.missing")}
                          </Badge>
                        )}
                      </div>
                    )
                  })}
                </div>
              </div>
              <div className="mt-2 flex items-center justify-between gap-3">
                <p className="text-xs text-muted-foreground">{t("jobs.keys.hint")}</p>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() =>
                    setSpec({
                      ...spec,
                      streams: spec.streams.map((s) => ({ ...s, primary_key: undefined, update_key: undefined })),
                    })
                  }
                >
                  <Eraser className="size-3.5" /> {t("jobs.keys.resetToDefault")}
                </Button>
              </div>
            </FieldGroup>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function ColumnPicker({
  columns,
  value,
  onChange,
  multi,
  placeholder,
}: {
  columns: { name: string; type: string }[]
  value: string[]
  onChange: (v: string[]) => void
  multi?: boolean
  placeholder?: string
}) {
  const { t } = useTranslation()
  if (columns.length === 0) {
    return <Input value={value.join(",")} onChange={(e) => onChange(e.target.value.split(",").map((s) => s.trim()).filter(Boolean))} placeholder={placeholder} className="mt-1.5 font-mono text-xs" />
  }
  return (
    <div className="mt-1.5 overflow-hidden rounded-lg border">
      {/* 联合主键：已选字段 chips（按勾选顺序，可移除） */}
      {multi && value.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5 border-b bg-muted/30 px-2.5 py-2">
          {value.map((name, i) => (
            <span
              key={name}
              className="inline-flex items-center gap-1 rounded-md border border-primary/30 bg-primary/10 py-0.5 pl-2 pr-1 font-mono text-[11px]"
            >
              <span className="text-muted-foreground">{i + 1}.</span>
              {name}
              <button
                type="button"
                aria-label={`remove ${name}`}
                onClick={() => onChange(value.filter((v) => v !== name))}
                className="rounded-sm p-0.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              >
                <X className="size-3" strokeWidth={2.5} />
              </button>
            </span>
          ))}
          <span className="ml-auto shrink-0 text-[10px] text-muted-foreground">
            {t("jobs.wizard.compositeSelected", { count: value.length })}
          </span>
        </div>
      )}
      <div className="scrollbar-thin max-h-40 overflow-y-auto">
        {columns.map((c) => (
          <label key={c.name} className="flex cursor-pointer items-center gap-2.5 border-b px-3 py-1.5 text-xs last:border-b-0 hover:bg-muted/50">
            <Checkbox
              checked={value.includes(c.name)}
              onCheckedChange={(checked) => {
                if (multi) onChange(checked ? [...value, c.name] : value.filter((v) => v !== c.name))
                else onChange(checked ? [c.name] : [])
              }}
            />
            <span className="font-mono">{c.name}</span>
            <span className="ml-auto text-muted-foreground">{c.type}</span>
          </label>
        ))}
      </div>
    </div>
  )
}

/* ----------------------------- Step 4: 高级 ------------------------------- */

const ADV_GROUPS: {
  icon: typeof TableProperties
  titleKey: string
  items: { key: "add_new_columns" | "adjust_column_type" | "retry_max" | "verify_rows" | "loaded_at"; labelKey: string; descKey: string; type: "switch" | "number" }[]
}[] = [
  {
    icon: TableProperties,
    titleKey: "advGroupStructure",
    items: [
      { key: "add_new_columns", labelKey: "schemaEvo", descKey: "schemaEvoDesc", type: "switch" },
      { key: "adjust_column_type", labelKey: "adjustType", descKey: "adjustTypeDesc", type: "switch" },
    ],
  },
  {
    icon: ShieldCheck,
    titleKey: "advGroupReliability",
    items: [
      { key: "retry_max", labelKey: "retry", descKey: "retryDesc", type: "number" },
      { key: "verify_rows", labelKey: "verify", descKey: "verifyDesc", type: "switch" },
    ],
  },
  {
    icon: Tags,
    titleKey: "advGroupMeta",
    items: [{ key: "loaded_at", labelKey: "metadata", descKey: "loadedAtDesc", type: "switch" }],
  },
]

function StepAdvanced({
  advanced,
  setAdvanced,
}: {
  advanced: { add_new_columns: boolean; adjust_column_type: boolean; retry_max: number; verify_rows: boolean; loaded_at: boolean }
  setAdvanced: (a: typeof advanced) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="space-y-6">
      <StepIntro n={4} />
      <Card>
        <CardContent className="p-6">
          {ADV_GROUPS.map((g, gi) => (
            <div key={g.titleKey} className={gi > 0 ? "mt-7 border-t pt-6" : undefined}>
              <div className="mb-1 flex items-center gap-2 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                <g.icon className="size-3.5" />
                {t(`jobs.wizard.${g.titleKey}`)}
              </div>
              {g.items.map((it) => (
                <div key={it.key} className="flex items-center justify-between gap-6 py-3">
                  <div className="min-w-0">
                    <div className="text-sm font-medium">{t(`jobs.wizard.${it.labelKey}`)}</div>
                    <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
                      {t(`jobs.wizard.${it.descKey}`)}
                    </p>
                  </div>
                  {it.type === "switch" ? (
                    <Switch
                      checked={advanced[it.key] as boolean}
                      onCheckedChange={(v) => setAdvanced({ ...advanced, [it.key]: v })}
                    />
                  ) : (
                    <div className="flex shrink-0 items-center gap-1.5">
                      <Input
                        type="number"
                        min={0}
                        max={10}
                        value={advanced.retry_max}
                        onChange={(e) => setAdvanced({ ...advanced, retry_max: Number(e.target.value) || 0 })}
                        className="w-16 text-center"
                      />
                      <span className="text-xs text-muted-foreground">{t("jobs.wizard.retryTimes")}</span>
                    </div>
                  )}
                </div>
              ))}
            </div>
          ))}
        </CardContent>
      </Card>
    </div>
  )
}

/* --------------------------- Step 5: 调度与保存 ---------------------------- */

const PRESETS: { key: string; label: string; cron: string }[] = [
  { key: "5min", label: "每 5 分钟", cron: "*/5 * * * *" },
  { key: "hourly", label: "每小时", cron: "0 * * * *" },
  { key: "daily6", label: "每天 06:00", cron: "0 6 * * *" },
  { key: "daily9", label: "每天 09:00", cron: "0 9 * * *" },
  { key: "daily2am", label: "每天 02:00", cron: "0 2 * * *" },
  { key: "weeklyMon", label: "每周一 09:00", cron: "0 9 * * 1" },
]

function StepFinal({
  name,
  setName,
  schedule,
  setSchedule,
  onShowYaml,
}: {
  name: string
  setName: (v: string) => void
  schedule: { enabled: boolean; cron: string; preset: string }
  setSchedule: (s: { enabled: boolean; cron: string; preset: string }) => void
  onShowYaml: () => void
}) {
  const { t } = useTranslation()
  const scheduleCards = [
    {
      key: "manual" as const,
      icon: MousePointerClick,
      chipCls: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
      title: t("jobs.wizard.scheduleNone"),
      desc: t("jobs.wizard.scheduleNoneDesc"),
      selected: !schedule.enabled,
      onClick: () => setSchedule({ ...schedule, enabled: false }),
    },
    {
      key: "cron" as const,
      icon: Clock,
      chipCls: "bg-violet-500/10 text-violet-600 dark:text-violet-400",
      title: t("jobs.wizard.scheduleEvery"),
      desc: schedule.enabled ? cronToHuman(schedule.cron) : t("jobs.wizard.scheduleEveryDesc"),
      selected: schedule.enabled,
      onClick: () => setSchedule({ ...schedule, enabled: true }),
    },
  ]
  return (
    <div className="space-y-6">
      <StepIntro n={5} />
      <Card>
        <CardContent className="space-y-7 p-6">
          <FieldGroup label={`${t("jobs.wizard.jobName")} *`}>
            <Input
              id="job-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("jobs.wizard.jobNamePlaceholder")}
              className="h-10"
            />
          </FieldGroup>

          <FieldGroup label={t("jobs.wizard.runMode")}>
            <div className="grid gap-2.5 sm:grid-cols-2">
              {scheduleCards.map((c) => (
                <button
                  key={c.key}
                  type="button"
                  onClick={c.onClick}
                  className={cn(
                    "relative rounded-xl border p-4 text-left transition-all duration-200 active:scale-[0.99]",
                    c.selected
                      ? "border-primary/60 bg-primary/[0.03] shadow-sm"
                      : "border-border hover:border-primary/30 hover:bg-muted/30",
                  )}
                >
                  <span
                    className={cn(
                      "absolute right-3.5 top-3.5 flex size-5 items-center justify-center rounded-full border transition-all",
                      c.selected
                        ? "border-primary bg-primary text-primary-foreground"
                        : "border-muted-foreground/30 text-transparent",
                    )}
                  >
                    <Check className="size-3" strokeWidth={3} />
                  </span>
                  <span className={cn("flex size-9 items-center justify-center rounded-lg", c.chipCls)}>
                    <c.icon className="size-4.5" />
                  </span>
                  <div className="mt-3 pr-6 text-sm font-semibold">{c.title}</div>
                  <p className="mt-1 text-xs leading-relaxed text-muted-foreground">{c.desc}</p>
                </button>
              ))}
            </div>
          </FieldGroup>

          {schedule.enabled && (
            <div className="space-y-3">
              <div className="flex flex-wrap gap-2">
                {PRESETS.map((p) => (
                  <button
                    key={p.key}
                    type="button"
                    onClick={() => setSchedule({ enabled: true, cron: p.cron, preset: p.key })}
                    className={cn(
                      "rounded-full border px-3 py-1.5 text-xs transition-all duration-200 active:scale-[0.97]",
                      schedule.cron === p.cron
                        ? "border-primary bg-primary/10 font-medium text-primary"
                        : "text-muted-foreground hover:border-primary/50 hover:text-foreground",
                    )}
                  >
                    {p.label}
                  </button>
                ))}
              </div>
              <div>
                <Label>{t("jobs.wizard.scheduleCron")}</Label>
                <div className="mt-1.5 flex items-center gap-3">
                  <Input
                    value={schedule.cron}
                    onChange={(e) => setSchedule({ enabled: true, cron: e.target.value, preset: "custom" })}
                    placeholder="0 9 * * *"
                    className="w-48 font-mono"
                  />
                  <span className="text-xs text-muted-foreground">= {cronToHuman(schedule.cron)}</span>
                </div>
              </div>
            </div>
          )}

          <div className="flex items-center gap-2.5 rounded-xl border border-dashed px-4 py-3.5">
            <Eye className="size-4 shrink-0 text-muted-foreground" />
            <span className="flex-1 text-sm text-muted-foreground">{t("jobs.wizard.yamlTeaser")}</span>
            <Button variant="outline" size="sm" onClick={onShowYaml}>
              {t("jobs.wizard.yamlPreview")}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

/* ------------------------------- YAML 预览 -------------------------------- */

function YamlPreview({ spec, onClose }: { spec: JobSpec; onClose: () => void }) {
  const { t } = useTranslation()
  const yaml = useMemo(() => specToYamlLocal(spec), [spec])
  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("jobs.wizard.yamlPreview")}</DialogTitle>
        </DialogHeader>
        <pre className="scrollbar-thin max-h-[50vh] overflow-auto rounded-lg bg-muted/50 p-4 font-mono text-xs leading-relaxed">
          {yaml}
        </pre>
        <Button
          variant="secondary"
          onClick={() => {
            navigator.clipboard.writeText(yaml)
            toast.success(t("common.copied"))
          }}
        >
          复制
        </Button>
      </DialogContent>
    </Dialog>
  )
}

/** 本地 YAML 渲染（与后端 ToYAML 导出格式一致的轻量实现，用于向导实时预览） */
function specToYamlLocal(spec: JobSpec): string {
  const modeMap: Record<string, string> = { replace: "full-refresh", truncate: "truncate", append: "incremental", upsert: "incremental", snapshot: "snapshot" }
  const lines: string[] = []
  lines.push(`source: ${spec.source_conn}`)
  lines.push(`target: ${spec.target_conn}`)
  lines.push("defaults:")
  const mode = modeMap[spec.defaults.mode ?? "replace"] ?? "full-refresh"
  lines.push(`  mode: ${mode}`)
  if (spec.defaults.update_key) lines.push(`  update_key: ${spec.defaults.update_key}`)
  if (spec.defaults.primary_key?.length) lines.push(`  primary_key: ${spec.defaults.primary_key.join(", ")}`)
  lines.push("streams:")
  for (const s of streamsOf(spec)) {
    const key = s.name || s.source_object
    lines.push(`  ${key}:`)
    // 与后端一致：同名默认也写显式 object（CLI 复制流必须显式指定 object）
    const obj = effectiveTarget(s.source_object ?? "", s.target_object)
    if (obj) lines.push(`    object: ${obj}`)
    if (s.update_key) lines.push(`    update_key: ${s.update_key}`)
    if (s.primary_key?.length) lines.push(`    primary_key: ${s.primary_key.join(", ")}`)
  }
  return lines.join("\n")
}

function isDbConn(name: string, conns?: Connection[]) {
  const c = (conns ?? []).find((x) => x.name === name)
  if (!c) return true
  return !["file", "s3", "gs", "azure", "abfs", "ftp", "sftp", "http", "gdrive"].includes(c.type)
}

function streamsOf(spec: JobSpec): StreamSpec[] {
  return (spec.streams ?? []).filter((s) => !s.disabled)
}

/** 破坏性确认的目标表清单：批量场景截断为前 5 张 + 剩余计数，避免弹窗刷屏 */
function confirmTablesText(spec: JobSpec, t: (k: string, v?: Record<string, unknown>) => string): string {
  const tables = streamsOf(spec).map((s) => s.target_object || s.source_object || "")
  if (tables.length <= 5) return tables.join(", ")
  return t("jobs.runConfirmTablesMore", { tables: tables.slice(0, 5).join(", "), count: tables.length - 5 })
}

function cap(s: string) {
  return s.charAt(0).toUpperCase() + s.slice(1)
}
