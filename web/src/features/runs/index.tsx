// Runs center: search/status/trigger filters + sort + pagination; live cards (SSE) + history
import { useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { useQuery } from "@tanstack/react-query"
import { useNavigate } from "react-router-dom"
import { Activity } from "lucide-react"
import { api } from "@/lib/api"
import { PageHeader } from "@/features/dashboard"
import { StatusBadge } from "@/components/status-badge"
import { EmptyState } from "@/components/empty-state"
import { Skeleton } from "@/components/ui/skeleton"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ListPagination, ListToolbar, NoMatchState } from "@/components/list-toolbar"
import { formatBytes, formatDuration, formatNumber, formatTime } from "@/lib/format"

const PAGE_SIZE = 20

export default function Runs() {
  const { t } = useTranslation()
  const nav = useNavigate()
  const [q, setQ] = useState("")
  const [status, setStatus] = useState("all")
  const [trigger, setTrigger] = useState("all")
  const [sort, setSort] = useState("newest")
  const [page, setPage] = useState(0)

  const params = new URLSearchParams()
  if (status !== "all") params.set("status", status)
  if (trigger !== "all") params.set("trigger", trigger)
  params.set("limit", "100")

  const { data, isLoading } = useQuery({
    queryKey: ["runs", status, trigger],
    queryFn: () => api.listRuns(params.toString()),
    refetchInterval: (query) => ((query.state.data?.active ?? 0) > 0 ? 2_000 : 8_000),
  })

  useEffect(() => setPage(0), [q, status, trigger, sort])

  const live = useMemo(
    () => (data?.runs ?? []).filter((r) => !isDone(r.status) && matchSearch(r, q)),
    [data, q],
  )
  const history = useMemo(() => {
    const out = (data?.runs ?? []).filter((r) => isDone(r.status) && matchSearch(r, q))
    return [...out].sort((a, b) => {
      if (sort === "oldest") return new Date(a.start_time).getTime() - new Date(b.start_time).getTime()
      if (sort === "longest") return (b.duration_ms ?? 0) - (a.duration_ms ?? 0)
      return new Date(b.start_time).getTime() - new Date(a.start_time).getTime()
    })
  }, [data, q, sort])

  const pages = Math.max(1, Math.ceil(history.length / PAGE_SIZE))
  const pageItems = history.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE)
  const hasFilters = q.trim() !== "" || status !== "all" || trigger !== "all"

  return (
    <div className="space-y-6">
      <PageHeader title={t("runs.title")} subtitle={t("runs.subtitle")} />

      <ListToolbar
        search={q}
        onSearch={setQ}
        searchPlaceholder={t("runs.searchPlaceholder")}
        sort={sort}
        onSort={setSort}
        sortOptions={[
          { value: "newest", label: t("common.sortByNewest") },
          { value: "oldest", label: t("common.sortByOldest") },
          { value: "longest", label: t("common.sortByLongest") },
        ]}
      >
        <Select value={status} onValueChange={setStatus}>
          <SelectTrigger size="sm" className="h-9 w-32 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("common.allStatus")}</SelectItem>
            <SelectItem value="running">{t("status.running")}</SelectItem>
            <SelectItem value="success">{t("status.success")}</SelectItem>
            <SelectItem value="error">{t("status.error")}</SelectItem>
            <SelectItem value="cancelled">{t("status.cancelled")}</SelectItem>
          </SelectContent>
        </Select>
        <Select value={trigger} onValueChange={setTrigger}>
          <SelectTrigger size="sm" className="h-9 w-32 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("runs.allTriggers")}</SelectItem>
            <SelectItem value="manual">{t("runs.triggerManual")}</SelectItem>
            <SelectItem value="schedule">{t("runs.triggerSchedule")}</SelectItem>
            <SelectItem value="retry">{t("runs.triggerRetry")}</SelectItem>
          </SelectContent>
        </Select>
      </ListToolbar>

      {isLoading ? (
        <Skeleton className="h-64" />
      ) : (data?.runs ?? []).length === 0 ? (
        <EmptyState icon={Activity} title={t("runs.emptyTitle")} body={t("runs.emptyBody")} />
      ) : (
        <div className="space-y-6">
          {live.length > 0 && (
            <section className="space-y-2">
              <h2 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                {t("runs.live")} · {live.length}
              </h2>
              {live.map((r) => (
                <LiveRunRow key={r.id} run={r} onClick={() => nav(`/runs/${r.id}`)} />
              ))}
            </section>
          )}
          {history.length > 0 ? (
            <section className="space-y-2">
              <h2 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{t("runs.history")}</h2>
              <div className="overflow-hidden rounded-xl border">
                {pageItems.map((r) => (
                  <button
                    key={r.id}
                    onClick={() => nav(`/runs/${r.id}`)}
                    className="flex w-full items-center gap-3 border-b px-4 py-2.5 text-left text-sm last:border-b-0 hover:bg-muted/50"
                  >
                    <StatusBadge status={r.status} />
                    <span className="min-w-0 flex-1 truncate font-medium">{r.job_name ?? r.id}</span>
                    <span className="hidden w-20 shrink-0 text-right text-xs text-muted-foreground sm:block">
                      {formatNumber(r.rows)} rows
                    </span>
                    <span className="hidden w-16 shrink-0 text-right text-xs text-muted-foreground sm:block">
                      {formatBytes(r.bytes)}
                    </span>
                    <span className="w-16 shrink-0 text-right text-xs text-muted-foreground">{formatDuration(r.duration_ms)}</span>
                    <span className="hidden w-36 shrink-0 text-right text-xs text-muted-foreground md:block">
                      {formatTime(r.start_time)}
                    </span>
                  </button>
                ))}
              </div>
              <ListPagination
                total={history.length}
                page={page}
                pages={pages}
                onPage={setPage}
                countLabel={t("common.total", { count: history.length })}
              />
            </section>
          ) : live.length === 0 ? (
            <NoMatchState
              hasFilters={hasFilters}
              onClear={() => {
                setQ("")
                setStatus("all")
                setTrigger("all")
              }}
            />
          ) : null}
        </div>
      )}
    </div>
  )
}

