// 运行前分析对话框：每张源表的字段/行数/存储 + 汇总指标 + 负载评估
// 布局：4xl 宽弹窗，固定头/尾 + 中部滚动；汇总带（分割网格）+ 负载面板 + 明细表（粘性表头、可展开字段）
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { useMutation } from "@tanstack/react-query"
import { toast } from "sonner"
import {
  AlertTriangle,
  ArrowRight,
  BarChart3,
  ChevronDown,
  Database,
  FileText,
  Gauge,
  Loader2,
  Timer,
} from "lucide-react"
import { api } from "@/lib/api"
import type { JobSpec, StreamAnalysis } from "@/lib/types"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { formatBytes, formatDuration, formatNumber } from "@/lib/format"

/** 明细表列模板：对象 | 字段 | 行数 | 大小 | 目标 */
const GRID = "grid grid-cols-[minmax(0,1fr)_3rem_5rem_5rem_8.5rem] items-center gap-3"

const LOAD_STYLES = {
  low: {
    panel: "border-emerald-500/30 bg-emerald-500/[0.06]",
    chip: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
    text: "text-emerald-600 dark:text-emerald-400",
  },
  medium: {
    panel: "border-amber-500/30 bg-amber-500/[0.06]",
    chip: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
    text: "text-amber-600 dark:text-amber-400",
  },
  high: {
    panel: "border-rose-500/30 bg-rose-500/[0.06]",
    chip: "bg-rose-500/10 text-rose-600 dark:text-rose-400",
    text: "text-rose-600 dark:text-rose-400",
  },
} as const

