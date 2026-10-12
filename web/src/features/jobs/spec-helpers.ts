// spec-helpers.ts — 前端镜像 server 端 spec.go 的语义（EffectiveTarget 等），
// 用于向导的占位符、冲突检测与本地 YAML 预览，保证“所见即所跑”。

export function hasWildcard(s: string): boolean {
  return /[*?]/.test(s)
}

export function isQuerySrc(s: string): boolean {
  const t = s.trim()
  return /^sql:/i.test(t) || /^select /i.test(t)
}

/**
 * 解析流的目标表名：显式值优先；表流默认与源同名（返回源全名占位）；
 * 通配符表流返回 sling 变量模板；查询流 / 具体文件路径必须显式指定（返回 ""）。
 * 与 server 端 EffectiveTarget 保持一致。
 */
export function effectiveTarget(source: string, target?: string): string {
  if (target) return target
  if (isQuerySrc(source)) return ""
  if (hasWildcard(source)) {
    return source.includes("/") ? "{stream_file_path}" : "{stream_schema}.{stream_table}"
  }
  if (source.includes("/")) return ""
  return source
}

export interface MappingIssue {
  source: string
  reason: "conflict" | "required"
}

/** 目标映射校验：非通配流的目标名冲突、必须显式却为空的流。 */
export function mappingIssues(spec: {
  streams?: { source_object?: string; target_object?: string; disabled?: boolean }[]
}): MappingIssue[] {
  const issues: MappingIssue[] = []
  const seen = new Map<string, string>() // lowercased target -> first source
  for (const s of spec.streams ?? []) {
    if (s.disabled) continue
    const src = s.source_object ?? ""
    if (!src) continue
    const eff = effectiveTarget(src, s.target_object)
    if (!eff) {
      issues.push({ source: src, reason: "required" })
      continue
    }
    if (hasWildcard(src)) continue // 展开期才能确定，静态不判冲突
    const key = eff.toLowerCase()
    if (seen.has(key)) {
      issues.push({ source: src, reason: "conflict" })
    } else {
      seen.set(key, src)
    }
  }
  return issues
}

/** 源对象拆出 schema 与表名（按最后一个点分隔，无 schema 时返回空）。 */
export function splitObject(obj: string): { schema: string; table: string } {
  const idx = obj.lastIndexOf(".")
  if (idx <= 0) return { schema: "", table: obj }
  return { schema: obj.slice(0, idx), table: obj.slice(idx + 1) }
}
