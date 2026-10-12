// src/lib/brand-icons.tsx — 数据源官方品牌图标
// 三级来源：simple-icons（npm 本地打包）→ devicon（SVG 内联）→ 官方品牌色字母徽标。
// 无任何运行时网络请求。
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

/** simple-icons 品牌图标映射（连接类型 → 官方图标） */
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

/** devicon SVG（Oracle / SQL Server / AWS 家族 / Azure 家族 / DynamoDB） */
const deviconMap: Record<string, string> = {
  oracle: oracleSvg,
  sqlserver: mssqlSvg,
  azuresql: mssqlSvg,
  // AWS 家族（S3/Athena/Redshift 用 AWS 官方标）
  s3: awsSvg,
  athena: awsSvg,
  redshift: awsSvg,
  // Azure 家族
  azure: azureSvg,
  azuredwh: azureSvg,
  azuretable: azureSvg,
  dynamodb: dynamoSvg,
}

/** 亮色品牌底（如黄）配白字不可读，按亮度阈值切换深/浅文字 */
function readableTextOn(hex: string): string {
  const r = parseInt(hex.slice(1, 3), 16) / 255
  const g = parseInt(hex.slice(3, 5), 16) / 255
  const b = parseInt(hex.slice(5, 7), 16) / 255
  return 0.2126 * r + 0.7152 * g + 0.0722 * b > 0.55 ? "#111827" : "#ffffff"
}

/** 无官方开源图标的品牌 → 官方品牌色 + 字母徽标 */
const monogramMap: Record<string, [string, string]> = {
  firebolt: ["F", "#FE354C"],
  lancedb: ["L", "#7C3AED"],
  motherduck: ["M", "#FFD300"],
  ducklake: ["DL", "#FFF000"],
  iceberg: ["I", "#0F4C81"],
  iceberg2: ["I", "#0F4C81"],
  db2: ["DB2", "#0F62FE"],
}

/** 通用（非品牌）类型 → lucide 图标 */
const genericMap: Record<string, ComponentType<{ className?: string }>> = {
  local: FolderOpen,
  file: FolderOpen,
  ftp: Server,
  sftp: Server,
  http: Globe,
  api: Plug,
}

/**
 * BrandIcon — 按连接类型渲染官方品牌图标。
 * 统一放在中性圆角芯片里，保证深浅色主题下的对比度与一致性。
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
