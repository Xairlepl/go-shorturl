package service

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/Xairlepl/go-shorturl/internal/cache"
	"github.com/Xairlepl/go-shorturl/internal/model"
	"github.com/Xairlepl/go-shorturl/internal/repository"
	"github.com/Xairlepl/go-shorturl/pkg/shortcode"
)

var (
	ErrURLNotFound  = errors.New("short url not found")
	ErrInvalidURL   = errors.New("invalid url")
	ErrCodeConflict = errors.New("failed to allocate unique code")
)

// maxCodeRetries 短码碰撞时的重试次数，依赖 code 唯一索引兜底。
const maxCodeRetries = 5

// visitBufferSize 访问明细缓冲区，写满时丢弃明细以保护跳转链路。
const visitBufferSize = 4096

// URLService 承载短链接的创建、查询与跳转解析。
type URLService struct {
	repo    *repository.URLRepository
	cache   *cache.Cache
	baseURL string
	logger  *slog.Logger

	// sf 合并同一 code 在缓存未命中时的并发回源请求，防止缓存击穿。
	sf singleflight.Group

	visits chan visitEvent

	// 热路径上的异常只累加计数，由后台 flusher 周期性汇总上报，
	// 避免高并发下逐请求打日志把日志 I/O 变成新的瓶颈。
	droppedVisits atomic.Uint64
	incrErrors    atomic.Uint64
}

// visitEvent 是投递给后台 worker 的访问明细，URLID 由 worker 按 code 解析。
type visitEvent struct {
	Code      string
	IP        string
	UserAgent string
	Referer   string
	At        time.Time
}

func NewURLService(repo *repository.URLRepository, c *cache.Cache, baseURL string, logger *slog.Logger) *URLService {
	return &URLService{
		repo:    repo,
		cache:   c,
		baseURL: strings.TrimRight(baseURL, "/"),
		logger:  logger,
		visits:  make(chan visitEvent, visitBufferSize),
	}
}

// CreateResult 创建短链接后的返回值。
type CreateResult struct {
	ID          uint64    `json:"id"`
	Code        string    `json:"code"`
	ShortURL    string    `json:"short_url"`
	OriginalURL string    `json:"original_url"`
	CreatedAt   time.Time `json:"created_at"`
}

// Create 校验 URL、生成唯一短码并回填缓存。
func (s *URLService) Create(ctx context.Context, userID uint64, originalURL string) (*CreateResult, error) {
	normalized, err := normalizeURL(originalURL)
	if err != nil {
		return nil, err
	}

	var created *model.ShortURL
	for i := 0; i < maxCodeRetries; i++ {
		code, err := shortcode.Generate(shortcode.DefaultLength)
		if err != nil {
			return nil, err
		}

		u := &model.ShortURL{UserID: userID, Code: code, OriginalURL: normalized}
		err = s.repo.Create(ctx, u)
		if err == nil {
			created = u
			break
		}
		if !isDuplicateKey(err) {
			return nil, err
		}
		s.logger.Warn("short code collision, retrying", "code", code, "attempt", i+1)
	}
	if created == nil {
		return nil, ErrCodeConflict
	}

	// 缓存失败不影响主流程，下次读取会自然重建。
	if err := s.cache.SetURL(ctx, created.Code, created.OriginalURL); err != nil {
		s.logger.Warn("failed to warm cache after create", "code", created.Code, "err", err)
	}

	return &CreateResult{
		ID:          created.ID,
		Code:        created.Code,
		ShortURL:    s.baseURL + "/" + created.Code,
		OriginalURL: created.OriginalURL,
		CreatedAt:   created.CreatedAt,
	}, nil
}

// Resolve 是跳转链路的核心：Redis 命中直接返回，未命中经 singleflight 回源。
func (s *URLService) Resolve(ctx context.Context, code string) (string, error) {
	if !shortcode.IsValid(code) {
		return "", ErrURLNotFound
	}

	target, err := s.cache.GetURL(ctx, code)
	switch {
	case err == nil:
		return target, nil
	case errors.Is(err, cache.ErrEmptyCached):
		return "", ErrURLNotFound
	case !errors.Is(err, cache.ErrCacheMiss):
		// Redis 故障时降级直接查库，保证可用性。
		s.logger.Warn("cache read failed, falling back to db", "code", code, "err", err)
	}

	// 同一 code 的并发未命中只放一个请求进 MySQL。
	// 用 WithoutCancel 脱离首个请求的生命周期，避免它断开时连带拖垮共享结果的其他请求。
	sfCtx := context.WithoutCancel(ctx)
	val, err, _ := s.sf.Do(code, func() (any, error) {
		loadCtx, cancel := context.WithTimeout(sfCtx, 3*time.Second)
		defer cancel()
		return s.loadFromDB(loadCtx, code)
	})
	if err != nil {
		return "", err
	}
	return val.(string), nil
}

