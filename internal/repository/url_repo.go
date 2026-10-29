package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/Xairlepl/go-shorturl/internal/model"
)

// URLRepository 短链接与访问明细的数据访问。
type URLRepository struct {
	db *gorm.DB
}

func NewURLRepository(db *gorm.DB) *URLRepository {
	return &URLRepository{db: db}
}

func (r *URLRepository) Create(ctx context.Context, u *model.ShortURL) error {
	return r.db.WithContext(ctx).Create(u).Error
}

// FindByCode 跳转链路的回源查询，走 code 唯一索引。
func (r *URLRepository) FindByCode(ctx context.Context, code string) (*model.ShortURL, error) {
	var u model.ShortURL
	err := r.db.WithContext(ctx).Where("code = ?", code).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *URLRepository) FindByIDAndUser(ctx context.Context, id, userID uint64) (*model.ShortURL, error) {
	var u model.ShortURL
	err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ListByUser 分页返回用户的短链接，按创建时间倒序。
func (r *URLRepository) ListByUser(ctx context.Context, userID uint64, limit, offset int) ([]model.ShortURL, int64, error) {
	var (
		list  []model.ShortURL
		total int64
	)
	q := r.db.WithContext(ctx).Model(&model.ShortURL{}).Where("user_id = ?", userID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("id DESC").Limit(limit).Offset(offset).Find(&list).Error
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *URLRepository) DeleteByIDAndUser(ctx context.Context, id, userID uint64) error {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.ShortURL{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// IncrVisitCount 由后台 flusher 批量调用，把 Redis 累计的增量加到 MySQL。
func (r *URLRepository) IncrVisitCount(ctx context.Context, code string, delta uint64) error {
	return r.db.WithContext(ctx).Model(&model.ShortURL{}).
		Where("code = ?", code).
		UpdateColumn("visit_count", gorm.Expr("visit_count + ?", delta)).Error
}

func (r *URLRepository) CreateVisit(ctx context.Context, v *model.URLVisit) error {
	return r.db.WithContext(ctx).Create(v).Error
}

func (r *URLRepository) CountVisits(ctx context.Context, urlID uint64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.URLVisit{}).Where("url_id = ?", urlID).Count(&n).Error
	return n, err
}
