package config

import (
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("默认配置应能加载: %v", err)
	}
	if cfg.Port != "8091" {
		t.Fatalf("端口默认 8091，得到 %s", cfg.Port)
	}
	if cfg.MigrationsDir != "migrations" {
		t.Fatalf("迁移目录默认 migrations，得到 %s", cfg.MigrationsDir)
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("SERVER_PORT", "9191")
	t.Setenv("DBOPS_MIGRATIONS_DIR", "/app/migrations")
	t.Setenv("DBOPS_DB_DSN", "postgres://u:p@db:5432/rc")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if cfg.Port != "9191" || cfg.MigrationsDir != "/app/migrations" {
		t.Fatalf("端口/迁移目录未被覆盖: %+v", cfg)
	}
	if cfg.DSN != "postgres://u:p@db:5432/rc" {
		t.Fatalf("DSN 未被覆盖: %s", cfg.DSN)
	}
}

func TestLoadComposesDSNFromParts(t *testing.T) {
	t.Setenv("DB_HOST", "postgres")
	t.Setenv("DB_PASSWORD", "p@ss/w:rd")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if strings.Contains(cfg.DSN, "p@ss/w:rd") {
		t.Fatalf("口令未做 URL 转义: %s", cfg.DSN)
	}
	if !strings.Contains(cfg.DSN, "@postgres:5432/risk_control") {
		t.Fatalf("host/port/db 拼装不符: %s", cfg.DSN)
	}
}

func TestLoadRejectsNonNumericPort(t *testing.T) {
	t.Setenv("SERVER_PORT", "abc")
	if _, err := Load(); err == nil {
		t.Fatal("端口不是数字时应报错，不能带着坏端口启动")
	}
}
