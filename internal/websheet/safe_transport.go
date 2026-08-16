package websheet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

type hostResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type dialContextFunc func(context.Context, string, string) (net.Conn, error)

func newWebsheetTransport(resolver hostResolver, dial dialContextFunc, allowPrivate bool) *http.Transport {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if dial == nil {
		d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		dial = d.DialContext
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// An environment proxy would resolve the target outside this process and
	// invalidate the checked-IP invariant.
	transport.Proxy = nil
	if allowPrivate {
		transport.DialContext = dial
	} else {
		transport.DialContext = pinnedDialContext(resolver, dial)
	}
	return transport
}

func pinnedDialContext(resolver hostResolver, dial dialContextFunc) dialContextFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid dial address %q", ErrUnsafeURL, address)
		}
		if isLocalHostname(host) {
			return nil, fmt.Errorf("%w: local host %q", ErrUnsafeURL, host)
		}

		var candidates []netip.Addr
		if literal, err := netip.ParseAddr(host); err == nil {
			candidates = append(candidates, literal.Unmap())
		} else {
			resolved, err := resolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("resolve websheet host: %w", err)
			}
			for _, item := range resolved {
				ip, ok := netip.AddrFromSlice(item.IP)
				if !ok {
					return nil, fmt.Errorf("%w: invalid address for %q", ErrUnsafeURL, host)
				}
				candidates = append(candidates, ip.Unmap())
			}
		}
		if len(candidates) == 0 {
			return nil, fmt.Errorf("%w: host %q resolved to no addresses", ErrUnsafeURL, host)
		}
		for _, ip := range candidates {
			if unsafeIP(ip) {
				return nil, fmt.Errorf("%w: private address %q", ErrUnsafeURL, ip)
			}
		}

		var dialErrs []error
		for _, ip := range candidates {
			if strings.HasSuffix(network, "4") && !ip.Is4() {
				continue
			}
			if strings.HasSuffix(network, "6") && !ip.Is6() {
				continue
			}
			conn, err := dial(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			dialErrs = append(dialErrs, err)
		}
		if len(dialErrs) == 0 {
			return nil, fmt.Errorf("no %s address available for %q", network, host)
		}
		return nil, errors.Join(dialErrs...)
	}
}
