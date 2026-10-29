package cache

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/redis/go-redis/v9"
)

// EmptyValue 缓存空对象的哨兵值，用于防止缓存穿透。
const EmptyValue = "__EMPTY__"

// ErrCacheMiss 表示 key 不存在。
var ErrCacheMiss = errors.New("cache miss")

// ErrEmptyCached 表示该 code 已被确认不存在（命中空值缓存）。
var ErrEmptyCached = errors.New("empty value cached")

// Cache 封装短链接相关的 Redis 操作。
type Cache struct {
	rdb           *redis.Client
	ttl           time.Duration
	emptyTTL      time.Duration
	visitCountKey string
}

func New(rdb *redis.Client, ttl, emptyTTL time.Duration) *Cache {
	return &Cache{
		rdb:           rdb,
		ttl:           ttl,
		emptyTTL:      emptyTTL,
		visitCountKey: "shorturl:visits",
	}
}

func urlKey(code string) string { return "shorturl:url:" + code }

// jitter 在 TTL 上叠加 ±10% 抖动，避免大量 key 同时过期造成缓存雪崩。
func (c *Cache) jitter(base time.Duration) time.Duration {
	if base <= 0 {
		return base
	}
	delta := int64(base / 10)
	if delta <= 0 {
		return base
	}
	return base + time.Duration(rand.Int63n(2*delta)-delta)
}

// GetURL 返回原始链接；未命中返回 ErrCacheMiss，命中空值返回 ErrEmptyCached。
func (c *Cache) GetURL(ctx context.Context, code string) (string, error) {
	val, err := c.rdb.Get(ctx, urlKey(code)).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrCacheMiss
	}
	if err != nil {
		return "", err
	}
	if val == EmptyValue {
		return "", ErrEmptyCached
	}
	return val, nil
}

func (c *Cache) SetURL(ctx context.Context, code, originalURL string) error {
	return c.rdb.Set(ctx, urlKey(code), originalURL, c.jitter(c.ttl)).Err()
}

// SetEmpty 写入短 TTL 的空值，抵挡对不存在 code 的重复冲击。
func (c *Cache) SetEmpty(ctx context.Context, code string) error {
	return c.rdb.Set(ctx, urlKey(code), EmptyValue, c.jitter(c.emptyTTL)).Err()
}

func (c *Cache) DeleteURL(ctx context.Context, code string) error {
	return c.rdb.Del(ctx, urlKey(code)).Err()
}

// IncrVisit 在跳转链路上只做一次 HINCRBY，写 MySQL 交给后台 flusher。
func (c *Cache) IncrVisit(ctx context.Context, code string) error {
	return c.rdb.HIncrBy(ctx, c.visitCountKey, code, 1).Err()
}

// PendingVisit 返回尚未落库的访问增量。
func (c *Cache) PendingVisit(ctx context.Context, code string) (uint64, error) {
	val, err := c.rdb.HGet(ctx, c.visitCountKey, code).Uint64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return val, nil
}

// DrainVisits 原子地取出并清空全部计数，避免与并发 HINCRBY 竞争丢失数据。
// 通过 RENAME 到临时 key 后再读取：RENAME 之后的 HINCRBY 会写入新的空 hash。
func (c *Cache) DrainVisits(ctx context.Context) (map[string]uint64, error) {
	tmpKey := fmt.Sprintf("%s:flushing:%d", c.visitCountKey, time.Now().UnixNano())

	if err := c.rdb.Rename(ctx, c.visitCountKey, tmpKey).Err(); err != nil {
		// key 不存在说明这一轮没有任何访问，属于正常情况。
		if errors.Is(err, redis.Nil) || err.Error() == "ERR no such key" {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = c.rdb.Del(context.WithoutCancel(ctx), tmpKey).Err() }()

	raw, err := c.rdb.HGetAll(ctx, tmpKey).Result()
	if err != nil {
		return nil, err
	}

	out := make(map[string]uint64, len(raw))
	for code, v := range raw {
		var n uint64
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n == 0 {
			continue
		}
		out[code] = n
	}
	return out, nil
}

// RestoreVisits 落库失败时把增量加回去，保证计数最终不丢。
func (c *Cache) RestoreVisits(ctx context.Context, code string, n uint64) error {
	return c.rdb.HIncrBy(ctx, c.visitCountKey, code, int64(n)).Err()
}
