// scripts/check-i18n.mjs — verify en.json and zh-CN.json define identical key sets.
// Exits 1 and prints missing/extra keys on any mismatch.
import { readFileSync } from "node:fs"
import { fileURLToPath } from "node:url"
import { dirname, join } from "node:path"

const dir = dirname(fileURLToPath(import.meta.url))
const en = JSON.parse(readFileSync(join(dir, "../src/i18n/en.json"), "utf8"))
const zhCN = JSON.parse(readFileSync(join(dir, "../src/i18n/zh-CN.json"), "utf8"))

/** Flatten a nested object into dot-separated keys (arrays count as leaf values). */
function flatten(obj, prefix = "", out = new Set()) {
  for (const [key, value] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (value && typeof value === "object" && !Array.isArray(value)) {
      flatten(value, path, out)
    } else {
      out.add(path)
    }
  }
  return out
}

const enKeys = flatten(en)
const zhKeys = flatten(zhCN)

const missingInZh = [...enKeys].filter((k) => !zhKeys.has(k)).sort()
const missingInEn = [...zhKeys].filter((k) => !enKeys.has(k)).sort()

if (missingInZh.length > 0) {
  console.error(`Missing in zh-CN.json (${missingInZh.length}):`)
  for (const k of missingInZh) console.error(`  - ${k}`)
}
if (missingInEn.length > 0) {
  console.error(`Missing in en.json (${missingInEn.length}):`)
  for (const k of missingInEn) console.error(`  - ${k}`)
}

if (missingInZh.length > 0 || missingInEn.length > 0) {
  console.error(`i18n check failed: en.json has ${enKeys.size} keys, zh-CN.json has ${zhKeys.size} keys.`)
  process.exit(1)
}

console.log(`i18n check passed: ${enKeys.size} keys in both en.json and zh-CN.json.`)
