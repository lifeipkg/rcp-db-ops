package migration

import (
	"context"
	"sort"
	"time"
)

// Execer 是一条语句的执行面：数据库连接池与事务都实现它。
//
// 台账写入走同一个 Execer（不是另开连接）：记账必须与表变更在同一个事务里，
// 否则会出现"表改了但没记"或"记了但表没改"。
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) error
}

// TxRunner 用一个事务包住 fn：fn 返回错误即整体回滚。
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, ex Execer) error) error
}

// Repo 是 schema_version 台账的读写面。
type Repo interface {
	EnsureSchema(ctx context.Context) error
	Applied(ctx context.Context) ([]Applied, error)
	Record(ctx context.Context, ex Execer, version, description string) error
	Forget(ctx context.Context, ex Execer, version string) error
}

// Applied 是台账里的一行。
type Applied struct {
	Version     string    `json:"version"`
	Description string    `json:"description"`
	ExecutedAt  time.Time `json:"executedAt"`
	Success     bool      `json:"success"`
}

// MigrateResult 对应契约 /dbops/migrate 的响应。
type MigrateResult struct {
	Applied        []Applied `json:"applied"`
	Skipped        int       `json:"skipped"`
	CurrentVersion string    `json:"currentVersion"`
}

// RollbackResult 对应契约 /dbops/rollback 的响应。
type RollbackResult struct {
	RolledBack     []string `json:"rolledBack"`
	CurrentVersion string   `json:"currentVersion"`
}

// StatusResult 对应契约 /dbops/versions 的响应。
type StatusResult struct {
	CurrentVersion string    `json:"currentVersion"`
	Applied        []Applied `json:"applied"`
	Pending        []string  `json:"pending"`
}

// Service 是迁移编排：扫描 → 比对台账 → 按序执行 / 逆序回滚。
type Service struct {
	repo Repo
	tx   TxRunner
	dir  string
}

func NewService(repo Repo, tx TxRunner, migrationsDir string) *Service {
	return &Service{repo: repo, tx: tx, dir: migrationsDir}
}

// Migrate 按版本顺序应用未执行的迁移；target 为空表示迁到最新。
//
// 每个版本一个事务：失败即整体回滚且不记账，不留"表改了一半、台账说成功"的半激活状态。
func (s *Service) Migrate(ctx context.Context, target string) (MigrateResult, error) {
	if err := s.repo.EnsureSchema(ctx); err != nil {
		return MigrateResult{}, err
	}
	files, err := Scan(s.dir)
	if err != nil {
		return MigrateResult{}, err
	}
	applied, err := s.repo.Applied(ctx)
	if err != nil {
		return MigrateResult{}, err
	}

	appliedSet := make(map[string]struct{}, len(applied))
	versions := make([]string, 0, len(applied)+len(files))
	for _, item := range applied {
		appliedSet[item.Version] = struct{}{}
		versions = append(versions, item.Version)
	}

	newly := make([]Applied, 0, len(files))
	skipped := 0
	for _, file := range files {
		if _, done := appliedSet[file.Version]; done {
			skipped++
			continue
		}
		if target != "" && CompareVersions(file.Version, target) > 0 {
			skipped++
			continue
		}
		executedAt := time.Now().UTC()
		if err := s.applyOne(ctx, file); err != nil {
			// 已成功的版本保留在台账里，失败的版本整体回滚：返回值只报错误，
			// 不返回"部分成功"的结果，避免调用方误以为可以继续。
			return MigrateResult{}, err
		}
		appliedSet[file.Version] = struct{}{}
		versions = append(versions, file.Version)
		newly = append(newly, Applied{
			Version:     file.Version,
			Description: file.Description,
			ExecutedAt:  executedAt,
			Success:     true,
		})
	}
	return MigrateResult{Applied: newly, Skipped: skipped, CurrentVersion: maxVersion(versions)}, nil
}

