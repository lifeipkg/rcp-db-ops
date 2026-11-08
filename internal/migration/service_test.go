package migration

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// execFunc 把函数收成 Execer，供 fake 事务注入失败。
type execFunc func(ctx context.Context, sql string, args ...any) error

func (f execFunc) Exec(ctx context.Context, sql string, args ...any) error {
	return f(ctx, sql, args...)
}

// fakeDB 同时扮演 Repo 与 TxRunner，并模拟"事务提交才生效"的记账语义。
//
// 这是本仓最关键的测试装置：真实 PostgreSQL 的事务回滚无法在没有数据库的环境里跑，
// 但"失败不记账、提交才生效"这条不变量必须被机械验证——它是本服务的核心承诺。
type fakeDB struct {
	applied      []Applied
	stagedLedger []Applied
	stagedForget []string
	committed    []string
	txCount      int
	failOn       string
	appliedErr   error
}

func (f *fakeDB) EnsureSchema(context.Context) error { return nil }

func (f *fakeDB) Applied(context.Context) ([]Applied, error) {
	if f.appliedErr != nil {
		return nil, f.appliedErr
	}
	return append([]Applied(nil), f.applied...), nil
}

func (f *fakeDB) Record(ctx context.Context, ex Execer, version, description string) error {
	if err := ex.Exec(ctx,
		"INSERT INTO schema_version(version, description) VALUES ($1, $2)", version, description); err != nil {
		return err
	}
	f.stagedLedger = append(f.stagedLedger, Applied{Version: version, Description: description, Success: true})
	return nil
}

func (f *fakeDB) Forget(ctx context.Context, ex Execer, version string) error {
	if err := ex.Exec(ctx, "DELETE FROM schema_version WHERE version = $1", version); err != nil {
		return err
	}
	f.stagedForget = append(f.stagedForget, version)
	return nil
}

func (f *fakeDB) InTx(ctx context.Context, fn func(ctx context.Context, ex Execer) error) error {
	f.txCount++
	f.stagedLedger = nil
	f.stagedForget = nil

	var statements []string
	ex := execFunc(func(_ context.Context, sql string, args ...any) error {
		if f.failOn != "" && strings.Contains(sql, f.failOn) {
			return errors.New("注入的失败")
		}
		statements = append(statements, sql)
		return nil
	})

	if err := fn(ctx, ex); err != nil {
		// 回滚：事务内的记账一并丢弃
		f.stagedLedger = nil
		f.stagedForget = nil
		return err
	}

	f.applied = append(f.applied, f.stagedLedger...)
	for _, version := range f.stagedForget {
		kept := make([]Applied, 0, len(f.applied))
		for _, item := range f.applied {
			if item.Version != version {
				kept = append(kept, item)
			}
		}
		f.applied = kept
	}
	f.committed = append(f.committed, statements...)
	f.stagedLedger = nil
	f.stagedForget = nil
	return nil
}

func (f *fakeDB) versions() []string {
	out := make([]string, 0, len(f.applied))
	for _, item := range f.applied {
		out = append(out, item.Version)
	}
	return out
}

func setup(t *testing.T) (*Service, *fakeDB, string) {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "V1.0.0__base.sql", "CREATE TABLE resource_a (event_id VARCHAR(64) PRIMARY KEY)")
	write(t, dir, "U1.0.0__base.sql", "DROP TABLE resource_a")
	write(t, dir, "V1.1.0__verdict.sql", "CREATE TABLE resource_b (event_id VARCHAR(64) PRIMARY KEY)")
	write(t, dir, "U1.1.0__verdict.sql", "DROP TABLE resource_b")
	db := &fakeDB{}
	return NewService(db, db, dir), db, dir
}

