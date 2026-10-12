// 设置·保留策略:运行记录/通知/审计保留天数(0 = 永久保留)
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

export function RetentionCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data: settings } = useQuery({ queryKey: ["settings"], queryFn: api.getSettings })

  const [runs, setRuns] = useState("")
  const [notifications, setNotifications] = useState("")
  const [audit, setAudit] = useState("")

  useEffect(() => {
    if (!settings) return
    setRuns(settings["retention_days"] ?? "90")
    setNotifications(settings["retention.notifications_days"] ?? "30")
    setAudit(settings["retention.audit_days"] ?? "365")
  }, [settings])

  const save = useMutation({
    mutationFn: () =>
      api.putSettings({
        retention_days: runs,
        "retention.notifications_days": notifications,
        "retention.audit_days": audit,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["settings"] })
      toast.success(t("common.saved"))
    },
    onError: (e) => toast.error(e.message),
  })

  const num = (v: string) => v === "" || /^\d+$/.test(v)

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("settings.retention")}</CardTitle>
        <CardDescription>{t("settings.retentionDesc")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-4 sm:grid-cols-3">
          <div className="space-y-1.5">
            <Label>{t("settings.retentionRuns")}</Label>
            <Input
              type="number" min={0} value={runs}
              onChange={(e) => setRuns(e.target.value)}
              className="w-28" aria-invalid={!num(runs)}
            />
          </div>
          <div className="space-y-1.5">
            <Label>{t("settings.retentionNotifications")}</Label>
            <Input
              type="number" min={0} value={notifications}
              onChange={(e) => setNotifications(e.target.value)}
              className="w-28" aria-invalid={!num(notifications)}
            />
          </div>
          <div className="space-y-1.5">
            <Label>{t("settings.retentionAudit")}</Label>
            <Input
              type="number" min={0} value={audit}
              onChange={(e) => setAudit(e.target.value)}
              className="w-28" aria-invalid={!num(audit)}
            />
          </div>
        </div>
        <p className="text-xs text-muted-foreground">{t("settings.retentionHint")}</p>
        <Button size="sm" onClick={() => save.mutate()} disabled={save.isPending || !num(runs) || !num(notifications) || !num(audit)}>
          {t("app.actions.save")}
        </Button>
      </CardContent>
    </Card>
  )
}
