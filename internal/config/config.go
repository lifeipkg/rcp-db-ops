package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
)

// Config 只从环境变量取值：迁移服务的凭据不落文件。
type Config struct {
	Port          string
	MigrationsDir string
	DSN           string
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Load 读取端口、迁移目录与连接串。
//
// 迁移目录的真源是部署仓 rcp-ops/migrations（容器内挂载为 /app/migrations），
// 本服务只读不写；目录是否存在由 Scanner 在扫描时判定（MIGRATIONS_DIR_MISSING）。
func Load() (Config, error) {
	cfg := Config{
		Port:          env("SERVER_PORT", "8091"),
		MigrationsDir: env("DBOPS_MIGRATIONS_DIR", "migrations"),
		DSN:           resolveDSN("postgres://risk_app:risk_app_pwd@localhost:5432/risk_control?sslmode=disable"),
	}
	if cfg.DSN == "" {
		return Config{}, fmt.Errorf("数据源未配置：给 DBOPS_DB_DSN，或给 DB_HOST/DB_PORT/DB_NAME/DB_USER/DB_PASSWORD")
	}
	if _, err := strconv.Atoi(cfg.Port); err != nil {
		return Config{}, fmt.Errorf("SERVER_PORT 不是数字: %q", cfg.Port)
	}
	return cfg, nil
}

// resolveDSN 优先级：显式 DSN > 分量拼装 > 默认值。
//
// 分量拼装让 K8s 复用既有的 rcp-db-credential Secret（username / password 两个 key），
// 不必把整条连接串塞进 Secret。
func resolveDSN(fallback string) string {
	if dsn := os.Getenv("DBOPS_DB_DSN"); dsn != "" {
		return dsn
	}
	host := os.Getenv("DB_HOST")
	if host == "" {
		return fallback
	}
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(env("DB_USER", "risk_app"), os.Getenv("DB_PASSWORD")),
		Host:   net.JoinHostPort(host, env("DB_PORT", "5432")),
		Path:   "/" + env("DB_NAME", "risk_control"),
	}
	q := u.Query()
	q.Set("sslmode", env("DB_SSLMODE", "disable"))
	u.RawQuery = q.Encode()
	return u.String()
}
