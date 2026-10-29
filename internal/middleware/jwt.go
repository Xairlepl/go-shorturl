package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Xairlepl/go-shorturl/pkg/jwtutil"
)

// ContextUserID 是 JWT 中间件写入 gin.Context 的 key。
const ContextUserID = "user_id"

// JWTAuth 校验 Authorization: Bearer <token>，通过后注入 user_id。
func JWTAuth(m *jwtutil.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "missing authorization header"})
			return
		}

		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid authorization header"})
			return
		}

		userID, err := m.Parse(strings.TrimSpace(parts[1]))
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid or expired token"})
			return
		}

		c.Set(ContextUserID, userID)
		c.Next()
	}
}

// CurrentUserID 读取中间件注入的用户 ID。
func CurrentUserID(c *gin.Context) uint64 {
	v, ok := c.Get(ContextUserID)
	if !ok {
		return 0
	}
	id, _ := v.(uint64)
	return id
}
