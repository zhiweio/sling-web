// 设置：语言/主题/通知/保留策略/备份/关于
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { useQuery } from "@tanstack/react-query"
import { HardDrive } from "lucide-react"
import { api } from "@/lib/api"
import { setLang } from "@/i18n"
import { PageHeader } from "@/features/dashboard"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useUi } from "@/store/ui"
import { NotifyCard } from "./notify-card"
import { RetentionCard } from "./retention-card"
import { BackupCard } from "./backup-card"

export default function Settings() {
  const { t, i18n } = useTranslation()
  const { theme, setTheme } = useUi()
  const [lang, setLangState] = useState(i18n.language)
  const { data: version } = useQuery({ queryKey: ["version"], queryFn: api.version })

  useEffect(() => {
    document.title = "sling 数据迁移工作台"
  }, [])

  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <PageHeader title={t("settings.title")} subtitle={t("settings.subtitle")} />

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
