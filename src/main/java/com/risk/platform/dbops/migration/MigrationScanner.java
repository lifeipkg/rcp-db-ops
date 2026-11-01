package com.risk.platform.dbops.migration;

import org.springframework.stereotype.Component;

import java.io.IOException;
import java.io.UncheckedIOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import java.util.stream.Stream;

/**
 * 扫描迁移目录，装配成有序的迁移单元。
 *
 * <p>命名约定：<code>V&lt;版本&gt;__&lt;描述&gt;.sql</code> 为正向，<code>U&lt;版本&gt;__&lt;描述&gt;.sql</code> 为回滚。
 * 版本按数字段比较（1.10.0 > 1.9.0），不是字符串比较。命名不合约定直接报错，不做猜测。
 */
@Component
public class MigrationScanner {

    private static final Pattern UP_PATTERN =
            Pattern.compile("^V(\\d+(?:\\.\\d+)*)__(.+)\\.sql$");
    private static final Pattern DOWN_PATTERN =
            Pattern.compile("^U(\\d+(?:\\.\\d+)*)__(.+)\\.sql$");

    public List<MigrationFile> scan(Path directory) {
        if (!Files.isDirectory(directory)) {
            throw new MigrationException("MIGRATIONS_DIR_MISSING",
                    "迁移目录不存在: " + directory.toAbsolutePath());
        }
        Map<String, String[]> pending = new LinkedHashMap<>();
        List<Path> files;
        try (Stream<Path> stream = Files.list(directory)) {
            files = stream.filter(Files::isRegularFile).sorted().toList();
        } catch (IOException ex) {
            throw new UncheckedIOException(ex);
        }

        for (Path file : files) {
            String name = file.getFileName().toString();
            Matcher up = UP_PATTERN.matcher(name);
            Matcher down = DOWN_PATTERN.matcher(name);
            if (up.matches()) {
                pending.computeIfAbsent(up.group(1), k -> new String[3])[1] = up.group(2);
                pending.get(up.group(1))[0] = read(file);
            } else if (down.matches()) {
                pending.computeIfAbsent(down.group(1), k -> new String[3])[2] = read(file);
            } else if (name.endsWith(".sql")) {
                throw new MigrationException("BAD_MIGRATION_NAME",
                        "迁移文件命名不合约定（应为 V<版本>__<描述>.sql）: " + name);
            }
        }

        List<MigrationFile> migrations = new ArrayList<>();
        pending.forEach((version, parts) -> {
            if (parts[0] == null) {
                throw new MigrationException("MISSING_UP_SCRIPT",
                        "版本 " + version + " 只有回滚脚本，没有正向脚本");
            }
            migrations.add(new MigrationFile(version, parts[1], parts[0], parts[2]));
        });
        migrations.sort(Comparator.comparing(MigrationFile::version, MigrationScanner::compareVersions));
        return migrations;
    }

    private static String read(Path file) {
        try {
            return Files.readString(file);
        } catch (IOException ex) {
            throw new UncheckedIOException(ex);
        }
    }

    /** 数字段比较：1.10.0 > 1.9.0，缺失段按 0 处理。 */
    public static int compareVersions(String left, String right) {
        String[] a = left.split("\\.");
        String[] b = right.split("\\.");
        int length = Math.max(a.length, b.length);
        for (int i = 0; i < length; i++) {
            int av = i < a.length ? Integer.parseInt(a[i]) : 0;
            int bv = i < b.length ? Integer.parseInt(b[i]) : 0;
            if (av != bv) {
                return Integer.compare(av, bv);
            }
        }
        return 0;
    }
}
