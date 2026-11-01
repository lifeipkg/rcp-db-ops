package com.risk.platform.dbops.migration;

import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Repository;

import java.sql.Timestamp;
import java.time.Instant;
import java.util.List;

/**
 * schema_version 台账。执行成功才记一行；失败不记账，避免"记了没做"。
 */
@Repository
public class MigrationRepository {

    private static final String CREATE_TABLE = """
            CREATE TABLE IF NOT EXISTS schema_version (
              version      VARCHAR(32) PRIMARY KEY,
              description  VARCHAR(200) NOT NULL,
              executed_at  TIMESTAMP NOT NULL,
              success      BOOLEAN NOT NULL
            )
            """;

    private final JdbcTemplate jdbc;

    public MigrationRepository(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public void ensureSchema() {
        jdbc.execute(CREATE_TABLE);
    }

    public List<AppliedMigration> applied() {
        return jdbc.query(
                "SELECT version, description, executed_at, success FROM schema_version ORDER BY version",
                (rs, rowNum) -> new AppliedMigration(
                        rs.getString("version"),
                        rs.getString("description"),
                        rs.getTimestamp("executed_at").toInstant(),
                        rs.getBoolean("success")));
    }

    public void record(String version, String description, boolean success) {
        jdbc.update("INSERT INTO schema_version(version, description, executed_at, success) VALUES (?, ?, ?, ?)",
                version, description, Timestamp.from(Instant.now()), success);
    }

    public void forget(String version) {
        jdbc.update("DELETE FROM schema_version WHERE version = ?", version);
    }

    /** 已应用版本号（按数字段升序）。 */
    public List<String> appliedVersions() {
        return applied().stream().map(AppliedMigration::version).toList();
    }

    public record AppliedMigration(String version, String description, Instant executedAt, boolean success) {
    }
}
