// Run detail: SSE live progress + log terminal (simplified virtual scrolling: windowed append) + cancel + stream details
import { useEffect, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery } from "@tanstack/react-query"
import { useNavigate, useParams } from "react-router-dom"
import { toast } from "sonner"
import { ArrowLeft, Download, Pause, Play, Search, Square, Terminal } from "lucide-react"
import { api } from "@/lib/api"
import type { RunSnapshot } from "@/lib/types"
import { StatusBadge } from "@/components/status-badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"
import { formatBytes, formatDuration, formatNumber, formatRate, formatTime } from "@/lib/format"

const MAX_LOG_LINES = 5000

export default function RunDetail() {
  const { t } = useTranslation()
  const { id } = useParams()
  const nav = useNavigate()

  const { data: initial } = useQuery({
    queryKey: ["run", id],
    queryFn: () => api.getRun(id!),
  })

  // local live snapshot (overwritten by SSE)
  const [snap, setSnap] = useState<RunSnapshot | null>(null)
  const [logs, setLogs] = useState<string[]>([])
  const [logFilter, setLogFilter] = useState("")
  const [autoFollow, setAutoFollow] = useState(true)
  const logRef = useRef<HTMLDivElement>(null)
  const loadedLogsRef = useRef(false)

  // SSE subscription
  useEffect(() => {
    if (!id) return
    const es = new EventSource("/api/events")
    es.addEventListener("execution", (ev) => {
      const msg = JSON.parse((ev as MessageEvent).data)
      if (msg.run_id === id) setSnap(msg.payload as RunSnapshot)
    })
    es.addEventListener("log", (ev) => {
      const msg = JSON.parse((ev as MessageEvent).data)
      if (msg.run_id !== id) return
      const line = msg.payload?.text ?? ""
      setLogs((prev) => {
        const next = [...prev, line.replace(/\n$/, "")]
        return next.length > MAX_LOG_LINES ? next.slice(-MAX_LOG_LINES) : next
      })
    })
    return () => es.close()
  }, [id])

  // initial log replay (once only)
  useEffect(() => {
    if (loadedLogsRef.current || !id) return
    loadedLogsRef.current = true
    api.runLogs(id, 0, 2000).then((res) => {
      setLogs(res.logs.map((l) => l.text.replace(/\n$/, "")))
    }).catch(() => {})
  }, [id])

  // auto-scroll
  useEffect(() => {
    if (autoFollow && logRef.current) {
      logRef.current.scrollTop = logRef.current.scrollHeight
    }
  }, [logs, autoFollow])

  const cancel = useMutation({
    mutationFn: () => api.cancelRun(id!),
    onSuccess: () => toast.success(t("runs.cancelSent")),
    onError: (e) => toast.error(e.message),
  })

  const data = snap ?? ((initial as unknown as RunSnapshot) ?? null)
  const running = data?.status === "running" || data?.status === "queued"
  const filteredLogs = useMemo(
    () => (logFilter ? logs.filter((l) => l.toLowerCase().includes(logFilter.toLowerCase())) : logs),
    [logs, logFilter],
  )

  if (!data) return <Skeleton className="h-96" />

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="ghost" size="icon" onClick={() => nav("/runs")}>
          <ArrowLeft className="size-4" />
        </Button>
        <StatusBadge status={data.status} className="text-sm" />
        <h1 className="min-w-0 truncate text-lg font-bold">{data.job_name ?? data.run_id}</h1>
        <div className="ml-auto flex gap-2">
          {running && (
            <Button variant="destructive" size="sm" onClick={() => cancel.mutate()}>
              <Square className="size-4" /> {t("app.actions.cancel")}
            </Button>
          )}
        </div>
      </div>

      {/* Metric bar */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-5">
        <Metric label={t("runs.rows")} value={formatNumber(data.rows)} highlight />
        <Metric label={t("runs.bytes")} value={formatBytes(data.bytes)} />
        <Metric label={t("runs.rate")} value={formatRate(data.rate)} />
        <Metric label={t("runs.duration")} value={formatDuration(data.duration_ms)} />
        <Metric label={t("runs.startTime")} value={formatTime(data.start_time)} />
      </div>

      {data.progress && running && (
        <div className="rounded-xl border border-primary/30 bg-primary/5 px-4 py-3 text-sm text-primary">
          <span className="mr-2 inline-block size-2 animate-pulse rounded-full bg-primary" />
          {data.progress}
        </div>
      )}

      {data.error && (
        <div className="rounded-xl border border-destructive/30 bg-destructive/5 p-4">
          <div className="text-sm font-semibold text-destructive">{t("runs.failedWhy")}</div>
          <pre className="scrollbar-thin mt-2 max-h-40 overflow-auto whitespace-pre-wrap font-mono text-xs leading-relaxed text-muted-foreground">
            {data.error}
          </pre>
        </div>
      )}

      {/* Stream details (replication) */}
      {(data.streams ?? []).length > 0 && (
        <section>
          <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{t("runs.streamDetail")}</h2>
          <div className="overflow-hidden rounded-xl border">
            {(data.streams ?? []).map((s, i) => (
              <div key={`${s.stream}-${i}`} className="flex items-center gap-3 border-b px-4 py-2 text-sm last:border-b-0">
                <StatusBadge status={s.status} />
                <span className="min-w-0 flex-1 truncate font-mono text-xs">{s.object ?? s.stream}</span>
                <span className="shrink-0 text-xs text-muted-foreground">{formatNumber(s.rows)} rows</span>
              </div>
            ))}
          </div>
        </section>
      )}

      {/* Log terminal */}
      <section>
        <div className="mb-2 flex items-center gap-2">
          <Terminal className="size-4 text-muted-foreground" />
          <h2 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{t("runs.logs")}</h2>
          <div className="relative ml-auto max-w-56">
            <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={logFilter}
              onChange={(e) => setLogFilter(e.target.value)}
              placeholder={t("runs.searchLogs")}
              className="h-8 pl-8 text-xs"
            />
          </div>
          <Button
            variant="ghost"
            size="sm"
            className="h-8 text-xs"
            onClick={() => setAutoFollow(!autoFollow)}
          >
            {autoFollow ? (
              <>
                <Pause className="size-3.5" /> {t("runs.pauseScroll")}
              </>
            ) : (
              <>
                <Play className="size-3.5" /> {t("runs.autoScroll")}
              </>
            )}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="h-8 text-xs"
            onClick={() => {
              const blob = new Blob([logs.join("\n")], { type: "text/plain" })
              const a = document.createElement("a")
              a.href = URL.createObjectURL(blob)
              a.download = `${id}.log`
              a.click()
              URL.revokeObjectURL(a.href)
            }}
          >
            <Download className="size-3.5" /> {t("runs.downloadLogs")}
          </Button>
        </div>
        <div
          ref={logRef}
          onScroll={(e) => {
            const el = e.currentTarget
            const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40
            if (!atBottom && autoFollow) setAutoFollow(false)
          }}
          className="scrollbar-thin h-96 overflow-y-auto rounded-xl bg-zinc-950 p-4 font-mono text-[11px] leading-relaxed text-zinc-300"
        >
          {filteredLogs.length === 0 ? (
            <span className="text-zinc-600">{t("runs.waitingLogs")}</span>
          ) : (
            filteredLogs.map((l, i) => <LogLineView key={i} line={l} />)
          )}
          {running && <span className="ml-0.5 inline-block h-3.5 w-2 animate-pulse bg-zinc-500" />}
        </div>
      </section>
    </div>
  )
}

function Metric({ label, value, highlight }: { label: string; value: string; highlight?: boolean }) {
  return (
    <div className={cn("rounded-xl border p-3", highlight && "border-primary/30 bg-primary/5")}>
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className={cn("mt-0.5 truncate text-base font-bold", highlight && "text-primary")}>{value}</div>
    </div>
  )
}

function LogLineView({ line }: { line: string }) {
  const isError = /error|failed|fatal/i.test(line)
  const isWarn = /warn/i.test(line)
  return (
    <div className={cn("whitespace-pre-wrap break-all", isError && "text-red-400", isWarn && "text-amber-400")}>
      {line}
    </div>
  )
}