func TestMigrateAll(t *testing.T) {
	service, db, _ := setup(t)

	result, err := service.Migrate(context.Background(), "")
	if err != nil {
		t.Fatalf("全量迁移失败: %v", err)
	}
	if len(result.Applied) != 2 {
		t.Fatalf("应应用 2 个版本，得到 %d: %+v", len(result.Applied), result.Applied)
	}
	if result.CurrentVersion != "1.1.0" {
		t.Fatalf("当前版本期望 1.1.0，得到 %s", result.CurrentVersion)
	}
	for _, item := range result.Applied {
		if !item.Success {
			t.Fatalf("应用成功应记 success=true: %+v", item)
		}
	}
	if len(db.versions()) != 2 {
		t.Fatalf("台账应记 2 行，得到 %v", db.versions())
	}
	if db.txCount != 2 {
		t.Fatalf("每个版本一个事务，期望 2 次，得到 %d", db.txCount)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	service, db, _ := setup(t)
	if _, err := service.Migrate(context.Background(), ""); err != nil {
		t.Fatalf("首次迁移失败: %v", err)
	}

	second, err := service.Migrate(context.Background(), "")
	if err != nil {
		t.Fatalf("重跑失败: %v", err)
	}
	if len(second.Applied) != 0 {
		t.Fatalf("重跑不应再应用任何版本: %+v", second.Applied)
	}
	if second.Skipped != 2 {
		t.Fatalf("重跑应跳过 2 个版本，得到 %d", second.Skipped)
	}
	if second.CurrentVersion != "1.1.0" {
		t.Fatalf("当前版本期望 1.1.0，得到 %s", second.CurrentVersion)
	}
	// 幂等意味着不重复执行 DDL：事务次数不应增加
	if db.txCount != 2 {
		t.Fatalf("重跑不应开启新事务，事务次数 %d", db.txCount)
	}
}

func TestMigrateToTarget(t *testing.T) {
	service, _, _ := setup(t)

	result, err := service.Migrate(context.Background(), "1.0.0")
	if err != nil {
		t.Fatalf("迁移到 1.0.0 失败: %v", err)
	}
	if len(result.Applied) != 1 || result.CurrentVersion != "1.0.0" {
		t.Fatalf("只应应用到 1.0.0: %+v", result)
	}

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("查台账失败: %v", err)
	}
	if len(status.Pending) != 1 || status.Pending[0] != "1.1.0" {
		t.Fatalf("待应用应为 [1.1.0]，得到 %v", status.Pending)
	}
}

func TestRollbackToTarget(t *testing.T) {
	service, db, _ := setup(t)
	if _, err := service.Migrate(context.Background(), ""); err != nil {
		t.Fatalf("前置迁移失败: %v", err)
	}

	result, err := service.RollbackTo(context.Background(), "1.0.0")
	if err != nil {
		t.Fatalf("回滚失败: %v", err)
	}
	if len(result.RolledBack) != 1 || result.RolledBack[0] != "1.1.0" {
		t.Fatalf("应只回滚 1.1.0，得到 %v", result.RolledBack)
	}
	if result.CurrentVersion != "1.0.0" {
		t.Fatalf("当前版本期望 1.0.0，得到 %s", result.CurrentVersion)
	}
	if got := db.versions(); len(got) != 1 || got[0] != "1.0.0" {
		t.Fatalf("台账应只剩 1.0.0，得到 %v", got)
	}
}

func TestRollbackAll(t *testing.T) {
	service, db, _ := setup(t)
	if _, err := service.Migrate(context.Background(), ""); err != nil {
		t.Fatalf("前置迁移失败: %v", err)
	}

	result, err := service.RollbackTo(context.Background(), "")
	if err != nil {
		t.Fatalf("全量回滚失败: %v", err)
	}
	// 逆序：先 1.1.0 再 1.0.0
	if len(result.RolledBack) != 2 || result.RolledBack[0] != "1.1.0" || result.RolledBack[1] != "1.0.0" {
		t.Fatalf("回滚顺序应逆序，得到 %v", result.RolledBack)
	}
	if result.CurrentVersion != "" {
		t.Fatalf("全部回滚后当前版本应为空，得到 %s", result.CurrentVersion)
	}
	if len(db.versions()) != 0 {
		t.Fatalf("台账应为空，得到 %v", db.versions())
	}
}

