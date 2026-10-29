package router

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Xairlepl/go-shorturl/internal/handler"
	"github.com/Xairlepl/go-shorturl/internal/middleware"
	"github.com/Xairlepl/go-shorturl/pkg/jwtutil"
)

// Deps 汇总路由装配所需的依赖。
type Deps struct {
	Auth   *handler.AuthHandler
	URL    *handler.URLHandler
	JWT    *jwtutil.Manager
	Logger *slog.Logger
}

// New 装配全部路由：/healthz、/api/v1/* 与根路径的 /:code 跳转。
func New(d Deps) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.Logger(d.Logger), middleware.Recovery(d.Logger), middleware.CORS())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := r.Group("/api/v1")
	{
		auth := api.Group("/auth")
		auth.POST("/register", d.Auth.Register)
		auth.POST("/login", d.Auth.Login)

		urls := api.Group("/urls", middleware.JWTAuth(d.JWT))
		urls.POST("", d.URL.Create)
		urls.GET("", d.URL.List)
		urls.DELETE("/:id", d.URL.Delete)
		urls.GET("/:id/stats", d.URL.Stats)
	}

	// 根路径的短码跳转放在最后，避免与 /api、/healthz 冲突。
	r.GET("/:code", d.URL.Redirect)

	return r
}
