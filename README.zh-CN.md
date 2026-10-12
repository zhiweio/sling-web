
<p align="center"><img src="logo-brand.png" alt="drawing" width="250"/></p>

<p align="center" style="margin-bottom: 0px">把数据从源端"弹射"到目标端。</p>
<p align="center">详见 <a href="https://docs.slingdata.io/">docs.slingdata.io</a></p>
<p align="center"><a href="README.md">English</a></p>

<p align="center">
  <a href="https://github.com/slingdata-io/sling-cli/blob/master/LICENSE">
    <img alt="GitHub" src="https://img.shields.io/github/license/slingdata-io/sling-cli"/>
  </a>
  <a href="https://goreportcard.com/report/github.com/slingdata-io/sling-cli">
    <img src="https://goreportcard.com/badge/github.com/slingdata-io/sling-cli" />
  </a>
  <a href="https://pkg.go.dev/github.com/slingdata-io/sling-cli">
    <img src="https://pkg.go.dev/badge/github.com/slingdata-io/sling-cli.svg" alt="Go Reference"/>
  </a>
  <a href="https://discord.gg/q5xtaSNDvp">
    <img alt="Discord" src="https://img.shields.io/discord/1210406123744796702?style=flat&logo=discord&label=discord"/>
  </a>
  <a href="https://github.com/slingdata-io/sling-cli/tags" rel="nofollow">
    <img alt="GitHub tag (latest SemVer pre-release)" src="https://img.shields.io/github/v/tag/slingdata-io/sling-cli?include_prereleases&label=version"/>
  </a>
  <a href="https://pypi.org/project/sling/" rel="nofollow">
    <img alt="Pip Downloads" src="https://img.shields.io/pepy/dt/sling"/>
  </a>
  <a href="https://pypi.org/project/sling/" rel="nofollow">
    <img alt="Pip Downloads" src="https://img.shields.io/docker/pulls/slingdata/sling"/>
  </a>
</p>

Sling 是一个由热情驱动、衍化为免费 CLI 产品的项目,提供使用 Extract & Load(EL)方式
创建与维护中小体量数据管道的简单方案。它专注于以下场景的数据搬运:

* 数据库 → 数据库
* 文件系统 → 数据库
* 数据库 → 文件系统
* API → 数据库
* API → 文件系统

https://github.com/slingdata-io/sling-cli/assets/7671010/e10ee716-1de8-4d53-8eb2-95c6d9d7f9f0