function matchSearch(r: { job_name?: string; id: string }, q: string) {
  const needle = q.trim().toLowerCase()
  if (!needle) return true
  return (r.job_name ?? "").toLowerCase().includes(needle) || r.id.toLowerCase().includes(needle)
}

function isDone(s: string) {
  return ["success", "error", "warning", "cancelled", "terminated", "interrupted", "skipped"].includes(s)
}

function LiveRunRow({ run, onClick }: { run: { id: string; job_name?: string; status: string; rows?: number; bytes?: number; duration_ms?: number; start_time: string; trigger: string }; onClick: () => void }) {
  const { t } = useTranslation()
  return (
    <button
      onClick={onClick}
      className="relative w-full overflow-hidden rounded-xl border bg-card p-4 text-left transition-shadow hover:shadow-md"
    >
      <div className="flex items-center gap-3">
        <StatusBadge status={run.status} />
        <span className="min-w-0 flex-1 truncate text-sm font-semibold">{run.job_name ?? run.id}</span>
        <span className="shrink-0 font-mono text-xs text-muted-foreground">{run.id}</span>
      </div>
      <div className="mt-2 flex items-center gap-4 text-xs text-muted-foreground">
        <span>{formatNumber(run.rows)} rows</span>
        <span>{formatBytes(run.bytes)}</span>
        <span>{formatDuration(run.duration_ms)}</span>
        <span className="ml-auto">{t(`runs.trigger${cap(run.trigger)}`) ?? run.trigger}</span>
      </div>
      {/* animated light border for running jobs */}
      {run.status === "running" && (
        <div className="absolute inset-x-0 bottom-0 h-0.5 overflow-hidden">
          <div className="h-full w-1/3 animate-[slide_2s_ease-in-out_infinite] bg-gradient-to-r from-transparent via-primary to-transparent" />
        </div>
      )}
    </button>
  )
}

function cap(s: string) {
  return s.charAt(0).toUpperCase() + s.slice(1)
}