func TestIrreversibleRollbackFails(t *testing.T) {
	service, db, dir := setup(t)
	write(t, dir, "V1.2.0__no_undo.sql", "CREATE TABLE audit_log (id INT)")
	if _, err := service.Migrate(context.Background(), ""); err != nil {
		t.Fatalf("前置迁移失败: %v", err)
	}

	_, err := service.RollbackTo(context.Background(), "1.1.0")
	var domain *Error
	if !errors.As(err, &domain) || domain.Code != CodeIrreversible {
		t.Fatalf("期望 %s，得到 %v", CodeIrreversible, err)
	}
	// 全量检查在动手之前：不可回滚时一行台账都不应被删
	if len(db.versions()) != 3 {
		t.Fatalf("不可回滚时不应改动台账，得到 %v", db.versions())
	}
}

func TestNothingToRollback(t *testing.T) {
	service, _, _ := setup(t)
	if _, err := service.Migrate(context.Background(), ""); err != nil {
		t.Fatalf("前置迁移失败: %v", err)
	}

	_, err := service.RollbackTo(context.Background(), "1.1.0")
	var domain *Error
	if !errors.As(err, &domain) || domain.Code != CodeNothingToRollback {
		t.Fatalf("期望 %s，得到 %v", CodeNothingToRollback, err)
	}
}

func TestMigrateFailureDoesNotRecord(t *testing.T) {
	service, db, _ := setup(t)
	// 让第二个版本的第一条语句失败：该版本必须整体回滚且不记账
	db.failOn = "CREATE TABLE resource_b"

	_, err := service.Migrate(context.Background(), "")
	var domain *Error
	if !errors.As(err, &domain) || domain.Code != CodeMigrationFailed {
		t.Fatalf("期望 %s，得到 %v", CodeMigrationFailed, err)
	}
	// 1.0.0 已成功提交，1.1.0 失败不记账 — 不存在"改了一半还说成功"
	if got := db.versions(); len(got) != 1 || got[0] != "1.0.0" {
		t.Fatalf("失败版本不应记账，台账得到 %v", got)
	}
}

func TestMigrateFailureKeepsLedgerOfPreviousVersions(t *testing.T) {
	service, db, _ := setup(t)
	db.failOn = "CREATE TABLE resource_b"
	_, _ = service.Migrate(context.Background(), "")

	// 修掉故障后重跑：只补 1.1.0，1.0.0 不重复执行
	db.failOn = ""
	result, err := service.Migrate(context.Background(), "")
	if err != nil {
		t.Fatalf("修好后的重跑应成功: %v", err)
	}
	if len(result.Applied) != 1 || result.Applied[0].Version != "1.1.0" {
		t.Fatalf("应只补 1.1.0，得到 %+v", result.Applied)
	}
	if result.Skipped != 1 {
		t.Fatalf("1.0.0 应被跳过，skipped 得到 %d", result.Skipped)
	}
}

func TestRollbackFailureKeepsLedger(t *testing.T) {
	service, db, _ := setup(t)
	if _, err := service.Migrate(context.Background(), ""); err != nil {
		t.Fatalf("前置迁移失败: %v", err)
	}
	db.failOn = "DROP TABLE resource_b"

	_, err := service.RollbackTo(context.Background(), "1.0.0")
	var domain *Error
	if !errors.As(err, &domain) || domain.Code != CodeMigrationFailed {
		t.Fatalf("期望 %s，得到 %v", CodeMigrationFailed, err)
	}
	// undo 失败的版本不能被从台账删掉：表还在，账也必须在
	if got := db.versions(); len(got) != 2 {
		t.Fatalf("回滚失败时台账应保留，得到 %v", got)
	}
}

func TestStatusReportsAppliedAndPending(t *testing.T) {
	service, _, _ := setup(t)
	if _, err := service.Migrate(context.Background(), ""); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("查台账失败: %v", err)
	}
	if status.CurrentVersion != "1.1.0" {
		t.Fatalf("当前版本期望 1.1.0，得到 %s", status.CurrentVersion)
	}
	if len(status.Applied) != 2 {
		t.Fatalf("已应用应为 2 条，得到 %d", len(status.Applied))
	}
	if status.Pending == nil {
		t.Fatal("pending 应是空数组而不是 nil：null 会让调用方以为字段缺失")
	}
}
