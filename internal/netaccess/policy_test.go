package netaccess

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestParseAndAllow(t *testing.T) {
	if _, err := Parse(Policy{Mode: "bogus"}); err == nil {
		t.Fatal("accepted invalid mode")
	}
	if _, err := Parse(Policy{Mode: "internal", AllowedCIDRs: []string{"not-a-cidr"}}); err == nil {
		t.Fatal("accepted invalid cidr")
	}
	parsed, err := Parse(Policy{Mode: "internal", AllowedCIDRs: []string{"203.0.113.0/24", "198.51.100.7"}})
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Allowed(mustAddr(t, "192.168.2.10")) || !parsed.Allowed(mustAddr(t, "127.0.0.1")) {
		t.Fatal("internal ranges should pass")
	}
	if parsed.Allowed(mustAddr(t, "8.8.8.8")) {
		t.Fatal("public IP should fail in internal mode")
	}
	if !parsed.Allowed(mustAddr(t, "203.0.113.9")) || !parsed.Allowed(mustAddr(t, "198.51.100.7")) {
		t.Fatal("extra cidr/ip not honored")
	}
	pub, err := Parse(Policy{Mode: "public"})
	if err != nil {
		t.Fatal(err)
	}
	if !pub.Allowed(mustAddr(t, "8.8.8.8")) {
		t.Fatal("public mode should allow any IP")
	}
}

func TestClientIPTrustsHeadersOnlyWhenAsked(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "8.8.8.8:5000"
	req.Header.Set("X-Forwarded-For", "192.168.1.20, 10.0.0.1")

	plain := Default()
	if got := plain.ClientIP(req); got.String() != "8.8.8.8" {
		t.Fatalf("untrusted XFF: %s", got)
	}
	trust := Parsed{Mode: ModeInternal, TrustProxy: true}
	if got := trust.ClientIP(req); got.String() != "192.168.1.20" {
		t.Fatalf("trusted XFF: %s", got)
	}
}

func TestUnmapIPv4Mapped(t *testing.T) {
	p := Default()
	if !p.Allowed(mustAddr(t, "::ffff:192.168.1.5")) {
		t.Fatal("mapped rfc1918 should be treated as IPv4")
	}
}

func mustAddr(t *testing.T, s string) netip.Addr {
	t.Helper()
	addr, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatalf("ParseAddr(%q): %v", s, err)
	}
	return addr
}
