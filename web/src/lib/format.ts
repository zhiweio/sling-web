// src/lib/format.ts — human-friendly formatting helpers
import i18n from "@/i18n"

export function formatBytes(bytes?: number): string {
  if (!bytes || bytes === 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const v = bytes / Math.pow(1024, i)
  return `${v >= 100 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

export function formatNumber(n?: number): string {
  if (n === undefined || n === null) return "0"
  return n.toLocaleString()
}

export function formatDuration(ms?: number): string {
  if (!ms || ms < 0) return "-"
  if (ms < 1000) return `${Math.round(ms)}ms`
  const s = ms / 1000
  if (s < 60) return `${s.toFixed(1)}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${Math.round(s % 60)}s`
  const h = Math.floor(m / 60)
  return `${h}h ${m % 60}m`
}

export function formatRate(rate?: number): string {
  if (!rate) return "-"
  if (rate >= 10000) return `${(rate / 1000).toFixed(1)}k rows/s`
  return `${Math.round(rate)} rows/s`
}

export function relativeTime(iso?: string | null): string {
  if (!iso) return "-"
  const d = new Date(iso)
  const diff = Date.now() - d.getTime()
  const sec = Math.round(diff / 1000)
  if (sec < 5) return i18n.t("common.relTime.now")
  if (sec < 60) return i18n.t("common.relTime.secondsAgo", { n: sec })
  const min = Math.round(sec / 60)
  if (min < 60) return i18n.t("common.relTime.minutesAgo", { n: min })
  const hr = Math.round(min / 60)
  if (hr < 24) return i18n.t("common.relTime.hoursAgo", { n: hr })
  const day = Math.round(hr / 24)
  if (day < 30) return i18n.t("common.relTime.daysAgo", { n: day })
  return d.toLocaleDateString()
}

export function formatTime(iso?: string | null): string {
  if (!iso) return "-"
  const d = new Date(iso)
  return d.toLocaleString(undefined, { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit" })
}

export function countdown(iso?: string | null): string {
  if (!iso) return "-"
  const diff = new Date(iso).getTime() - Date.now()
  if (diff <= 0) return i18n.t("common.countdown.soon")
  const min = Math.floor(diff / 60000)
  if (min < 1) return i18n.t("common.countdown.seconds", { n: Math.round(diff / 1000) })
  if (min < 60) return i18n.t("common.countdown.minutes", { n: min })
  const hr = Math.floor(min / 60)
  if (hr < 24) return i18n.t("common.countdown.hoursMinutes", { h: hr, m: min % 60 })
  return i18n.t("common.countdown.days", { n: Math.floor(hr / 24) })
}

/** cron expression → human-readable text (covers common presets) */
export function cronToHuman(cron?: string): string {
  if (!cron) return i18n.t("common.cron.manual")
  const parts = cron.trim().split(/\s+/)
  if (parts.length !== 5) return cron
  const [min, hour, dom, _mon, dow] = parts

  if (min.startsWith("*/")) {
    return i18n.t("common.cron.everyNMinutes", { n: min.slice(2) })
  }
  if (hour.startsWith("*/")) {
    return i18n.t("common.cron.everyNHours", { n: hour.slice(2) })
  }
  if (hour === "*" && (min === "0" || min === "*")) return i18n.t("common.cron.everyMinute")
  if (dom === "*" && dow === "*" && hour !== "*") {
    return i18n.t("common.cron.daily", { time: `${hour.padStart(2, "0")}:${min.padStart(2, "0")}` })
  }
  if (dow !== "*" && dom === "*") {
    const dayKey = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"][Number(dow)]
    const dayName = dayKey ? i18n.t(`common.cron.${dayKey}`) : dow
    return i18n.t("common.cron.weekdayAt", { day: dayName, time: `${hour.padStart(2, "0")}:${min.padStart(2, "0")}` })
  }
  return cron
}
