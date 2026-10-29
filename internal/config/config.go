package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 保存服务运行所需的全部配置，全部来自环境变量。
type Config struct {
	AppPort  string
	BaseURL  string
	MySQLDSN string

	RedisAddr     string
	RedisPassword string
	RedisDB       int

	JWTSecret string
	JWTTTL    time.Duration

	// LogLevel 压测时设为 warn/error，避免每请求一条访问日志成为 I/O 瓶颈。
	LogLevel slog.Level

	CacheTTL      time.Duration
	EmptyCacheTTL time.Duration

	VisitFlushInterval time.Duration
}

// Load 读取环境变量并填充默认值，便于本地直接 go run。
func Load() *Config {
	return &Config{
		AppPort:  env("APP_PORT", "8080"),
		BaseURL:  env("BASE_URL", "http://localhost:8080"),
		MySQLDSN: env("MYSQL_DSN", "root:root@tcp(localhost:3307)/shorturl?charset=utf8mb4&parseTime=True&loc=Local"),

		RedisAddr:     env("REDIS_ADDR", "localhost:6379"),
		RedisPassword: env("REDIS_PASSWORD", ""),
		RedisDB:       envInt("REDIS_DB", 0),

		JWTSecret: env("JWT_SECRET", "change-me"),
		JWTTTL:    envDuration("JWT_TTL", 24*time.Hour),

		LogLevel: envLogLevel("LOG_LEVEL", slog.LevelInfo),

		CacheTTL:      envDuration("CACHE_TTL", time.Hour),
		EmptyCacheTTL: envDuration("EMPTY_CACHE_TTL", time.Minute),

		VisitFlushInterval: envDuration("VISIT_FLUSH_INTERVAL", 10*time.Second),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envLogLevel(key string, def slog.Level) slog.Level {
	switch strings.ToLower(os.Getenv(key)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return def
	}
}
