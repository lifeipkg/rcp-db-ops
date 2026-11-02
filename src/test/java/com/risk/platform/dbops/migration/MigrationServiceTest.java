package com.risk.platform.dbops.migration;

import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.test.context.DynamicPropertyRegistry;
import org.springframework.test.context.DynamicPropertySource;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

@SpringBootTest(properties = "spring.cloud.nacos.discovery.enabled=false")
class MigrationServiceTest {

    private static final Path MIGRATIONS = Path.of("target/test-migrations");

    @Autowired
    private MigrationService service;

    @Autowired
    private JdbcTemplate jdbc;

    @DynamicPropertySource
    static void properties(DynamicPropertyRegistry registry) {
        registry.add("dbops.migrations-dir", MIGRATIONS::toString);
        registry.add("spring.datasource.url",
                () -> "jdbc:h2:mem:dbops-test;MODE=PostgreSQL;DB_CLOSE_DELAY=-1");
    }

    @BeforeAll
    static void prepareMigrations() throws IOException {
        Files.createDirectories(MIGRATIONS);
    }

    @BeforeEach
    void reset() throws IOException {
        clearMigrations();
        write("V1.0.0__base.sql", "CREATE TABLE patrol_event (event_id VARCHAR(64) PRIMARY KEY, domain VARCHAR(16))");
        write("U1.0.0__base.sql", "DROP TABLE patrol_event");
        write("V1.1.0__verdict.sql", "CREATE TABLE patrol_verdict (event_id VARCHAR(64) PRIMARY KEY, decision VARCHAR(16))");
        write("U1.1.0__verdict.sql", "DROP TABLE patrol_verdict");

        jdbc.execute("DROP TABLE IF EXISTS patrol_event");
        jdbc.execute("DROP TABLE IF EXISTS patrol_verdict");
        jdbc.execute("DROP TABLE IF EXISTS schema_version");
    }

    @Test
    @DisplayName("全量迁移：按序应用并记台账")
    void migrateAll() {
        MigrationService.MigrateResult result = service.migrate(null);

        assertEquals(2, result.applied().size());
        assertEquals("1.1.0", result.currentVersion());
        assertEquals(2, result.applied().stream().filter(a -> a.success()).count());
    }

    @Test
    @DisplayName("重复执行幂等：第二次全部 skipped，不重复建表")
    void migrateIsIdempotent() {
        service.migrate(null);
        MigrationService.MigrateResult second = service.migrate(null);

        assertTrue(second.applied().isEmpty());
        assertEquals(2, second.skipped());
        assertEquals("1.1.0", second.currentVersion());
    }

    @Test
    @DisplayName("指定目标版本只应用到该版本")
    void migrateToTarget() {
        MigrationService.MigrateResult result = service.migrate("1.0.0");

        assertEquals(1, result.applied().size());
        assertEquals("1.0.0", result.currentVersion());
        assertEquals(List.of("1.1.0"), service.status().pending());
    }

    @Test
    @DisplayName("回滚到指定版本：逆序执行 undo 并删台账")
    void rollbackToTarget() {
        service.migrate(null);

        MigrationService.RollbackResult result = service.rollbackTo("1.0.0");

        assertEquals(List.of("1.1.0"), result.rolledBack());
        assertEquals("1.0.0", result.currentVersion());
        assertEquals(List.of("1.0.0"), service.status().applied().stream()
                .map(MigrationRepository.AppliedMigration::version).toList());
    }

    @Test
    @DisplayName("无回滚脚本的版本回滚时明确报错，不静默跳过")
    void irreversibleRollbackFails() throws IOException {
        write("V1.2.0__no_undo.sql", "CREATE TABLE audit_log (id INT)");
        service.migrate(null);

        MigrationException ex = assertThrows(MigrationException.class, () -> service.rollbackTo("1.1.0"));
        assertEquals("IRREVERSIBLE_MIGRATION", ex.code());

        Files.deleteIfExists(MIGRATIONS.resolve("V1.2.0__no_undo.sql"));
    }

    @Test
    @DisplayName("没有可回滚版本时报错")
    void nothingToRollback() {
        service.migrate(null);
        MigrationException ex = assertThrows(MigrationException.class, () -> service.rollbackTo("1.1.0"));
        assertEquals("NOTHING_TO_ROLLBACK", ex.code());
    }

    private static void clearMigrations() throws IOException {
        try (var files = Files.list(MIGRATIONS)) {
            for (Path file : files.toList()) {
                Files.deleteIfExists(file);
            }
        }
    }

    private static void write(String name, String content) throws IOException {
        Files.writeString(MIGRATIONS.resolve(name), content);
    }
}
