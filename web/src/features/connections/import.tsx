// import.tsx — connections import dialog: upload/paste a connections file
// (sling env.yaml / JSON) or scan the server machine's sling environment,
// review what was found, then import the selection into the encrypted store.
import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import { ChevronLeft, FileText, Loader2, Lock, Radar, Upload, X } from "lucide-react"
import { api } from "@/lib/api"
import type { ImportEntry } from "@/lib/types"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { Alert, AlertDescription } from "@/components/ui/alert"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { BrandIcon } from "@/lib/brand-icons"
import { cn } from "@/lib/utils"

const FORMAT_EXAMPLE = `connections:
  MY_PG:
    type: postgres
    host: db.example.com
    port: 5432
    username: admin
    password: secret`

function fileKb(bytes: number) {
  return bytes < 1024 ? `${bytes} B` : `${(bytes / 1024).toFixed(1)} KB`
}

export function ImportDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [step, setStep] = useState<"source" | "review">("source")
  const [mode, setMode] = useState<"file" | "env">("file")
  const [content, setContent] = useState("")
  const [fileName, setFileName] = useState<string | null>(null)
  const [fileSize, setFileSize] = useState(0)
  const [dragOver, setDragOver] = useState(false)
  const [entries, setEntries] = useState<ImportEntry[]>([])
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [overwrite, setOverwrite] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  function reset() {
    setStep("source")
    setMode("file")
    setContent("")
    setFileName(null)
    setFileSize(0)
    setEntries([])
    setSelected(new Set())
    setOverwrite(false)
  }
  function close() {
    onOpenChange(false)
    reset()
  }

  const preview = useMutation({
    mutationFn: async () =>
      mode === "env" ? (await api.previewImportEnv()).found : (await api.previewImportConns(content)).found,
    onSuccess: (found) => {
      setEntries(found)
      setSelected(new Set(found.filter((e) => e.valid).map((e) => e.name)))
      setOverwrite(false)
      setStep("review")
    },
    onError: (e) => toast.error(e.message),
  })

  const commit = useMutation({
    mutationFn: async () => {
      const names = entries.filter((e) => selected.has(e.name)).map((e) => e.name)
      const onConflict = overwrite ? "overwrite" : "skip"
      return mode === "env" ? api.importEnvConns(names, onConflict) : api.importConns(content, names, onConflict)
    },
    onSuccess: (res) => {
      if (res.imported.length > 0) {
        toast.success(t("connections.importSuccess", { count: res.imported.length, names: res.imported.join(", ") }))
        qc.invalidateQueries({ queryKey: ["conns"] })
      }
      if (res.skipped.length > 0) {
        toast.info(t("connections.importSkipped", { count: res.skipped.length }))
      }
      if (res.imported.length === 0 && res.skipped.length === 0) {
        toast.info(t("connections.importNoneSelected"))
      }
      close()
    },
    onError: (e) => toast.error(e.message),
  })

  function onFile(f: File) {
    if (!/\.(ya?ml|json)$/i.test(f.name)) {
      toast.error(t("connections.importBadFile"))
      return
    }
    f.text().then((text) => {
      setFileName(f.name)
      setFileSize(f.size)
      setContent(text)
    })
  }

  // what the import button will actually write (existing names only apply
  // when overwrite is on)
  const willImport = entries.filter((e) => e.valid && selected.has(e.name) && (overwrite || !e.exists))
  const validEntries = entries.filter((e) => e.valid)
  const anyExists = entries.some((e) => e.exists)
  const allChecked = validEntries.length > 0 && validEntries.every((e) => selected.has(e.name))

  return (
    <Dialog open={open} onOpenChange={(v) => !v && close()}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("connections.importDialogTitle")}</DialogTitle>
          <DialogDescription className="sr-only">{t("connections.importDialogTitle")}</DialogDescription>
        </DialogHeader>

        {step === "source" ? (
          <>
            <Tabs value={mode} onValueChange={(v) => setMode(v as "file" | "env")}>
              <TabsList className="w-full">
                <TabsTrigger value="file" className="flex-1">
                  <Upload className="size-3.5" /> {t("connections.importTabFile")}
                </TabsTrigger>
                <TabsTrigger value="env" className="flex-1">
                  <Radar className="size-3.5" /> {t("connections.importTabEnv")}
                </TabsTrigger>
              </TabsList>

              <TabsContent value="file" className="space-y-3">
                {fileName ? (
                  <div className="flex items-center gap-3 rounded-lg border px-3 py-2.5">
                    <FileText className="size-5 shrink-0 text-primary" />
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm font-medium">{fileName}</div>
                      <div className="text-xs text-muted-foreground">{fileKb(fileSize)}</div>
                    </div>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => {
                        setFileName(null)
                        setContent("")
                      }}
                    >
                      <X className="size-4" />
                    </Button>
                  </div>
                ) : (
                  <button
                    type="button"
                    onClick={() => fileRef.current?.click()}
                    onDragOver={(e) => {
                      e.preventDefault()
                      setDragOver(true)
                    }}
                    onDragLeave={() => setDragOver(false)}
                    onDrop={(e) => {
                      e.preventDefault()
                      setDragOver(false)
                      const f = e.dataTransfer.files?.[0]
                      if (f) onFile(f)
                    }}
                    className={cn(
                      "flex w-full flex-col items-center gap-1.5 rounded-lg border-2 border-dashed px-4 py-8 text-center transition-colors",
                      dragOver ? "border-primary bg-primary/5" : "border-border hover:border-primary/50 hover:bg-muted/40",
                    )}
                  >
                    <Upload className="size-7 text-muted-foreground" />
                    <span className="text-sm font-medium">{t("connections.importDropzone")}</span>
                    <span className="text-xs text-muted-foreground">{t("connections.importDropHint")}</span>
                  </button>
                )}
                <input
                  ref={fileRef}
                  type="file"
                  accept=".yaml,.yml,.json"
                  className="hidden"
                  onChange={(e) => {
                    const f = e.target.files?.[0]
                    if (f) onFile(f)
                    e.target.value = ""
                  }}
                />

                {!fileName && (
                  <div className="space-y-1.5">
                    <Label htmlFor="import-paste">{t("connections.importPaste")}</Label>
                    <Textarea
                      id="import-paste"
                      value={content}
                      onChange={(e) => setContent(e.target.value)}
                      placeholder={t("connections.importPastePlaceholder")}
                      className="min-h-24 font-mono text-xs"
                    />
                  </div>
                )}
              </TabsContent>

              <TabsContent value="env">
                <div className="flex flex-col items-center gap-2 rounded-lg border border-dashed px-4 py-8 text-center">
                  <Radar className="size-7 text-muted-foreground" />
                  <p className="max-w-sm text-sm text-muted-foreground">{t("connections.importEnvDesc")}</p>
                </div>
              </TabsContent>
            </Tabs>

            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Lock className="size-3.5 shrink-0" />
              {t("connections.importSecureNote")}
            </div>

            <DialogFooter>
              <Button variant="outline" onClick={close}>
                {t("app.actions.cancelEdit")}
              </Button>
              <Button
                onClick={() => preview.mutate()}
                disabled={preview.isPending || (mode === "file" && content.trim() === "")}
              >
                {preview.isPending && <Loader2 className="size-4 animate-spin" />}
                {mode === "env" ? t("connections.importScanBtn") : t("connections.importPreviewBtn")}
              </Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <div className="text-sm font-medium">
              {mode === "env" ? t("connections.importEnvFound", { count: entries.length }) : t("connections.importFound", { count: entries.length })}
            </div>

            {entries.length === 0 ? (
              <div className="space-y-3">
                <Alert>
                  <AlertDescription>{t("connections.importNoConns")}</AlertDescription>
                </Alert>
                <div className="space-y-1.5">
                  <div className="text-xs font-medium text-muted-foreground">{t("connections.importFormatHint")}</div>
                  <pre className="scrollbar-thin overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs leading-relaxed">
                    {FORMAT_EXAMPLE}
                  </pre>
                </div>
              </div>
            ) : (
              <>
                <div className="scrollbar-thin max-h-[46vh] space-y-1.5 overflow-y-auto pr-1">
                  {validEntries.length > 1 && (
                    <label className="flex cursor-pointer items-center gap-2 px-1 pb-1 text-xs text-muted-foreground">
                      <Checkbox
                        checked={allChecked}
                        onCheckedChange={(v) =>
                          setSelected(v === true ? new Set(validEntries.map((e) => e.name)) : new Set())
                        }
                      />
                      {t("connections.importSelectAll")}
                    </label>
                  )}
                  {entries.map((e) => (
                    <label
                      key={e.name}
                      className={cn(
                        "flex items-center gap-3 rounded-lg border px-3 py-2.5",
                        e.valid ? "cursor-pointer hover:bg-muted/40" : "opacity-60",
                      )}
                    >
                      <Checkbox
                        checked={selected.has(e.name)}
                        disabled={!e.valid}
                        onCheckedChange={(v) =>
                          setSelected((cur) => {
                            const next = new Set(cur)
                            if (v === true) next.add(e.name)
                            else next.delete(e.name)
                            return next
                          })
                        }
                      />
                      <BrandIcon type={e.type || "unknown"} className="size-6 shrink-0" />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <span className="truncate text-sm font-medium">{e.name}</span>
                          {!e.valid ? (
                            <Badge variant="outline" className="h-5 shrink-0 border-destructive/40 text-[10px] text-destructive">
                              {t("connections.importStatusInvalid")}
                            </Badge>
                          ) : e.exists ? (
                            <Badge variant="outline" className="h-5 shrink-0 border-amber-500/40 text-[10px] text-amber-600 dark:text-amber-400">
                              {t("connections.importStatusExists")}
                            </Badge>
                          ) : (
                            <Badge variant="outline" className="h-5 shrink-0 border-emerald-500/40 text-[10px] text-emerald-600 dark:text-emerald-400">
                              {t("connections.importStatusNew")}
                            </Badge>
                          )}
                        </div>
                        <div className="mt-0.5 truncate text-xs text-muted-foreground">
                          {e.valid ? (
                            e.source ?? (e.type || "").toUpperCase()
                          ) : (
                            <span className="text-destructive">{e.error}</span>
                          )}
                          {e.valid && e.masked_secret && (
                            <span className="text-amber-600 dark:text-amber-400">
                              {" · "}
                              {t("connections.importMaskedNote")}
                            </span>
                          )}
                        </div>
                      </div>
                      {e.valid && (e.secret_fields?.length ?? 0) > 0 && (
                        <span className="hidden shrink-0 items-center gap-1 text-xs text-muted-foreground sm:flex">
                          <Lock className="size-3" />
                          {e.secret_fields!.join(", ")}
                        </span>
                      )}
                    </label>
                  ))}
                </div>

                {anyExists && (
                  <div className="flex items-start justify-between gap-3 rounded-lg border px-3 py-2.5">
                    <div className="space-y-0.5">
                      <Label htmlFor="import-overwrite" className="text-sm">
                        {t("connections.importOverwrite")}
                      </Label>
                      <p className="text-xs text-muted-foreground">{t("connections.importOverwriteHint")}</p>
                    </div>
                    <Switch
                      id="import-overwrite"
                      checked={overwrite}
                      onCheckedChange={(v) => {
                        setOverwrite(v)
                        if (!v) {
                          // deselected existing names: they'd be skipped anyway
                          setSelected((cur) => {
                            const next = new Set(cur)
                            for (const e of entries) if (e.exists) next.delete(e.name)
                            return next
                          })
                        }
                      }}
                    />
                  </div>
                )}
              </>
            )}

            <DialogFooter>
              <Button variant="outline" onClick={() => setStep("source")} disabled={commit.isPending}>
                <ChevronLeft className="size-4" /> {t("connections.importBack")}
              </Button>
              <Button onClick={() => commit.mutate()} disabled={commit.isPending || willImport.length === 0}>
                {commit.isPending && <Loader2 className="size-4 animate-spin" />}
                {t("connections.importBtn", { count: willImport.length })}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