部分核心特性:
- 单二进制部署(Go 构建)。见[安装](https://docs.slingdata.io/sling-cli/getting-started)页面。
- 自定义 SQL 作为数据流:`--src-stream='select * from my_table where col1 > 10'`
- 通过 [`sling conns`](https://docs.slingdata.io/sling-cli/environment#managing-connections) 子命令管理/查看/测试/发现连接
- 也可以用环境变量作为连接(`export MY_PG='postgres//...'`)
- 提供 YAML 或 JSON 配置(非常适合 git 版本管理)。
- 强大的 [Replication](https://docs.slingdata.io/sling-cli/run/configuration/replication) 逻辑,可用通配符(`my_schema.*`)复制大量表。
- 读取已有的 [DBT 连接](https://docs.slingdata.io/sling-cli/environment#dbt-profiles-dbt-profiles.yml)
- 在 YAML / JSON 配置中使用环境变量(`select * from my_table where date = '{date}'`)
- 便捷的[转换](https://docs.slingdata.io/sling-cli/run/configuration/transformations),如 `flatten` 选项可自动把嵌套字段展开为列。
- 运行前置/后置 SQL 命令。
- 更多!

---

[Replication](https://docs.slingdata.io/sling-cli/run/configuration/replication) 示例:

<p align="center"><img src="https://github.com/slingdata-io/sling-cli/assets/7671010/fcbd2c90-7d6c-4a03-9933-ba1fbeaad838" alt="replication.yaml" width="700"></p>

---

可用连接器:
- **数据库**:[`adbc`](https://docs.slingdata.io/connections/database-connections/adbc) [`azuredwh`](https://docs.slingdata.io/connections/database-connections/azuredwh) [`azuresql`](https://docs.slingdata.io/connections/database-connections/azuresql) [`azuretable`](https://docs.slingdata.io/connections/database-connections/azuretable) [`bigquery`](https://docs.slingdata.io/connections/database-connections/bigquery) [`bigtable`](https://docs.slingdata.io/connections/database-connections/bigtable) [`clickhouse`](https://docs.slingdata.io/connections/database-connections/clickhouse) [`d1`](https://docs.slingdata.io/connections/database-connections/d1) [`databricks`](https://docs.slingdata.io/connections/database-connections/databricks) [`dbase`](https://docs.slingdata.io/connections/database-connections/dbase) [`duckdb`](https://docs.slingdata.io/connections/database-connections/duckdb) [`dynamodb`](https://docs.slingdata.io/connections/database-connections/dynamodb) [`elasticsearch`](https://docs.slingdata.io/connections/database-connections/elasticsearch) [`opensearch`](https://docs.slingdata.io/connections/database-connections/opensearch) [`exasol`](https://docs.slingdata.io/connections/database-connections/exasol) [`fabric`](https://docs.slingdata.io/connections/database-connections/fabric) [`firebolt`](https://docs.slingdata.io/connections/database-connections/firebolt) [`mariadb`](https://docs.slingdata.io/connections/database-connections/mariadb) [`mongodb`](https://docs.slingdata.io/connections/database-connections/mongodb) [`motherduck`](https://docs.slingdata.io/connections/database-connections/motherduck) [`mysql`](https://docs.slingdata.io/connections/database-connections/mysql) [`odbc`](https://docs.slingdata.io/connections/database-connections/odbc) [`oracle`](https://docs.slingdata.io/connections/database-connections/oracle) [`postgres`](https://docs.slingdata.io/connections/database-connections/postgres) [`prometheus`](https://docs.slingdata.io/connections/database-connections/prometheus) [`proton`](https://docs.slingdata.io/connections/database-connections/proton) [`redshift`](https://docs.slingdata.io/connections/database-connections/redshift) [`scylladb`](https://docs.slingdata.io/connections/database-connections/scylladb) [`snowflake`](https://docs.slingdata.io/connections/database-connections/snowflake) [`sqlite`](https://docs.slingdata.io/connections/database-connections/sqlite) [`sqlserver`](https://docs.slingdata.io/connections/database-connections/sqlserver) [`starrocks`](https://docs.slingdata.io/connections/database-connections/starrocks) [`trino`](https://docs.slingdata.io/connections/database-connections/trino)

- **数据湖**:[`athena`](https://docs.slingdata.io/connections/datalake-connections/athena) [`Ducklake`](https://docs.slingdata.io/connections/datalake-connections/ducklake) [`iceberg`](https://docs.slingdata.io/connections/datalake-connections/iceberg) [`S3 Tables`](https://docs.slingdata.io/connections/datalake-connections/iceberg) [`lancedb`](https://docs.slingdata.io/connections/datalake-connections/lancedb)

- **文件系统**:[`azure`](https://docs.slingdata.io/connections/file-connections/azure) [`b2`](https://docs.slingdata.io/connections/file-connections/b2) [`dospaces`](https://docs.slingdata.io/connections/file-connections/dospaces) [`gs`](https://docs.slingdata.io/connections/file-connections/gs) [`local`](https://docs.slingdata.io/connections/file-connections/local) [`minio`](https://docs.slingdata.io/connections/file-connections/minio) [`r2`](https://docs.slingdata.io/connections/file-connections/r2) [`s3`](https://docs.slingdata.io/connections/file-connections/s3) [`sftp`](https://docs.slingdata.io/connections/file-connections/sftp) [`wasabi`](https://docs.slingdata.io/connections/file-connections/wasabi)
- **文件格式**:`csv`、`parquet`、`xlsx`、`json`、`geojson`、`avro`、`xml`、`sas7bday`

其他链接:
- https://slingdata.io
- https://docs.slingdata.io
- https://blog.slingdata.io

---

## sling-server —— 浏览器工作台

除 CLI 之外,本仓库还包含 **sling-server**:打包进同一个单二进制的浏览器版数据迁移
工作台(Go 服务端 + 内嵌 React UI,构建于零改动的 sling 引擎之上)。它让非技术用户
也能完成完整旅程——连接管理、5 步迁移向导、调度、实时运行监控,以及治理能力(加密
凭据保险库、通知、审计日志、备份),并提供中英双语界面。

```bash
just build-server                # 构建单一二进制(内嵌前端)
./cmd/sling-server/sling-server  # 运行于 http://127.0.0.1:5666
```

亮点:41 种连接器类型的引导式向导、AES-256-GCM 凭据保险库、cron 调度、SSE 实时进度
与日志终端、配置版本化与回滚、可直接在 CLI 中运行的 YAML 导出、Docker 与 systemd
部署。

架构、安全模型与完整功能矩阵见 [server/README.zh-CN.md](server/README.zh-CN.md)。

---

想快速把 CSV 或 JSON 文件导入数据库?用 sling:

```bash
cat my_file.csv | sling run --tgt-conn MYDB --tgt-object my_schema.my_table
```

想在两个数据库之间复制数据?用 sling:
```bash
sling run --src-conn PG_DB --src-stream public.transactions \
  --tgt-conn MYSQL_DB --tgt-object mysql.bank_transactions \
  --mode full-refresh
```

`sling conns` 命令还可以轻松管理本地连接:

```bash
$ sling conns set MY_PG url='postgresql://postgres:myPassword@pghost:5432/postgres'

$ sling conns list
+--------------------------+-----------------+-------------------+
| CONN NAME                | CONN TYPE       | SOURCE            |
+--------------------------+-----------------+-------------------+
| AWS_S3                   | FileSys - S3    | sling env yaml    |
| FINANCE_BQ               | DB - BigQuery   | sling env yaml    |
| DO_SPACES                | FileSys - S3    | sling env yaml    |
| LOCALHOST_DEV            | DB - PostgreSQL | dbt profiles yaml |
| MSSQL                    | DB - SQLServer  | sling env yaml    |
| MYSQL                    | DB - MySQL      | sling env yaml    |
| ORACLE_DB                | DB - Oracle     | env variable      |
| MY_PG                    | DB - PostgreSQL | sling env yaml    |
+--------------------------+-----------------+-------------------+

$ sling conns discover LOCALHOST_DEV
9:05AM INF Found 344 streams:
 - "public"."accounts"
 - "public"."bills"
 - "public"."connections"
 ...
```

---

Databricks:Unity Catalog Volumes 是文件连接(`type: databricks-volume`)。Delta 直接入库在 `type: databricks` 上使用 `copy_method: zerobus`。见 https://docs.slingdata.io/connections/database-connections/databricks

---

## 安装

#### Mac / Linux 一行命令

```shell
curl -fsSL https://slingdata.io/install.sh | bash

# 即装即用!
sling -h
```

#### Windows 一行命令(PowerShell)

```powershell
irm https://slingdata.io/install.ps1 | iex

# 即装即用!
sling -h
```

#### Mac 使用 Brew

```shell
brew install slingdata-io/sling/sling
```

#### Windows 使用 Scoop

```powershell
scoop bucket add sling https://github.com/slingdata-io/scoop-sling.git
scoop install sling
```

#### 手动下载二进制

macOS、Linux 与 Windows 的预编译二进制见[发布页](https://github.com/slingdata-io/sling-cli/releases)。

### 从源码编译

要求:
- 安装 Go 1.22+(https://go.dev/doc/install)
- 安装 C 编译器([gcc](https://www.google.com/search?q=install+gcc&oq=install+gcc)、[tdm-gcc](https://jmeubank.github.io/tdm-gcc/)、[mingw](https://www.google.com/search?q=install+mingw) 等)
- `CGO_ENABLED=1`(SQLite 与 Databricks Zerobus 需要;本仓库构建脚本已设置)

#### Linux 或 Mac
```bash
git clone https://github.com/slingdata-io/sling-cli.git
cd sling-cli
bash scripts/build.sh

./sling --help
```

#### Windows(PowerShell)
```bash
git clone https://github.com/slingdata-io/sling-cli.git
cd sling-cli

.\scripts\build.ps1

.\sling --help
```

### 通过 Python 包装器安装

`pip install sling`

之后即可在命令行运行 `sling --help`。

仓库链接:https://github.com/slingdata-io/sling-python

### 自动化开发构建

以下是官方开发构建链接,即下一个版本的最新构建。

- **Linux (x64)**: https://f.slingdata.io/dev/latest/sling_linux_amd64.tar.gz
- **Mac (arm64)**: https://f.slingdata.io/dev/latest/sling_darwin_arm64.tar.gz
- **Windows (x64)**: https://f.slingdata.io/dev/latest/sling_windows_amd64.tar.gz

如果使用 Python 包装器,请解压下载的二进制并将路径放入环境变量 `SLING_BINARY` 后再运行你的 Python 程序。

## 参与贡献

欢迎为改进 Sling 贡献力量!完整指南(开发环境、测试套件、PR 流程)见
[CONTRIBUTING.md](CONTRIBUTING.md)。以下是快速要点。

### 测试指南

Sling 有三个主要测试套件:Database、File 与 CLI。贡献时请确保你的改动通过相关测试。

#### 运行测试

运行完整测试套件请执行以下命令,但你需要按[此处](https://github.com/slingdata-io/sling-cli/blob/main/cmd/sling/sling_test.go#L52-L83)定义所有需要的连接,因此建议定向运行特定测试。

```sh
./scripts/build.sh
./scripts/test.sh
```

#### 定向运行测试

可以通过环境变量定向运行特定测试或套件:

1. Database 套件:
   ```sh
   cd cmd/sling
   go test -v -run TestSuiteDatabasePostgres             # 运行全部 Postgres 测试
   go test -v -run TestSuiteDatabasePostgres -- 1-3 # 运行 Postgres 测试 1、2、3
   ```

2. File 套件:
   ```sh
   cd cmd/sling
   go test -v -run TestSuiteFileS3               # 运行全部 S3 测试
   go test -v -run TestSuiteFileS3 -- 1,2,3 # 运行 S3 测试 1、2、3
   ```

3. CLI 套件:
   ```sh
   cd cmd/sling
   export SLING_BIN=./sling
   go test -v -run TestCLI             # 运行全部 CLI 测试
   go test -v -run TestCLI -- 31+ # 运行 CLI 测试 31 及之后所有测试
   ```

可以指定单个测试编号、区间,或用 `+` 后缀运行从某编号起的所有测试:

- `go test -v -run TestCLI -- "1,2,3"`:运行测试 1、2、3
- `go test -v -run TestSuiteFileS3 -- "1-5"`:运行测试 1 至 5
- `go test -v -run TestCLI -- "3+"`:运行测试 3 及之后所有测试

#### 测试套件概览

1. **Database 套件**:测试数据库相关功能。
   - 位于:`cmd/sling/sling_test.go`
   - 配置:`tests/suite.db.template.tsv`

2. **File 套件**:测试文件系统操作。
   - 位于:`cmd/sling/sling_test.go`
   - 配置:`tests/suite.file.template.tsv`

3. **CLI 套件**:测试命令行界面功能。
   - 位于:`cmd/sling/sling_cli_test.go`
   - 配置:`tests/suite.cli.yaml`

4. **工作台套件**(sling-server):Go 单元测试与针对真实二进制的全栈 Playwright E2E 测试。
   ```sh
   just test-server    # server/ 单元测试
   just build-server   # e2e 前置
   just test-e2e       # Playwright 套件(见 e2e/README.zh-CN.md)
   ```

### 新增测试

引入新功能或修复 bug 时,请补充相关测试,主要面向 `cmd/sling/suite.cli.yaml` 中的 CLI 套件文件。Database 与 File 套件是跨连接器通用的模板,对改动更敏感,因此对这些套件的更改将在内部管理。

在 CLI 套件文件中新增测试条目时,可在 `tests/replications` 目录创建新的 replication 文件,或在 `tests/files` 目录添加对应源文件,并在新测试条目中写明期望输出或期望行数/流数。

### Pull Request 流程

1. 确保代码符合现有风格并通过所有测试。
2. 如接口有变化,请更新 README.md。
3. 创建标题与描述清晰的 Pull Request。

感谢你为 Sling 贡献力量!
