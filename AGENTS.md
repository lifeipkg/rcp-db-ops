# AGENTS.md · rcp-db-ops

本仓规则。工作区级流程见 `<工作区根>/AGENTS.md`。

## 本仓是什么

唯一能执行 DDL 的服务（Go 1.22 + Gin + pgx，端口 8091）。命中**数据迁移**高风险标签：
改迁移执行逻辑、改台账结构、改回滚语义，都必须有独立复核记录与回滚验证。
早期 Java/Spring Boot 实现已退役，服务名、端口、契约与迁移语义不变。

| 项 | 值 |
|---|---|
| 服务名 / 端口 | `db-ops` / `8091` |
| 契约 | `rcp-contracts/contracts/openapi/db-ops.yaml` |
| 技术栈 | Go 1.22 + Gin 1.10.0 + pgx v5（版本真源：`rcp-ontology/ontology/abox/platform.ttl` 的 `rcp:stack`） |
| 迁移真源 | `rcp-ops/migrations`（容器内 `/app/migrations`） |

## 硬约束

1. **迁移脚本不在本仓。** 真源是 `rcp-ops/migrations`；本仓只负责执行与记账。想在服务仓里塞 DDL 就是 RL-004。
2. **失败必须整体回滚。** 一个版本一个事务；"跑到一半失败但记了成功"是最坏状态，比直接报错更危险。台账写入必须走同一个事务连接（`Record` / `Forget` 接收 `Execer`），不许另开连接。
3. **不可回滚要说出来。** 缺 undo 脚本时回滚报 `IRREVERSIBLE_MIGRATION`；不允许静默跳过或"尽力回滚"。检查在动手之前完成，不边回滚边判定。
4. **版本号只用数字段。** 字符串比较会出错（`1.10 < 1.9`），统一走 `migration.CompareVersions`。
5. **台账不可手改。** `schema_version` 是执行结果，不是配置；修正历史要走新的迁移版本。
6. **领域逻辑不 import 驱动。** `internal/migration` 只依赖 `Execer` / `TxRunner` / `Repo` 三个接口，pgx 实现放 `internal/pg`：这样"失败不记账""逆序回滚"这类不变量能在没有数据库的环境里被断言。
7. **路由写成完整字面量。** `rcp-contracts/tools/contract_check.py` 用正则抽 `r.GET`/`r.POST` 与 OpenAPI 做两向差集；探活 `/actuator/health` 用 `r.Handle` 注册，不计入契约路由。

## 验证

```bash
go vet ./... && go test ./... && go build ./...
python ../rcp-contracts/tools/contract_check.py --repo . \
  --contract ../rcp-contracts/contracts/openapi/db-ops.yaml
```

改执行逻辑时必须补三类用例：幂等重跑、失败回滚（注入失败语句）、不可回滚版本的报错路径。
另有一条必须保留的用例：**undo 失败时台账不被删**（表还在，账也必须在）。

事务语义由 fake 实现模拟「提交才生效」，真实 PostgreSQL 的 DDL 锁行为不在单测覆盖范围内——
结论里不许把 fake 的绿灯说成"已在真实库验证"。
