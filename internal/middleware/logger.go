package middleware

import (
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// ContextRequestID 是 request id 在 gin.Context 中的 key。
const ContextRequestID = "request_id"

var requestSeq atomic.Uint64

// RequestID 为每个请求生成或透传 X-Request-ID，便于串联日志。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-ID")
		if rid == "" {
			rid = strconv.FormatUint(requestSeq.Add(1), 36)
		}
		c.Set(ContextRequestID, rid)
		c.Header("X-Request-ID", rid)
		c.Next()
	}
}

// Logger 输出结构化访问日志，替代 gin 默认的文本日志。
func Logger(l *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		attrs := []any{
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"latency_ms", float64(time.Since(start).Microseconds()) / 1000,
			"ip", c.ClientIP(),
			"request_id", c.GetString(ContextRequestID),
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, "errors", c.Errors.String())
			l.Error("request failed", attrs...)
			return
		}
		l.Info("request", attrs...)
	}
}

// Recovery 捕获 panic，返回 500 并记录堆栈，避免进程退出。
func Recovery(l *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, err any) {
		l.Error("panic recovered",
			"err", err,
			"path", c.Request.URL.Path,
			"request_id", c.GetString(ContextRequestID),
		)
		c.AbortWithStatusJSON(500, gin.H{"error": "internal server error"})
	})
}

// CORS 允许跨域调试，生产环境应收敛 Origin 白名单。
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Request-ID")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
