// Settings: account / language / theme / notifications / retention / backup / about
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { useNavigate } from "react-router-dom"
import { useQuery } from "@tanstack/react-query"
import { HardDrive, KeyRound } from "lucide-react"
import { api } from "@/lib/api"
import { setLang } from "@/i18n"
import { PageHeader } from "@/features/dashboard"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useUi } from "@/store/ui"
import { NotifyCard } from "./notify-card"
import { RetentionCard } from "./retention-card"
import { BackupCard } from "./backup-card"

export default function Settings() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const { theme, setTheme } = useUi()
  const [lang, setLangState] = useState(i18n.language)
  const { data: version } = useQuery({ queryKey: ["version"], queryFn: api.version })
  const { data: session } = useQuery({ queryKey: ["session"], queryFn: () => api.session() })

  useEffect(() => {
    document.title = i18n.t("app.docTitle")
  }, [])

  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <PageHeader title={t("settings.title")} subtitle={t("settings.subtitle")} />

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("settings.account")}</CardTitle>
        </CardHeader>
        <CardContent className="flex items-center justify-between gap-4">
          <div className="flex min-w-0 items-center gap-3">
            <span className="flex size-9 shrink-0 items-center justify-center rounded-full bg-primary/15 text-sm font-semibold text-primary">
              {(session?.username ?? "?")[0].toUpperCase()}
            </span>
            <div className="min-w-0">
              <div className="truncate text-sm font-medium">{session?.username ?? "-"}</div>
              <div className="text-xs text-muted-foreground">{t("settings.accountRole")}</div>
            </div>
          </div>
          <Button variant="outline" size="sm" onClick={() => navigate("/change-password")}>
            <KeyRound className="size-4" />
            {t("auth.changePassword")}
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("settings.language")}</CardTitle>
        </CardHeader>
        <CardContent>
          <Select
            value={lang}
            onValueChange={(v) => {
              setLangState(v)
              setLang(v as "zh-CN" | "en")
            }}
          >
            <SelectTrigger className="w-48"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="zh-CN">简体中文</SelectItem>
              <SelectItem value="en">English</SelectItem>
            </SelectContent>
          </Select>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("settings.theme")}</CardTitle>
        </CardHeader>
        <CardContent>
          <Select value={theme} onValueChange={(v) => setTheme(v as "light" | "dark" | "system")}>
            <SelectTrigger className="w-48"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="light">{t("settings.themeLight")}</SelectItem>
              <SelectItem value="dark">{t("settings.themeDark")}</SelectItem>
              <SelectItem value="system">{t("settings.themeSystem")}</SelectItem>
            </SelectContent>
          </Select>
        </CardContent>
      </Card>

      <NotifyCard />

      <RetentionCard />

      <BackupCard />

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("settings.about")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 text-sm text-muted-foreground">
          <div className="flex justify-between">
            <span>{t("settings.version")}</span>
            <span className="font-mono">{version?.version} · core {version?.core}</span>
          </div>
          <div className="flex justify-between">
            <span>{t("settings.dataDir")}</span>
            <span className="font-mono text-xs">{version?.data_dir}</span>
          </div>
          <div className="flex items-center justify-between">
            <span>{t("settings.engine")}</span>
            <span className="flex items-center gap-1.5 font-mono text-xs">
              <HardDrive className="size-3.5" />
              {version?.go}
            </span>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
