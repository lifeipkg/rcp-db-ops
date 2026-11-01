package com.risk.platform.dbops.migration;

import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Service;
import org.springframework.transaction.support.TransactionTemplate;

import java.nio.file.Path;
import java.time.Instant;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;

/**
 * 迁移执行器：按版本顺序应用、记录台账、按需回滚。
 *
 * <p>每个版本一个事务：失败即整体回滚且不记账，避免出现"表改了一半、台账说成功"的半激活状态。
 * 回滚不做"尽力而为"——缺少 undo 脚本直接报错，而不是静默跳过留下不一致的表。
 */
@Service
public class MigrationService {

    private final JdbcTemplate jdbc;
    private final MigrationRepository repository;
    private final MigrationScanner scanner;
    private final MigrationsProperties properties;
    private final TransactionTemplate transactionTemplate;

    public MigrationService(JdbcTemplate jdbc, MigrationRepository repository,
                            MigrationScanner scanner, MigrationsProperties properties,
                            TransactionTemplate transactionTemplate) {
        this.jdbc = jdbc;
        this.repository = repository;
        this.scanner = scanner;
        this.properties = properties;
        this.transactionTemplate = transactionTemplate;
    }

    public MigrateResult migrate(String targetVersion) {
        repository.ensureSchema();
        List<MigrationFile> all = scanner.scan(Path.of(properties.migrationsDir()));
        List<String> applied = new ArrayList<>(repository.appliedVersions());
        List<MigrationRepository.AppliedMigration> newlyApplied = new ArrayList<>();
        int skipped = 0;

        for (MigrationFile migration : all) {
            if (applied.contains(migration.version())) {
                skipped++;
                continue;
            }
            if (targetVersion != null && !targetVersion.isBlank()
                    && MigrationScanner.compareVersions(migration.version(), targetVersion) > 0) {
                skipped++;
                continue;
            }
            applyOne(migration);
            applied.add(migration.version());
            newlyApplied.add(new MigrationRepository.AppliedMigration(
                    migration.version(), migration.description(), Instant.now(), true));
        }

        return new MigrateResult(newlyApplied, skipped, currentVersion(applied));
    }

    public RollbackResult rollbackTo(String targetVersion) {
        repository.ensureSchema();
        List<MigrationFile> all = scanner.scan(Path.of(properties.migrationsDir()));
        List<String> applied = new ArrayList<>(repository.appliedVersions());

        List<MigrationFile> toRollback = all.stream()
                .filter(m -> applied.contains(m.version()))
                .filter(m -> targetVersion == null || targetVersion.isBlank()
                        || MigrationScanner.compareVersions(m.version(), targetVersion) > 0)
                .sorted(Comparator.comparing(MigrationFile::version, MigrationScanner::compareVersions).reversed())
                .toList();

        if (toRollback.isEmpty()) {
            throw new MigrationException("NOTHING_TO_ROLLBACK",
                    "没有可回滚的版本（目标版本: " + targetVersion + "）");
        }
        for (MigrationFile migration : toRollback) {
            if (!migration.reversible()) {
                throw new MigrationException("IRREVERSIBLE_MIGRATION",
                        "版本 " + migration.version() + " 没有回滚脚本（U" + migration.version() + "__*.sql），不可回滚");
            }
        }

        List<String> rolledBack = new ArrayList<>();
        for (MigrationFile migration : toRollback) {
            transactionTemplate.executeWithoutResult(status -> {
                for (String statement : splitStatements(migration.downSql())) {
                    jdbc.execute(statement);
                }
                repository.forget(migration.version());
            });
            applied.remove(migration.version());
            rolledBack.add(migration.version());
        }
        return new RollbackResult(rolledBack, currentVersion(applied));
    }

    public StatusResult status() {
        repository.ensureSchema();
        List<MigrationFile> all = scanner.scan(Path.of(properties.migrationsDir()));
        List<String> applied = repository.appliedVersions();
        List<String> pending = all.stream()
                .map(MigrationFile::version)
                .filter(v -> !applied.contains(v))
                .toList();
        return new StatusResult(currentVersion(applied), repository.applied(), pending);
    }

    private void applyOne(MigrationFile migration) {
        transactionTemplate.executeWithoutResult(status -> {
            try {
                for (String statement : splitStatements(migration.upSql())) {
                    jdbc.execute(statement);
                }
                repository.record(migration.version(), migration.description(), true);
            } catch (RuntimeException ex) {
                throw new MigrationException("MIGRATION_FAILED",
                        "版本 " + migration.version() + " 执行失败，已回滚该版本: " + ex.getMessage(), ex);
            }
        });
    }

    private static List<String> splitStatements(String script) {
        List<String> statements = new ArrayList<>();
        for (String part : script.split(";")) {
            String trimmed = part.strip();
            if (!trimmed.isEmpty()) {
                statements.add(trimmed);
            }
        }
        return statements;
    }

    private static String currentVersion(List<String> applied) {
        return applied.stream()
                .max(MigrationScanner::compareVersions)
                .orElse("");
    }

    public record MigrateResult(List<MigrationRepository.AppliedMigration> applied,
                                int skipped, String currentVersion) {
    }

    public record RollbackResult(List<String> rolledBack, String currentVersion) {
    }

    public record StatusResult(String currentVersion,
                               List<MigrationRepository.AppliedMigration> applied,
                               List<String> pending) {
    }
}
