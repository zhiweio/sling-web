// src/lib/sse.ts — EventSource subscription hook (auto-reconnect + per-run filtering)
import { useEffect, useRef } from "react"
import type { RunSnapshot } from "./types"

export interface SseHandlers {
  onExecution?: (snap: RunSnapshot) => void
  onLog?: (runId: string, line: { level: number; text: string }) => void
  onNotification?: (n: unknown) => void
  onJob?: (j: unknown) => void
}

export function useSSE(handlers: SseHandlers) {
  const ref = useRef(handlers)
  ref.current = handlers

  useEffect(() => {
    const es = new EventSource("/api/events")

    const wrap = <T,>(cb?: (v: T) => void) => (ev: MessageEvent) => {
      if (!cb) return
      try {
        cb(JSON.parse(ev.data))
      } catch {
        /* ignore malformed */
      }
    }

    const onExecution = wrap<{ type: string; run_id: string; payload: RunSnapshot }>((m) =>
      ref.current.onExecution?.(m.payload),
    )
    const onLog = wrap<{ type: string; run_id: string; payload: { level: number; text: string } }>((m) =>
      ref.current.onLog?.(m.run_id, m.payload),
    )
    const onNotification = wrap((m) => ref.current.onNotification?.(m))
    const onJob = wrap((m) => ref.current.onJob?.(m))

    es.addEventListener("execution", onExecution as EventListener)
    es.addEventListener("log", onLog as EventListener)
    es.addEventListener("notification", onNotification as EventListener)
    es.addEventListener("job", onJob as EventListener)

    return () => {
      es.close()
    }
  }, [])
}
