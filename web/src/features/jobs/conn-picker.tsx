// 连接选择器（向导步骤 1/2）：搜索 + 类型筛选 + 排序 + 分页
// 兼容几十个连接的场景：工具栏常驻，行式列表替代卡片墙，健康圆点标注测试状态。
import { useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { Check, Search, XCircle } from "lucide-react"
import type { Connection } from "@/lib/types"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { ListPagination } from "@/components/list-toolbar"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { cn } from "@/lib/utils"
import { BrandIcon } from "@/lib/brand-icons"

const PAGE_SIZE = 8

type SortKey = "name" | "type" | "newest"

export function ConnPicker({
  value,
  onSelect,
  conns,
}: {
  value: string
  onSelect: (name: string) => void
  conns: Connection[]
}) {
  const { t } = useTranslation()
  const [q, setQ] = useState("")
  const [typeFilter, setTypeFilter] = useState("all")
  const [sort, setSort] = useState<SortKey>("name")
  const [page, setPage] = useState(0)

  // 筛选/排序变化时回到第一页
  useEffect(() => setPage(0), [q, typeFilter, sort])

  const types = useMemo(() => {
    const counts = new Map<string, number>()
    for (const c of conns) counts.set(c.type, (counts.get(c.type) ?? 0) + 1)
    return [...counts.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
  }, [conns])

  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase()
    const out = conns.filter(
      (c) =>
        (typeFilter === "all" || c.type === typeFilter) &&
        (!needle ||
          c.name.toLowerCase().includes(needle) ||
          c.type.toLowerCase().includes(needle)),
    )
    return [...out].sort((a, b) => {
      if (sort === "type") return a.type.localeCompare(b.type) || a.name.localeCompare(b.name)
      if (sort === "newest")
        return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
      return a.name.localeCompare(b.name)
    })
  }, [conns, q, typeFilter, sort])

  const pages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE))
  const pageItems = filtered.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE)
  const hasFilters = q.trim() !== "" || typeFilter !== "all"

  if (conns.length === 0) {
    return (
      <div className="rounded-xl border border-dashed p-6 text-center text-sm text-muted-foreground">
        {t("jobs.wizard.noConnsYet")}
      </div>
    )
  }

  return (
    <div className="space-y-2.5">
      {/* 工具栏：搜索 + 类型筛选 + 排序 */}
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-44 flex-1">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && filtered.length > 0) onSelect(filtered[0].name)
            }}
            placeholder={t("jobs.wizard.searchConns")}
            aria-label={t("jobs.wizard.searchConns")}
            className="h-9 pl-8 text-sm"
          />
        </div>
        <Select value={typeFilter} onValueChange={setTypeFilter}>
          <SelectTrigger size="sm" className="h-9 w-36 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">
              {t("jobs.wizard.allTypes")} · {conns.length}
            </SelectItem>
            {types.map(([ty, n]) => (
              <SelectItem key={ty} value={ty}>
                <span className="flex items-center gap-2">
                  <BrandIcon type={ty} className="size-4 rounded-sm" />
                  <span>{ty}</span>
                  <span className="ml-1 text-muted-foreground">{n}</span>
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={sort} onValueChange={(v) => setSort(v as SortKey)}>
          <SelectTrigger size="sm" className="h-9 w-32 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="name">{t("jobs.wizard.sortByName")}</SelectItem>
            <SelectItem value="type">{t("jobs.wizard.sortByType")}</SelectItem>
            <SelectItem value="newest">{t("jobs.wizard.sortByNewest")}</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {/* 行式列表 */}
      <div className="overflow-hidden rounded-xl border">
        {pageItems.map((c) => {
          const selected = value === c.name
          return (
            <button
              key={c.name}
              type="button"
              onClick={() => onSelect(c.name)}
              className={cn(
                "flex w-full items-center gap-3 border-b px-3.5 py-2.5 text-left transition-colors last:border-b-0 hover:bg-muted/40 active:scale-[0.995]",
                selected && "bg-primary/[0.04]",
              )}
            >
              <BrandIcon type={c.type} className="size-8 shrink-0" />
              <span className="min-w-0 flex-1">
                <span className="flex items-center gap-2">
                  <span className="truncate text-sm font-medium">{c.name}</span>
                  {c.last_test_status === "success" && (
                    <span
                      className="size-1.5 shrink-0 rounded-full bg-emerald-500"
                      title={t("jobs.wizard.testOk")}
                    />
                  )}
                  {c.last_test_status === "failed" && (
                    <span
                      className="size-1.5 shrink-0 rounded-full bg-rose-500"
                      title={t("jobs.wizard.testFailed")}
                    />
                  )}
                </span>
                <span className="block truncate text-xs text-muted-foreground">
                  {c.type}
                  {c.last_test_ms != null && c.last_test_status === "success"
                    ? ` · ${c.last_test_ms}ms`
                    : ""}
                </span>
              </span>
              <span
                className={cn(
                  "flex size-5 shrink-0 items-center justify-center rounded-full border transition-all",
                  selected
                    ? "border-primary bg-primary text-primary-foreground"
                    : "border-muted-foreground/30 text-transparent",
                )}
              >
                <Check className="size-3" strokeWidth={3} />
              </span>
            </button>
          )
        })}
        {filtered.length === 0 && (
          <div className="flex flex-col items-center gap-2 px-4 py-8 text-sm text-muted-foreground">
            <XCircle className="size-5 opacity-50" />
            {t("jobs.wizard.noMatch")}
            {hasFilters && (
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  setQ("")
                  setTypeFilter("all")
                }}
              >
                {t("jobs.wizard.clearFilters")}
              </Button>
            )}
          </div>
        )}
      </div>

      {/* 分页脚注：总数 + 页码 */}
      <ListPagination
        total={filtered.length}
        page={page}
        pages={pages}
        onPage={setPage}
        countLabel={t("jobs.wizard.connCount", { count: filtered.length })}
      />
    </div>
  )
}
