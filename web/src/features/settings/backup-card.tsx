// 设置·数据备份:自动备份(频率/保留份数)+ 立即备份 + 快照文件列表(下载/删除)+ 手动恢复说明
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Download, HardDriveDownload, Info, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { formatBytes, formatTime } from "@/lib/format"
import type { BackupFile } from "@/lib/types"

// 频率预设 → cron
const FREQ_PRESETS = [
  { value: "0 3 * * *", key: "settings.backupFreqDaily" },
  { value: "0 */6 * * *", key: "settings.backupFreq6h" },
  { value: "0 3 * * 0", key: "settings.backupFreqWeekly" },
]

export function BackupCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data: settings } = useQuery({ queryKey: ["settings"], queryFn: api.getSettings })
  const { data: backups } = useQuery({ queryKey: ["backups"], queryFn: api.backups })
  const [deleting, setDeleting] = useState<BackupFile | null>(null)

  const putSetting = useMutation({
    mutationFn: (kv: Record<string, string>) => api.putSettings(kv),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["settings"] }),
    onError: (e) => toast.error(e.message),
  })

  const createNow = useMutation({
    mutationFn: api.createBackup,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["backups"] })
      toast.success(t("settings.backupCreated"))
    },
    onError: (e) => toast.error(e.message),
  })

  const autoEnabled = settings?.["backup.auto_enabled"] !== "false"
  const cron = settings?.["backup.cron"] ?? "0 3 * * *"
  const keep = settings?.["backup.keep_count"] ?? "7"
  const knownPreset = FREQ_PRESETS.some((p) => p.value === cron)

  const downloadHref = (name: string) => {
    const token = new URLSearchParams(location.search).get("token")
    return `/api/backups/${encodeURIComponent(name)}/download${token ? `?token=${token}` : ""}`
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("settings.backup")}</CardTitle>
        <CardDescription>{t("settings.backupDesc")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        {/* 自动备份配置 */}
        <div className="space-y-3">
          <label className="flex cursor-pointer items-center justify-between text-sm">
            <span>{t("settings.backupAuto")}</span>
            <Switch
              checked={autoEnabled}
              onCheckedChange={(v) => putSetting.mutate({ "backup.auto_enabled": String(v) })}
            />
          </label>
          {autoEnabled && (
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label>{t("settings.backupFreq")}</Label>
                <Select
                  value={knownPreset ? cron : "custom"}
                  onValueChange={(v) => v !== "custom" && putSetting.mutate({ "backup.cron": v })}
                >
                  <SelectTrigger className="w-44"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {FREQ_PRESETS.map((p) => (
                      <SelectItem key={p.value} value={p.value}>{t(p.key)}</SelectItem>
                    ))}
                    {!knownPreset && <SelectItem value="custom" disabled>{cron}</SelectItem>}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label>{t("settings.backupKeep")}</Label>
                <Input
                  type="number" min={1} defaultValue={keep} key={keep}
                  className="w-24"
                  onBlur={(e) => {
                    if (e.target.value !== keep && Number(e.target.value) >= 1) {
                      putSetting.mutate({ "backup.keep_count": e.target.value })
                    }
                  }}
                />
              </div>
            </div>
          )}
        </div>

        <Button variant="secondary" size="sm" onClick={() => createNow.mutate()} disabled={createNow.isPending}>
          <HardDriveDownload className="size-4" /> {t("settings.backupNow")}
        </Button>

        {/* 备份文件列表 */}
        <div className="space-y-2">
          <span className="text-sm font-medium">{t("settings.backupList")}</span>
          {(backups?.backups ?? []).length === 0 ? (
            <div className="rounded-lg border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
              {t("settings.backupEmpty")}
            </div>
          ) : (
            <div className="overflow-hidden rounded-lg border">
              {(backups?.backups ?? []).map((b) => (
                <div key={b.name} className="flex flex-wrap items-center gap-3 border-b px-3 py-2.5 text-sm last:border-b-0">
                  <Badge variant="secondary" className="shrink-0 text-[10px]">
                    {t(b.kind === "auto" ? "settings.backupKindAuto" : "settings.backupKindManual")}
                  </Badge>
                  <span className="min-w-0 flex-1 truncate font-mono text-xs">{b.name}</span>
                  <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{formatBytes(b.size)}</span>
                  <span className="hidden shrink-0 text-xs tabular-nums text-muted-foreground md:inline">
                    {formatTime(b.created_at)}
                  </span>
                  <a href={downloadHref(b.name)} download>
                    <Button variant="ghost" size="icon" className="size-8" title={t("settings.backupDownload")}>
                      <Download className="size-3.5" />
                    </Button>
                  </a>
                  <Button
                    variant="ghost" size="icon" className="size-8 text-destructive"
                    onClick={() => setDeleting(b)}
                    title={t("app.actions.delete")}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* 恢复说明 */}
        <div className="flex gap-2 rounded-lg bg-muted/50 px-3 py-2.5 text-xs text-muted-foreground">
          <Info className="mt-0.5 size-3.5 shrink-0" />
          <span>{t("settings.backupRestoreHint", { dir: backups?.dir ?? "~/.sling/backups" })}</span>
        </div>
      </CardContent>

      <AlertDialog open={deleting !== null} onOpenChange={(v) => !v && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("common.confirmDelete")}</AlertDialogTitle>
            <AlertDialogDescription>{deleting?.name}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.close")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (deleting) {
                  api.deleteBackup(deleting.name)
                    .then(() => qc.invalidateQueries({ queryKey: ["backups"] }))
                    .catch((e) => toast.error(e.message))
                    .finally(() => setDeleting(null))
                }
              }}
            >
              {t("app.actions.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  )
}
