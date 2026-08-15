// 按来源 IP 限制谁能打开管理面。
//
// 默认 internal：回环、RFC1918、链路本地、IPv6 ULA。
// public：不限制来源。额外 CIDR 在两种模式下都放行。
// 默认不看 X-Forwarded-For，避免客户端伪造内网地址绕过。
package netaccess

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

const (
	ModeInternal = "internal"
	ModePublic   = "public"
)

type Policy struct {
	Mode              string   `json:"mode"`
	AllowedCIDRs      []string `json:"allowed_cidrs"`
	TrustProxyHeaders bool     `json:"trust_proxy_headers"`
}

type Parsed struct {
	Mode       string
	CIDRs      []netip.Prefix
	TrustProxy bool
}

var builtinInternal = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fc00::/7"),
}

func Default() Parsed {
	return Parsed{Mode: ModeInternal}
}

func NormalizeMode(in string) string {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "", ModeInternal, "lan", "private":
		return ModeInternal
	case ModePublic, "open", "any":
		return ModePublic
	default:
		return strings.ToLower(strings.TrimSpace(in))
	}
}

func Parse(p Policy) (Parsed, error) {
	mode := NormalizeMode(p.Mode)
	if mode != ModeInternal && mode != ModePublic {
		return Parsed{}, fmt.Errorf("mode 必须是 %s 或 %s", ModeInternal, ModePublic)
	}
	out := Parsed{Mode: mode, TrustProxy: p.TrustProxyHeaders}
	for _, raw := range p.AllowedCIDRs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		prefix, err := parseCIDROrIP(raw)
		if err != nil {
			return Parsed{}, err
		}
		out.CIDRs = append(out.CIDRs, prefix)
	}
	return out, nil
}

func parseCIDROrIP(raw string) (netip.Prefix, error) {
	if prefix, err := netip.ParsePrefix(raw); err == nil {
		return prefix.Masked(), nil
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Prefix{}, errors.New("无效的 CIDR 或 IP: " + raw)
	}
	bits := 32
	if addr.Is6() {
		bits = 128
	}
	return netip.PrefixFrom(addr, bits), nil
}

func (p Parsed) Allowed(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	addr = addr.Unmap()
	if p.Mode == ModePublic {
		return true
	}
	if addr.IsLoopback() {
		return true
	}
	for _, prefix := range builtinInternal {
		if prefix.Contains(addr) {
			return true
		}
	}
	for _, prefix := range p.CIDRs {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func (p Parsed) ClientIP(r *http.Request) netip.Addr {
	if r == nil {
		return netip.Addr{}
	}
	if p.TrustProxy {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
			if addr, err := netip.ParseAddr(first); err == nil {
				return addr.Unmap()
			}
		}
		if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
			if addr, err := netip.ParseAddr(realIP); err == nil {
				return addr.Unmap()
			}
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

func (p Parsed) CIDRStrings() []string {
	if len(p.CIDRs) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(p.CIDRs))
	for _, c := range p.CIDRs {
		out = append(out, c.String())
	}
	return out
}

func (p Parsed) Policy() Policy {
	return Policy{
		Mode:              p.Mode,
		AllowedCIDRs:      p.CIDRStrings(),
		TrustProxyHeaders: p.TrustProxy,
	}
}
