package shortcode

import (
	"crypto/rand"
	"math/big"
)

const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// DefaultLength 6 位 Base62 约 568 亿种组合，足够本项目规模。
const DefaultLength = 6

// Generate 使用 crypto/rand 生成 Base62 短码，避免可预测的顺序码。
func Generate(n int) (string, error) {
	if n <= 0 {
		n = DefaultLength
	}
	max := big.NewInt(int64(len(alphabet)))
	buf := make([]byte, n)
	for i := range buf {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		buf[i] = alphabet[idx.Int64()]
	}
	return string(buf), nil
}

// IsValid 校验短码是否只包含 Base62 字符。
func IsValid(code string) bool {
	if code == "" || len(code) > 16 {
		return false
	}
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		default:
			return false
		}
	}
	return true
}
