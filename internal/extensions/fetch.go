package extensions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrURLRejected = errors.New("插件 URL 被拒绝")
	ErrTooLarge    = errors.New("插件包超过 64MiB")
)

func rejectURL(u *url.URL) error {
	if u == nil || u.Scheme != "https" {
		return fmt.Errorf("%w: 只允许 HTTPS", ErrURLRejected)
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return fmt.Errorf("%w: 缺少主机名", ErrURLRejected)
	}
	if ip := net.ParseIP(host); ip != nil && forbiddenIP(ip) {
		return fmt.Errorf("%w: 禁止访问内网地址", ErrURLRejected)
	}
	return nil
}

func forbiddenIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		// 100.64.0.0/10 CGNAT, 169.254.0.0 already link-local, 192.0.0.0/24 IETF
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true
		}
		if ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 0 {
			return true
		}
		return false
	}
	// IPv6 ULA fc00::/7
	if len(ip) == net.IPv6len && (ip[0]&0xfe) == 0xfc {
		return true
	}
	return false
}

func fetchHTTPS(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrURLRejected, err)
	}
	if err := rejectURL(u); err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	client := &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("%w: 重定向过多", ErrURLRejected)
			}
			return rejectURL(req.URL)
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(address)
				if err != nil {
					return nil, err
				}
				ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				if err != nil {
					return nil, err
				}
				var last error
				for _, ipa := range ips {
					if forbiddenIP(ipa.IP) {
						last = fmt.Errorf("%w: 禁止访问内网地址", ErrURLRejected)
						continue
					}
					conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ipa.IP.String(), port))
					if err != nil {
						last = err
						continue
					}
					return conn, nil
				}
				if last == nil {
					last = fmt.Errorf("%w: 主机没有可拨的地址", ErrURLRejected)
				}
				return nil, last
			},
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("下载插件失败: HTTP %d", resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrTooLarge
	}
	return data, nil
}
