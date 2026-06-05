package utils

import (
	"encoding/base64"
	"testing"
)

func TestBase64Encode(t *testing.T) {
	tests := []struct {
		name     string
		s1       string
		s2       string
		expected string
	}{
		{
			name:     "simple credentials",
			s1:       "user",
			s2:       "pass",
			expected: base64.StdEncoding.EncodeToString([]byte("user:pass")),
		},
		{
			name:     "complex credentials",
			s1:       "client-id-123",
			s2:       "secret-key-456",
			expected: base64.StdEncoding.EncodeToString([]byte("client-id-123:secret-key-456")),
		},
		{
			name:     "empty strings",
			s1:       "",
			s2:       "",
			expected: base64.StdEncoding.EncodeToString([]byte(":")),
		},
		{
			name:     "special characters",
			s1:       "user@domain",
			s2:       "p@$$w0rd!",
			expected: base64.StdEncoding.EncodeToString([]byte("user@domain:p@$$w0rd!")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Base64Encode(tt.s1, tt.s2)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}

			decoded, err := base64.StdEncoding.DecodeString(result)
			if err != nil {
				t.Fatalf("failed to decode result: %v", err)
			}
			expectedDecoded := tt.s1 + ":" + tt.s2
			if string(decoded) != expectedDecoded {
				t.Errorf("decoded value '%s' doesn't match expected '%s'", string(decoded), expectedDecoded)
			}
		})
	}
}

func BenchmarkBase64Encode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Base64Encode("client-id", "client-secret")
	}
}

// ─── ParseCalVer ────────────────────────────────────────────────────────────

func TestParseCalVer_Valid(t *testing.T) {
	tests := []struct {
		input string
		want  [3]int
	}{
		{"2026.04.0", [3]int{2026, 4, 0}},
		{"2026.4.0", [3]int{2026, 4, 0}},
		{"2025.12.3", [3]int{2025, 12, 3}},
		{"2024.01.99", [3]int{2024, 1, 99}},
		{"2030.06.1", [3]int{2030, 6, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseCalVer(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseCalVer_Invalid(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"2026.04"},
		{"2026"},
		{""},
		{"2026.04.abc"},
		{"abc.04.0"},
		{"2026.xx.0"},
		{"2026.04.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := ParseCalVer(tt.input)
			if err == nil {
				t.Errorf("expected error for %q, got nil", tt.input)
			}
		})
	}
}

// ─── CompareCalVer ──────────────────────────────────────────────────────────

func TestCompareCalVer(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"2026.04.0", "2026.04.0", 0},
		{"2027.01.0", "2026.04.0", 1},
		{"2025.12.0", "2026.04.0", -1},
		{"2026.05.0", "2026.04.0", 1},
		{"2026.03.0", "2026.04.0", -1},
		{"2026.04.1", "2026.04.0", 1},
		{"2026.04.0", "2026.04.1", -1},
		{"2026.04.0", "2026.04.0", 0},
		{"2026.03.9", "2026.04.0", -1},
		{"2026.05.0", "2026.04.0", 1},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			a, err := ParseCalVer(tt.a)
			if err != nil {
				t.Fatalf("parse %q: %v", tt.a, err)
			}
			b, err := ParseCalVer(tt.b)
			if err != nil {
				t.Fatalf("parse %q: %v", tt.b, err)
			}
			got := CompareCalVer(a, b)
			if got != tt.want {
				t.Errorf("CompareCalVer(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
