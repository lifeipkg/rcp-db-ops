package com.risk.platform.dbops.migration;

import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

class MigrationScannerTest {

    private static Path dir;
    private final MigrationScanner scanner = new MigrationScanner();

    @BeforeAll
    static void createDir() throws IOException {
        dir = Files.createTempDirectory("rcp-migrations-scanner");
    }

    @Test
    @DisplayName("按数字段排序，1.10.0 排在 1.9.0 之后")
    void sortsByNumericSegment() throws IOException {
        Path target = Files.createTempDirectory(dir, "sort");
        Files.createDirectories(target);
        write(target, "V1.9.0__nine.sql", "CREATE TABLE t9 (id INT)");
        write(target, "V1.10.0__ten.sql", "CREATE TABLE t10 (id INT)");
        Files.createDirectories(target);

        List<MigrationFile> migrations = scanner.scan(target);
        assertEquals(List.of("1.9.0", "1.10.0"), migrations.stream().map(MigrationFile::version).toList());
    }

    @Test
    @DisplayName("缺回滚脚本时 reversible=false，而不是报错")
    void marksIrreversible() throws IOException {
        Path target = Files.createDirectory(dir.resolve("irreversible"));
        write(target, "V1.0.0__only_up.sql", "CREATE TABLE t (id INT)");

        List<MigrationFile> migrations = scanner.scan(target);
        assertEquals(1, migrations.size());
        assertFalse(migrations.get(0).reversible());
    }

    @Test
    @DisplayName("命名不合约定直接报错，不猜测")
    void rejectsBadName() throws IOException {
        Path target = Files.createDirectory(dir.resolve("badname"));
        write(target, "oops.sql", "SELECT 1");

        MigrationException ex = assertThrows(MigrationException.class, () -> scanner.scan(target));
        assertEquals("BAD_MIGRATION_NAME", ex.code());
    }

    @Test
    @DisplayName("只有回滚脚本没有正向脚本时报错")
    void rejectsDownWithoutUp() throws IOException {
        Path target = Files.createDirectory(dir.resolve("orphan_down"));
        write(target, "U1.0.0__orphan.sql", "DROP TABLE t");

        MigrationException ex = assertThrows(MigrationException.class, () -> scanner.scan(target));
        assertEquals("MISSING_UP_SCRIPT", ex.code());
    }

    @Test
    @DisplayName("目录不存在时报明确错误")
    void rejectsMissingDir() {
        MigrationException ex = assertThrows(MigrationException.class,
                () -> scanner.scan(dir.resolve("not-exists")));
        assertEquals("MIGRATIONS_DIR_MISSING", ex.code());
    }

    @Test
    @DisplayName("版本比较按数字段")
    void comparesNumeric() {
        assertTrue(MigrationScanner.compareVersions("1.10.0", "1.9.0") > 0);
        assertTrue(MigrationScanner.compareVersions("1.0", "1.0.0") == 0);
        assertTrue(MigrationScanner.compareVersions("2.0.0", "10.0.0") < 0);
    }

    private static void write(Path dir, String name, String content) throws IOException {
        Files.writeString(dir.resolve(name), content);
    }
}
