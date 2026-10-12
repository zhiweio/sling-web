// spec-helpers.ts — frontend mirror of the server-side spec.go semantics (EffectiveTarget etc.),
// used for wizard placeholders, conflict detection and the local YAML preview, so what you see is what runs.

export function hasWildcard(s: string): boolean {
  return /[*?]/.test(s)
}

export function isQuerySrc(s: string): boolean {
  const t = s.trim()
  return /^sql:/i.test(t) || /^select /i.test(t)
}

/**
 * Resolve a stream's target table name: explicit value wins; table streams default to
 * the source name (returns the full source name as the placeholder); wildcard table
 * streams return a sling variable template; query streams / concrete file paths must
 * be explicit (returns ""). Consistent with the server-side EffectiveTarget.
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

/** Target mapping validation: duplicate target names for non-wildcard streams, and streams that must be explicit but are empty. */
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
    if (hasWildcard(src)) continue // resolved at expansion time; no static conflict check
    const key = eff.toLowerCase()
    if (seen.has(key)) {
      issues.push({ source: src, reason: "conflict" })
    } else {
      seen.set(key, src)
    }
  }
  return issues
}

/** Split a source object into schema and table (by the last dot; empty schema when there is none). */
export function splitObject(obj: string): { schema: string; table: string } {
  const idx = obj.lastIndexOf(".")
  if (idx <= 0) return { schema: "", table: obj }
  return { schema: obj.slice(0, idx), table: obj.slice(idx + 1) }
}
