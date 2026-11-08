// Package pg 是迁移领域接口的 PostgreSQL 实现。
//
// 领域逻辑（internal/migration）不 import 本包：扫描、排序、回滚顺序这些判定
// 可以在没有数据库的环境里被完整断言，驱动只负责执行。
package pg

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lifeipkg/rcp-db-ops/internal/migration"
)

const createLedger = `
CREATE TABLE IF NOT EXISTS schema_version (
  version      VARCHAR(32) PRIMARY KEY,
  description  VARCHAR(200) NOT NULL,
  executed_at  TIMESTAMP NOT NULL,
  success      BOOLEAN NOT NULL
)`

// executor 是 pgxpool.Pool 与 pgx.Tx 的共同方法集。
type executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// execer 把 pgx 的 Exec 收成领域接口（丢弃 CommandTag）。
type execer struct{ conn executor }

func (e execer) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := e.conn.Exec(ctx, sql, args...)
	return err
}

// Pool 同时是 Repo、TxRunner 与 Execer：迁移执行器只依赖接口，不依赖 pgx。
type Pool struct {
	pool *pgxpool.Pool
}

// Open 建连接池并 Ping 一次：连不上就启动失败。
func Open(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("连接串不可解析: %w", err)
	}
	// 迁移是一次性重活，不需要大连接池
	cfg.MaxConns = 4
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("建立连接池失败: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("数据库不可达: %w", err)
	}
	return &Pool{pool: pool}, nil
}

func (p *Pool) Close()                       { p.pool.Close() }
func (p *Pool) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

func (p *Pool) Exec(ctx context.Context, sql string, args ...any) error {
	return execer{p.pool}.Exec(ctx, sql, args...)
}

// InTx 在事务里执行 fn；fn 出错即回滚，commit 失败也返回错误。
func (p *Pool) InTx(ctx context.Context, fn func(ctx context.Context, ex migration.Execer) error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	// 回滚失败不覆盖原始错误：原始错误才是调用方需要知道的原因
	if err := fn(ctx, execer{tx}); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}
	return nil
}

func (p *Pool) EnsureSchema(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, createLedger); err != nil {
		return fmt.Errorf("建台账失败: %w", err)
	}
	return nil
}

// Applied 读台账。按数字段排序（不是字符串序）：1.10.0 要排在 1.9.0 之后。
func (p *Pool) Applied(ctx context.Context) ([]migration.Applied, error) {
	rows, err := p.pool.Query(ctx,
		"SELECT version, description, executed_at, success FROM schema_version")
	if err != nil {
		return nil, fmt.Errorf("读台账失败: %w", err)
	}
	defer rows.Close()

	applied := make([]migration.Applied, 0, 8)
	for rows.Next() {
		var item migration.Applied
		if err := rows.Scan(&item.Version, &item.Description, &item.ExecutedAt, &item.Success); err != nil {
			return nil, fmt.Errorf("台账行不可解析: %w", err)
		}
		applied = append(applied, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读台账失败: %w", err)
	}
	sort.Slice(applied, func(i, j int) bool {
		return migration.CompareVersions(applied[i].Version, applied[j].Version) < 0
	})
	return applied, nil
}

func (p *Pool) Record(ctx context.Context, ex migration.Execer, version, description string) error {
	return ex.Exec(ctx,
		"INSERT INTO schema_version(version, description, executed_at, success) VALUES ($1, $2, $3, $4)",
		version, description, time.Now().UTC(), true)
}

func (p *Pool) Forget(ctx context.Context, ex migration.Execer, version string) error {
	return ex.Exec(ctx, "DELETE FROM schema_version WHERE version = $1", version)
}
