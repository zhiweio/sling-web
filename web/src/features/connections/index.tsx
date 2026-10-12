// 连接中心：卡片网格（搜索/类型/状态筛选 + 排序 + 分页）+ 添加向导 + 详情
import { useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate, useParams } from "react-router-dom"
import { toast } from "sonner"
import {
  CheckCircle2,
  Database,
  Eye,
  FileText,
  Folder,
  Loader2,
  Lock,
  Plus,
  Table,
  Trash2,
  Upload,
  XCircle,
} from "lucide-react"
import { api } from "@/lib/api"
import type { ConnTypeDef, Connection } from "@/lib/types"
import { PageHeader } from "@/features/dashboard"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog"
import { EmptyState } from "@/components/empty-state"
import { ListPagination, ListToolbar, NoMatchState } from "@/components/list-toolbar"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { cn } from "@/lib/utils"
import { BrandIcon } from "@/lib/brand-icons"
import { formatTime } from "@/lib/format"

const PAGE_SIZE = 12

export default function Connections() {
  const { t } = useTranslation()
  const { name: selected } = useParams()
  const [q, setQ] = useState("")
  const [typeF, setTypeF] = useState("all")
  const [statusF, setStatusF] = useState("all")
  const [sort, setSort] = useState("name")
  const [page, setPage] = useState(0)
  const [wizardOpen, setWizardOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<Connection | null>(null)

  const { data: conns, isLoading } = useQuery({ queryKey: ["conns"], queryFn: api.listConns })

  useEffect(() => setPage(0), [q, typeF, statusF, sort])

  const types = useMemo(() => {
    const counts = new Map<string, number>()
    for (const c of conns ?? []) counts.set(c.type, (counts.get(c.type) ?? 0) + 1)
    return [...counts.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
  }, [conns])

  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase()
    const out = (conns ?? []).filter(
      (c) =>
        (typeF === "all" || c.type === typeF) &&
        (statusF === "all" ||
          (statusF === "success" && c.last_test_status === "success") ||
          (statusF === "error" && c.last_test_status === "error") ||
          (statusF === "untested" && !c.last_test_status)) &&
        (!needle || c.name.toLowerCase().includes(needle) || c.type.toLowerCase().includes(needle)),
    )
    return [...out].sort((a, b) => {
      if (sort === "type") return a.type.localeCompare(b.type) || a.name.localeCompare(b.name)
      if (sort === "newest") return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
      return a.name.localeCompare(b.name)
    })
  }, [conns, q, typeF, statusF, sort])

  const pages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE))
  const pageItems = filtered.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE)
  const hasFilters = q.trim() !== "" || typeF !== "all" || statusF !== "all"

  return (
    <div className="space-y-6">
      <PageHeader
        title={t("connections.title")}
        subtitle={t("connections.subtitle")}
        actions={
          <>
            <ImportButton />
            <Button onClick={() => setWizardOpen(true)}>
              <Plus className="size-4" /> {t("app.actions.newConnection")}
            </Button>
          </>
        }
      />

      <ListToolbar
        search={q}
        onSearch={setQ}
        searchPlaceholder={t("connections.searchPlaceholder")}
        sort={sort}
        onSort={setSort}
        sortOptions={[
          { value: "name", label: t("common.sortByName") },
          { value: "type", label: t("common.sortByType") },
          { value: "newest", label: t("common.sortByNewest") },
        ]}
      >
        <Select value={typeF} onValueChange={setTypeF}>
          <SelectTrigger size="sm" className="h-9 w-36 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">
              {t("common.allTypes")} · {(conns ?? []).length}
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
        <Select value={statusF} onValueChange={setStatusF}>
          <SelectTrigger size="sm" className="h-9 w-32 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("common.allStatus")}</SelectItem>
            <SelectItem value="success">{t("connections.testedOk")}</SelectItem>
            <SelectItem value="error">{t("connections.testedFail")}</SelectItem>
            <SelectItem value="untested">{t("connections.neverTested")}</SelectItem>
          </SelectContent>
        </Select>
      </ListToolbar>

      {isLoading ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-36" />
          ))}
        </div>
      ) : (conns ?? []).length === 0 ? (
        <EmptyState icon={Database} title={t("connections.emptyTitle")} body={t("connections.emptyBody")} />
      ) : filtered.length === 0 ? (
        <NoMatchState
          hasFilters={hasFilters}
          onClear={() => {
            setQ("")
            setTypeF("all")
            setStatusF("all")
          }}
        />
      ) : (
        <>
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {pageItems.map((c) => (
              <ConnCard key={c.name} conn={c} onDelete={() => setDeleteTarget(c)} />
            ))}
          </div>
          <ListPagination
            total={filtered.length}
            page={page}
            pages={pages}
            onPage={setPage}
            countLabel={t("jobs.wizard.connCount", { count: filtered.length })}
          />
        </>
      )}

      {wizardOpen && (
        <ConnWizard
          onClose={() => setWizardOpen(false)}
          onSaved={() => {
            setWizardOpen(false)
            toast.success(t("common.saved"))
          }}
        />
      )}

      {selected && <ConnDetail name={selected} />}

      <AlertDialog open={!!deleteTarget} onOpenChange={(v) => !v && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("connections.deleteConfirmTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("connections.deleteConfirmBody", { name: deleteTarget?.name })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("app.actions.cancelEdit")}</AlertDialogCancel>
            <DeleteConfirm conn={deleteTarget} onDone={() => setDeleteTarget(null)} />
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

function ConnCard({ conn, onDelete }: { conn: Connection; onDelete: () => void }) {
  const { t } = useTranslation()
  const nav = useNavigate()
  const status =
    conn.last_test_status === "success"
      ? { icon: CheckCircle2, cls: "text-emerald-500", label: t("connections.testedOk") }
      : conn.last_test_status === "error"
        ? { icon: XCircle, cls: "text-destructive", label: t("connections.testedFail") }
        : { icon: null, cls: "text-muted-foreground", label: t("connections.neverTested") }

  return (
    <Card
      className="group cursor-pointer p-5 transition-all hover:shadow-md"
      onClick={() => nav(`/connections/${encodeURIComponent(conn.name)}`)}
    >
      <div className="flex items-start justify-between">
        <BrandIcon type={conn.type} className="size-10 rounded-xl" />
        <Button
          variant="ghost"
          size="icon"
          className="opacity-0 transition-opacity group-hover:opacity-100"
          onClick={(e) => {
            e.stopPropagation()
            onDelete()
          }}
        >
          <Trash2 className="size-4 text-destructive" />
        </Button>
      </div>
      <div className="mt-3 truncate text-sm font-semibold">{conn.name}</div>
      <div className="mt-0.5 flex items-center gap-2 text-xs text-muted-foreground">
        <span className="uppercase">{conn.type}</span>
        <Badge variant="secondary" className="h-5 gap-1 px-1.5 text-[10px]">
          <Lock className="size-3" /> {t("connections.encrypted")}
        </Badge>
      </div>
      <div className={cn("mt-3 flex items-center gap-1.5 text-xs", status.cls)}>
        {status.icon && <status.icon className="size-3.5" />}
        {status.label}
        {conn.last_test_at && <span className="text-muted-foreground">· {formatTime(conn.last_test_at)}</span>}
      </div>
    </Card>
  )
}

function DeleteConfirm({ conn, onDone }: { conn: Connection | null; onDone: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const del = useMutation({
    mutationFn: () => api.deleteConn(conn!.name),
    onSuccess: () => {
      toast.success(t("common.saved"))
      qc.invalidateQueries({ queryKey: ["conns"] })
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

function ImportButton() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const imp = useMutation({
    mutationFn: () => api.importConns(),
    onSuccess: (res) => {
      const imported = res.imported ?? []
      if (imported.length > 0) {
        toast.success(`已导入 ${imported.length} 个连接：${imported.join(", ")}`)
        qc.invalidateQueries({ queryKey: ["conns"] })
      } else {
        toast.info("未发现可导入的连接")
      }
    },
    onError: (e) => toast.error(e.message),
  })
  return (
    <Button variant="outline" onClick={() => imp.mutate()} disabled={imp.isPending}>
      {imp.isPending ? <Loader2 className="size-4 animate-spin" /> : <Upload className="size-4" />}
      {t("app.actions.import")}
    </Button>
  )
}

/* ------------------------------- 添加向导 -------------------------------- */

function ConnWizard({ onClose, onSaved }: { onClose: () => void; onSaved: () => void }) {
  const { t } = useTranslation()
  const [step, setStep] = useState(0)
  const [typeDef, setTypeDef] = useState<ConnTypeDef | null>(null)
  const [name, setName] = useState("")
  const [props, setProps] = useState<Record<string, string>>({})
  const [testResult, setTestResult] = useState<{ success: boolean; error?: string; ms: number } | null>(null)
  const [testing, setTesting] = useState(false)
  const [saving, setSaving] = useState(false)

  const { data: types } = useQuery({ queryKey: ["conn-types"], queryFn: api.connTypes })

  const grouped = useMemo(() => {
    const g: Record<string, ConnTypeDef[]> = { database: [], file: [], api: [] }
    for (const ty of types ?? []) (g[ty.kind] ??= []).push(ty)
    return g
  }, [types])


  const filledRequired = useMemo(() => {
    if (!typeDef) return false
    const req = typeDef.required ?? []
    return req.every((r) => (props[r] ?? "").trim() !== "")
  }, [typeDef, props])

  async function doTest() {
    setTesting(true)
    setTestResult(null)
    try {
      const res = await api.testConn({ name, type: typeDef!.type, props })
      setTestResult(res)
    } catch (e) {
      console.error("test connection failed", e)
      setTestResult({ success: false, error: (e as Error).message, ms: 0 })
    } finally {
      setTesting(false)
    }
  }

  async function doSave() {
    setSaving(true)
    try {
      await api.createConn({ name, type: typeDef!.type, props })
      onSaved()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("connections.addTitle")}</DialogTitle>
          <DialogDescription className="sr-only">{t("connections.addTitle")}</DialogDescription>
        </DialogHeader>

        {/* 步骤指示 */}
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          {[t("connections.stepType"), t("connections.stepProps"), t("connections.stepDone")].map((s, i) => (
            <div key={s} className="flex items-center gap-2">
              <span
                className={cn(
                  "flex size-5 items-center justify-center rounded-full border text-[10px] font-bold",
                  i < step ? "border-primary bg-primary text-primary-foreground" : i === step ? "border-primary text-primary" : "border-border",
                )}
              >
                {i + 1}
              </span>
              <span className={i === step ? "font-medium text-foreground" : ""}>{s}</span>
              {i < 2 && <span className="mx-1 h-px w-8 bg-border" />}
            </div>
          ))}
        </div>

        {step === 0 && (
          <div className="scrollbar-thin max-h-[60vh] space-y-4 overflow-y-auto pr-1">
            {(["database", "file", "api"] as const).map((kind) =>
              grouped[kind]?.length ? (
                <div key={kind}>
                  <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                    {kind === "database" ? "数据库" : kind === "file" ? "文件 / 云存储" : "API"}
                  </div>
                  <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
                    {grouped[kind].map((ty) => (
                      <button
                        key={ty.type}
                        onClick={() => {
                          setTypeDef(ty)
                          setProps(Object.fromEntries((ty.properties ?? []).filter((p) => !p.secret && typeof p.default === "string" && p.name !== "name").map((p) => [p.name, String(p.default)])))
                          setStep(1)
                        }}
                        className="flex items-center gap-2.5 rounded-lg border px-3 py-2.5 text-left text-sm hover:border-primary hover:bg-primary/5"
                      >
                        <BrandIcon type={ty.type} className="size-6" />
                        <span className="truncate">{ty.title}</span>
                      </button>
                    ))}
                  </div>
                </div>
              ) : null,
            )}
          </div>
        )}

        {step === 1 && typeDef && (
          <div className="space-y-4">
            <div>
              <Label htmlFor="conn-name">连接名称 *</Label>
              <Input
                id="conn-name"
                value={name}
                onChange={(e) => setName(e.target.value.toUpperCase().replace(/\s+/g, "_"))}
                placeholder={(typeDef.properties?.find((p) => p.name === "name")?.examples ?? ["MY_CONN"])[0]}
                className="mt-1.5"
              />
              <p className="mt-1 text-xs text-muted-foreground">将用于任务中引用此连接</p>
            </div>

            {(typeDef.properties ?? [])
              .filter((p) => p.name !== "name")
              .map((p) => (
                <PropField key={p.name} def={p} value={props[p.name] ?? ""} onChange={(v) => setProps((s) => ({ ...s, [p.name]: v }))} />
              ))}

            {testResult && (
              <div
                className={cn(
                  "flex items-start gap-3 rounded-xl border p-4",
                  testResult.success ? "border-emerald-500/30 bg-emerald-500/5" : "border-destructive/30 bg-destructive/5",
                )}
              >
                {testResult.success ? (
                  <CheckCircle2 className="size-5 shrink-0 text-emerald-500" />
                ) : (
                  <XCircle className="size-5 shrink-0 text-destructive" />
                )}
                <div className="min-w-0 text-sm">
                  <div className="font-semibold">
                    {testResult.success ? t("connections.testedOk") : t("connections.testedFail")} · {testResult.ms}ms
                  </div>
                  {!testResult.success && testResult.error && (
                    <div className="mt-1 break-all text-xs text-muted-foreground">{testResult.error}</div>
                  )}
                </div>
              </div>
            )}

            <div className="flex items-center justify-between pt-2">
              <Button variant="outline" onClick={() => setStep(0)}>
                {t("app.actions.prev")}
              </Button>
              <div className="flex gap-2">
                <Button
                  disabled={!name.trim() || !filledRequired}
                  onClick={doTest}
                  variant="secondary"
                >
                  {testing ? <Loader2 className="size-4 animate-spin" /> : null}
                  {t("app.actions.test")}
                </Button>
                {testResult?.success && (
                  <Button onClick={() => setStep(2)}>{t("app.actions.next")}</Button>
                )}
              </div>
            </div>
          </div>
        )}

        {step === 2 && typeDef && (
          <div className="space-y-4">
            {testResult?.success ? (
              <div className="flex items-center gap-3 rounded-xl border border-emerald-500/30 bg-emerald-500/5 p-4">
                <CheckCircle2 className="size-6 shrink-0 text-emerald-500" />
                <div>
                  <div className="text-sm font-semibold">{t("connections.testedOk")} · {testResult.ms}ms</div>
                  <div className="text-xs text-muted-foreground">{name} ({typeDef.title})</div>
                </div>
              </div>
            ) : (
              testResult && (
                <div className="flex items-start gap-3 rounded-xl border border-destructive/30 bg-destructive/5 p-4">
                  <XCircle className="size-6 shrink-0 text-destructive" />
                  <div className="min-w-0">
                    <div className="text-sm font-semibold">{t("connections.testedFail")}</div>
                    <div className="mt-1 break-all text-xs text-muted-foreground">{testResult.error}</div>
                  </div>
                </div>
              )
            )}
            <div className="flex items-center justify-between">
              <Button variant="outline" onClick={() => setStep(1)}>
                {t("app.actions.prev")}
              </Button>
              <div className="flex gap-2">
                <Button variant="secondary" onClick={doTest} disabled={testing}>
                  {t("app.actions.test")}
                </Button>
                <Button onClick={doSave} disabled={saving || !testResult?.success}>
                  {saving && <Loader2 className="size-4 animate-spin" />}
                  {t("app.actions.save")}
                </Button>
              </div>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

function PropField({
  def,
  value,
  onChange,
}: {
  def: { name: string; type: string; title?: string; description?: string; examples?: string[]; options?: string[]; secret?: boolean; required?: boolean }
  value: string
  onChange: (v: string) => void
}) {
  const isSecret = def.secret
  return (
    <div>
      <Label htmlFor={`prop-${def.name}`}>
        {def.title || def.name}
        {def.required && <span className="ml-0.5 text-destructive">*</span>}
        {isSecret && <Lock className="ml-1.5 inline size-3 text-muted-foreground" />}
      </Label>
      {def.options && def.options.length > 0 ? (
        <select
          id={`prop-${def.name}`}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className="mt-1.5 flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm"
        >
          <option value="">—</option>
          {def.options.map((o) => (
            <option key={o} value={o}>
              {o}
            </option>
          ))}
        </select>
      ) : (
        <Input
          id={`prop-${def.name}`}
          type={isSecret ? "password" : def.type === "integer" ? "number" : "text"}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={def.examples?.[0] ?? ""}
          autoComplete="off"
          className="mt-1.5"
        />
      )}
      {def.description && <p className="mt-1 text-xs text-muted-foreground">{def.description}</p>}
    </div>
  )
}

/* -------------------------------- 连接详情 ------------------------------- */

function ConnDetail({ name }: { name: string }) {
  const { t } = useTranslation()
  const nav = useNavigate()
  const qc = useQueryClient()
  const { data: detail } = useQuery({
    queryKey: ["conn", name],
    queryFn: () => api.getConn(name),
  })
  const [testing, setTesting] = useState(false)

  const { data: discover } = useQuery({
    queryKey: ["conn-discover", name],
    queryFn: () => api.discover(name, "table"),
    enabled: !!detail,
    staleTime: 60_000,
  })

  async function test() {
    setTesting(true)
    try {
      const res = await api.testConn({ name })
      if (res.success) toast.success(t("connections.testedOk"))
      else toast.error(res.error || t("connections.testedFail"))
      qc.invalidateQueries({ queryKey: ["conns"] })
      qc.invalidateQueries({ queryKey: ["conn", name] })
    } finally {
      setTesting(false)
    }
  }

  return (
    <Dialog open onOpenChange={(v) => !v && nav("/connections")}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {detail?.name}
            <Badge variant="secondary" className="gap-1 text-[10px]">
              <Lock className="size-3" />
              {t("connections.encrypted")}
            </Badge>
            {detail?.used_by && detail.used_by.length > 0 && (
              <Badge variant="outline" className="text-[10px]">
                {t("connections.usedBy", { count: detail.used_by.length })}
              </Badge>
            )}
          </DialogTitle>
        </DialogHeader>

        <div className="flex gap-2">
          <Button size="sm" variant="secondary" onClick={test} disabled={testing}>
            {testing ? <Loader2 className="size-4 animate-spin" /> : null}
            {t("app.actions.test")}
          </Button>
        </div>

        <Tabs defaultValue="schema" className="mt-2">
          <TabsList>
            <TabsTrigger value="schema">{t("connections.schema")}</TabsTrigger>
            <TabsTrigger value="props">{t("connections.props")}</TabsTrigger>
          </TabsList>

          <TabsContent value="schema" className="max-h-[50vh]">
            {discover?.schemas && discover.schemas.length > 0 ? (
              <div className="scrollbar-thin max-h-[50vh] overflow-y-auto pr-1">
                {discover.schemas.map((schema) => (
                  <div key={schema.name} className="mb-3">
                    <div className="mb-1 font-mono text-xs font-semibold text-muted-foreground">{schema.name}</div>
                    <div className="overflow-hidden rounded-lg border">
                      {schema.tables.map((tb) => (
                        <TablePreviewRow key={tb.name} connName={name} table={`${schema.name}.${tb.name}`} name={tb.name} isView={tb.is_view} />
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            ) : discover?.nodes && discover.nodes.length > 0 ? (
              <div className="scrollbar-thin max-h-[50vh] overflow-y-auto rounded-lg border p-2 font-mono text-xs">
                {discover.nodes.map((n) => (
                  <div key={n.path} className="flex items-center justify-between rounded px-2 py-1 hover:bg-muted/50">
                    <span className="flex min-w-0 items-center gap-1.5">
                      {n.is_dir ? (
                        <Folder className="size-3.5 shrink-0 text-amber-500" />
                      ) : (
                        <FileText className="size-3.5 shrink-0 text-muted-foreground" />
                      )}
                      <span className="truncate">{n.name}</span>
                    </span>
                    <span className="shrink-0 pl-2 text-muted-foreground">{n.updated_at}</span>
                  </div>
                ))}
              </div>
            ) : (
              <div className="py-10 text-center text-sm text-muted-foreground">{t("connections.emptySchema")}</div>
            )}
          </TabsContent>

          <TabsContent value="props">
            <div className="space-y-1.5 rounded-lg border p-4 text-sm">
              {detail &&
                Object.entries(detail.props).map(([k, v]) => (
                  <div key={k} className="flex gap-4">
                    <span className="w-40 shrink-0 text-muted-foreground">{k}</span>
                    <span className="min-w-0 flex-1 break-all font-mono text-xs">{String(v)}</span>
                  </div>
                ))}
            </div>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}

function TablePreviewRow({ connName, table, name, isView }: { connName: string; table: string; name: string; isView?: boolean }) {
  const [open, setOpen] = useState(false)
  const { data } = useQuery({
    queryKey: ["conn-preview", connName, table],
    queryFn: () => api.preview(connName, table, 20),
    enabled: open,
    staleTime: 30_000,
  })

  const cols = data?.columns ?? []
  const rows = data?.rows ?? []

  return (
    <div className="border-b last:border-b-0">
      <button onClick={() => setOpen(!open)} className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs hover:bg-muted/50">
        <span className="flex min-w-0 items-center gap-1.5">
          {isView ? (
            <Eye className="size-3.5 shrink-0 text-muted-foreground" />
          ) : (
            <Table className="size-3.5 shrink-0 text-primary" />
          )}
          <span className="truncate font-mono">{name}</span>
        </span>
        {open && <span className="ml-auto text-muted-foreground">{rows.length} 行示例</span>}
      </button>
      {open && (
        <div className="overflow-x-auto border-t bg-muted/30 px-3 py-2">
          {rows.length === 0 ? (
            <span className="text-xs text-muted-foreground">{t0("common.loading")}</span>
          ) : (
            <table className="w-full text-left text-[11px]">
              <thead>
                <tr className="text-muted-foreground">
                  {cols.map((c) => (
                    <th key={c.name} className="whitespace-nowrap px-2 py-1 font-medium">
                      {c.name}
                      <span className="ml-1 opacity-50">{c.type}</span>
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="font-mono">
                {rows.map((r, i) => (
                  <tr key={i} className="border-t border-border/50">
                    {cols.map((c) => (
                      <td key={c.name} className="max-w-40 truncate whitespace-nowrap px-2 py-1">
                        {r[c.name] === null ? <span className="opacity-40">NULL</span> : String(r[c.name])}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
    </div>
  )
}

function t0(key: string) {
  return key.split(".").pop() ?? key
}
