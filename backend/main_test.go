package main

import (
	"testing"
)

func TestMaskPassword(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"postgres://user:pass@host:5432/db", "postgres://user:%2A%2A%2A%2A@host:5432/db"},
		{"postgres://user:pass:extra@host:5432/db", "postgres://user:%2A%2A%2A%2A@host:5432/db"},
		{"http://google.com", "http://google.com"},
	}

	for _, tt := range tests {
		result := maskPassword(tt.url)
		if result != tt.expected {
			t.Errorf("maskPassword(%s) = %s; want %s", tt.url, result, tt.expected)
		}
	}
}
