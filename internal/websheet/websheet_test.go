package websheet

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreateRejectsPrivateAndNonHTTPSURLs(t *testing.T) {
	b := New(Config{})
	for _, raw := range []string{
		"http://example.com/",
		"https://127.0.0.1/",
		"https://10.0.0.1/",
		"https://192.168.1.1/",
		"file:///etc/passwd",
	} {
		if _, err := b.Create(context.Background(), Request{URL: raw}); !errors.Is(err, ErrUnsafeURL) {
			t.Fatalf("Create(%q) err=%v, want ErrUnsafeURL", raw, err)
		}
	}
}

func TestCreateAllowsPublicHTTPS(t *testing.T) {
	b := New(Config{})
	s, err := b.Create(context.Background(), Request{URL: "https://attdashboard.wireless.att.com/softphone/primary/reseller/r017"})
	if err != nil {
		t.Fatal(err)
	}
	info := s.Info()
	if info.EmbedURL == "" || info.Method != "GET" {
		t.Fatalf("info=%+v", info)
	}
}

func TestInfoEmbedURLCarriesSessionAccessToken(t *testing.T) {
	b := New(Config{})
	s, err := b.Create(context.Background(), Request{URL: "https://203.0.113.10/"})
	if err != nil {
		t.Fatal(err)
	}
	info := s.Info()
	if !strings.Contains(info.EmbedURL, "token=") {
		t.Fatalf("EmbedURL=%q, want session token query", info.EmbedURL)
	}

	validReq := httptest.NewRequest(http.MethodGet, info.EmbedURL, nil)
	if err := s.Authorize(validReq); err != nil {
		t.Fatalf("Authorize(valid token) error=%v", err)
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/api/websheets/"+info.ID, nil)
	if err := s.Authorize(missingReq); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Authorize(missing token) error=%v, want ErrUnauthorized", err)
	}

	token := infoToken(t, info)
	pathReq := httptest.NewRequest(http.MethodGet, "/api/websheets/"+info.ID+"/session/"+url.PathEscape(token)+"/proxy/https/example.com/app.js", nil)
	if err := s.Authorize(pathReq); err != nil {
		t.Fatalf("Authorize(path token) error=%v", err)
	}

	wrongPathReq := httptest.NewRequest(http.MethodGet, "/api/websheets/"+info.ID+"/session/wrong/proxy/https/example.com/app.js?token="+url.QueryEscape(token), nil)
	if err := s.Authorize(wrongPathReq); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Authorize(wrong path token with valid query) error=%v, want ErrUnauthorized", err)
	}
}

func TestPostBootstrapProxiesRawUserDataBody(t *testing.T) {
	const rawPostData = "method%3Dupdate-tc-loc%26devicetype%3Dphone%26authtoken%3DAXBCRgUM"
	var gotBody string
	var gotContentType string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><html><body>ok</body></html>")
	}))
	defer upstream.Close()

	b := New(Config{AllowPrivateHosts: true})
	s, err := b.Create(context.Background(), Request{
		URL:         upstream.URL + "/softphone/primary/reseller/r017",
		UserData:    rawPostData,
		ContentType: "application/x-www-form-urlencoded",
	})
	if err != nil {
		t.Fatal(err)
	}

	bootstrapReq := httptest.NewRequest(http.MethodGet, s.Info().EmbedURL, nil)
	bootstrapRec := httptest.NewRecorder()
	if err := s.ServeBootstrap(bootstrapRec, bootstrapReq); err != nil {
		t.Fatal(err)
	}
	action := extractFormAction(t, bootstrapRec.Body.String())

	proxyReq := httptest.NewRequest(http.MethodPost, action, nil)
	proxyReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	proxyRec := httptest.NewRecorder()
	if err := s.Proxy(proxyRec, proxyReq); err != nil {
		t.Fatal(err)
	}
	if gotBody != rawPostData {
		t.Fatalf("proxied body=%q want raw %q", gotBody, rawPostData)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type=%q want application/x-www-form-urlencoded", gotContentType)
	}
}

func TestRewriteHTMLKeepsProxyURLsRelativeToBrowserOrigin(t *testing.T) {
	b := New(Config{AllowPrivateHosts: true})
	s, err := b.Create(context.Background(), Request{URL: "https://attdashboard.wireless.att.com/softphone/primary/reseller/r017"})
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimPrefix(strings.SplitN(s.Info().EmbedURL, "token=", 2)[1], "")
	base, err := url.Parse("https://attdashboard.wireless.att.com/softphone/primary/reseller/r017")
	if err != nil {
		t.Fatal(err)
	}

	rewritten := s.rewriteHTML(
		`<html><head><base href="/softphone/"><script src="main-es2015.js"></script></head></html>`,
		base,
		token,
		true,
	)
	if strings.Contains(rewritten, "http://127.0.0.1:7575") {
		t.Fatalf("rewritten html leaked backend origin: %s", rewritten)
	}
	if !strings.Contains(rewritten, `/api/websheets/`) || !strings.Contains(rewritten, `/session/`+token+`/proxy/https/attdashboard.wireless.att.com/softphone/main-es2015.js`) {
		t.Fatalf("rewritten html missing relative proxy URL: %s", rewritten)
	}
	if strings.Contains(rewritten, "?token=") {
		t.Fatalf("rewritten html kept capability in a non-inherited query: %s", rewritten)
	}
}

