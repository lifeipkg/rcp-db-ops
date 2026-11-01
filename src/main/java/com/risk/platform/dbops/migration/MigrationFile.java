package com.risk.platform.dbops.migration;

/**
 * 一个版本化迁移单元。up 必填，down 可缺省（缺省表示不可回滚，回滚时明确报错而不是静默跳过）。
 */
public record MigrationFile(String version, String description, String upSql, String downSql) {

    public boolean reversible() {
        return downSql != null && !downSql.isBlank();
    }
}
