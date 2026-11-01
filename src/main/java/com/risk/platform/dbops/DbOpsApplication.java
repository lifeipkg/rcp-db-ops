package com.risk.platform.dbops;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.boot.context.properties.ConfigurationPropertiesScan;

/**
 * 数据库操作服务：版本化 DDL 的唯一执行者（红线 RL-004）。
 *
 * <p>迁移真源是部署仓的 <code>migrations/</code> 目录，本服务负责按版本顺序执行、
 * 记录台账、并在有 undo 文件时支持回滚。
 */
@SpringBootApplication
@ConfigurationPropertiesScan
public class DbOpsApplication {

    public static void main(String[] args) {
        SpringApplication.run(DbOpsApplication.class, args);
    }
}
