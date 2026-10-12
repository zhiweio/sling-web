// 列表页统一工具栏与分页：搜索 + 筛选（children 传入 Select）+ 排序 + 分页脚注。
// /connections、/jobs、/runs 与向导连接选择器共用同一套交互语言。
import { useTranslation } from "react-i18next"
import { ChevronLeft, ChevronRight, Search } from "lucide-react"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"

export type SortOption = { value: string; label: string }

export function ListToolbar({
  search,
  onSearch,
  searchPlaceholder,
  sort,
  onSort,
  sortOptions,
  children,
}: {
  search: string
  onSearch: (v: string) => void
  searchPlaceholder: string
  sort: string
  onSort: (v: string) => void
  sortOptions: SortOption[]
  children?: React.ReactNode
}) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <div className="relative min-w-44 max-w-sm flex-1">
        <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => onSearch(e.target.value)}
          placeholder={searchPlaceholder}
          aria-label={searchPlaceholder}
          className="h-9 pl-8 text-sm"
        />
      </div>
      {children}
      <Select value={sort} onValueChange={onSort}>
        <SelectTrigger size="sm" className="h-9 w-32 text-xs">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {sortOptions.map((o) => (
            <SelectItem key={o.value} value={o.value}>
              {o.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

/** 分页脚注：左侧总数 + 右侧 ‹ 当前/总页 › 翻页器；仅一页时不渲染 */
export function ListPagination({
  total,
  page,
  pages,
  onPage,
  countLabel,
}: {
  total: number
  page: number
  pages: number
  onPage: (p: number) => void
  countLabel?: string
}) {
  const { t } = useTranslation()
  if (total === 0 || pages <= 1) return null
  return (
    <div className="flex items-center justify-between text-xs text-muted-foreground">
      <span className="tabular-nums">{countLabel ?? t("common.total", { count: total })}</span>
      <span className="flex items-center gap-1">
        <Button
          variant="ghost"
          size="icon"
          className="size-7"
          disabled={page === 0}
          onClick={() => onPage(Math.max(0, page - 1))}
          aria-label="previous page"
        >
          <ChevronLeft className="size-3.5" />
        </Button>
        <span className="tabular-nums">
          {t("common.pageOf", { cur: page + 1, total: pages })}
        </span>
        <Button
          variant="ghost"
          size="icon"
          className="size-7"
          disabled={page >= pages - 1}
          onClick={() => onPage(Math.min(pages - 1, page + 1))}
          aria-label="next page"
        >
          <ChevronRight className="size-3.5" />
        </Button>
      </span>
    </div>
  )
}

/** 筛选无结果的空态（区别于完全无数据）：提示 + 一键清除 */
export function NoMatchState({ hasFilters, onClear }: { hasFilters: boolean; onClear: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed px-4 py-12 text-center text-sm text-muted-foreground">
      <Search className="size-5 opacity-40" />
      {t("common.noMatch")}
      {hasFilters && (
        <Button variant="outline" size="sm" onClick={onClear}>
          {t("common.clearFilters")}
        </Button>
      )}
    </div>
  )
}
