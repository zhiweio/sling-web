// Notification center: all/unread + type filter, per-item and mark-all-read, click through to the related run
import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link } from "react-router-dom"
import { Bell, CheckCheck, ExternalLink } from "lucide-react"
import { api } from "@/lib/api"
import type { Notification } from "@/lib/types"
import { cn } from "@/lib/utils"
import { PageHeader } from "@/features/dashboard"
import { EmptyState } from "@/components/empty-state"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { ListPagination, NoMatchState } from "@/components/list-toolbar"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { relativeTime } from "@/lib/format"

const PAGE_SIZE = 20

const TYPE_OPTIONS = ["run_success", "run_failed", "run_cancelled", "paused", "system"]

export default function Notifications() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [tab, setTab] = useState<"all" | "unread">("all")
  const [type, setType] = useState("all")
  const [page, setPage] = useState(0)

  const { data, isLoading } = useQuery({
    queryKey: ["notifications", "page", tab, type],
    queryFn: () => api.notifications({ unread: tab === "unread", type: type === "all" ? undefined : type, limit: 200 }),
  })

  const markRead = useMutation({
    mutationFn: (ids?: number[]) => api.markRead(ids),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notifications"] }),
  })

  const items = useMemo(() => data?.notifications ?? [], [data])
  const pages = Math.max(1, Math.ceil(items.length / PAGE_SIZE))
  const pageItems = items.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE)
  const hasFilters = tab !== "all" || type !== "all"

  return (
    <div className="space-y-6">
      <PageHeader title={t("notify.title")} subtitle={t("notify.subtitle")} />

      <div className="flex flex-wrap items-center gap-2">
        <div className="flex overflow-hidden rounded-lg border">
          <button
            className={cn("h-9 px-3 text-sm", tab === "all" ? "bg-primary text-primary-foreground" : "hover:bg-muted")}
            onClick={() => { setTab("all"); setPage(0) }}
          >
            {t("notify.tabAll")}
          </button>
          <button
            className={cn("h-9 px-3 text-sm", tab === "unread" ? "bg-primary text-primary-foreground" : "hover:bg-muted")}
            onClick={() => { setTab("unread"); setPage(0) }}
          >
            {t("notify.tabUnread")}
            {(data?.unread ?? 0) > 0 && <span className="ml-1.5 text-xs opacity-70">{data?.unread}</span>}
          </button>
        </div>
        <Select value={type} onValueChange={(v) => { setType(v); setPage(0) }}>
          <SelectTrigger size="sm" className="h-9 w-40 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("notify.allTypes")}</SelectItem>
            {TYPE_OPTIONS.map((v) => (
              <SelectItem key={v} value={v}>{t(`notify.type.${v}`)}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="flex-1" />
        {items.some((n) => !n.read) && (
          <Button
            variant="outline"
            size="sm"
            className="h-9"
            onClick={() => markRead.mutate(undefined)}
            disabled={markRead.isPending}
          >
            <CheckCheck className="size-4" /> {t("notify.markAllRead")}
          </Button>
        )}
      </div>

      {isLoading ? (
        <Skeleton className="h-64" />
      ) : items.length === 0 ? (
        hasFilters ? (
          <NoMatchState
            hasFilters={hasFilters}
            onClear={() => { setTab("all"); setType("all") }}
          />
        ) : (
          <EmptyState icon={Bell} title={t("notify.emptyTitle")} body={t("notify.emptyBody")} />
        )
      ) : (
        <div className="space-y-4">
          <div className="overflow-hidden rounded-xl border">
            {pageItems.map((n) => (
              <NotificationRow key={n.id} n={n} onRead={() => markRead.mutate([n.id])} />
            ))}
          </div>
          <ListPagination total={items.length} page={page} pages={pages} onPage={setPage} />
        </div>
      )}
    </div>
  )
}

function NotificationRow({ n, onRead }: { n: Notification; onRead: () => void }) {
  const { t } = useTranslation()
  const runId = useMemo(() => {
    try {
      const meta = n.meta_json ? JSON.parse(n.meta_json) : {}
      return typeof meta.run_id === "string" ? meta.run_id : ""
    } catch {
      return ""
    }
  }, [n.meta_json])

  return (
    <div className={cn("flex gap-3 border-b px-4 py-3 last:border-b-0", !n.read && "bg-primary/5")}>
      <span
        className={cn(
          "mt-1.5 size-2 shrink-0 rounded-full",
          n.level === "error" ? "bg-destructive" : n.level === "warning" ? "bg-amber-500" : "bg-emerald-500",
        )}
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">{n.title}</span>
          <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
            {t(`notify.type.${n.type}`, n.type)}
          </span>
        </div>
        {n.body && <div className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">{n.body}</div>}
        <div className="mt-1 flex items-center gap-3 text-[11px] text-muted-foreground">
          <span>{relativeTime(n.created_at)}</span>
          {runId && (
            <Link to={`/runs/${runId}`} className="inline-flex items-center gap-0.5 text-primary hover:underline">
              {t("notify.viewRun")} <ExternalLink className="size-3" />
            </Link>
          )}
        </div>
      </div>
      {!n.read && (
        <Button variant="ghost" size="sm" className="h-7 shrink-0 self-center text-xs" onClick={onRead}>
          {t("notify.markRead")}
        </Button>
      )}
    </div>
  )
}