export function AnalysisDialog({
  open,
  onOpenChange,
  spec,
  jobId,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  spec?: JobSpec
  jobId?: string
}) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState<string | null>(null)

  const analyze = useMutation({
    mutationFn: () => (jobId ? api.analyzeJob(jobId) : api.analyzeSpec(spec!)),
    onError: (e) => toast.error(e.message),
  })

  // auto-run on open
  if (open && !analyze.data && !analyze.isPending && !analyze.isError) {
    analyze.mutate()
  }

  const data = analyze.data
  const load = (data?.totals.load ?? "low") as keyof typeof LOAD_STYLES
  const loadStyle = LOAD_STYLES[load] ?? LOAD_STYLES.low

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[85vh] flex-col gap-0 overflow-hidden p-0 sm:max-w-4xl">
        {/* 头部：标题 + 数据链路 + 分析时间（固定不滚动） */}
        <DialogHeader className="shrink-0 border-b px-6 py-4">
          <DialogTitle className="flex flex-wrap items-center gap-x-3 gap-y-1.5 pr-8 text-base">
            <span className="flex items-center gap-2">
              <BarChart3 className="size-5 text-primary" />
              {t("analysis.title")}
            </span>
            {data && (
              <span className="flex items-center gap-1.5 rounded-full bg-muted px-2.5 py-1 text-xs font-normal text-muted-foreground">
                {data.source_conn}
                <ArrowRight className="size-3" />
                {data.target_conn}
              </span>
            )}
            {data?.analyzed_at && (
              <span className="ml-auto text-xs font-normal text-muted-foreground">
                {t("analysis.analyzedAt")} {new Date(data.analyzed_at).toLocaleTimeString()}
              </span>
            )}
          </DialogTitle>
        </DialogHeader>

        {/* 中部：可滚动内容区 */}
        <div className="scrollbar-thin min-h-0 flex-1 overflow-y-auto px-6 py-5">
          {analyze.isPending && (
            <div className="flex flex-col items-center gap-3 py-20 text-muted-foreground">
              <Loader2 className="size-8 animate-spin text-primary" />
              <div className="text-sm">{t("analysis.running")}</div>
              <div className="max-w-sm text-center text-xs">{t("analysis.runningHint")}</div>
            </div>
          )}

          {analyze.isError && (
            <div className="flex items-start gap-3 rounded-xl border border-destructive/30 bg-destructive/5 p-4 text-sm">
              <AlertTriangle className="size-5 shrink-0 text-destructive" />
              <div>{(analyze.error as Error).message}</div>
            </div>
          )}

          {data && (
            <div className="space-y-5">
              {/* 汇总指标带：分割网格，数值大字号 */}
              <div className="grid grid-cols-3 gap-px overflow-hidden rounded-xl border bg-border sm:grid-cols-5">
                <Stat label={t("analysis.tables")} value={formatNumber(data.totals.tables)} />
                <Stat label={t("analysis.columns")} value={formatNumber(data.totals.columns)} />
                <Stat
                  label={t("analysis.rows")}
                  value={data.totals.rows_known ? formatNumber(data.totals.rows) : t("analysis.unknown")}
                />
                <Stat
                  label={t("analysis.size")}
                  value={data.totals.size_known ? formatBytes(data.totals.size_bytes) : t("analysis.unknown")}
                />
                <Stat
                  label={t("analysis.newTargets")}
                  value={formatNumber(data.totals.new_targets)}
                  highlight={data.totals.new_targets > 0 ? "text-emerald-600 dark:text-emerald-400" : undefined}
                />
              </div>

              {/* 负载评估面板：级别 + 预计耗时 + 说明 */}
              <div className={cn("flex flex-wrap items-center gap-x-8 gap-y-4 rounded-xl border p-4", loadStyle.panel)}>
                <div className="flex items-center gap-3">
                  <span className={cn("flex size-10 items-center justify-center rounded-lg", loadStyle.chip)}>
                    <Gauge className="size-5" />
                  </span>
                  <div>
                    <div className="text-[11px] text-muted-foreground">{t("analysis.load")}</div>
                    <div className={cn("text-xl font-bold leading-tight", loadStyle.text)}>
                      {t(`analysis.load_${data.totals.load}`)}
                    </div>
                  </div>
                </div>
                <div className="h-9 w-px bg-border" />
                <div className="flex items-center gap-3">
                  <span className="flex size-10 items-center justify-center rounded-lg bg-muted">
                    <Timer className="size-5 text-muted-foreground" />
                  </span>
                  <div>
                    <div className="text-[11px] text-muted-foreground">{t("analysis.estimate")}</div>
                    <div className="text-xl font-bold leading-tight tabular-nums">
                      {formatDuration(data.totals.estimated_seconds * 1000)}
                    </div>
                  </div>
                </div>
                <p className="ml-auto hidden max-w-56 text-right text-xs leading-relaxed text-muted-foreground lg:block">
                  {t("analysis.loadDesc")}
                  <br />
                  {t("analysis.estimateHint")}
                </p>
              </div>

              {/* 每张表明细：粘性表头 + 可展开字段列表 */}
              <div className="overflow-hidden rounded-xl border">
                <div
                  className={cn(
                    GRID,
                    "sticky top-0 z-10 border-b bg-muted/90 px-4 py-2.5 text-xs font-medium text-muted-foreground backdrop-blur",
                  )}
                >
                  <span>{t("analysis.object")}</span>
                  <span className="text-right">{t("analysis.cols")}</span>
                  <span className="text-right">{t("analysis.rows")}</span>
                  <span className="text-right">{t("analysis.size")}</span>
                  <span className="text-right">{t("analysis.target")}</span>
                </div>
                {data.streams.map((s) => (
                  <StreamRow
                    key={s.source_object + (s.target_object ?? "")}
                    stream={s}
                    expanded={expanded === s.source_object}
                    onToggle={() => setExpanded(expanded === s.source_object ? null : s.source_object)}
                  />
                ))}
                {data.streams.length === 0 && (
                  <div className="px-4 py-10 text-center text-sm text-muted-foreground">
                    {t("analysis.noStreams")}
                  </div>
                )}
              </div>
            </div>
          )}
        </div>

        {/* 底部：刷新 + 数据来源说明（固定不滚动） */}
        {data && (
          <div className="flex shrink-0 items-center justify-between gap-3 border-t px-6 py-3">
            <Button variant="outline" size="sm" onClick={() => analyze.mutate()}>
              {t("app.actions.refresh")}
            </Button>
            <p className="text-xs text-muted-foreground">{t("analysis.footer")}</p>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

/** 分割网格统计单元：细线分隔，无独立卡片边框，降低视觉噪音 */
function Stat({ label, value, highlight }: { label: string; value: string; highlight?: string }) {
  return (
    <div className="bg-background px-4 py-3">
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className={cn("mt-1 text-xl font-bold leading-none tabular-nums", highlight)}>{value}</div>
    </div>
  )
}

function StreamRow({
  stream,
  expanded,
  onToggle,
}: {
  stream: StreamAnalysis
  expanded: boolean
  onToggle: () => void
}) {
  const { t } = useTranslation()

  if (stream.error) {
    return (
      <div className="flex items-center gap-2.5 border-b px-4 py-2.5 text-xs last:border-b-0">
        <AlertTriangle className="size-3.5 shrink-0 text-destructive" />
        <span className="shrink-0 font-mono text-destructive">{stream.source_object}</span>
        <span className="min-w-0 flex-1 truncate text-muted-foreground" title={stream.error}>
          {stream.error}
        </span>
      </div>
    )
  }

  const hasColumns = (stream.columns ?? []).length > 0

  return (
    <div className="last:border-b-0">
      <button
        onClick={onToggle}
        title={stream.source_object}
        className={cn(
          GRID,
          "w-full border-b px-4 py-2.5 text-left transition-colors hover:bg-muted/40",
          expanded && "bg-muted/30",
        )}
      >
        <span className="flex min-w-0 items-center gap-2">
          {stream.kind === "file" ? (
            <FileText className="size-3.5 shrink-0 text-muted-foreground" />
          ) : (
            <Database className="size-3.5 shrink-0 text-muted-foreground" />
          )}
          <span className="truncate font-mono text-xs">{stream.source_object}</span>
          <ChevronDown
            className={cn(
              "size-3 shrink-0 text-muted-foreground transition-transform",
              hasColumns ? "opacity-100" : "opacity-0",
              expanded && "rotate-180",
            )}
          />
        </span>
        <span className="text-right text-xs tabular-nums">{formatNumber(stream.column_count)}</span>
        <span className="text-right text-xs tabular-nums">
          {stream.rows != null ? formatNumber(stream.rows) : "—"}
        </span>
        <span className="text-right text-xs tabular-nums">
          {stream.size_bytes != null ? formatBytes(stream.size_bytes) : "—"}
        </span>
        <span className="flex justify-end">
          {stream.target_exists ? (
            <Badge variant="secondary" className="text-[10px] tabular-nums">
              {t("analysis.exists")}
              {stream.target_rows != null ? ` · ${formatNumber(stream.target_rows)}` : ""}
            </Badge>
          ) : (
            <Badge variant="outline" className="border-emerald-500/40 bg-emerald-500/10 text-[10px] text-emerald-600 dark:text-emerald-400">
              {t("analysis.new")}
            </Badge>
          )}
        </span>
      </button>

      {expanded && (
        <div className="space-y-3 border-b bg-muted/20 px-4 py-3">
          {/* 目标映射：源 → 目标（重命名时才显示） */}
          {stream.target_object && stream.target_object !== stream.source_object && (
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <span className="font-mono">{stream.source_object}</span>
              <ArrowRight className="size-3" />
              <span className="font-mono text-foreground">{stream.target_object}</span>
            </div>
          )}

          {/* 字段明细：两/三列网格，名称 + 类型对齐 */}
          {hasColumns ? (
            <div>
              <div className="mb-2 text-[11px] font-medium text-muted-foreground">
                {t("analysis.columnsDetail")} · {stream.columns!.length}
              </div>
              <div className="grid gap-1 sm:grid-cols-2 lg:grid-cols-3">
                {stream.columns!.map((c) => (
                  <div
                    key={c.name}
                    className="flex items-baseline justify-between gap-3 rounded-md bg-background px-2.5 py-1.5"
                  >
                    <span className="truncate font-mono text-xs">{c.name}</span>
                    {c.type && (
                      <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{c.type}</span>
                    )}
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <div className="text-xs text-muted-foreground">—</div>
          )}
        </div>
      )}

      {(stream.warnings ?? []).length > 0 && (
        <div className="space-y-1 border-b bg-amber-500/5 px-4 py-2 text-[11px] text-amber-700 dark:text-amber-400">
          {(stream.warnings ?? []).map((w, i) => (
            <div key={i} className="flex items-start gap-1.5">
              <AlertTriangle className="mt-0.5 size-3 shrink-0" />
              <span>{w}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
