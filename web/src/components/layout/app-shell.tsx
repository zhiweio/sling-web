// 深色侧栏 + 内容区布局（Linear 风格）
import { useEffect, useState } from "react"
import { NavLink, Link, useLocation } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import {
  Activity,
  ArrowRightLeft,
  Bell,
  Database,
  LayoutDashboard,
  Menu,
  Moon,
  ScrollText,
  Settings as SettingsIcon,
  Sun,
  X,
} from "lucide-react"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useUi } from "@/store/ui"
import { Button } from "@/components/ui/button"
import { relativeTime } from "@/lib/format"

export function AppShell({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation()
  const location = useLocation()
  const [mobileOpen, setMobileOpen] = useState(false)
  const [notifOpen, setNotifOpen] = useState(false)

  useEffect(() => setMobileOpen(false), [location.pathname])

  const nav = [
    { to: "/", label: t("app.nav.dashboard"), icon: LayoutDashboard, end: true },
    { to: "/connections", label: t("app.nav.connections"), icon: Database },
    { to: "/jobs", label: t("app.nav.jobs"), icon: ArrowRightLeft },
    { to: "/runs", label: t("app.nav.runs"), icon: Activity },
    { to: "/notifications", label: t("app.nav.notifications"), icon: Bell },
    { to: "/audit", label: t("app.nav.audit"), icon: ScrollText },
    { to: "/settings", label: t("app.nav.settings"), icon: SettingsIcon },
  ]

  return (
    <div className="flex h-screen overflow-hidden bg-background">
      {/* 桌面侧栏 */}
      <aside className="hidden w-60 shrink-0 flex-col bg-sidebar text-sidebar-foreground md:flex">
        <SidebarContent nav={nav} />
      </aside>

      {/* 移动端抽屉 */}
      {mobileOpen && (
        <div className="fixed inset-0 z-50 md:hidden">
          <div className="absolute inset-0 bg-black/50" onClick={() => setMobileOpen(false)} />
          <aside className="absolute inset-y-0 left-0 flex w-64 flex-col bg-sidebar text-sidebar-foreground">
            <SidebarContent nav={nav} onClose={() => setMobileOpen(false)} />
          </aside>
        </div>
      )}

      <div className="flex min-w-0 flex-1 flex-col">
        {/* 顶栏 */}
        <header className="flex h-14 shrink-0 items-center gap-2 border-b bg-background px-4">
          <Button variant="ghost" size="icon" className="md:hidden" onClick={() => setMobileOpen(true)}>
            <Menu className="size-4" />
          </Button>
          <div className="flex-1" />
          <NotificationBell open={notifOpen} onOpenChange={setNotifOpen} />
          <ThemeToggle />
        </header>

        <main className="scrollbar-thin min-h-0 flex-1 overflow-y-auto">
          <div className="mx-auto w-full max-w-6xl px-4 py-6 md:px-8">{children}</div>
        </main>
      </div>
    </div>
  )
}

function SidebarContent({
  nav,
  onClose,
}: {
  nav: { to: string; label: string; icon: React.ComponentType<{ className?: string }>; end?: boolean }[]
  onClose?: () => void
}) {
  const { t } = useTranslation()
  const { data: notif } = useQuery({
    queryKey: ["notifications", "unread-count"],
    queryFn: () => api.notifications({ unread: true }),
    refetchInterval: 30_000,
  })
  const activeRuns = useActiveRunsCount()

  return (
    <>
      <div className="flex h-14 items-center gap-2 px-5">
        <div className="flex size-7 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
          <ArrowRightLeft className="size-4" />
        </div>
        <span className="text-sm font-semibold tracking-tight">{t("app.title")}</span>
        {onClose && (
          <Button variant="ghost" size="icon" className="ml-auto" onClick={onClose}>
            <X className="size-4" />
          </Button>
        )}
      </div>

      <nav className="flex-1 space-y-1 px-3 py-2">
        {nav.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.end}
            className={({ isActive }) =>
              cn(
                "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                isActive
                  ? "bg-sidebar-accent text-sidebar-accent-foreground"
                  : "text-sidebar-foreground/70 hover:bg-sidebar-accent/60 hover:text-sidebar-foreground",
              )
            }
          >
            <item.icon className="size-4 shrink-0" />
            {item.label}
            {item.to === "/runs" && activeRuns > 0 && (
              <span className="ml-auto flex items-center gap-1 text-xs text-primary">
                <span className="relative flex size-2">
                  <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-primary opacity-60" />
                  <span className="relative inline-flex size-2 rounded-full bg-primary" />
                </span>
                {activeRuns}
              </span>
            )}
            {item.to === "/notifications" && (notif?.unread ?? 0) > 0 && (
              <span className="ml-auto rounded-full bg-destructive px-1.5 text-[10px] font-bold text-destructive-foreground">
                {notif!.unread > 99 ? "99+" : notif!.unread}
              </span>
            )}
          </NavLink>
        ))}
      </nav>

      <div className="border-t border-sidebar-border px-5 py-3 text-xs text-sidebar-foreground/50">
        <div className="flex items-center gap-2">
          <span className="size-1.5 rounded-full bg-emerald-400" />
          {notif?.unread
            ? t("app.unreadCount", { count: notif.unread })
            : t("app.serverRunning")}
        </div>
      </div>
    </>
  )
}

