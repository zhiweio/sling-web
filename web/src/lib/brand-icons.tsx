// src/lib/brand-icons.tsx — official brand icons for data sources
// Three-tier sources: simple-icons (bundled from npm) → devicon (inline SVG) →
// letter monogram in the official brand color. No runtime network requests.
import type { ComponentType } from "react"
import {
  siPostgresql,
  siMysql,
  siMariadb,
  siSqlite,
  siSnowflake,
  siDatabricks,
  siGooglebigquery,
  siClickhouse,
  siMongodb,
  siDuckdb,
  siElasticsearch,
  siOpensearch,
  siGooglecloud,
  siCloudflare,
  siGoogledrive,
  siApachehadoop,
  siTrino,
} from "simple-icons"

import oracleSvg from "devicon/icons/oracle/oracle-original.svg?raw"
import mssqlSvg from "devicon/icons/microsoftsqlserver/microsoftsqlserver-original.svg?raw"
import awsSvg from "devicon/icons/amazonwebservices/amazonwebservices-original-wordmark.svg?raw"
import azureSvg from "devicon/icons/azure/azure-original.svg?raw"
import dynamoSvg from "devicon/icons/dynamodb/dynamodb-original.svg?raw"

import { cn } from "@/lib/utils"
import { Database, FolderOpen, Globe, Plug, Server } from "lucide-react"

type SimpleIcon = { title: string; path: string; hex: string }

/** simple-icons brand icon map (connection type → official icon) */
const simpleMap: Record<string, SimpleIcon> = {
  postgres: siPostgresql,
  postgresql: siPostgresql,
  mysql: siMysql,
  mariadb: siMariadb,
  sqlite: siSqlite,
  snowflake: siSnowflake,
  databricks: siDatabricks,
  bigquery: siGooglebigquery,
  clickhouse: siClickhouse,
  mongodb: siMongodb,
  duckdb: siDuckdb,
  elasticsearch: siElasticsearch,
  opensearch: siOpensearch,
  gs: siGooglecloud, // Google Cloud Storage
  bigtable: siGooglecloud,
  gdrive: siGoogledrive,
  hdfs: siApachehadoop,
  trino: siTrino,
  d1: siCloudflare,
  r2: siCloudflare,
}

/** devicon SVGs (Oracle / SQL Server / AWS family / Azure family / DynamoDB) */
const deviconMap: Record<string, string> = {
  oracle: oracleSvg,
  sqlserver: mssqlSvg,
  azuresql: mssqlSvg,
  // AWS family (S3/Athena/Redshift use the official AWS mark)
  s3: awsSvg,
  athena: awsSvg,
  redshift: awsSvg,
  // Azure family
  azure: azureSvg,
  azuredwh: azureSvg,
  azuretable: azureSvg,
  dynamodb: dynamoSvg,
}

/** Light brand backgrounds (e.g. yellow) are unreadable with white text — switch dark/light text by luminance threshold */
function readableTextOn(hex: string): string {
  const r = parseInt(hex.slice(1, 3), 16) / 255
  const g = parseInt(hex.slice(3, 5), 16) / 255
  const b = parseInt(hex.slice(5, 7), 16) / 255
  return 0.2126 * r + 0.7152 * g + 0.0722 * b > 0.55 ? "#111827" : "#ffffff"
}

/** Brands without open-source icons → official brand color + letter monogram */
const monogramMap: Record<string, [string, string]> = {
  firebolt: ["F", "#FE354C"],
  lancedb: ["L", "#7C3AED"],
  motherduck: ["M", "#FFD300"],
  ducklake: ["DL", "#FFF000"],
  iceberg: ["I", "#0F4C81"],
  iceberg2: ["I", "#0F4C81"],
  db2: ["DB2", "#0F62FE"],
}

/** Generic (non-brand) types → lucide icons */
const genericMap: Record<string, ComponentType<{ className?: string }>> = {
  local: FolderOpen,
  file: FolderOpen,
  ftp: Server,
  sftp: Server,
  http: Globe,
  api: Plug,
}

/**
 * BrandIcon — renders the official brand icon for a connection type.
 * Always wrapped in a neutral rounded chip to keep contrast and consistency across light/dark themes.
 */
export function BrandIcon({ type, className }: { type: string; className?: string }) {
  const t = type.toLowerCase()

  const simple = simpleMap[t]
  if (simple) {
    return (
      <span
        className={cn(
          "inline-flex shrink-0 items-center justify-center rounded-lg bg-black/5 dark:bg-white/10",
          className,
        )}
        title={simple.title}
      >
        <svg viewBox="0 0 24 24" className="h-[58%] w-[58%]" fill={`#${simple.hex}`} aria-hidden>
          <path d={simple.path} />
        </svg>
      </span>
    )
  }

  const devicon = deviconMap[t]
  if (devicon) {
    return (
      <span
        className={cn(
          "inline-flex shrink-0 items-center justify-center rounded-lg bg-black/5 dark:bg-white/10 [&>svg]:h-[72%] [&>svg]:w-[72%]",
          className,
        )}
        dangerouslySetInnerHTML={{ __html: devicon }}
      />
    )
  }

  const mono = monogramMap[t]
  if (mono) {
    return (
      <span
        className={cn(
          "inline-flex shrink-0 items-center justify-center rounded-lg font-extrabold leading-none",
          className,
        )}
        style={{ backgroundColor: mono[1], color: readableTextOn(mono[1]) }}
        title={t}
      >
        <span className="text-[42%]">{mono[0]}</span>
      </span>
    )
  }

  const Generic = genericMap[t] ?? Database
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary",
        className,
      )}
    >
      <Generic className="h-[58%] w-[58%]" />
    </span>
  )
}
