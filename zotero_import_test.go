package main

import (
	"testing"
)

func TestClassifyURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/user/repo", "C2"},
		{"https://news.ycombinator.com/item?id=123", "C16"},
		{"https://arxiv.org/abs/2301.00001", "C15"},
		{"https://www.nytimes.com/article", "C10"},
		{"https://quran.com/1", "C14"},
		{"https://youtube.com/watch?v=abc", "C5"},
		{"https://thingiverse.com/thing:12345", "C1"},
		{"https://un.org/en/rights", "C25"},
		{"https://sdf.org/", "C17"},
		{"https://plan9.io/doc", "C24"},
		{"https://random-site.example.com/page", ""},
	}

	for _, tt := range tests {
		got := classifyURL(tt.url)
		if got != tt.want {
			t.Errorf("classifyURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com/page", "https://example.com/page"},
		{"https://example.com/page/", "https://example.com/page"},
		{"HTTPS://EXAMPLE.COM/Page", "https://example.com/page"},
	}

	for _, tt := range tests {
		got := normalizeURL(tt.input)
		if got != tt.want {
			t.Errorf("normalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short: got %q", got)
	}
	if got := truncate("hello world", 5); got != "he..." {
		t.Errorf("truncate long: got %q", got)
	}
	if got := truncate("", 5); got != "" {
		t.Errorf("truncate empty: got %q", got)
	}
}