function useActiveRunsCount() {
  const { data } = useQuery({
    queryKey: ["runs", "active-count"],
    queryFn: () => api.listRuns("limit=1"),
    refetchInterval: 5_000,
  })
  return data?.active ?? 0
}

function ThemeToggle() {
  const { theme, setTheme } = useUi()
  const next = theme === "dark" ? "light" : "dark"
  return (
    <Button variant="ghost" size="icon" onClick={() => setTheme(next)} title="theme">
      {theme === "dark" ? <Sun className="size-4" /> : <Moon className="size-4" />}
    </Button>
  )
}

function NotificationBell({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({
    queryKey: ["notifications"],
    queryFn: () => api.notifications({ limit: 20 }),
    refetchInterval: 30_000,
    enabled: open,
  })
  const unread = data?.unread ?? 0

  return (
    <div className="relative">
      <Button variant="ghost" size="icon" onClick={() => onOpenChange(!open)} title={t("app.nav.notifications")}>
        <Bell className="size-4" />
        {unread > 0 && (
          <span className="absolute right-1 top-1 flex min-w-4 items-center justify-center rounded-full bg-destructive px-1 text-[10px] font-bold text-destructive-foreground">
            {unread > 9 ? "9+" : unread}
          </span>
        )}
      </Button>
      {open && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => onOpenChange(false)} />
          <div className="absolute right-0 top-11 z-50 w-96 rounded-xl border bg-popover p-2 text-popover-foreground shadow-xl">
            <div className="flex items-center justify-between px-2 py-1">
              <span className="text-sm font-semibold">{t("app.nav.notifications")}</span>
              <Button
                variant="ghost"
                size="sm"
                className="h-7 text-xs"
                onClick={async () => {
                  await api.markRead()
                  qc.invalidateQueries({ queryKey: ["notifications"] })
                }}
              >
                {t("notify.markAllRead")}
              </Button>
            </div>
            <div className="scrollbar-thin max-h-96 overflow-y-auto">
              {(data?.notifications ?? []).length === 0 && (
                <div className="px-3 py-8 text-center text-sm text-muted-foreground">{t("notify.emptyTitle")}</div>
              )}
              {(data?.notifications ?? []).map((n) => (
                <div
                  key={n.id}
                  className={cn(
                    "flex gap-2 rounded-lg px-3 py-2 text-sm",
                    !n.read && "bg-primary/5",
                  )}
                >
                  <span
                    className={cn(
                      "mt-1.5 size-2 shrink-0 rounded-full",
                      n.level === "error"
                        ? "bg-destructive"
                        : n.level === "warning"
                          ? "bg-amber-500"
                          : "bg-emerald-500",
                    )}
                  />
                  <div className="min-w-0 flex-1">
                    <div className="truncate font-medium">{n.title}</div>
                    {n.body && <div className="line-clamp-2 text-xs text-muted-foreground">{n.body}</div>}
                    <div className="mt-0.5 text-[11px] text-muted-foreground">{relativeTime(n.created_at)}</div>
                  </div>
                </div>
              ))}
            </div>
            <div className="border-t px-2 py-1.5 text-center">
              <Link
                to="/notifications"
                className="text-xs text-primary hover:underline"
                onClick={() => onOpenChange(false)}
              >
                {t("notify.viewAll")}
              </Link>
            </div>
          </div>
        </>
      )}
    </div>
  )
}
