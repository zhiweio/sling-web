// Login: single-admin entry point. The session is an HttpOnly cookie issued
// by POST /api/auth/login; this page only collects credentials and surfaces
// errors inline (wrong password, lockout countdown).
import { useEffect, useState } from "react"
import { useLocation, useNavigate } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ArrowRightLeft, Database, Eye, EyeOff, Loader2, Play, Workflow } from "lucide-react"
import { api, ApiError } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

export function Login() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const location = useLocation()

  // already signed in? straight to the workbench
  const { data: session } = useQuery({ queryKey: ["session"], queryFn: () => api.session() })
  useEffect(() => {
    if (session?.authenticated) navigate("/", { replace: true })
  }, [session?.authenticated, navigate])

  const [username, setUsername] = useState("admin")
  const [password, setPassword] = useState("")
  const [showPw, setShowPw] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [lockSeconds, setLockSeconds] = useState(0)

  // lockout countdown; while it runs its message takes over the error line
  useEffect(() => {
    if (lockSeconds <= 0) return
    const id = setTimeout(() => setLockSeconds((s) => s - 1), 1000)
    return () => clearTimeout(id)
  }, [lockSeconds])
  const shownError = lockSeconds > 0 ? t("auth.lockedOut", { seconds: lockSeconds }) : error

  const login = useMutation({
    mutationFn: () => api.login(username.trim(), password),
    onSuccess: (data) => {
      qc.setQueryData(["session"], data)
      qc.invalidateQueries()
      const from = (location.state as { from?: string } | null)?.from
      navigate(from && from !== "/login" ? from : "/", { replace: true })
    },
    onError: (e) => {
      if (e instanceof ApiError && e.status === 429) {
        const s = Number(e.body.retry_after ?? 30)
        setLockSeconds(s)
        setError(null)
      } else if (e instanceof ApiError && e.status === 401) {
        setError(t("auth.invalidCredentials"))
      } else {
        setError(e.message)
      }
    },
  })

  return (
    <div className="grid min-h-[100dvh] bg-background md:grid-cols-2">
      {/* Brand panel — desktop only, mirrors the sidebar palette */}
      <aside className="relative hidden flex-col justify-between overflow-hidden bg-sidebar p-10 text-sidebar-foreground md:flex">
        <div
          className="pointer-events-none absolute inset-0"
          style={{ background: "radial-gradient(600px circle at 20% 0%, oklch(0.55 0.22 273 / 0.18), transparent 65%)" }}
        />
        <div className="relative flex items-center gap-2.5">
          <div className="flex size-7 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
            <ArrowRightLeft className="size-4" />
          </div>
          <span className="text-sm font-semibold tracking-tight">sling-server</span>
        </div>

        <div className="relative max-w-md">
          <h2 className="text-3xl font-bold tracking-tight">{t("app.title")}</h2>
          <p className="mt-3 text-sm leading-relaxed text-sidebar-foreground/60">{t("auth.tagline")}</p>
          <ul className="mt-10 space-y-5">
            {[
              { icon: Database, label: t("auth.pointConnections") },
              { icon: Workflow, label: t("auth.pointPipelines") },
              { icon: Play, label: t("auth.pointMonitor") },
            ].map(({ icon: Icon, label }) => (
              <li key={label} className="flex items-center gap-3 text-sm text-sidebar-foreground/70">
                <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-sidebar-accent/60">
                  <Icon className="size-4" />
                </span>
                {label}
              </li>
            ))}
          </ul>
        </div>

        <p className="relative text-xs text-sidebar-foreground/40">{t("auth.panelFooter")}</p>
      </aside>

      {/* Form */}
      <main className="flex items-center justify-center px-6 py-12">
        <div className="w-full max-w-sm">
          <div className="mb-8 flex items-center gap-2.5 md:hidden">
            <div className="flex size-7 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
              <ArrowRightLeft className="size-4" />
            </div>
            <span className="text-sm font-semibold tracking-tight">sling-server</span>
          </div>

          <h1 className="text-2xl font-bold tracking-tight">{t("auth.signIn")}</h1>
          <p className="mt-1.5 text-sm text-muted-foreground">{t("auth.signInSub")}</p>

          <form
            className="mt-8 space-y-5"
            onSubmit={(e) => {
              e.preventDefault()
              if (!login.isPending && lockSeconds <= 0) {
                setError(null)
                login.mutate()
              }
            }}
          >
            <div className="space-y-2">
              <Label htmlFor="login-username">{t("auth.username")}</Label>
              <Input
                id="login-username"
                autoFocus
                autoComplete="username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                aria-invalid={!!shownError}
                aria-describedby={shownError ? "login-error" : undefined}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="login-password">{t("auth.password")}</Label>
              <div className="relative">
                <Input
                  id="login-password"
                  type={showPw ? "text" : "password"}
                  autoComplete="current-password"
                  className="pr-10"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  aria-invalid={!!shownError}
                  aria-describedby={shownError ? "login-error" : undefined}
                />
                <button
                  type="button"
                  onClick={() => setShowPw((v) => !v)}
                  className="absolute inset-y-0 right-0 flex w-10 items-center justify-center text-muted-foreground transition-colors hover:text-foreground"
                  aria-label={showPw ? t("auth.hidePassword") : t("auth.showPassword")}
                  tabIndex={-1}
                >
                  {showPw ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                </button>
              </div>
            </div>

            {shownError && (
              <p id="login-error" role="alert" className="text-sm text-destructive">
                {shownError}
              </p>
            )}

            <Button type="submit" className="w-full" disabled={login.isPending || lockSeconds > 0 || !username.trim() || !password}>
              {login.isPending && <Loader2 className="size-4 animate-spin" aria-hidden />}
              {login.isPending ? t("auth.signingIn") : t("auth.signIn")}
            </Button>
          </form>
        </div>
      </main>
    </div>
  )
}