// RollbackTo 逆序执行 undo 脚本，回滚到 target（不含 target 本身）；target 为空表示全部回滚。
//
// 先全量检查可回滚性再动手：跑到一半才发现某个版本缺 undo，会留下比不回滚更难收拾的中间态。
func (s *Service) RollbackTo(ctx context.Context, target string) (RollbackResult, error) {
	if err := s.repo.EnsureSchema(ctx); err != nil {
		return RollbackResult{}, err
	}
	files, err := Scan(s.dir)
	if err != nil {
		return RollbackResult{}, err
	}
	applied, err := s.repo.Applied(ctx)
	if err != nil {
		return RollbackResult{}, err
	}

	appliedSet := make(map[string]struct{}, len(applied))
	versions := make([]string, 0, len(applied))
	for _, item := range applied {
		appliedSet[item.Version] = struct{}{}
		versions = append(versions, item.Version)
	}

	toRollback := make([]File, 0, len(files))
	for _, file := range files {
		if _, done := appliedSet[file.Version]; !done {
			continue
		}
		if target == "" || CompareVersions(file.Version, target) > 0 {
			toRollback = append(toRollback, file)
		}
	}
	if len(toRollback) == 0 {
		return RollbackResult{}, newError(CodeNothingToRollback,
			"没有可回滚的版本（目标版本: %q）", target)
	}
	for _, file := range toRollback {
		if !file.Reversible() {
			return RollbackResult{}, newError(CodeIrreversible,
				"版本 %s 没有回滚脚本（U%s__*.sql），不可回滚", file.Version, file.Version)
		}
	}

	sort.Slice(toRollback, func(i, j int) bool {
		return CompareVersions(toRollback[i].Version, toRollback[j].Version) > 0
	})

	rolledBack := make([]string, 0, len(toRollback))
	for _, file := range toRollback {
		current := file
		err := s.tx.InTx(ctx, func(ctx context.Context, ex Execer) error {
			for _, statement := range SplitStatements(current.DownSQL) {
				if err := ex.Exec(ctx, statement); err != nil {
					return err
				}
			}
			return s.repo.Forget(ctx, ex, current.Version)
		})
		if err != nil {
			return RollbackResult{}, &Error{
				Code:    CodeMigrationFailed,
				Message: "版本 " + current.Version + " 回滚失败，已回滚该版本: " + err.Error(),
				Err:     err,
			}
		}
		delete(appliedSet, current.Version)
		for i, version := range versions {
			if version == current.Version {
				versions = append(versions[:i], versions[i+1:]...)
				break
			}
		}
		rolledBack = append(rolledBack, current.Version)
	}
	return RollbackResult{RolledBack: rolledBack, CurrentVersion: maxVersion(versions)}, nil
}

// Status 返回当前版本、已应用台账与待应用版本。
func (s *Service) Status(ctx context.Context) (StatusResult, error) {
	if err := s.repo.EnsureSchema(ctx); err != nil {
		return StatusResult{}, err
	}
	files, err := Scan(s.dir)
	if err != nil {
		return StatusResult{}, err
	}
	applied, err := s.repo.Applied(ctx)
	if err != nil {
		return StatusResult{}, err
	}

	appliedSet := make(map[string]struct{}, len(applied))
	versions := make([]string, 0, len(applied))
	for _, item := range applied {
		appliedSet[item.Version] = struct{}{}
		versions = append(versions, item.Version)
	}

	pending := make([]string, 0, len(files))
	for _, file := range files {
		if _, done := appliedSet[file.Version]; !done {
			pending = append(pending, file.Version)
		}
	}
	if applied == nil {
		// 台账为空时输出 []，不是 null：null 会被调用方当成"字段缺失"
		applied = []Applied{}
	}
	return StatusResult{
		CurrentVersion: maxVersion(versions),
		Applied:        applied,
		Pending:        pending,
	}, nil
}

func (s *Service) applyOne(ctx context.Context, file File) error {
	err := s.tx.InTx(ctx, func(ctx context.Context, ex Execer) error {
		for _, statement := range SplitStatements(file.UpSQL) {
			if err := ex.Exec(ctx, statement); err != nil {
				return err
			}
		}
		return s.repo.Record(ctx, ex, file.Version, file.Description)
	})
	if err != nil {
		return &Error{
			Code:    CodeMigrationFailed,
			Message: "版本 " + file.Version + " 执行失败，已回滚该版本: " + err.Error(),
			Err:     err,
		}
	}
	return nil
}
