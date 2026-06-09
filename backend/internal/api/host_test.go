package api

import "testing"

func TestExtractCertbotVersion(t *testing.T) {
	tests := []struct {
		output string
		want   string
	}{
		{"certbot 5.4.0", "5.4.0"},
		{"certbot v5.10.1", "5.10.1"},
		{"certbot, version 4.9", "4.9"},
		{"installed", ""},
	}

	for _, tt := range tests {
		if got := extractCertbotVersion(tt.output); got != tt.want {
			t.Fatalf("extractCertbotVersion(%q) = %q, want %q", tt.output, got, tt.want)
		}
	}
}

func TestCertbotVersionAtLeast54(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"5.4", true},
		{"5.4.0", true},
		{"5.10", true},
		{"6.0.0", true},
		{"5.3.9", false},
		{"4.99", false},
		{"5", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := certbotVersionAtLeast(tt.version, 5, 4); got != tt.want {
			t.Fatalf("certbotVersionAtLeast(%q, 5, 4) = %v, want %v", tt.version, got, tt.want)
		}
	}
}
