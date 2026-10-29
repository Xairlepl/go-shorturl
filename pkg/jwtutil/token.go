package jwtutil

import (
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken 覆盖签名错误、过期、格式非法等所有校验失败场景。
var ErrInvalidToken = errors.New("invalid token")

// Manager 负责 JWT 的签发与校验。
type Manager struct {
	secret []byte
	ttl    time.Duration
}

func NewManager(secret string, ttl time.Duration) *Manager {
	return &Manager{secret: []byte(secret), ttl: ttl}
}

// Generate 以 user id 作为 subject 签发 token。
func (m *Manager) Generate(userID uint64, username string) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(m.ttl)
	claims := jwt.MapClaims{
		"sub":  strconv.FormatUint(userID, 10),
		"name": username,
		"iat":  now.Unix(),
		"exp":  expiresAt.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

// Parse 校验签名与有效期，返回 user id。
func (m *Manager) Parse(tokenString string) (uint64, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return m.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !token.Valid {
		return 0, ErrInvalidToken
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, ErrInvalidToken
	}
	sub, err := claims.GetSubject()
	if err != nil {
		return 0, ErrInvalidToken
	}
	userID, err := strconv.ParseUint(sub, 10, 64)
	if err != nil {
		return 0, ErrInvalidToken
	}
	return userID, nil
}
