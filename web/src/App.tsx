import { lazy, Suspense, useEffect } from "react"
import { Navigate, Outlet, Route, Routes, useLocation } from "react-router-dom"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { AppShell } from "@/components/layout/app-shell"
import { api } from "@/lib/api"
import { useSSE } from "@/lib/sse"
import type { RunSnapshot } from "@/lib/types"
import i18n from "@/i18n"
import { Toaster } from "@/components/ui/sonner"
import { Login } from "@/features/auth/login"
import { ChangePassword } from "@/features/auth/change-password"

const Dashboard = lazy(() => import("@/features/dashboard"))
const Connections = lazy(() => import("@/features/connections"))
const Jobs = lazy(() => import("@/features/jobs"))
const JobEditor = lazy(() => import("@/features/jobs/editor"))
const Runs = lazy(() => import("@/features/runs"))
const RunDetail = lazy(() => import("@/features/runs/detail"))
const Notifications = lazy(() => import("@/features/notifications"))
const Audit = lazy(() => import("@/features/audit"))
const Settings = lazy(() => import("@/features/settings"))

export function Loading() {
  return (
    <div className="flex h-[50vh] items-center justify-center text-muted-foreground">
      <div className="size-6 animate-spin rounded-full border-2 border-primary border-t-transparent" />
    </div>
  )
}

/** Global SSE bridge: run events refresh query caches in real time */
function SseBridge() {
  const qc = useQueryClient()
  const location = useLocation()

  useSSE({
    onExecution: (snap: RunSnapshot) => {
      qc.setQueriesData<{ runs: (RunSnapshot & { id?: string })[] }>({ queryKey: ["runs"] }, (old) =>
        old
          ? {
              ...old,
              runs: old.runs.some((r) => r.run_id === snap.run_id)
                ? old.runs.map((r) => (r.run_id === snap.run_id ? snap : r))
                : [snap, ...old.runs].slice(0, 200),
            }
          : old,
      )
      // local cache for the running run's detail page
      qc.setQueryData(["run", snap.run_id], snap)
      if (snap.status === "running") {
        document.title = i18n.t("app.docTitleRunning", { name: snap.job_name ?? i18n.t("app.defaultJobName") })
      } else {
        document.title = i18n.t("app.docTitle")
        qc.invalidateQueries({ queryKey: ["runs"] })
        qc.invalidateQueries({ queryKey: ["jobs"] })
        qc.invalidateQueries({ queryKey: ["stats"] })
      }
    },
    onNotification: () => qc.invalidateQueries({ queryKey: ["notifications"] }),
    onJob: () => {
      qc.invalidateQueries({ queryKey: ["jobs"] })
      // editor detail queries carry the live run decoration too
      qc.invalidateQueries({ queryKey: ["job"] })
      qc.invalidateQueries({ queryKey: ["stats"] })
    },
    onError: () => qc.invalidateQueries({ queryKey: ["session"] }),
  })

  useEffect(() => {
    // restore the base title on page change (the detail page may have set a custom one)
    return () => {
      document.title = i18n.t("app.docTitle")
    }
  }, [location.pathname])

  return null
}

/** Any 401 from the API client re-probes the session; the guard below reacts */
function UnauthorizedBridge() {
  const qc = useQueryClient()
  useEffect(() => {
    const handler = () => qc.invalidateQueries({ queryKey: ["session"] })
    window.addEventListener("sling:unauthorized", handler)
    return () => window.removeEventListener("sling:unauthorized", handler)
  }, [qc])
  return null
}

/** Gate every workbench route behind the session probe */
function RequireAuth() {
  const location = useLocation()
  const { data, isPending } = useQuery({
    queryKey: ["session"],
    queryFn: () => api.session(),
    staleTime: 60_000,
  })

  if (isPending) return <Loading />
  if (!data?.authenticated) return <Navigate to="/login" replace state={{ from: location.pathname }} />
  // a session that still carries the default password must change it first
  if (data.must_change_password && location.pathname !== "/change-password") {
    return <Navigate to="/change-password" replace />
  }
  return <Outlet />
}

/** Authenticated routes inside the workbench shell */
function ShellLayout() {
  return (
    <AppShell>
      <SseBridge />
      <Suspense fallback={<Loading />}>
        <Outlet />
      </Suspense>
    </AppShell>
  )
}

export default function App() {
  return (
    <>
      <UnauthorizedBridge />
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route element={<RequireAuth />}>
          <Route path="/change-password" element={<ChangePassword />} />
          <Route element={<ShellLayout />}>
            <Route path="/" element={<Dashboard />} />
            <Route path="/connections" element={<Connections />} />
            <Route path="/connections/:name" element={<Connections />} />
            <Route path="/jobs" element={<Jobs />} />
            <Route path="/jobs/new" element={<JobEditor />} />
            <Route path="/jobs/:id/edit" element={<JobEditor />} />
            <Route path="/jobs/:id" element={<JobEditor />} />
            <Route path="/runs" element={<Runs />} />
            <Route path="/runs/:id" element={<RunDetail />} />
            <Route path="/notifications" element={<Notifications />} />
            <Route path="/audit" element={<Audit />} />
            <Route path="/settings" element={<Settings />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Route>
      </Routes>
      <Toaster position="top-center" richColors />
    </>
  )
}
