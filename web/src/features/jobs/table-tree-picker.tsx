// table-tree-picker.tsx — Step1 的 schema → 表 分组树选择器。
// 设计参照 Airbyte/Fivetran 的流选择：schema 节点三态勾选（全选/半选）+
// 搜索自动展开 + 大列表虚拟滚动（@tanstack/react-virtual）。
// 选中状态仍以 schema.table 全名写入 spec.streams，与既有向导逻辑兼容。
import { useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { useVirtualizer } from "@tanstack/react-virtual"
import { ChevronRight, ListChecks, Search, X } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { cn } from "@/lib/utils"

export interface PickerTable {
  full: string // schema.table
  name: string
  isView?: boolean
}

export interface PickerSchema {
  name: string
  tables: PickerTable[]
}

interface TableTreePickerProps {
  schemas: PickerSchema[]
  selected: Set<string>
  onToggle: (full: string) => void
  onSetMany: (fulls: string[], selected: boolean) => void
}

type Row =
  | { kind: "schema"; name: string; tables: PickerTable[]; visibleCount: number; totalCount: number }
  | { kind: "table"; table: PickerTable }

export function TableTreePicker({ schemas, selected, onToggle, onSetMany }: TableTreePickerProps) {
  const { t } = useTranslation()
  const [q, setQ] = useState("")
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  const scrollRef = useRef<HTMLDivElement>(null)

  const needle = q.trim().toLowerCase()

  // 过滤后的 schema（搜索时只保留命中的表）
  const filtered = useMemo(() => {
    if (!needle) return schemas
    return schemas
      .map((s) => ({
        ...s,
        tables: s.tables.filter((tb) => tb.full.toLowerCase().includes(needle)),
      }))
      .filter((s) => s.tables.length > 0)
  }, [schemas, needle])

  // 展开的 schema：搜索时全部展开；否则非折叠的
  const expanded = useMemo(() => {
    const set = new Set<string>()
    for (const s of filtered) {
      if (needle || !collapsed.has(s.name)) set.add(s.name)
    }
    return set
  }, [filtered, needle, collapsed])

  const totalFiltered = useMemo(() => filtered.reduce((n, s) => n + s.tables.length, 0), [filtered])
  const selectedFiltered = useMemo(
    () => filtered.reduce((n, s) => n + s.tables.filter((tb) => selected.has(tb.full)).length, 0),
    [filtered, selected],
  )
  const allFilteredSelected = totalFiltered > 0 && selectedFiltered === totalFiltered

  // 扁平化虚拟行：schema 头 + 展开的表行
  const rows = useMemo(() => {
    const out: Row[] = []
    for (const s of filtered) {
      out.push({
        kind: "schema",
        name: s.name,
        tables: s.tables,
        visibleCount: s.tables.length,
        totalCount: schemas.find((x) => x.name === s.name)?.tables.length ?? s.tables.length,
      })
      if (expanded.has(s.name)) {
        for (const tb of s.tables) out.push({ kind: "table", table: tb })
      }
    }
    return out
  }, [filtered, expanded, schemas])

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: (i) => (rows[i]?.kind === "schema" ? 36 : 34),
    overscan: 12,
  })

  function toggleSchema(tables: PickerTable[]) {
    const visible = tables.filter((tb) => !needle || tb.full.toLowerCase().includes(needle))
    const allSel = visible.every((tb) => selected.has(tb.full))
    onSetMany(visible.map((tb) => tb.full), !allSel)
  }

  return (
    <div className="overflow-hidden rounded-xl border">
      {/* 搜索 + 全选/清空 */}
      <div className="flex items-center gap-2 border-b bg-muted/40 px-3 py-2">
        <Search className="size-3.5 shrink-0 text-muted-foreground" />
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder={t("jobs.wizard.searchTables")}
          className="h-5 w-full bg-transparent text-xs outline-none placeholder:text-muted-foreground"
        />
        {q && (
          <button
            type="button"
            aria-label="clear search"
            onClick={() => setQ("")}
            className="shrink-0 rounded-sm p-0.5 text-muted-foreground hover:text-foreground"
          >
            <X className="size-3.5" />
          </button>
        )}
        <span className="shrink-0 text-[10px] tabular-nums text-muted-foreground">
          {selectedFiltered}/{totalFiltered}
        </span>
        <Button
          variant="ghost"
          size="sm"
          className="h-6 shrink-0 px-2 text-[11px]"
          disabled={totalFiltered === 0 || (allFilteredSelected && selectedFiltered === selected.size)}
          onClick={() => onSetMany(filtered.flatMap((s) => s.tables.map((tb) => tb.full)), true)}
        >
          <ListChecks className="size-3" /> {t("jobs.wizard.selectAll")}
        </Button>
        <Button
          variant="ghost"
          size="sm"
          className="h-6 shrink-0 px-2 text-[11px]"
          disabled={selected.size === 0}
          onClick={() => onSetMany([...selected], false)}
        >
          {t("jobs.wizard.clearAll")}
        </Button>
      </div>

      <div ref={scrollRef} className="scrollbar-thin max-h-80 overflow-y-auto">
        {rows.length === 0 ? (
          <div className="px-3 py-8 text-center text-xs text-muted-foreground">
            {t("jobs.wizard.noMatchTables")}
          </div>
        ) : (
          <div style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
            {virtualizer.getVirtualItems().map((vRow) => {
              const row = rows[vRow.index]
              const top = vRow.start
              if (row.kind === "schema") {
                const selCount = row.tables.filter((tb) => selected.has(tb.full)).length
                const checked = selCount === row.visibleCount && row.visibleCount > 0
                const indeterminate = selCount > 0 && !checked
                const isOpen = expanded.has(row.name)
                return (
                  <div
                    key={`s-${row.name}-${vRow.key}`}
                    style={{ position: "absolute", top, height: vRow.size, left: 0, right: 0 }}
                    className="flex items-center gap-2 border-b bg-muted/30 px-3"
                  >
                    <button
                      type="button"
                      aria-label={isOpen ? "collapse" : "expand"}
                      onClick={() =>
                        setCollapsed((prev) => {
                          const next = new Set(prev)
                          if (next.has(row.name)) next.delete(row.name)
                          else next.add(row.name)
                          return next
                        })
                      }
                      className="rounded-sm p-0.5 text-muted-foreground transition-transform duration-200 hover:text-foreground"
                    >
                      <ChevronRight className={cn("size-3.5", isOpen && "rotate-90")} />
                    </button>
                    <Checkbox
                      checked={indeterminate ? "indeterminate" : checked}
                      onCheckedChange={() => toggleSchema(row.tables)}
                    />
                    <span className="truncate font-mono text-xs font-semibold">{row.name}</span>
                    <Badge variant="outline" className="ml-auto shrink-0 text-[10px] tabular-nums">
                      {t("jobs.wizard.schemaTableCount", { count: row.totalCount })}
                    </Badge>
                  </div>
                )
              }
              const tb = row.table
              const isSel = selected.has(tb.full)
              return (
                <label
                  key={`t-${tb.full}-${vRow.key}`}
                  style={{ position: "absolute", top, height: vRow.size, left: 0, right: 0 }}
                  className={cn(
                    "flex cursor-pointer items-center gap-2.5 border-b pl-10 pr-3 text-sm transition-colors last:border-b-0 hover:bg-muted/50",
                    isSel && "bg-primary/[0.04]",
                  )}
                >
                  <Checkbox checked={isSel} onCheckedChange={() => onToggle(tb.full)} />
                  <span className="truncate font-mono text-xs">{tb.name}</span>
                  {tb.isView && (
                    <Badge variant="outline" className="ml-auto shrink-0 text-[10px]">
                      view
                    </Badge>
                  )}
                </label>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