func TestBridgePathPrefixCarriesCapabilityForOpaqueOriginRequests(t *testing.T) {
	b := New(Config{AllowPrivateHosts: true})
	s, err := b.Create(context.Background(), Request{URL: "https://attdashboard.wireless.att.com/softphone/primary/reseller/r017"})
	if err != nil {
		t.Fatal(err)
	}
	token := strings.SplitN(s.Info().EmbedURL, "token=", 2)[1]
	base, err := url.Parse("https://attdashboard.wireless.att.com/softphone/primary/reseller/r017")
	if err != nil {
		t.Fatal(err)
	}

	script := s.bridgeScript(token, base)
	prefix := extractJSStringConst(t, script, "absolutePathProxyPrefix")
	if !strings.Contains(prefix, "/session/"+token+"/proxy/") || strings.Contains(prefix, "?token=") {
		t.Fatalf("appendable path prefix=%q want path capability and no query token", prefix)
	}
	if !strings.Contains(script, `const websheetToken = "`) {
		t.Fatalf("bridge script missing separate websheet token: %s", script)
	}
	if !strings.Contains(script, `credentials: "omit"`) || !strings.Contains(script, "this.withCredentials = false") {
		t.Fatalf("bridge script does not disable browser credentials: %s", script)
	}
}

func TestRelativeDynamicAssetInheritsPathCapability(t *testing.T) {
	b := New(Config{AllowPrivateHosts: true})
	s, err := b.Create(context.Background(), Request{URL: "https://example.com/app/index.html"})
	if err != nil {
		t.Fatal(err)
	}
	token := infoToken(t, s.Info())
	documentURL, err := url.Parse(s.proxyURL("https://example.com/app/index.html", token))
	if err != nil {
		t.Fatal(err)
	}
	chunkURL := documentURL.ResolveReference(&url.URL{Path: "chunk.123.js"}).String()
	want := "/session/" + token + "/proxy/https/example.com/app/chunk.123.js"
	if !strings.Contains(chunkURL, want) {
		t.Fatalf("relative chunk URL=%q want inherited capability path %q", chunkURL, want)
	}
	if strings.Contains(chunkURL, "token=") {
		t.Fatalf("relative chunk URL=%q must not rely on a query capability", chunkURL)
	}
	if got := s.callbackURL(token); !strings.Contains(got, "/session/"+token+"/callback") || strings.Contains(got, "token=") {
		t.Fatalf("callback URL=%q want path capability", got)
	}
}

func TestProxyHeadersDoNotLeakLocalCredentials(t *testing.T) {
	source := http.Header{
		"Authorization":    {"Bearer management"},
		"Cookie":           {"vodoge_session=secret"},
		"X-Websheet-Token": {"capability"},
		"X-Carrier-Header": {"preserved"},
	}
	destination := make(http.Header)
	copyProxyHeaders(destination, source)
	for _, key := range []string{"Authorization", "Cookie", "X-Websheet-Token"} {
		if got := destination.Get(key); got != "" {
			t.Fatalf("%s leaked upstream as %q", key, got)
		}
	}
	if got := destination.Get("X-Carrier-Header"); got != "preserved" {
		t.Fatalf("ordinary carrier header=%q want preserved", got)
	}
}

func TestBridgeDetectsATTAddressValidationOnlyForMutationResponses(t *testing.T) {
	b := New(Config{AllowPrivateHosts: true})
	s, err := b.Create(context.Background(), Request{URL: "https://attdashboard.wireless.att.com/softphone/primary/reseller/r017"})
	if err != nil {
		t.Fatal(err)
	}
	token := strings.SplitN(s.Info().EmbedURL, "token=", 2)[1]
	base, err := url.Parse("https://attdashboard.wireless.att.com/softphone/primary/reseller/r017")
	if err != nil {
		t.Fatal(err)
	}

	script := s.bridgeScript(token, base)
	for _, marker := range []string{
		"inspectATTAddressResponse",
		"e911AddressValidated",
		`status === "validated"`,
		`method === "GET"`,
		"window.top.postMessage",
		"BroadcastChannel",
		"localStorage.setItem",
		"vodoge-websheet-complete",
	} {
		if !strings.Contains(script, marker) {
			t.Fatalf("bridge script missing %q: %s", marker, script)
		}
	}
}

