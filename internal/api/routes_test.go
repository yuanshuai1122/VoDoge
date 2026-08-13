package api

import (
	"strings"
	"testing"
)

// 路由表存在的理由就是消灭"注册一份、白名单一份"的分叉，
// 所以白名单必须真的从表里派生，而不是又一份手抄。
func TestSSETokenWhitelistIsDerivedFromTheRouteTable(t *testing.T) {
	s := &Server{}
	paths := s.sseTokenQueryPaths()

	want := []string{
		"/api/logs/stream",
		"/api/devices/:device_id/overview/stream",
		"/api/devices/:device_id/operator_selection/scan/stream",
		"/api/devices/:device_id/esim/actions/download/stream",
	}
	for _, p := range want {
		if _, ok := paths[p]; !ok {
			t.Fatalf("路径 %s 不在 ?token= 白名单里；原生 EventSource 无法设置请求头，它会一律 401", p)
		}
	}
	if len(paths) != len(want) {
		t.Fatalf("白名单有 %d 条，期望 %d 条：%v。token 进 URL 会落入访问日志与浏览器历史，不应对非流式端点开放",
			len(paths), len(want), paths)
	}
}

// 只有流式端点才该收 query 凭证。新增路由时若顺手打开了 sseToken，这里会拦下。
func TestOnlyStreamingRoutesAcceptQueryToken(t *testing.T) {
	s := &Server{}
	for _, r := range s.routes() {
		if !r.sseToken {
			continue
		}
		if r.method != "GET" {
			t.Fatalf("%s %s 标了 sseToken，但只有 GET 的 SSE 端点需要它", r.method, r.path)
		}
		if !strings.HasSuffix(r.path, "/stream") {
			t.Fatalf("%s 标了 sseToken 却不是流式端点；token 进 URL 的代价只有 EventSource 值得付", r.path)
		}
	}
}

func TestRouteTableHasNoDuplicateMethodPathPairs(t *testing.T) {
	s := &Server{}
	seen := map[string]bool{}
	for _, r := range s.routes() {
		key := r.method + " " + r.path
		if seen[key] {
			// gin 遇到重复注册会 panic，这里给出比 panic 更明确的定位
			t.Fatalf("路由重复注册: %s", key)
		}
		seen[key] = true
	}
}

// authInHandler 是"中间件放行、handler 自己校验"，与"真的公开"是两回事。
// 混淆这两者会让 /rotateip 看起来像个无鉴权端点——它不是。
func TestRoutesOutsideAuthMiddlewareAreDeliberate(t *testing.T) {
	s := &Server{}
	open := map[string]authMode{}
	for _, r := range s.routes() {
		if r.auth != authRequired {
			open[r.method+" "+r.path] = r.auth
		}
	}

	want := map[string]authMode{
		"GET /docs":                    authNone,
		"GET /docs/assets/*filepath":   authNone,
		"GET /openapi.yaml":            authNone,
		"GET /openapi.json":            authNone,
		"POST /auth/login":             authNone,
		"OPTIONS /logs/stream":         authNone,
		"POST /rotateip":               authInHandler,
		"GET /websheets/:id":           authInHandler,
		"GET /websheets/:id/status":    authInHandler,
		"POST /websheets/:id/callback": authInHandler,
		"POST /websheets/:id/done":     authInHandler,
	}
	for _, path := range []string{"/websheets/:id/proxy", "/websheets/:id/proxy/*target"} {
		for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
			want[method+" "+path] = authInHandler
		}
	}

	for key, mode := range open {
		expected, ok := want[key]
		if !ok {
			t.Fatalf("%s 不在 authMiddleware 之下，且不在已知豁免清单里。"+
				"若确实不需要用户令牌，请在本测试里连同理由一起记下来", key)
		}
		if expected != mode {
			t.Fatalf("%s 的 authMode=%d，期望 %d", key, mode, expected)
		}
	}
	for key := range want {
		if _, ok := open[key]; !ok {
			t.Fatalf("%s 已进入 authMiddleware（或不再存在），请更新本测试", key)
		}
	}
}
