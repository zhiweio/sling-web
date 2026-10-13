// ChangePassword: forced at first login (default credentials) and reachable
// from the user menu / settings. On success the session's must-change flag
// clears and the router returns to the workbench.
import { useEffect, useState } from "react"
import { useNavigate } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { ArrowRightLeft, Loader2 } from "lucide-react"
import { toast } from "sonner"
import { api } from "@/lib/api"
import i18n from "@/i18n"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

export function ChangePassword() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const navigate = useNavigate()

  useEffect(() => {
    document.title = i18n.t("app.docTitle")
  }, [])

  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [confirm, setConfirm] = useState("")

  const localError =
    next.length > 0 && next.length < 8
      ? t("auth.passwordTooShort")
      : confirm.length > 0 && confirm !== next
        ? t("auth.passwordMismatch")
        : null

  const change = useMutation({
    mutationFn: () => api.changePassword(current, next),
    onSuccess: () => {
      // clear the must-change flag; also refresh everything for the new session
      qc.setQueryData(["session"], (old: { authenticated: boolean; username?: string } | undefined) =>
        old ? { ...old, must_change_password: false } : old,
      )
      qc.invalidateQueries()
      toast.success(t("auth.passwordChanged"))
      navigate("/", { replace: true })
    },
    onError: (e) => toast.error(t("auth.passwordChangeFailed"), { description: e.message }),
  })

  const canSubmit = current.length > 0 && next.length >= 8 && confirm === next && !change.isPending

  return (
    <div className="flex min-h-[100dvh] items-center justify-center bg-background px-6 py-12">
      <div className="w-full max-w-sm">
        <div className="mb-8 flex items-center gap-2.5">
          <div className="flex size-7 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
            <ArrowRightLeft className="size-4" />
          </div>
          <span className="text-sm font-semibold tracking-tight">sling-server</span>
        </div>

        <h1 className="text-2xl font-bold tracking-tight">{t("auth.changePassword")}</h1>
        <p className="mt-1.5 text-sm text-muted-foreground">{t("auth.changePasswordSub")}</p>

        <p className="mt-4 rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2.5 text-sm text-amber-700 dark:text-amber-400">
          {t("auth.mustChangeNotice")}
        </p>

        <form
          className="mt-6 space-y-5"
          onSubmit={(e) => {
            e.preventDefault()
            if (canSubmit) change.mutate()
          }}
        >
          <div className="space-y-2">
            <Label htmlFor="pw-current">{t("auth.currentPassword")}</Label>
            <Input
              id="pw-current"
              type="password"
              autoComplete="current-password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="pw-new">{t("auth.newPassword")}</Label>
            <Input
              id="pw-new"
              type="password"
              autoComplete="new-password"
              value={next}
              onChange={(e) => setNext(e.target.value)}
              aria-invalid={!!localError && next.length > 0 && next.length < 8}
              aria-describedby={localError ? "pw-error" : undefined}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="pw-confirm">{t("auth.confirmPassword")}</Label>
            <Input
              id="pw-confirm"
              type="password"
              autoComplete="new-password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              aria-invalid={!!localError}
              aria-describedby={localError ? "pw-error" : undefined}
            />
          </div>

          {localError && (
            <p id="pw-error" role="alert" className="text-sm text-destructive">
              {localError}
            </p>
          )}

          <Button type="submit" className="w-full" disabled={!canSubmit}>
            {change.isPending && <Loader2 className="size-4 animate-spin" aria-hidden />}
            {change.isPending ? t("auth.changing") : t("auth.changePassword")}
          </Button>
        </form>
      </div>
    </div>
  )
}
