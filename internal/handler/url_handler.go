package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Xairlepl/go-shorturl/internal/middleware"
	"github.com/Xairlepl/go-shorturl/internal/service"
)

// URLHandler 处理短链接管理接口与跳转。
type URLHandler struct {
	svc *service.URLService
}

func NewURLHandler(svc *service.URLService) *URLHandler {
	return &URLHandler{svc: svc}
}

type createURLRequest struct {
	URL string `json:"url" binding:"required,max=2048"`
}

func (h *URLHandler) Create(c *gin.Context) {
	var req createURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	res, err := h.svc.Create(c.Request.Context(), middleware.CurrentUserID(c), req.URL)
	switch {
	case errors.Is(err, service.ErrInvalidURL):
		c.JSON(http.StatusBadRequest, gin.H{"error": "url must be a valid http(s) address"})
		return
	case errors.Is(err, service.ErrCodeConflict):
		_ = c.Error(err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to allocate short code, please retry"})
		return
	case err != nil:
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusCreated, res)
}

func (h *URLHandler) List(c *gin.Context) {
	page := atoiDefault(c.Query("page"), 1, 1, 1<<20)
	size := atoiDefault(c.Query("page_size"), 20, 1, 100)

	list, total, err := h.svc.List(c.Request.Context(), middleware.CurrentUserID(c), size, (page-1)*size)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     list,
		"total":     total,
		"page":      page,
		"page_size": size,
	})
}

func (h *URLHandler) Stats(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	stats, err := h.svc.Stats(c.Request.Context(), middleware.CurrentUserID(c), id)
	switch {
	case errors.Is(err, service.ErrURLNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "short url not found"})
		return
	case err != nil:
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, stats)
}

func (h *URLHandler) Delete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	err := h.svc.Delete(c.Request.Context(), middleware.CurrentUserID(c), id)
	switch {
	case errors.Is(err, service.ErrURLNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "short url not found"})
		return
	case err != nil:
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.Status(http.StatusNoContent)
}

// Redirect 是核心读路径：解析短码后立即 302，统计异步进行。
func (h *URLHandler) Redirect(c *gin.Context) {
	code := c.Param("code")

	target, err := h.svc.Resolve(c.Request.Context(), code)
	switch {
	case errors.Is(err, service.ErrURLNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "short url not found"})
		return
	case err != nil:
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	h.svc.RecordVisit(c.Request.Context(), code, c.ClientIP(), c.Request.UserAgent(), c.Request.Referer())
	c.Redirect(http.StatusFound, target)
}

func parseID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, false
	}
	return id, true
}

func atoiDefault(raw string, def, lo, hi int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < lo || n > hi {
		return def
	}
	return n
}
