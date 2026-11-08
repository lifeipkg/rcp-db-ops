# rcp-db-ops · 数据库操作服务（Go）

版本化 DDL 的**唯一执行者**（红线 RL-004）。业务仓不得自带建表/改表语句。

| 项 | 值 |
|---|---|
| 服务名 | `db-ops` |
| 端口 | `8091` |
| 技术栈 | Go 1.22 + Gin 1.10 + pgx v5（PostgreSQL） |
| 契约 | `rcp-contracts/contracts/openapi/db-ops.yaml` |
| 迁移真源 | `rcp-ops/migrations/`（容器内挂载为 `/app/migrations`） |
| 探活 | `GET /actuator/health`（不在公开契约内） |

早期 Java/Spring Boot 实现在本次切换中退役，服务名、端口、契约与迁移语义不变；
变更面见文末「从 Java 切到 Go 的破坏性变更」。

## 能力

- `POST /dbops/migrate` —— 按版本顺序应用未执行的迁移；可指定 `targetVersion`
- `GET  /dbops/versions` —— 台账：当前版本、已应用、待应用
- `POST /dbops/rollback` —— 逆序执行 undo 脚本回滚；`targetVersion` 为空串表示全部回滚

## 命名与纪律

| 文件 | 含义 |
|---|---|
| `V<版本>__<描述>.sql` | 正向脚本，必填 |
| `U<版本>__<描述>.sql` | 回滚脚本，可选；**缺省即不可回滚** |

- 版本按**数字段**比较（`1.10.0 > 1.9.0`），不是字符串；
- 每个版本一个事务：失败整体回滚且不记账，不留"表改了一半"的半激活状态；
- 台账写入（`Record` / `Forget`）走**同一个事务连接**：记账与表变更要么都生效、要么都不生效；
- 缺 undo 脚本时回滚**明确报错**（`IRREVERSIBLE_MIGRATION`），不静默跳过；
- 回滚前先全量检查可回滚性，再动手执行：不会出现"回滚了两个版本才发现第三个不可回滚"。

## 错误码

| code | 含义 |
|---|---|
| `MIGRATIONS_DIR_MISSING` | 迁移目录不存在 |
| `BAD_MIGRATION_NAME` | 文件名不合约定 |
| `MISSING_UP_SCRIPT` | 只有回滚脚本没有正向脚本 |
| `MIGRATION_FAILED` | 某个版本执行失败（该版本已回滚） |
| `IRREVERSIBLE_MIGRATION` | 目标范围内有版本缺 undo 脚本 |
| `NOTHING_TO_ROLLBACK` | 目标范围内没有可回滚版本 |

## 配置

| 变量 | 默认值 | 说明 |
|---|---|---|
| `SERVER_PORT` | `8091` | 监听端口 |
| `DBOPS_MIGRATIONS_DIR` | `migrations` | 迁移目录（部署侧挂载点） |
| `DBOPS_DB_DSN` | 内嵌演示值 | 完整连接串；优先于分量拼装 |
| `DB_HOST` / `DB_PORT` / `DB_NAME` / `DB_USER` / `DB_PASSWORD` / `DB_SSLMODE` | — | 分量拼装 DSN，供 K8s 复用 `rcp-db-credential` Secret |

## 构建与验证

```bash
go vet ./... && go test ./... && go build ./...
python ../rcp-contracts/tools/contract_check.py --repo . \
  --contract ../rcp-contracts/contracts/openapi/db-ops.yaml
docker build -t rcp-db-ops:local .
```

单测覆盖三类必须验证的路径：幂等重跑、失败回滚（注入失败语句）、不可回滚版本的报错。
事务语义用 fake 实现模拟「提交才生效」——`internal/migration/service_test.go` 断言
失败版本不记账、失败后重跑只补失败的那个版本、undo 失败时台账不被删。
真实 PostgreSQL 的事务与 DDL 锁行为未在本仓单测中覆盖。

## 已知边界

- 脚本按分号切分，字符串字面量里出现分号会被误切。迁移脚本由平台维护、内容可审，
  这里不为罕见形态引入 SQL 解析器。
- 迁移执行没有分布式锁：同一目录被两个实例并发跑，靠 `schema_version` 主键冲突打断
  其中一个（会返回 `MIGRATION_FAILED`），不保证"只会有一个赢家"。当前按 1 副本部署。
- 台账 `schema_version` 不可手改：修正历史要走新的迁移版本。

## 从 Java 切到 Go 的破坏性变更

| 变更 | 影响 | 替代 |
|---|---|---|
| 构建 Maven → Go modules | `mvn verify` 不再适用 | `go vet/test/build`，CI 走 `go-service.yml` |
| 产物 jar → 静态二进制 | 镜像与启动命令变化 | 见 `Dockerfile`（alpine + 非 root） |
| 配置 `application.yml` → 环境变量 | `SPRING_DATASOURCE_*` 失效 | `DBOPS_DB_DSN` 或 `DB_*` 分量 |
| 探活 `/actuator/health/readiness`、`/liveness` → `/actuator/health` | K8s 探针路径需改 | 单一探活，与 `rcp-iam` 一致 |
| 不再向 Nacos 注册 | 服务发现由 k8s DNS 承担（`fallback` 本来就是 `k8s-dns`） | 按 Service 名解析 |
| 数据库驱动 PostgreSQL（pgx） | 只支持 PostgreSQL | 平台已全量 PostgreSQL 16 |