func extractJSStringConst(t *testing.T, script string, name string) string {
	t.Helper()
	pattern := regexp.MustCompile(`const\s+` + regexp.QuoteMeta(name) + `\s*=\s*"([^"]*)"`)
	match := pattern.FindStringSubmatch(script)
	if len(match) != 2 {
		t.Fatalf("script missing const %s: %s", name, script)
	}
	return match[1]
}

func extractFormAction(t *testing.T, html string) string {
	t.Helper()
	match := regexp.MustCompile(`action="([^"]+)"`).FindStringSubmatch(html)
	if len(match) != 2 {
		t.Fatalf("bootstrap html missing form action: %s", html)
	}
	return match[1]
}

func infoToken(t *testing.T, info Info) string {
	t.Helper()
	parsed, err := url.Parse(info.EmbedURL)
	if err != nil {
		t.Fatal(err)
	}
	token := parsed.Query().Get("token")
	if token == "" {
		t.Fatalf("EmbedURL=%q missing token", info.EmbedURL)
	}
	return token
}

func TestSessionExpires(t *testing.T) {
	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	b := New(Config{
		TTL: time.Minute,
		Now: func() time.Time { return now },
	})
	s, err := b.Create(context.Background(), Request{URL: "https://example.com/"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := b.Get(s.Info().ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get expired err=%v, want ErrNotFound", err)
	}
}

// 承载表单在运营商域下打开，跨源既读不到内容也收不到关闭事件，
// 前端只能轮询这个快照——所以它必须在流程结束后仍然可读。
func TestSessionStatusRemainsReadableAfterDone(t *testing.T) {
	b := New(Config{})
	s, err := b.Create(context.Background(), Request{URL: "https://example.com/"})
	if err != nil {
		t.Fatal(err)
	}

	if st := s.Status(); st.Finished {
		t.Fatalf("status=%+v want not finished before any callback", st)
	}

	s.Callback(Callback{Event: "finishFlow", ResultCode: "0"})
	s.Done()

	got, err := b.Get(s.Info().ID)
	if err != nil {
		t.Fatalf("Get() after Done err=%v, want the session retained until TTL", err)
	}
	st := got.Status()
	if !st.Finished || st.Event != "finishFlow" || st.ResultCode != "0" {
		t.Fatalf("status=%+v want finished with the last callback preserved", st)
	}
}

// Status 可以被反复读取；channel 上的回调会被消费掉，不能拿来回答"到哪一步了"。
func TestSessionStatusIsRepeatable(t *testing.T) {
	b := New(Config{})
	s, err := b.Create(context.Background(), Request{URL: "https://example.com/"})
	if err != nil {
		t.Fatal(err)
	}
	s.Callback(Callback{Event: "addressValidated"})

	for i := range 3 {
		if st := s.Status(); st.Event != "addressValidated" {
			t.Fatalf("read %d: status=%+v want the callback still visible", i, st)
		}
	}
}

// 会话持有运营商站点的 cookie jar。以前靠"流程结束就删"清理，改为 TTL 回收后
// 必须确认从未被再次访问的会话也会被扫掉，否则 broker 会无限增长。
func TestBrokerSweepsExpiredSessionsNotJustTheOneBeingFetched(t *testing.T) {
	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	b := New(Config{
		TTL: time.Minute,
		Now: func() time.Time { return now },
	})
	stale, err := b.Create(context.Background(), Request{URL: "https://example.com/stale"})
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(2 * time.Minute)
	if _, err := b.Create(context.Background(), Request{URL: "https://example.com/fresh"}); err != nil {
		t.Fatal(err)
	}

	b.mu.Lock()
	_, present := b.sessions[stale.Info().ID]
	count := len(b.sessions)
	b.mu.Unlock()

	if present || count != 1 {
		t.Fatalf("sessions=%d stalePresent=%v want the expired session swept", count, present)
	}
}

func TestSendLatestNeverBlocksWhenChannelCannotAccept(t *testing.T) {
	done := make(chan struct{})
	go func() {
		sendLatest(make(chan int), 1)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sendLatest blocked on its final send")
	}
}

func TestSendLatestConcurrentSendersDoNotBlockOnFullQueue(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })

	ch := make(chan int, 1)
	ch <- -1
	start := make(chan struct{})
	const senders = 256
	var wg sync.WaitGroup
	wg.Add(senders)
	for value := range senders {
		go func() {
			defer wg.Done()
			<-start
			sendLatest(ch, value)
		}()
	}
	close(start)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("concurrent sendLatest callers blocked after another sender refilled the queue")
	}
	select {
	case <-ch:
	default:
		t.Fatal("sendLatest left the latest-value queue empty")
	}
}
