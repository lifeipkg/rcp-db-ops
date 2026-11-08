package migration

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("写临时迁移失败: %v", err)
	}
}

func TestScanSortsByNumericSegment(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "V1.9.0__nine.sql", "CREATE TABLE t9 (id INT)")
	write(t, dir, "V1.10.0__ten.sql", "CREATE TABLE t10 (id INT)")

	files, err := Scan(dir)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if len(files) != 2 || files[0].Version != "1.9.0" || files[1].Version != "1.10.0" {
		t.Fatalf("按数字段排序不符: %+v", files)
	}
	if files[0].Description != "nine" || files[1].UpSQL == "" {
		t.Fatalf("描述/正向脚本装配不符: %+v", files)
	}
}

func TestScanPairsUpAndDown(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "V1.0.0__base.sql", "CREATE TABLE t (id INT)")
	write(t, dir, "U1.0.0__base.sql", "DROP TABLE t")
	write(t, dir, "V1.1.0__only_up.sql", "CREATE TABLE u (id INT)")

	files, err := Scan(dir)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if !files[0].Reversible() {
		t.Fatal("有 undo 脚本时应 reversible")
	}
	// 缺 undo 只是标记为不可回滚，不是扫描错误：可回滚性在回滚时才判定
	if files[1].Reversible() {
		t.Fatal("缺 undo 脚本时不应 reversible")
	}
}

func TestScanRejectsBadName(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "oops.sql", "SELECT 1")

	_, err := Scan(dir)
	var domain *Error
	if !errors.As(err, &domain) || domain.Code != CodeBadMigrationName {
		t.Fatalf("期望 %s，得到 %v", CodeBadMigrationName, err)
	}
}

func TestScanRejectsDownWithoutUp(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "U1.0.0__orphan.sql", "DROP TABLE t")

	_, err := Scan(dir)
	var domain *Error
	if !errors.As(err, &domain) || domain.Code != CodeMissingUpScript {
		t.Fatalf("期望 %s，得到 %v", CodeMissingUpScript, err)
	}
}

func TestScanRejectsMissingDir(t *testing.T) {
	_, err := Scan(filepath.Join(t.TempDir(), "not-exists"))
	var domain *Error
	if !errors.As(err, &domain) || domain.Code != CodeMigrationsDirMissing {
		t.Fatalf("期望 %s，得到 %v", CodeMigrationsDirMissing, err)
	}
}

func TestScanIgnoresNonSQLFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "README.md", "不是迁移")
	write(t, dir, "V1.0.0__base.sql", "SELECT 1")

	files, err := Scan(dir)
	if err != nil {
		t.Fatalf("非 .sql 文件应被忽略: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("应只装配 1 个迁移，得到 %d", len(files))
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		left, right string
		want        int
	}{
		{"1.10.0", "1.9.0", 1},
		{"1.0", "1.0.0", 0},
		{"2.0.0", "10.0.0", -1},
		{"1.0.0", "1.0.1", -1},
		{"1.0.0", "1.0.0", 0},
	}
	for _, tc := range cases {
		if got := CompareVersions(tc.left, tc.right); got != tc.want {
			t.Fatalf("%s vs %s 期望 %d，得到 %d", tc.left, tc.right, tc.want, got)
		}
	}
}

func TestSplitStatements(t *testing.T) {
	got := SplitStatements("CREATE TABLE a (id INT);\n\n  DROP TABLE b ;  ")
	if len(got) != 2 || got[0] != "CREATE TABLE a (id INT)" || got[1] != "DROP TABLE b" {
		t.Fatalf("分句不符: %#v", got)
	}
	if empty := SplitStatements("  ;  ; "); len(empty) != 0 {
		t.Fatalf("空白片段应被丢弃: %#v", empty)
	}
}

func TestMaxVersionUsesNumericCompare(t *testing.T) {
	if got := maxVersion([]string{"1.9.0", "1.10.0"}); got != "1.10.0" {
		t.Fatalf("数字段取最大失败: %s", got)
	}
	if got := maxVersion(nil); got != "" {
		t.Fatalf("空集合应返回空串，得到 %q", got)
	}
}
