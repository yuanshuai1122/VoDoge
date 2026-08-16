package websheet

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type resolverFunc func(context.Context, string) ([]net.IPAddr, error)

func (f resolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return f(ctx, host)
}

type scriptedResolver struct {
	mu      sync.Mutex
	answers [][]net.IPAddr
	calls   int
}

func (r *scriptedResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := r.calls
	r.calls++
	if index >= len(r.answers) {
		index = len(r.answers) - 1
	}
	return append([]net.IPAddr(nil), r.answers[index]...), nil
}

func ipAnswers(values ...string) []net.IPAddr {
	answers := make([]net.IPAddr, 0, len(values))
	for _, value := range values {
		answers = append(answers, net.IPAddr{IP: net.ParseIP(value)})
	}
	return answers
}

func TestTransportRejectsDNSRebindingBeforeBaseDial(t *testing.T) {
	resolver := &scriptedResolver{answers: [][]net.IPAddr{
		ipAnswers("203.0.113.7"),
		ipAnswers("127.0.0.1"),
	}}
	baseCalls := 0
	b := New(Config{
		resolver: resolver,
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			baseCalls++
			return nil, errors.New("base dial must not run")
		},
	})
	session, err := b.Create(context.Background(), Request{URL: "https://carrier.example/start"})
	if err != nil {
		t.Fatal(err)
	}
	transport := session.client.Transport.(*http.Transport)
	if _, err := transport.DialContext(context.Background(), "tcp", "carrier.example:443"); !errors.Is(err, ErrUnsafeURL) {
		t.Fatalf("dial error=%v want ErrUnsafeURL after DNS rebinding", err)
	}
	if baseCalls != 0 {
		t.Fatalf("base dial called %d times after private resolution", baseCalls)
	}
}

func TestPinnedDialUsesOnlyCheckedLiteralAddress(t *testing.T) {
	resolver := resolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		return ipAnswers("203.0.113.7"), nil
	})
	sentinel := errors.New("stop after capture")
	var gotAddress string
	dial := pinnedDialContext(resolver, func(_ context.Context, _, address string) (net.Conn, error) {
		gotAddress = address
		return nil, sentinel
	})
	if _, err := dial(context.Background(), "tcp", "carrier.example:443"); !errors.Is(err, sentinel) {
		t.Fatalf("dial error=%v want sentinel", err)
	}
	if gotAddress != "203.0.113.7:443" || strings.Contains(gotAddress, "carrier.example") {
		t.Fatalf("base dial address=%q want checked literal 203.0.113.7:443", gotAddress)
	}
}

func TestPinnedDialRejectsAnyUnsafeAnswer(t *testing.T) {
	tests := []struct {
		name    string
		answers []net.IPAddr
	}{
		{name: "mixed public and private", answers: ipAnswers("203.0.113.7", "10.0.0.8")},
		{name: "IPv4 loopback", answers: ipAnswers("127.0.0.1")},
		{name: "IPv6 loopback", answers: ipAnswers("::1")},
		{name: "mapped IPv4 loopback", answers: ipAnswers("::ffff:127.0.0.1")},
		{name: "mapped IPv4 private", answers: ipAnswers("::ffff:10.0.0.8")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			baseCalls := 0
			dial := pinnedDialContext(resolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
				return tc.answers, nil
			}), func(context.Context, string, string) (net.Conn, error) {
				baseCalls++
				return nil, errors.New("unexpected base dial")
			})
			if _, err := dial(context.Background(), "tcp", "carrier.example:443"); !errors.Is(err, ErrUnsafeURL) {
				t.Fatalf("dial error=%v want ErrUnsafeURL", err)
			}
			if baseCalls != 0 {
				t.Fatalf("base dial called %d times", baseCalls)
			}
		})
	}
}

func TestPinnedDialSupportsPublicIPv4AndIPv6(t *testing.T) {
	tests := []struct {
		name       string
		answer     string
		network    string
		wantTarget string
	}{
		{name: "IPv4", answer: "203.0.113.7", network: "tcp4", wantTarget: "203.0.113.7:443"},
		{name: "IPv6", answer: "2001:db8::7", network: "tcp6", wantTarget: "[2001:db8::7]:443"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sentinel := errors.New("captured")
			var got string
			dial := pinnedDialContext(resolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
				return ipAnswers(tc.answer), nil
			}), func(_ context.Context, _, address string) (net.Conn, error) {
				got = address
				return nil, sentinel
			})
			if _, err := dial(context.Background(), tc.network, "carrier.example:443"); !errors.Is(err, sentinel) {
				t.Fatalf("dial error=%v want sentinel", err)
			}
			if got != tc.wantTarget {
				t.Fatalf("base dial address=%q want %q", got, tc.wantTarget)
			}
		})
	}
}

func TestRedirectPolicyRejectsUnsafeTargetsAndTenthRedirect(t *testing.T) {
	resolver := resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host == "private.example" {
			return ipAnswers("192.168.1.5"), nil
		}
		return ipAnswers("203.0.113.7"), nil
	})
	b := New(Config{resolver: resolver})
	session, err := b.Create(context.Background(), Request{URL: "https://carrier.example/start"})
	if err != nil {
		t.Fatal(err)
	}
	checks := []string{
		"https://localhost/",
		"https://127.0.0.1/",
		"https://private.example/",
		"http://carrier.example/downgrade",
	}
	for _, target := range checks {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := session.client.CheckRedirect(req, nil); !errors.Is(err, ErrUnsafeURL) {
			t.Errorf("redirect to %s error=%v want ErrUnsafeURL", target, err)
		}
	}
	safe, _ := http.NewRequest(http.MethodGet, "https://carrier.example/next", nil)
	via := make([]*http.Request, 10)
	if err := session.client.CheckRedirect(safe, via); err == nil || !strings.Contains(err.Error(), "10 redirects") {
		t.Fatalf("tenth redirect error=%v", err)
	}
}

func TestWebsheetTransportIgnoresEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9999")
	t.Setenv("https_proxy", "http://127.0.0.1:9999")
	transport := newWebsheetTransport(resolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		return ipAnswers("203.0.113.7"), nil
	}), nil, false)
	if transport.Proxy != nil {
		t.Fatal("websheet transport must not consult environment proxy settings")
	}
}

func TestAllowPrivateHostsPreservesDirectDialBehavior(t *testing.T) {
	sentinel := errors.New("captured")
	var got string
	b := New(Config{
		AllowPrivateHosts: true,
		dialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			got = address
			return nil, sentinel
		},
	})
	session, err := b.Create(context.Background(), Request{URL: "http://127.0.0.1/start"})
	if err != nil {
		t.Fatal(err)
	}
	transport := session.client.Transport.(*http.Transport)
	if _, err := transport.DialContext(context.Background(), "tcp", "127.0.0.1:80"); !errors.Is(err, sentinel) {
		t.Fatalf("dial error=%v want sentinel", err)
	}
	if got != "127.0.0.1:80" {
		t.Fatalf("AllowPrivateHosts dial address=%q want original private target", got)
	}
}
