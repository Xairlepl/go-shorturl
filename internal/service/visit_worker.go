package service

import (
	"context"
	"errors"
	"time"

	"github.com/Xairlepl/go-shorturl/internal/model"
	"github.com/Xairlepl/go-shorturl/internal/repository"
)

// StartVisitWorker 启动两个后台协程：
//   - detail：消费缓冲区，异步写 url_visits 明细
//   - flusher：定时把 Redis 中的访问增量批量落库到 short_urls.visit_count
//
// 返回的函数会在关闭时等待两者退出（由 main 的 errgroup/WaitGroup 管理）。
func (s *URLService) StartVisitWorker(ctx context.Context, interval time.Duration) func() {
	done := make(chan struct{}, 2)

	go func() {
		defer func() { done <- struct{}{} }()
		s.consumeVisitDetails(ctx)
	}()

	go func() {
		defer func() { done <- struct{}{} }()
		s.runFlusher(ctx, interval)
	}()

	return func() {
		<-done
		<-done
	}
}

// consumeVisitDetails 把缓冲区中的明细写入 MySQL，退出前排空剩余事件。
func (s *URLService) consumeVisitDetails(ctx context.Context) {
	// idCache 缓存 code -> url_id，避免每条明细都查一次库。
	idCache := make(map[string]uint64)

	write := func(ev visitEvent) {
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()

		urlID, ok := idCache[ev.Code]
		if !ok {
			u, err := s.repo.FindByCode(writeCtx, ev.Code)
			if err != nil {
				if !errors.Is(err, repository.ErrNotFound) {
					s.logger.Warn("visit detail: lookup failed", "code", ev.Code, "err", err)
				}
				return
			}
			urlID = u.ID
			idCache[ev.Code] = urlID
		}

		v := &model.URLVisit{
			URLID:     urlID,
			IP:        ev.IP,
			UserAgent: ev.UserAgent,
			Referer:   ev.Referer,
			VisitedAt: ev.At,
		}
		if err := s.repo.CreateVisit(writeCtx, v); err != nil {
			s.logger.Warn("visit detail: insert failed", "code", ev.Code, "err", err)
		}
	}

	for {
		select {
		case ev := <-s.visits:
			write(ev)
		case <-ctx.Done():
			// 关闭前尽量排空缓冲区，减少明细丢失。
			for {
				select {
				case ev := <-s.visits:
					write(ev)
				default:
					s.logger.Info("visit detail worker stopped")
					return
				}
			}
		}
	}
}

// runFlusher 周期性地把 Redis 计数批量同步到 MySQL，关闭时再 flush 一次。
func (s *URLService) runFlusher(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.FlushVisitCounts(ctx)
		case <-ctx.Done():
			// 退出前落库，避免最后一个周期的计数丢失。
			s.FlushVisitCounts(context.WithoutCancel(ctx))
			s.logger.Info("visit flusher stopped")
			return
		}
	}
}

// FlushVisitCounts 取出 Redis 中累计的增量并写入 MySQL，失败的增量会被放回 Redis。
func (s *URLService) FlushVisitCounts(ctx context.Context) {
	flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	counts, err := s.cache.DrainVisits(flushCtx)
	if err != nil {
		s.logger.Warn("flusher: drain failed", "err", err)
		return
	}
	if len(counts) == 0 {
		return
	}

	var flushed, failed int
	for code, n := range counts {
		if err := s.repo.IncrVisitCount(flushCtx, code, n); err != nil {
			s.logger.Warn("flusher: update failed, restoring counter", "code", code, "delta", n, "err", err)
			// 落库失败就加回 Redis，下个周期重试，保证计数最终一致。
			if rerr := s.cache.RestoreVisits(flushCtx, code, n); rerr != nil {
				s.logger.Error("flusher: restore failed, counts lost", "code", code, "delta", n, "err", rerr)
			}
			failed++
			continue
		}
		flushed++
	}

	s.logger.Info("flusher: visit counts synced", "codes", flushed, "failed", failed)
}
