// mapping-table.tsx — batch mapping table for Step 2.
// Design follows Airbyte's namespace/stream mapping: defaults mirror the source name (the placeholder
// shows the effective name, consistent with the server-side EffectiveTarget fallback), bulk naming rules
// (target schema remapping + prefix/suffix), per-row overrides, target-existence badges and conflict detection.
import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { useQuery } from "@tanstack/react-query"
import { ArrowRight, Copy, TriangleAlert, Wand2 } from "lucide-react"
import { api } from "@/lib/api"
import type { Connection, JobSpec, StreamSpec } from "@/lib/types"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { cn } from "@/lib/utils"
import { effectiveTarget, hasWildcard, mappingIssues, splitObject } from "./spec-helpers"

const KEEP_SCHEMA = "__keep__"

export function MappingTable({
  spec,
  setSpec,
  conns,
}: {
  spec: JobSpec
  setSpec: (s: JobSpec) => void
  conns: Connection[]
}) {
  const { t } = useTranslation()
  const [ruleSchema, setRuleSchema] = useState(KEEP_SCHEMA)
  const [rulePrefix, setRulePrefix] = useState("")
  const [ruleSuffix, setRuleSuffix] = useState("")

  const targetIsDb = useMemo(() => {
    const c = conns.find((x) => x.name === spec.target_conn)
    if (!c) return true
    return !["file", "s3", "gs", "azure", "abfs", "ftp", "sftp", "http", "gdrive"].includes(c.type)
  }, [conns, spec.target_conn])

  const destructive = spec.defaults.mode === "replace" || spec.defaults.mode === "truncate"

  // fetch the target's full table list once: existence badges + schema remap candidates
  const { data: tgtDiscover, isFetching: tgtFetching } = useQuery({
    queryKey: ["wizard-target-discover", spec.target_conn],
    queryFn: () => api.discover(spec.target_conn, "table"),
    enabled: !!spec.target_conn && targetIsDb,
    staleTime: 60_000,
  })

  const existing = useMemo(() => {
    const set = new Set<string>()
    for (const s of tgtDiscover?.schemas ?? []) {
      for (const tb of s.tables ?? []) set.add(`${s.name}.${tb.name}`.toLowerCase())
    }
    return set
  }, [tgtDiscover])

  const tgtSchemas = useMemo(
    () => (tgtDiscover?.schemas ?? []).map((s) => s.name).filter(Boolean),
    [tgtDiscover],
  )

  const streams = (spec.streams ?? []).filter((s) => !s.disabled)
  const issues = useMemo(() => mappingIssues(spec), [spec])
  const issueBySource = useMemo(() => {
    const m = new Map<string, "conflict" | "required">()
    for (const it of issues) m.set(it.source, it.reason)
    return m
  }, [issues])

  function setStream(idx: number, patch: Partial<StreamSpec>) {
    const next = [...spec.streams]
    next[idx] = { ...next[idx], ...patch }
    setSpec({ ...spec, streams: next })
  }

  /** Apply bulk rules: target schema remapping + prefix/suffix (written explicitly, still tweakable per row) */
  function applyRules() {
    const next = spec.streams.map((s) => {
      if (s.disabled) return s
      const src = s.source_object ?? ""
      if (!src || hasWildcard(src) || src.includes("/")) return s // only table streams take part in rules
      const { schema, table } = splitObject(src)
      const targetSchema = ruleSchema === KEEP_SCHEMA ? schema : ruleSchema
      const name = `${rulePrefix}${table}${ruleSuffix}`
      const object = targetSchema ? `${targetSchema}.${name}` : name
      return { ...s, target_object: object }
    })
    setSpec({ ...spec, streams: next })
  }

  function resetNames() {
    setSpec({
      ...spec,
      streams: spec.streams.map((s) => ({ ...s, target_object: "" })),
    })
  }

  // summary
  const summary = useMemo(() => {
    let exists = 0
    let fresh = 0
    let unknown = 0
    for (const s of streams) {
      const eff = effectiveTarget(s.source_object ?? "", s.target_object)
      if (!eff || hasWildcard(s.source_object ?? "") || eff.includes("{")) unknown++
      else if (existing.has(eff.toLowerCase())) exists++
      else fresh++
    }
    return { total: streams.length, exists, fresh, unknown }
  }, [streams, existing])

  const conflictCount = issues.filter((i) => i.reason === "conflict").length
  const requiredCount = issues.filter((i) => i.reason === "required").length

  return (
    <div className="space-y-3">
      {/* Bulk naming rules (shown with ≥2 streams) */}
      {streams.length > 1 && targetIsDb && (
        <div className="rounded-xl border bg-muted/20 p-3">
          <div className="mb-2 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
            <Wand2 className="size-3.5" /> {t("jobs.mapping.rulesTitle")}
          </div>
          <div className="grid items-end gap-2 sm:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_minmax(0,1fr)_auto_auto]">
            <div>
              <Label className="text-xs">{t("jobs.mapping.targetSchema")}</Label>
              <Select value={ruleSchema} onValueChange={setRuleSchema}>
                <SelectTrigger className="mt-1 h-8 w-full text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={KEEP_SCHEMA} className="text-xs">
                    {t("jobs.mapping.keepSchema")}
                  </SelectItem>
                  {tgtSchemas.map((s) => (
                    <SelectItem key={s} value={s} className="font-mono text-xs">
                      {s}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label className="text-xs">{t("jobs.mapping.prefix")}</Label>
              <Input
                value={rulePrefix}
                onChange={(e) => setRulePrefix(e.target.value)}
                placeholder="ods_"
                className="mt-1 h-8 font-mono text-xs"
              />
            </div>
            <div>
              <Label className="text-xs">{t("jobs.mapping.suffix")}</Label>
              <Input
                value={ruleSuffix}
                onChange={(e) => setRuleSuffix(e.target.value)}
                placeholder="_copy"
                className="mt-1 h-8 font-mono text-xs"
              />
            </div>
            <Button variant="secondary" size="sm" className="h-8" onClick={applyRules}>
              {t("jobs.mapping.applyRules")}
            </Button>
            <Button variant="ghost" size="sm" className="h-8" onClick={resetNames}>
              {t("jobs.mapping.resetNames")}
            </Button>
          </div>
        </div>
      )}

      {/* Mapping table */}
      <div className="overflow-hidden rounded-xl border">
        <div className="scrollbar-thin max-h-96 overflow-y-auto">
          {spec.streams.map((s, idx) => {
            if (s.disabled) return null
            const src = s.source_object ?? ""
            const eff = effectiveTarget(src, s.target_object)
            const issue = issueBySource.get(src)
            const isWildcard = hasWildcard(src)
            const existsKnown = eff && !isWildcard && !eff.includes("{")
            const targetExists = existsKnown ? existing.has(eff.toLowerCase()) : false
            return (
              <div
                key={src}
                className={cn(
                  "flex flex-wrap items-center gap-2 border-b px-3 py-2 transition-colors last:border-b-0 hover:bg-muted/30",
                  issue && "bg-destructive/[0.04]",
                )}
              >
                <span className="min-w-0 flex-1 truncate rounded-lg border bg-muted/30 px-3 py-2 font-mono text-xs">
                  {src}
                </span>
                <ArrowRight className="size-3.5 shrink-0 text-muted-foreground" />
                <div className="relative min-w-44 max-w-72 flex-1">
                  <Input
                    value={s.target_object ?? ""}
                    placeholder={eff || t("jobs.mapping.targetRequired")}
                    className="h-9 font-mono text-xs"
                    onChange={(e) => setStream(idx, { target_object: e.target.value })}
                  />
                </div>
                {/* Status badges: conflict > missing required > existence */}
                {issue === "conflict" ? (
                  <Badge variant="outline" className="shrink-0 gap-1 border-destructive/40 bg-destructive/10 text-[10px] text-destructive">
                    <TriangleAlert className="size-3" /> {t("jobs.mapping.conflict")}
                  </Badge>
                ) : issue === "required" ? (
                  <Badge variant="outline" className="shrink-0 gap-1 border-destructive/40 bg-destructive/10 text-[10px] text-destructive">
                    <TriangleAlert className="size-3" /> {t("jobs.mapping.targetRequired")}
                  </Badge>
                ) : targetExists ? (
                  <Badge
                    variant="outline"
                    className={cn(
                      "shrink-0 text-[10px]",
                      destructive
                        ? "border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400"
                        : "border-border text-muted-foreground",
                    )}
                    title={destructive ? t("jobs.mapping.existsDestructiveHint") : undefined}
                  >
                    {t("jobs.mapping.exists")}
                  </Badge>
                ) : existsKnown ? (
                  <Badge variant="outline" className="shrink-0 border-emerald-500/40 bg-emerald-500/10 text-[10px] text-emerald-600 dark:text-emerald-400">
                    {t("jobs.mapping.fresh")}
                  </Badge>
                ) : null}
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-8 shrink-0"
                  title={t("jobs.wizard.useSourceName")}
                  disabled={isWildcard}
                  onClick={() => setStream(idx, { target_object: src })}
                >
                  <Copy className="size-3.5" />
                </Button>
              </div>
            )
          })}
        </div>

        {/* Summary bar */}
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
          {tgtFetching ? (
            <span>{t("jobs.mapping.checking")}</span>
          ) : (
            <>
              <span>{t("jobs.mapping.summaryCount", { count: summary.total })}</span>
              {targetIsDb && (
                <>
                  <span>· {t("jobs.mapping.summaryFresh", { count: summary.fresh })}</span>
                  <span>· {t("jobs.mapping.summaryExists", { count: summary.exists })}</span>
                </>
              )}
              {conflictCount > 0 && (
                <span className="font-medium text-destructive">
                  · {t("jobs.mapping.summaryConflict", { count: conflictCount })}
                </span>
              )}
              {requiredCount > 0 && (
                <span className="font-medium text-destructive">
                  · {t("jobs.mapping.summaryRequired", { count: requiredCount })}
                </span>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  )
}
