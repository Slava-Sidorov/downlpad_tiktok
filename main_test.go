package main

import (
	"math/rand"
	"testing"
)

func TestNormalizeProxy(t *testing.T) {
	tests := map[string]string{
		"1.2.3.4:1080:user:pass":        "socks5://user:pass@1.2.3.4:1080",
		"user:pass@1.2.3.4:1080":        "socks5://user:pass@1.2.3.4:1080",
		"1.2.3.4:1080":                  "socks5://1.2.3.4:1080",
		"  1.2.3.4:1080  ":              "socks5://1.2.3.4:1080",
		"socks5://1.2.3.4:1080":         "socks5://1.2.3.4:1080",
		"http://user:pass@1.2.3.4:8080": "http://user:pass@1.2.3.4:8080",
		"":                              "",
		"   ":                           "",
	}

	for input, want := range tests {
		got, err := normalizeProxy(input)
		if err != nil {
			t.Fatalf("normalizeProxy(%q) unexpected error: %v", input, err)
		}
		if got != want {
			t.Fatalf("normalizeProxy(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeProxyRejectsUnsupportedFormats(t *testing.T) {
	invalid := []string{
		"not-a-valid-proxy",
		"1.2.3.4",
		"1.2.3.4:",
		":1080",
		"user:pass@1.2.3.4",
		"user@1.2.3.4:1080",
		"1.2.3.4:1080:user",
	}

	for _, input := range invalid {
		if got, err := normalizeProxy(input); err == nil {
			t.Fatalf("normalizeProxy(%q) = %q, want error", input, got)
		}
	}
}

func TestSelectProxySampleIndexes(t *testing.T) {
	tests := []struct {
		name      string
		total     int
		maxChecks int
		wantLen   int
	}{
		{name: "empty", total: 0, maxChecks: 5, wantLen: 0},
		{name: "less than max", total: 2, maxChecks: 5, wantLen: 2},
		{name: "more than max", total: 10, maxChecks: 5, wantLen: 5},
		{name: "disabled", total: 10, maxChecks: 0, wantLen: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectProxySampleIndexes(tt.total, tt.maxChecks, rand.New(rand.NewSource(1)))
			if len(got) != tt.wantLen {
				t.Fatalf("len(selectProxySampleIndexes(%d, %d)) = %d, want %d", tt.total, tt.maxChecks, len(got), tt.wantLen)
			}

			seen := make(map[int]bool, len(got))
			for _, index := range got {
				if index < 0 || index >= tt.total {
					t.Fatalf("index %d is out of range [0, %d)", index, tt.total)
				}
				if seen[index] {
					t.Fatalf("duplicate index %d", index)
				}
				seen[index] = true
			}
		})
	}
}
