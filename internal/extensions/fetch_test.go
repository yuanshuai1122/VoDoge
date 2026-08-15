package extensions

import (
	"errors"
	"net"
	"net/url"
	"testing"
)

func TestRejectURL(t *testing.T) {
	cases := []string{
		"http://example.com/p.zip",
		"https://127.0.0.1/p.zip",
		"https://10.0.0.5/p.zip",
		"https://192.168.1.1/p.zip",
		"https://169.254.1.1/p.zip",
		"https://[::1]/p.zip",
		"ftp://example.com/p.zip",
	}
	for _, raw := range cases {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := rejectURL(u); !errors.Is(err, ErrURLRejected) {
			t.Fatalf("%s: err=%v", raw, err)
		}
	}
	u, _ := url.Parse("https://example.com/plugin.zip")
	if err := rejectURL(u); err != nil {
		t.Fatal(err)
	}
}

func TestForbiddenIP(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.0.1", "100.64.1.2", "::1", "fc00::1"} {
		if !forbiddenIP(net.ParseIP(s)) {
			t.Fatalf("%s should be forbidden", s)
		}
	}
	if forbiddenIP(net.ParseIP("1.1.1.1")) {
		t.Fatal("1.1.1.1 should be allowed")
	}
}
