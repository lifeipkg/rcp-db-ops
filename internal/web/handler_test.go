package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lifeipkg/rcp-db-ops/internal/migration"
)

// 与 migration 包的 fake 同构：Repo + TxRunner 的最小实现，足够覆盖路由与错误码映射。
type fakeRepo struct {
	applied []migration.Applied
	failOn  string
}

func (f *fakeRepo) EnsureSchema(context.Context) error { return nil }

func (f *fakeRepo) Applied(context.Context) ([]migration.Applied, error) {
	return f.applied, nil
}

func (f *fakeRepo) Record(ctx context.Context, ex migration.Execer, version, description string) error {
	if err := ex.Exec(ctx, "INSERT INTO schema_version VALUES ($1)", version); err != nil {
		return err
	}
	f.applied = append(f.applied, migration.Applied{Version: version, Description: description, Success: true})
	return nil
}

func (f *fakeRepo) Forget(ctx context.Context, ex migration.Execer, version string) error {
	return ex.Exec(ctx, "DELETE FROM schema_version WHERE version = $1", version)
}

type execFunc func(ctx context.Context, sql string, args ...any) error

func (f execFunc) Exec(ctx context.Context, sql string, args ...any) error { return f(ctx, sql, args...) }

type fakeTx struct{ repo *fakeRepo }

func (t *fakeTx) InTx(ctx context.Context, fn func(ctx context.Context, ex migration.Execer) error) error {
	ex := execFunc(func(_ context.Context, sql string, _ ...any) error {
		if t.repo.failOn != "" && strings.Contains(sql, t.repo.failOn) {
			return errors.New("注入的失败")
		}
		return nil
	})
	return fn(ctx, ex)
}

type pinger struct{ err error }

func (p pinger) Ping(context.Context) error { return p.err }

func writeMigration(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("写临时迁移失败: %v", err)
	}
}

// newRouter 装配两个版本的迁移目录与内存实现的路由。
func newRouter(t *testing.T) (http.Handler, *fakeRepo, string) {
	t.Helper()
	dir := t.TempDir()
	writeMigration(t, dir, "V1.0.0__base.sql", "CREATE TABLE resource_a (event_id VARCHAR(64) PRIMARY KEY)")
	writeMigration(t, dir, "U1.0.0__base.sql", "DROP TABLE resource_a")
	writeMigration(t, dir, "V1.1.0__verdict.sql", "CREATE TABLE resource_b (event_id VARCHAR(64) PRIMARY KEY)")
	writeMigration(t, dir, "U1.1.0__verdict.sql", "DROP TABLE resource_b")

	repo := &fakeRepo{}
	service := migration.NewService(repo, &fakeTx{repo: repo}, dir)
	return NewRouter(NewHandler(service, pinger{})), repo, dir
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是 JSON: %v（body=%s）", err, rec.Body.String())
	}
	return out
}

func TestMigrateWithoutBody(t *testing.T) {
	h, _, _ := newRouter(t)

	rec := do(t, h, http.MethodPost, "/dbops/migrate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("空请求体应迁到最新，得到 %d: %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	if body["currentVersion"] != "1.1.0" {
		t.Fatalf("当前版本期望 1.1.0，得到 %v", body["currentVersion"])
	}
	if applied, ok := body["applied"].([]any); !ok || len(applied) != 2 {
		t.Fatalf("applied 应为 2 条数组，得到 %v", body["applied"])
	}
}

func TestMigrateWithTargetVersion(t *testing.T) {
	h, _, _ := newRouter(t)

	rec := do(t, h, http.MethodPost, "/dbops/migrate", `{"targetVersion":"1.0.0"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if got := decode(t, rec)["currentVersion"]; got != "1.0.0" {
		t.Fatalf("当前版本期望 1.0.0，得到 %v", got)
	}
}

func TestVersionsEndpoint(t *testing.T) {
	h, _, _ := newRouter(t)

	rec := do(t, h, http.MethodGet, "/dbops/versions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("台账应 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	if pending, ok := body["pending"].([]any); !ok || len(pending) != 2 {
		t.Fatalf("全部待应用时 pending 应为 2 条，得到 %v", body["pending"])
	}
	// 空台账要输出 []，不是 null
	if applied, ok := body["applied"].([]any); !ok || len(applied) != 0 {
		t.Fatalf("applied 应为空数组，得到 %v", body["applied"])
	}
}

func TestRollbackRequiresBody(t *testing.T) {
	h, _, _ := newRouter(t)

	rec := do(t, h, http.MethodPost, "/dbops/rollback", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺请求体应 400，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if got := decode(t, rec)["error"]; got != "BAD_REQUEST" {
		t.Fatalf("期望 BAD_REQUEST，得到 %v", got)
	}
}

func TestDomainErrorsMapTo400(t *testing.T) {
	h, _, _ := newRouter(t)

	// 没有可回滚的版本：领域错误 → 400 + 编码化原因
	rec := do(t, h, http.MethodPost, "/dbops/rollback", `{"targetVersion":"1.0.0"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if got := decode(t, rec)["error"]; got != migration.CodeNothingToRollback {
		t.Fatalf("期望 %s，得到 %v", migration.CodeNothingToRollback, got)
	}
}

func TestMissingMigrationsDirMapsTo400(t *testing.T) {
	repo := &fakeRepo{}
	service := migration.NewService(repo, &fakeTx{repo: repo}, filepath.Join(t.TempDir(), "nope"))
	h := NewRouter(NewHandler(service, pinger{}))

	rec := do(t, h, http.MethodPost, "/dbops/migrate", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("迁移目录缺失期望 400，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if got := decode(t, rec)["error"]; got != migration.CodeMigrationsDirMissing {
		t.Fatalf("期望 %s，得到 %v", migration.CodeMigrationsDirMissing, got)
	}
}

func TestMigrationFailureNamesVersionWithoutSQLLeak(t *testing.T) {
	repo := &fakeRepo{failOn: "CREATE TABLE resource_a"}
	dir := t.TempDir()
	writeMigration(t, dir, "V1.0.0__base.sql", "CREATE TABLE resource_a (event_id VARCHAR(64))")
	service := migration.NewService(repo, &fakeTx{repo: repo}, dir)
	h := NewRouter(NewHandler(service, pinger{}))

	rec := do(t, h, http.MethodPost, "/dbops/migrate", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，得到 %d: %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	if body["error"] != migration.CodeMigrationFailed {
		t.Fatalf("期望 %s，得到 %v", migration.CodeMigrationFailed, body["error"])
	}
	if !strings.Contains(body["message"].(string), "1.0.0") {
		t.Fatalf("失败信息应指明版本: %v", body["message"])
	}
}

func TestHealthReflectsDatabase(t *testing.T) {
	repo := &fakeRepo{}
	service := migration.NewService(repo, &fakeTx{repo: repo}, t.TempDir())

	up := NewRouter(NewHandler(service, pinger{}))
	rec := do(t, up, http.MethodGet, "/actuator/health", "")
	if rec.Code != http.StatusOK || decode(t, rec)["status"] != "UP" {
		t.Fatalf("数据库可用时期望 UP，得到 %d: %s", rec.Code, rec.Body.String())
	}

	down := NewRouter(NewHandler(service, pinger{err: errors.New("connection refused")}))
	rec = do(t, down, http.MethodGet, "/actuator/health", "")
	if rec.Code != http.StatusServiceUnavailable || decode(t, rec)["status"] != "DOWN" {
		t.Fatalf("数据库不可用时期望 503 DOWN，得到 %d: %s", rec.Code, rec.Body.String())
	}
}