func (s *URLService) loadFromDB(ctx context.Context, code string) (string, error) {
	u, err := s.repo.FindByCode(ctx, code)
	if errors.Is(err, repository.ErrNotFound) {
		if err := s.cache.SetEmpty(ctx, code); err != nil {
			s.logger.Warn("failed to cache empty value", "code", code, "err", err)
		}
		return "", ErrURLNotFound
	}
	if err != nil {
		return "", err
	}

	if err := s.cache.SetURL(ctx, u.Code, u.OriginalURL); err != nil {
		s.logger.Warn("failed to backfill cache", "code", code, "err", err)
	}
	return u.OriginalURL, nil
}

// RecordVisit 只做一次 Redis INCR，并把明细投递到缓冲区，不阻塞跳转响应。
func (s *URLService) RecordVisit(ctx context.Context, code, ip, userAgent, referer string) {
	if err := s.cache.IncrVisit(ctx, code); err != nil {
		// 只累加计数，不在热路径上打日志：Redis 故障时逐请求打日志会自己变成瓶颈。
		s.incrErrors.Add(1)
	}

	select {
	case s.visits <- visitEvent{
		Code:      code,
		IP:        truncate(ip, 45),
		UserAgent: truncate(userAgent, 512),
		Referer:   truncate(referer, 512),
		At:        time.Now(),
	}:
	default:
		// 缓冲区满时丢弃明细，保护主链路；聚合计数仍然准确。
		// 同样不打日志，由后台 flusher 周期性汇总上报丢弃量。
		s.droppedVisits.Add(1)
	}
}

// List 返回当前用户的短链接列表，附带 Redis 中尚未落库的增量。
func (s *URLService) List(ctx context.Context, userID uint64, limit, offset int) ([]map[string]any, int64, error) {
	list, total, err := s.repo.ListByUser(ctx, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	out := make([]map[string]any, 0, len(list))
	for _, u := range list {
		pending, err := s.cache.PendingVisit(ctx, u.Code)
		if err != nil {
			s.logger.Warn("failed to read pending visits", "code", u.Code, "err", err)
		}
		out = append(out, map[string]any{
			"id":           u.ID,
			"code":         u.Code,
			"short_url":    s.baseURL + "/" + u.Code,
			"original_url": u.OriginalURL,
			"visit_count":  u.VisitCount + pending,
			"created_at":   u.CreatedAt,
		})
	}
	return out, total, nil
}

// Stats 返回单条短链接的统计信息。
func (s *URLService) Stats(ctx context.Context, userID, id uint64) (map[string]any, error) {
	u, err := s.repo.FindByIDAndUser(ctx, id, userID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrURLNotFound
	}
	if err != nil {
		return nil, err
	}

	pending, err := s.cache.PendingVisit(ctx, u.Code)
	if err != nil {
		s.logger.Warn("failed to read pending visits", "code", u.Code, "err", err)
	}
	detailCount, err := s.repo.CountVisits(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"id":              u.ID,
		"code":            u.Code,
		"short_url":       s.baseURL + "/" + u.Code,
		"original_url":    u.OriginalURL,
		"visit_count":     u.VisitCount + pending,
		"persisted_count": u.VisitCount,
		"pending_count":   pending,
		"detail_records":  detailCount,
		"created_at":      u.CreatedAt,
	}, nil
}

// Delete 先删库再失效缓存；缓存删除失败只记日志，不回滚事务。
func (s *URLService) Delete(ctx context.Context, userID, id uint64) error {
	u, err := s.repo.FindByIDAndUser(ctx, id, userID)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrURLNotFound
	}
	if err != nil {
		return err
	}

	if err := s.repo.DeleteByIDAndUser(ctx, id, userID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrURLNotFound
		}
		return err
	}

	if err := s.cache.DeleteURL(ctx, u.Code); err != nil {
		s.logger.Warn("failed to invalidate cache after delete", "code", u.Code, "err", err)
	}
	return nil
}

// normalizeURL 只允许 http/https，防止 javascript: 等协议造成的开放重定向滥用。
func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 {
		return "", ErrInvalidURL
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", ErrInvalidURL
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", ErrInvalidURL
	}
	if parsed.Host == "" {
		return "", ErrInvalidURL
	}
	return parsed.String(), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
