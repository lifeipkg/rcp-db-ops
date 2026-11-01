# rcp-db-ops · 数据库操作服务

版本化 DDL 的**唯一执行者**（红线 RL-004）。业务仓不得自带建表/改表语句。

| 项 | 值 |
|---|---|
| 服务名 | `db-ops` |
| 端口 | `8091` |
| 契约 | `rcp-contracts/contracts/openapi/db-ops.yaml` |
| 迁移真源 | `rcp-ops/migrations/`（容器内挂载为 `./migrations`） |

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
- 缺 undo 脚本时回滚**明确报错**（`IRREVERSIBLE_MIGRATION`），不静默跳过。

## 错误码

| code | 含义 |
|---|---|
| `MIGRATIONS_DIR_MISSING` | 迁移目录不存在 |
| `BAD_MIGRATION_NAME` | 文件名不合约定 |
| `MISSING_UP_SCRIPT` | 只有回滚脚本没有正向脚本 |
| `MIGRATION_FAILED` | 某个版本执行失败（该版本已回滚） |
| `IRREVERSIBLE_MIGRATION` | 目标范围内有版本缺 undo 脚本 |
| `NOTHING_TO_ROLLBACK` | 目标范围内没有可回滚版本 |

## 构建与验证

```bash
mvn -B verify
python ../rcp-contracts/tools/contract_check.py \
  --repo . --contract ../rcp-contracts/contracts/openapi/db-ops.yaml
```
