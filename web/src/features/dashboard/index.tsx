// 总览页：统计卡 + 趋势 + 即将运行 + 最近执行 + 连接健康
import { useTranslation } from "react-i18next"
import { useQuery } from "@tanstack/react-query"
import { useNavigate } from "react-router-dom"
import {
  Activity,
  ArrowRight,
  ArrowRightLeft,
  CheckCircle2,
  Database,
  Timer,
  TrendingUp,
} from "lucide-react"
import { api } from "@/lib/api"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { StatusBadge } from "@/components/status-badge"
import { EmptyState } from "@/components/empty-state"
import { countdown, formatNumber, relativeTime } from "@/lib/format"

export default function Dashboard() {
  const { t } = useTranslation()
  const nav = useNavigate()
  const { data: stats, isLoading } = useQuery({ queryKey: ["stats"], queryFn: api.stats, refetchInterval: 10_000 })
  const { data: runs } = useQuery({
    queryKey: ["runs", "recent"],
    queryFn: () => api.listRuns("limit=8"),
    refetchInterval: 5_000,
  })
  const { data: conns } = useQuery({ queryKey: ["conns"], queryFn: api.listConns })

  if (isLoading) return <DashboardSkeleton />

  const trend = stats?.trend ?? []
  const totalRuns = trend.reduce((a, d) => a + d.runs, 0)
  const totalOk = trend.reduce((a, d) => a + d.success, 0)
  const successRate = totalRuns ? Math.round((totalOk / totalRuns) * 100) : 100
  const totalRows = trend.reduce((a, d) => a + d.rows, 0)
  const maxRuns = Math.max(...trend.map((d) => d.runs), 1)

  const hasAnything = (stats?.jobs ?? 0) > 0 || (conns?.length ?? 0) > 0

  return (
    <div className="space-y-6">
      <PageHeader title={t("dashboard.title")} subtitle={t("dashboard.subtitle")} />

      {!hasAnything && (
        <EmptyState
          icon={ArrowRightLeft}
          title={t("dashboard.emptyTitle")}
          body={t("dashboard.emptyBody")}
          action={t("dashboard.emptyCta")}
          onAction={() => nav("/connections")}
        />
      )}

      {/* 统计卡 */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <StatCard icon={Activity} label={t("dashboard.statRuns")} value={formatNumber(totalRuns)} />
        <StatCard icon={CheckCircle2} label={t("dashboard.statSuccess")} value={`${successRate}%`} tone={successRate >= 90 ? "ok" : successRate >= 60 ? "warn" : "bad"} />
        <StatCard icon={TrendingUp} label={t("dashboard.statRows")} value={formatNumber(totalRows)} />
        <StatCard icon={ArrowRightLeft} label={t("dashboard.statJobs")} value={formatNumber(stats?.jobs)} />
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        {/* 14 天趋势迷你柱状图（纯 CSS，无重依赖） */}
        <Card className="lg:col-span-2">
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">{t("dashboard.statRuns")} · 14d</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex h-36 items-end gap-1.5">
              {trend.length === 0 && (
                <div className="flex h-full w-full items-center justify-center text-sm text-muted-foreground">
                  {t("common.empty")}
                </div>
              )}
              {trend.map((d) => {
                const h = Math.max((d.runs / maxRuns) * 100, 4)
                const bad = d.failed > 0
                return (
                  <div key={d.day} className="group relative flex-1" title={`${d.day} · ${d.runs} 次 · 成功 ${d.success}`}>
                    <div
                      className={`w-full rounded-sm transition-all group-hover:opacity-80 ${bad ? "bg-amber-500/70" : "bg-primary/70"}`}
                      style={{ height: `${h}%` }}
                    />
                  </div>
                )
              })}
            </div>
          </CardContent>
        </Card>

        {/* 即将运行 */}
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
              <Timer className="size-4" /> {t("dashboard.upcoming")}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            {(stats?.upcoming ?? []).length === 0 && (
              <div className="py-6 text-center text-sm text-muted-foreground">{t("common.empty")}</div>
            )}
            {(stats?.upcoming ?? []).slice(0, 6).map((u) => (
              <button
                key={u.job_id + u.at}
                onClick={() => nav(`/jobs/${u.job_id}/edit`)}
                className="flex w-full items-center justify-between rounded-lg border px-3 py-2 text-left text-sm hover:bg-muted/50"
              >
                <span className="truncate font-medium">{u.job_name}</span>
                <span className="ml-2 shrink-0 text-xs text-muted-foreground">{countdown(u.at)}</span>
              </button>
            ))}
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        {/* 最近执行 */}
        <Card className="lg:col-span-2">
          <CardHeader className="flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">{t("dashboard.recentRuns")}</CardTitle>
            <Button variant="ghost" size="sm" onClick={() => nav("/runs")}>
              {t("app.nav.runs")} <ArrowRight className="size-3.5" />
            </Button>
          </CardHeader>
          <CardContent className="space-y-1.5">
            {(runs?.runs ?? []).length === 0 && (
              <div className="py-6 text-center text-sm text-muted-foreground">{t("common.empty")}</div>
            )}
            {(runs?.runs ?? []).map((r) => (
              <button
                key={r.id}
                onClick={() => nav(`/runs/${r.id}`)}
                className="flex w-full items-center gap-3 rounded-lg border px-3 py-2 text-left text-sm hover:bg-muted/50"
              >
                <StatusBadge status={r.status} />
                <span className="min-w-0 flex-1 truncate font-medium">{r.job_name ?? r.id}</span>
                <span className="hidden shrink-0 text-xs text-muted-foreground sm:block">
                  {formatNumber(r.rows)} rows
                </span>
                <span className="shrink-0 text-xs text-muted-foreground">{relativeTime(r.start_time)}</span>
              </button>
            ))}
          </CardContent>
        </Card>

        {/* 连接健康 */}
        <Card>
          <CardHeader className="flex-row items-center justify-between pb-2">
            <CardTitle className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
              <Database className="size-4" /> {t("dashboard.connHealth")}
            </CardTitle>
            <Button variant="ghost" size="sm" onClick={() => nav("/connections")}>
              {t("app.nav.connections")} <ArrowRight className="size-3.5" />
            </Button>
          </CardHeader>
          <CardContent className="space-y-1.5">
            {(conns ?? []).length === 0 && (
              <div className="py-6 text-center text-sm text-muted-foreground">{t("common.empty")}</div>
            )}
            {(conns ?? []).slice(0, 8).map((c) => (
              <div key={c.name} className="flex items-center gap-2 rounded-lg border px-3 py-2 text-sm">
                <span
                  className={`size-2 shrink-0 rounded-full ${
                    c.last_test_status === "success"
                      ? "bg-emerald-500"
                      : c.last_test_status === "error"
                        ? "bg-destructive"
                        : "bg-muted-foreground/40"
                  }`}
                />
                <span className="min-w-0 flex-1 truncate font-medium">{c.name}</span>
                <span className="shrink-0 text-xs uppercase text-muted-foreground">{c.type}</span>
              </div>
            ))}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

export function PageHeader({ title, subtitle, actions }: { title: string; subtitle?: string; actions?: React.ReactNode }) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h1 className="text-xl font-bold tracking-tight">{title}</h1>
        {subtitle && <p className="mt-0.5 text-sm text-muted-foreground">{subtitle}</p>}
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  )
}

function StatCard({
  icon: Icon,
  label,
  value,
  tone,
}: {
  icon: React.ComponentType<{ className?: string }>
  label: string
  value: string
  tone?: "ok" | "warn" | "bad"
}) {
  return (
    <Card>
      <CardContent className="flex items-center gap-3 py-4">
        <div
          className={`flex size-10 shrink-0 items-center justify-center rounded-xl ${
            tone === "ok"
              ? "bg-emerald-500/10 text-emerald-600"
              : tone === "warn"
                ? "bg-amber-500/10 text-amber-600"
                : tone === "bad"
                  ? "bg-destructive/10 text-destructive"
                  : "bg-primary/10 text-primary"
          }`}
        >
          <Icon className="size-5" />
        </div>
        <div className="min-w-0">
          <div className="truncate text-lg font-bold leading-tight">{value}</div>
          <div className="truncate text-xs text-muted-foreground">{label}</div>
        </div>
      </CardContent>
    </Card>
  )
}

function DashboardSkeleton() {
  return (
    <div className="space-y-6">
      <Skeleton className="h-8 w-48" />
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-20" />
        ))}
      </div>
      <Skeleton className="h-48" />
    </div>
  )
}
