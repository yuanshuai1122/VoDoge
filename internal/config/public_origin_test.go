package config

import (
	"strings"
	"testing"
)

func TestNormalizePublicOrigin(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: ""},
		{name: "trim and canonicalize", raw: "  HTTPS://VODOGE.COM:443  ", want: "https://vodoge.com"},
		{name: "custom port", raw: "http://VODOGE.COM:8080", want: "http://vodoge.com:8080"},
		{name: "IPv6", raw: "https://[2001:DB8::1]:8443", want: "https://[2001:db8::1]:8443"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizePublicOrigin(tt.raw)
			if err != nil {
				t.Fatalf("NormalizePublicOrigin(%q) error = %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizePublicOrigin(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestNormalizePublicOriginRejectsNonOrigins(t *testing.T) {
	invalid := []string{
		"vodoge.com",
		"//vodoge.com",
		"ftp://vodoge.com",
		"https://",
		"https://user:password@vodoge.com",
		"https://vodoge.com/",
		"https://vodoge.com/path",
		"https://vodoge.com?query=value",
		"https://vodoge.com?",
		"https://vodoge.com#fragment",
		"https://vodoge.com#",
		"https://vodoge.com:",
		"https://vodoge.com:0",
		"https://vodoge.com:65536",
	}
	for _, raw := range invalid {
		t.Run(raw, func(t *testing.T) {
			if got, err := NormalizePublicOrigin(raw); err == nil {
				t.Fatalf("NormalizePublicOrigin(%q) = %q, want error", raw, got)
			}
		})
	}
}

func TestLoadPublicOrigins(t *testing.T) {
	path := writeTempConfig(t, "server:\n  port: 7575\n  public_url: https://VODOGE.COM:443\n  plugin_public_url: https://PLUGINS.VODOGE.COM:443\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.PublicURL != "https://vodoge.com" {
		t.Fatalf("PublicURL = %q", cfg.Server.PublicURL)
	}
	if cfg.Server.PluginPublicURL != "https://plugins.vodoge.com" {
		t.Fatalf("PluginPublicURL = %q", cfg.Server.PluginPublicURL)
	}
}

func TestLoadRejectsSamePublicOrigin(t *testing.T) {
	path := writeTempConfig(t, "server:\n  public_url: https://VODOGE.COM\n  plugin_public_url: https://vodoge.com:443\n")
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "different origins") {
		t.Fatalf("Load() error = %v, want distinct-origin error", err)
	}
}
