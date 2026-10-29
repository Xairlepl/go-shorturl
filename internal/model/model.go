package model

import "time"

// User 平台用户。
type User struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Username     string    `gorm:"type:varchar(64);uniqueIndex;not null" json:"username"`
	PasswordHash string    `gorm:"type:varchar(255);not null" json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ShortURL 短链接元数据，code 上有唯一索引用于跳转查询。
type ShortURL struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      uint64    `gorm:"index;not null" json:"user_id"`
	Code        string    `gorm:"type:varchar(16);uniqueIndex;not null" json:"code"`
	OriginalURL string    `gorm:"type:varchar(2048);not null" json:"original_url"`
	VisitCount  uint64    `gorm:"not null;default:0" json:"visit_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// URLVisit 访问明细，异步写入，不在跳转链路上同步落库。
type URLVisit struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	URLID     uint64    `gorm:"index;not null" json:"url_id"`
	IP        string    `gorm:"type:varchar(45)" json:"ip"`
	UserAgent string    `gorm:"type:varchar(512)" json:"user_agent"`
	Referer   string    `gorm:"type:varchar(512)" json:"referer"`
	VisitedAt time.Time `gorm:"index" json:"visited_at"`
}

func (URLVisit) TableName() string { return "url_visits" }
