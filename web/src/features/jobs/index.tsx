// 任务列表：搜索（名称/源/目标）/调度筛选/排序/分页 + 卡片操作（运行/暂停/复制/导出/删除）
import { useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "react-router-dom"
import { toast } from "sonner"
import {
  AlarmClock,
  ArrowRight,
  ArrowRightLeft,
  CalendarClock,
  BarChart3,
  Copy,
  Download,
  Loader2,
  MoreHorizontal,
  PauseCircle,
  Play,
  PlayCircle,
  Plus,
  Trash2,
} from "lucide-react"
import { api } from "@/lib/api"
import type { Job } from "@/lib/types"
import { PageHeader } from "@/features/dashboard"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { EmptyState } from "@/components/empty-state"
import { ListPagination, ListToolbar, NoMatchState } from "@/components/list-toolbar"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { AnalysisDialog } from "@/features/jobs/analysis-dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { countdown, cronToHuman, relativeTime } from "@/lib/format"

const PAGE_SIZE = 10

export default function Jobs() {
  const { t } = useTranslation()
  const nav = useNavigate()
  const [q, setQ] = useState("")
  const [schedF, setSchedF] = useState("all")
  const [sort, setSort] = useState("newest")
  const [page, setPage] = useState(0)
  const [deleteTarget, setDeleteTarget] = useState<Job | null>(null)
  const [analyzeTarget, setAnalyzeTarget] = useState<Job | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ["jobs"],
    queryFn: () => api.listJobs(),
    refetchInterval: 10_000,
  })

  useEffect(() => setPage(0), [q, schedF, sort])

  // 源/目标连接名（从 spec_json 提取一次，供搜索）
  const connsOf = useMemo(() => {
    const m = new Map<string, [string, string]>()
    for (const j of data?.jobs ?? []) {
      try {
        const s = JSON.parse(j.spec_json ?? "{}")
        m.set(j.id, [s.source_conn ?? "", s.target_conn ?? ""])
      } catch {
        m.set(j.id, ["", ""])
      }
    }
    return m
  }, [data])

  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase()
    const out = (data?.jobs ?? []).filter((j) => {
      if (j.is_template) return false
      if (schedF !== "all" && (schedF === "scheduled") !== j.schedule_enabled) return false
      if (!needle) return true
      const [src, tgt] = connsOf.get(j.id) ?? ["", ""]
      return (
        j.name.toLowerCase().includes(needle) ||
        (j.description ?? "").toLowerCase().includes(needle) ||
        src.toLowerCase().includes(needle) ||
        tgt.toLowerCase().includes(needle)
      )
    })
    return [...out].sort((a, b) => {
      if (sort === "name") return a.name.localeCompare(b.name)
      if (sort === "nextRun") {
        const av = a.next_run_at ? new Date(a.next_run_at).getTime() : Infinity
        const bv = b.next_run_at ? new Date(b.next_run_at).getTime() : Infinity
        return av - bv
      }
      return new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()
    })
  }, [data, q, schedF, sort, connsOf])

  const pages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE))
  const pageItems = filtered.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE)
  const hasFilters = q.trim() !== "" || schedF !== "all"

  return (
    <div className="space-y-6">
      <PageHeader
        title={t("jobs.title")}
        subtitle={t("jobs.subtitle")}
        actions={
          <Button onClick={() => nav("/jobs/new")}>
            <Plus className="size-4" /> {t("app.actions.newJob")}
          </Button>
        }
      />

      <ListToolbar
        search={q}
        onSearch={setQ}
        searchPlaceholder={t("jobs.searchPlaceholder")}
        sort={sort}
        onSort={setSort}
        sortOptions={[
          { value: "newest", label: t("common.sortByNewest") },
          { value: "name", label: t("common.sortByName") },
          { value: "nextRun", label: t("common.sortByNextRun") },
        ]}
      >
        <Select value={schedF} onValueChange={setSchedF}>
          <SelectTrigger size="sm" className="h-9 w-32 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("common.allSchedules")}</SelectItem>
            <SelectItem value="scheduled">{t("jobs.wizard.scheduleEvery")}</SelectItem>
            <SelectItem value="manual">{t("jobs.wizard.scheduleNone")}</SelectItem>
          </SelectContent>
        </Select>
      </ListToolbar>

      {isLoading ? (
        <div className="space-y-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-24" />
          ))}
        </div>
      ) : (data?.jobs ?? []).length === 0 ? (
        <EmptyState
          icon={ArrowRightLeft}
          title={t("jobs.emptyTitle")}
          body={t("jobs.emptyBody")}
          action={t("app.actions.newJob")}
          onAction={() => nav("/jobs/new")}
        />
      ) : filtered.length === 0 ? (
        <NoMatchState
          hasFilters={hasFilters}
          onClear={() => {
            setQ("")
            setSchedF("all")
          }}
        />
      ) : (
        <>
          <div className="space-y-3">
            {pageItems.map((job) => (
              <JobRow key={job.id} job={job} onDelete={() => setDeleteTarget(job)} onAnalyze={() => setAnalyzeTarget(job)} />
            ))}
          </div>
          <ListPagination total={filtered.length} page={page} pages={pages} onPage={setPage} />
        </>
      )}

      {analyzeTarget && (
        <AnalysisDialog open onOpenChange={() => setAnalyzeTarget(null)} jobId={analyzeTarget.id} />
      )}

      <AlertDialog open={!!deleteTarget} onOpenChange={(v) => !v && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("jobs.deleteConfirmTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("jobs.deleteConfirmBody", { name: deleteTarget?.name })}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("app.actions.cancelEdit")}</AlertDialogCancel>
            <JobDeleteAction job={deleteTarget} onDone={() => setDeleteTarget(null)} />
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

function JobDeleteAction({ job, onDone }: { job: Job | null; onDone: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const del = useMutation({
    mutationFn: () => api.deleteJob(job!.id),
    onSuccess: () => {
      toast.success(t("common.saved"))
      qc.invalidateQueries({ queryKey: ["jobs"] })
      onDone()
    },
    onError: (e) => toast.error(e.message),
  })
  return (
    <AlertDialogAction
      className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
      onClick={(e) => {
        e.preventDefault()
        del.mutate()
      }}
    >
      {t("app.actions.delete")}
    </AlertDialogAction>
  )
}

function JobRow({ job, onDelete, onAnalyze }: { job: Job; onDelete: () => void; onAnalyze: () => void }) {
  const { t } = useTranslation()
  const nav = useNavigate()
  const qc = useQueryClient()

  const spec = useMemo(() => {
    try {
      return JSON.parse(job.spec_json ?? "{}")
    } catch {
      return {}
    }
  }, [job.spec_json])

  const run = useMutation({
    mutationFn: () => api.runJob(job.id),
    onSuccess: () => {
      toast.success(`已触发「${job.name}」`)
      qc.invalidateQueries({ queryKey: ["runs"] })
    },
    onError: (e) => toast.error(e.message),
  })

  const toggleSchedule = useMutation({
    mutationFn: () => api.setSchedule(job.id, { enabled: !job.schedule_enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["jobs"] }),
    onError: (e) => toast.error(e.message),
  })

  const dup = useMutation({
    mutationFn: () => api.duplicateJob(job.id),
    onSuccess: () => {
      toast.success("已复制")
      qc.invalidateQueries({ queryKey: ["jobs"] })
    },
    onError: (e) => toast.error(e.message),
  })

  const streamCount = spec?.streams?.length ?? 1
  const modeLabel: Record<string, string> = {
    replace: t("jobs.wizard.modeReplace"),
    truncate: t("jobs.wizard.modeTruncate"),
    append: t("jobs.wizard.modeAppend"),
    upsert: t("jobs.wizard.modeUpsert"),
    snapshot: t("jobs.wizard.modeSnapshot"),
  }
  const mode = modeLabel[spec?.defaults?.mode] ?? spec?.defaults?.mode ?? "-"

  return (
    <div
      className="group flex cursor-pointer items-center gap-4 rounded-xl border bg-card p-4 transition-all hover:shadow-md"
      onClick={() => nav(`/jobs/${job.id}/edit`)}
    >
      <div className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary">
        <ArrowRightLeft className="size-5" />
      </div>

      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="truncate text-sm font-semibold">{job.name}</span>
          {job.auto_paused ? (
            <Badge variant="destructive" className="gap-1 text-[10px]">
              <AlarmClock className="size-3" /> {t("jobs.paused")}
            </Badge>
          ) : job.schedule_enabled ? (
            <Badge variant="secondary" className="gap-1 text-[10px]">
              <CalendarClock className="size-3" /> {cronToHuman(job.schedule_cron)}
            </Badge>
          ) : (
            <Badge variant="outline" className="text-[10px]">
              {t("jobs.scheduleOff")}
            </Badge>
          )}
          <Badge variant="outline" className="text-[10px]">
            {mode}
          </Badge>
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
          <span className="flex items-center gap-1.5 font-mono">
            {spec.source_conn}
            <ArrowRight className="size-3 shrink-0 text-muted-foreground" />
            {spec.target_conn}
          </span>
          {streamCount > 1 && <span>{t("jobs.streams", { count: streamCount })}</span>}
          {job.last_run_at && (
            <span>
              {t("jobs.lastRun")} {relativeTime(job.last_run_at)}
            </span>
          )}
          {job.next_run_at && job.schedule_enabled && !job.auto_paused && (
            <span className="text-primary">
              {t("jobs.nextRun")} {countdown(job.next_run_at)}
            </span>
          )}
          {(job.consecutive_failures ?? 0) > 0 && (
            <span className="text-amber-600">{t("jobs.consecutiveFailures", { count: job.consecutive_failures })}</span>
          )}
        </div>
      </div>

      <div className="flex shrink-0 items-center gap-1" onClick={(e) => e.stopPropagation()}>
        <Button size="sm" onClick={() => run.mutate()} disabled={run.isPending} title={t("app.actions.run")}>
          {run.isPending ? <Loader2 className="size-4 animate-spin" /> : <Play className="size-4" />}
        </Button>
        {job.schedule_enabled && (
          <Button size="sm" variant="ghost" onClick={() => toggleSchedule.mutate()} title={t("app.actions.pause")}>
            <PauseCircle className="size-4" />
          </Button>
        )}
        {!job.schedule_enabled && (job.schedule_cron ?? "") !== "" && (
          <Button size="sm" variant="ghost" onClick={() => toggleSchedule.mutate()} title={t("app.actions.resume")}>
            <PlayCircle className="size-4" />
          </Button>
        )}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="sm" variant="ghost">
              <MoreHorizontal className="size-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={() => nav(`/jobs/${job.id}/edit`)}>
              {t("app.actions.edit")}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={onAnalyze}>
              <BarChart3 className="size-4" /> {t("analysis.title")}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => dup.mutate()}>
              <Copy className="size-4" /> {t("app.actions.duplicate")}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={async () => {
                const res = await api.exportJob(job.id)
                const blob = new Blob([res.yaml], { type: "text/yaml" })
                const a = document.createElement("a")
                a.href = URL.createObjectURL(blob)
                a.download = `${job.name}.yaml`
                a.click()
                URL.revokeObjectURL(a.href)
              }}
            >
              <Download className="size-4" /> {t("app.actions.export")}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem className="text-destructive" onClick={onDelete}>
              <Trash2 className="size-4" /> {t("app.actions.delete")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  )
}
