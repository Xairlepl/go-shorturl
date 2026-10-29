package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Xairlepl/go-shorturl/internal/model"
	"github.com/Xairlepl/go-shorturl/internal/repository"
	"github.com/Xairlepl/go-shorturl/pkg/jwtutil"
)

var (
	ErrUsernameTaken      = errors.New("username already taken")
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrInvalidInput       = errors.New("invalid input")
)

// AuthService 处理注册与登录。
type AuthService struct {
	users *repository.UserRepository
	jwt   *jwtutil.Manager
}

func NewAuthService(users *repository.UserRepository, jwt *jwtutil.Manager) *AuthService {
	return &AuthService{users: users, jwt: jwt}
}

// TokenResult 登录/注册成功后返回给调用方的凭证。
type TokenResult struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	UserID    uint64    `json:"user_id"`
	Username  string    `json:"username"`
}

func (s *AuthService) Register(ctx context.Context, username, password string) (*TokenResult, error) {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 || len(password) < 6 || len(password) > 72 {
		return nil, ErrInvalidInput
	}

	exists, err := s.users.ExistsByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrUsernameTaken
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	u := &model.User{Username: username, PasswordHash: string(hash)}
	if err := s.users.Create(ctx, u); err != nil {
		// 并发注册同名用户时唯一索引会拦截，转换成业务错误。
		if isDuplicateKey(err) {
			return nil, ErrUsernameTaken
		}
		return nil, err
	}

	return s.issue(u)
}

func (s *AuthService) Login(ctx context.Context, username, password string) (*TokenResult, error) {
	u, err := s.users.FindByUsername(ctx, strings.TrimSpace(username))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// 统一错误信息，避免暴露用户名是否存在。
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}

	return s.issue(u)
}

func (s *AuthService) issue(u *model.User) (*TokenResult, error) {
	token, expiresAt, err := s.jwt.Generate(u.ID, u.Username)
	if err != nil {
		return nil, err
	}
	return &TokenResult{
		Token:     token,
		ExpiresAt: expiresAt,
		UserID:    u.ID,
		Username:  u.Username,
	}, nil
}

func isDuplicateKey(err error) bool {
	return err != nil && strings.Contains(err.Error(), "1062")
}
