package shortcode

import "testing"

func TestGenerateLength(t *testing.T) {
	for _, n := range []int{1, 6, 16} {
		code, err := Generate(n)
		if err != nil {
			t.Fatalf("Generate(%d) returned error: %v", n, err)
		}
		if len(code) != n {
			t.Errorf("Generate(%d) length = %d, want %d", n, len(code), n)
		}
		if !IsValid(code) {
			t.Errorf("Generate(%d) produced invalid code %q", n, code)
		}
	}
}

func TestGenerateNonPositiveUsesDefault(t *testing.T) {
	code, err := Generate(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(code) != DefaultLength {
		t.Errorf("length = %d, want DefaultLength %d", len(code), DefaultLength)
	}
}

// 随机性不做统计学检验，只确认没有退化成固定值。
func TestGenerateIsNotConstant(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 200; i++ {
		code, err := Generate(DefaultLength)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		seen[code] = struct{}{}
	}
	if len(seen) < 190 {
		t.Errorf("only %d unique codes out of 200, generator looks degenerate", len(seen))
	}
}

func TestIsValid(t *testing.T) {
	tests := []struct {
		code string
		want bool
	}{
		{"abc123", true},
		{"ABCxyz", true},
		{"0", true},
		{"0123456789abcdef", true},
		{"", false},
		{"01234567890abcdefg", false}, // 超过 16 位
		{"abc-123", false},
		{"abc_123", false},
		{"abc 123", false},
		{"abc/../x", false},
		{"中文", false},
	}
	for _, tt := range tests {
		if got := IsValid(tt.code); got != tt.want {
			t.Errorf("IsValid(%q) = %v, want %v", tt.code, got, tt.want)
		}
	}
}
