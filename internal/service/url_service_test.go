package service

import "testing"

func TestNormalizeURLAccepts(t *testing.T) {
	tests := []string{
		"http://example.com",
		"https://example.com/path?q=1#frag",
		"  https://example.com/with-spaces  ",
		"HTTPS://example.com",
	}
	for _, raw := range tests {
		if _, err := normalizeURL(raw); err != nil {
			t.Errorf("normalizeURL(%q) returned error: %v", raw, err)
		}
	}
}

func TestNormalizeURLRejects(t *testing.T) {
	tests := []string{
		"",
		"   ",
		"javascript:alert(1)",      // XSS / 开放重定向滥用
		"data:text/html,<script>",  // 同上
		"file:///etc/passwd",       // 本地文件
		"ftp://example.com",        // 非 http(s) 协议
		"example.com",              // 缺少 scheme
		"/relative/path",           // 相对路径
		"http://",                  // 缺少 host
		string(make([]byte, 2049)), // 超长
	}
	for _, raw := range tests {
		if _, err := normalizeURL(raw); err == nil {
			t.Errorf("normalizeURL(%q) = nil error, want ErrInvalidURL", raw)
		}
	}
}

func TestNormalizeURLTrimsWhitespace(t *testing.T) {
	got, err := normalizeURL("  https://example.com/a  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://example.com/a" {
		t.Errorf("got %q, want %q", got, "https://example.com/a")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abcdef", 3); got != "abc" {
		t.Errorf("truncate = %q, want %q", got, "abc")
	}
	if got := truncate("ab", 5); got != "ab" {
		t.Errorf("truncate = %q, want %q", got, "ab")
	}
}
