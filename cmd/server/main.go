package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/Xairlepl/go-shorturl/internal/cache"
	"github.com/Xairlepl/go-shorturl/internal/config"
	"github.com/Xairlepl/go-shorturl/internal/handler"
	"github.com/Xairlepl/go-shorturl/internal/model"
	"github.com/Xairlepl/go-shorturl/internal/repository"
	"github.com/Xairlepl/go-shorturl/internal/router"
	"github.com/Xairlepl/go-shorturl/internal/service"
	"github.com/Xairlepl/go-shorturl/pkg/jwtutil"
)

func main() {
	cfg := config.Load()

	// 日志级别可通过 LOG_LEVEL 调整；压测时设为 warn 可关掉每请求的访问日志。
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	if cfg.JWTSecret == "change-me" {
		logger.Warn("JWT_SECRET is using the insecure default, set it via environment variable")
	}

	db, err := openMySQL(cfg.MySQLDSN)
	if err != nil {
		logger.Error("failed to connect mysql", "err", err)
		os.Exit(1)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	pingCtx, cancelPing := context.WithTimeout(context.Background(), 5*time.Second)
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		cancelPing()
		logger.Error("failed to connect redis", "err", err)
		os.Exit(1)
	}
	cancelPing()

	// 装配依赖：repository -> cache -> service -> handler -> router
	userRepo := repository.NewUserRepository(db)
	urlRepo := repository.NewURLRepository(db)
	c := cache.New(rdb, cfg.CacheTTL, cfg.EmptyCacheTTL)

	jwtManager := jwtutil.NewManager(cfg.JWTSecret, cfg.JWTTTL)
	authSvc := service.NewAuthService(userRepo, jwtManager)
	urlSvc := service.NewURLService(urlRepo, c, cfg.BaseURL, logger)

	// 后台协程负责访问明细落库与计数批量同步。
	workerCtx, stopWorkers := context.WithCancel(context.Background())
	waitWorkers := urlSvc.StartVisitWorker(workerCtx, cfg.VisitFlushInterval)

	engine := router.New(router.Deps{
		Auth:   handler.NewAuthHandler(authSvc),
		URL:    handler.NewURLHandler(urlSvc),
		JWT:    jwtManager,
		Logger: logger,
	})

	srv := &http.Server{
		Addr:              ":" + cfg.AppPort,
		Handler:           engine,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("server started", "addr", srv.Addr, "base_url", cfg.BaseURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	// 优雅退出：先停止接收新请求，再关闭后台协程与连接。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
	}

	stopWorkers()
	waitWorkers()

	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
	_ = rdb.Close()

	logger.Info("bye")
}

func openMySQL(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Warn),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	// 连接池参数按压测并发调整，避免高并发下频繁建连。
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetMaxIdleConns(20)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := db.AutoMigrate(&model.User{}, &model.ShortURL{}, &model.URLVisit{}); err != nil {
		return nil, err
	}
	return db, nil
}
