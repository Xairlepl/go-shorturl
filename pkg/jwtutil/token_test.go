package jwtutil

import (
	"errors"
	"testing"
	"time"
)

func TestGenerateAndParse(t *testing.T) {
	m := NewManager("test-secret", time.Hour)

	token, expiresAt, err := m.Generate(42, "alice")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if !expiresAt.After(time.Now()) {
		t.Errorf("expiresAt %v should be in the future", expiresAt)
	}

	userID, err := m.Parse(token)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if userID != 42 {
		t.Errorf("userID = %d, want 42", userID)
	}
}

func TestParseRejectsWrongSecret(t *testing.T) {
	token, _, err := NewManager("secret-a", time.Hour).Generate(1, "alice")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if _, err := NewManager("secret-b", time.Hour).Parse(token); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("err = %v, want ErrInvalidToken", err)
	}
}

func TestParseRejectsExpiredToken(t *testing.T) {
	m := NewManager("test-secret", -time.Minute) // 签发即过期
	token, _, err := m.Generate(1, "alice")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if _, err := m.Parse(token); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("err = %v, want ErrInvalidToken", err)
	}
}

func TestParseRejectsMalformedToken(t *testing.T) {
	m := NewManager("test-secret", time.Hour)
	for _, bad := range []string{"", "not-a-token", "a.b.c", "Bearer xyz"} {
		if _, err := m.Parse(bad); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("Parse(%q) err = %v, want ErrInvalidToken", bad, err)
		}
	}
}

// alg=none 是 JWT 的经典漏洞，必须被 WithValidMethods 拦截。
func TestParseRejectsNoneAlgorithm(t *testing.T) {
	m := NewManager("test-secret", time.Hour)
	// {"alg":"none","typ":"JWT"}.{"sub":"1","exp":<far future>}. (空签名)
	noneToken := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiIxIiwiZXhwIjo0MTAyNDQ0ODAwfQ."
	if _, err := m.Parse(noneToken); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("alg=none token was accepted, err = %v", err)
	}
}
