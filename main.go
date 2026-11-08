package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lifeipkg/rcp-db-ops/internal/config"
	"github.com/lifeipkg/rcp-db-ops/internal/migration"
	"github.com/lifeipkg/rcp-db-ops/internal/pg"
	"github.com/lifeipkg/rcp-db-ops/internal/web"
)

// 数据库操作服务：版本化 DDL 的唯一执行者（RL-004）。
//
// 迁移真源是部署仓的 migrations/ 目录；本服务只负责按版本顺序执行、记账，
// 并在有 undo 脚本时支持回滚。
func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[db-ops] 配置不可用: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pg.Open(ctx, cfg.DSN)
	if err != nil {
		log.Fatalf("[db-ops] %v", err)
	}
	defer pool.Close()

	service := migration.NewService(pool, pool, cfg.MigrationsDir)
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           web.NewRouter(web.NewHandler(service, pool)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		// 迁移是长事务：写超时给足，避免大表 DDL 被连接超时打断
		WriteTimeout: 10 * time.Minute,
		IdleTimeout:  60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		log.Printf("[db-ops] listening on :%s, migrations=%s", cfg.Port, cfg.MigrationsDir)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[db-ops] 监听失败: %v", err)
		}
	}()

	<-stop
	// 迁移中途被强杀会留下未事务化的中间态，因此这里不设短超时：等它自己收尾
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[db-ops] 优雅退出超时: %v", err)
	}
	log.Print("[db-ops] stopped")
}
