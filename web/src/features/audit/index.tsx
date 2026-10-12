// 审计日志：action/实体类型筛选 + 分页浏览配置与运行操作痕迹
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { useQuery } from "@tanstack/react-query"
import { ScrollText } from "lucide-react"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import { PageHeader } from "@/features/dashboard"
import { EmptyState } from "@/components/empty-state"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { ListPagination, ListToolbar, NoMatchState } from "@/components/list-toolbar"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { formatTime } from "@/lib/format"

const PAGE_SIZE = 20

const ACTIONS = ["create", "update", "delete", "run", "cancel", "rollback", "import", "duplicate", "backup", "prune", "misfire"]
const ENTITY_TYPES = ["job", "connection", "setting", "run", "channel", "backup"]

// action → badge 配色分类
const ACTION_TONE: Record<string, string> = {
  create: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
  update: "bg-muted text-muted-foreground",
  delete: "bg-destructive/10 text-destructive",
  run: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
  cancel: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  rollback: "bg-purple-500/10 text-purple-600 dark:text-purple-400",
  backup: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
}

export default function Audit() {
  const { t } = useTranslation()
  const [q, setQ] = useState("")
  const [action, setAction] = useState("all")
  const [entityType, setEntityType] = useState("all")
  const [page, setPage] = useState(0)

  const params = new URLSearchParams()
  if (action !== "all") params.set("action", action)
  if (entityType !== "all") params.set("entity_type", entityType)
  params.set("limit", "200")

  const { data, isLoading } = useQuery({
    queryKey: ["audit", action, entityType],
    queryFn: () => api.audit(params.toString()),
  })

  const entries = (data?.entries ?? []).filter((e) => {
    if (!q.trim()) return true
    const needle = q.trim().toLowerCase()
    return (
      e.entity_id?.toLowerCase().includes(needle) ||
      e.detail?.toLowerCase().includes(needle) ||
      e.action.toLowerCase().includes(needle)
    )
  })
  const pages = Math.max(1, Math.ceil(entries.length / PAGE_SIZE))
  const pageItems = entries.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE)
  const hasFilters = q.trim() !== "" || action !== "all" || entityType !== "all"

  return (
    <div className="space-y-6">
      <PageHeader title={t("audit.title")} subtitle={t("audit.subtitle")} />

      <ListToolbar
        search={q}
        onSearch={(v) => { setQ(v); setPage(0) }}
        searchPlaceholder={t("audit.searchPlaceholder")}
        sort="newest"
        onSort={() => {}}
        sortOptions={[{ value: "newest", label: t("common.sortByNewest") }]}
      >
        <Select value={action} onValueChange={(v) => { setAction(v); setPage(0) }}>
          <SelectTrigger size="sm" className="h-9 w-36 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("audit.allActions")}</SelectItem>
            {ACTIONS.map((a) => (
              <SelectItem key={a} value={a}>{t(`audit.action.${a}`)}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={entityType} onValueChange={(v) => { setEntityType(v); setPage(0) }}>
          <SelectTrigger size="sm" className="h-9 w-36 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("common.allTypes")}</SelectItem>
            {ENTITY_TYPES.map((e) => (
              <SelectItem key={e} value={e}>{t(`audit.entity.${e}`)}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </ListToolbar>

      {isLoading ? (
        <Skeleton className="h-64" />
      ) : entries.length === 0 ? (
        hasFilters ? (
          <NoMatchState hasFilters={hasFilters} onClear={() => { setQ(""); setAction("all"); setEntityType("all") }} />
        ) : (
          <EmptyState icon={ScrollText} title={t("audit.emptyTitle")} body={t("audit.emptyBody")} />
        )
      ) : (
        <div className="space-y-4">
          <div className="overflow-hidden rounded-xl border">
            {pageItems.map((e) => (
              <div key={e.id} className="flex flex-wrap items-center gap-3 border-b px-4 py-2.5 text-sm last:border-b-0">
                <span className="w-36 shrink-0 text-xs tabular-nums text-muted-foreground">{formatTime(e.created_at)}</span>
                <Badge variant="secondary" className={cn("shrink-0 font-mono text-[11px]", ACTION_TONE[e.action] ?? "")}>
                  {t(`audit.action.${e.action}`, e.action)}
                </Badge>
                <span className="shrink-0 text-xs text-muted-foreground">{t(`audit.entity.${e.entity_type}`, e.entity_type)}</span>
                {e.entity_id && <span className="max-w-40 truncate font-mono text-xs">{e.entity_id}</span>}
                {e.detail && (
                  <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground" title={e.detail}>
                    {e.detail}
                  </span>
                )}
              </div>
            ))}
          </div>
          <ListPagination total={entries.length} page={page} pages={pages} onPage={setPage} />
        </div>
      )}
    </div>
  )
}
