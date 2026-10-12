// src/lib/format.ts — 人性化格式化工具
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
  if (sec < 5) return "刚刚"
  if (sec < 60) return `${sec} 秒前`
  const min = Math.round(sec / 60)
  if (min < 60) return `${min} 分钟前`
  const hr = Math.round(min / 60)
  if (hr < 24) return `${hr} 小时前`
  const day = Math.round(hr / 24)
  if (day < 30) return `${day} 天前`
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
  if (diff <= 0) return "即将运行"
  const min = Math.floor(diff / 60000)
  if (min < 1) return `${Math.round(diff / 1000)} 秒后`
  if (min < 60) return `${min} 分钟后`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr} 小时 ${min % 60} 分后`
  return `${Math.floor(hr / 24)} 天后`
}

/** cron 表达式 → 人类可读中文（覆盖常用预设） */
export function cronToHuman(cron?: string): string {
  if (!cron) return "手动"
  const parts = cron.trim().split(/\s+/)
  if (parts.length !== 5) return cron
  const [min, hour, dom, _mon, dow] = parts

  if (min.startsWith("*/")) {
    const n = min.slice(2)
    return `每 ${n} 分钟`
  }
  if (hour.startsWith("*/")) {
    const n = hour.slice(2)
    return `每 ${n} 小时`
  }
  if (hour === "*" && (min === "0" || min === "*")) return "每分钟"
  if (dom === "*" && dow === "*" && hour !== "*") {
    return `每天 ${hour.padStart(2, "0")}:${min.padStart(2, "0")}`
  }
  if (dow !== "*" && dom === "*") {
    const days = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"]
    const dayName = days[Number(dow)] ?? dow
    return `${dayName} ${hour.padStart(2, "0")}:${min.padStart(2, "0")}`
  }
  return cron
}
