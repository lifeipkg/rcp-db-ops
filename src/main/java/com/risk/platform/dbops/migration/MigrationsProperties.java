package com.risk.platform.dbops.migration;

import org.springframework.boot.context.properties.ConfigurationProperties;

/** 迁移配置：迁移目录、是否允许自动执行。 */
@ConfigurationProperties(prefix = "dbops")
public record MigrationsProperties(String migrationsDir, boolean failOnMissingUndo) {

    public MigrationsProperties {
        migrationsDir = migrationsDir == null || migrationsDir.isBlank() ? "migrations" : migrationsDir;
    }
}
