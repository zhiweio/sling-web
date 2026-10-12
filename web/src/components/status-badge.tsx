// 状态徽章 — 统一运行状态视觉语言
import { useTranslation } from "react-i18next"
import { cn } from "@/lib/utils"
import type { ExecStatus } from "@/lib/types"

const styles: Record<string, string> = {
  created: "bg-muted text-muted-foreground",
  queued: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
  running: "bg-indigo-500/10 text-indigo-600 dark:text-indigo-400",
  success: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
  warning: "bg-amber-500/10 text-amber-600 dark:text-amber-500",
  error: "bg-destructive/10 text-destructive",
  cancelled: "bg-muted text-muted-foreground",
  terminated: "bg-muted text-muted-foreground",
  interrupted: "bg-amber-500/10 text-amber-600",
  skipped: "bg-muted text-muted-foreground",
}

const dots: Record<string, string> = {
  running: "bg-indigo-500",
  queued: "bg-blue-500",
  success: "bg-emerald-500",
  warning: "bg-amber-500",
  error: "bg-destructive",
}

export function StatusBadge({ status, className }: { status: ExecStatus | string; className?: string }) {
  const { t } = useTranslation()
  const key = status in styles ? status : "created"
  const live = status === "running" || status === "queued"
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap",
        styles[key],
        className,
      )}
    >
      {live && (
        <span className="relative flex size-1.5">
          {status === "running" && (
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-indigo-500 opacity-60" />
          )}
          <span className={cn("relative inline-flex size-1.5 rounded-full", dots[key] ?? "bg-muted-foreground")} />
        </span>
      )}
      {t(`status.${key}`)}
    </span>
  )
}
