package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// requestSessionToken 只对 SSE 白名单端点接受 ?token=。浏览器原生 EventSource
// 无法设置请求头，这些流式端点必须有 query 回退；但 token 出现在 URL 中会进入
// 访问日志与浏览器历史，因此普通端点必须继续拒绝 query 凭证。
func TestRequestSessionTokenQueryFallbackOnlyForSSERoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name    string
		route   string
		reqPath string
		wantTok string
	}{
		{
			name:    "日志流接受 query token",
			route:   "/api/logs/stream",
			reqPath: "/api/logs/stream?token=abc",
			wantTok: "abc",
		},
		{
			name:    "设备概览流接受 query token",
			route:   "/api/devices/:device_id/overview/stream",
			reqPath: "/api/devices/dev1/overview/stream?token=abc",
			wantTok: "abc",
		},
		{
			name:    "eSIM 下载流接受 query token",
			route:   "/api/devices/:device_id/esim/actions/download",
			reqPath: "/api/devices/dev1/esim/actions/download?smdp=x&token=abc",
			wantTok: "abc",
		},
		{
			name:    "运营商扫描流接受 query token",
			route:   "/api/devices/:device_id/operator_selection/scan/stream",
			reqPath: "/api/devices/dev1/operator_selection/scan/stream?token=abc",
			wantTok: "abc",
		},
		{
			name:    "普通端点拒绝 query token",
			route:   "/api/devices",
			reqPath: "/api/devices?token=abc",
			wantTok: "",
		},
		{
			name:    "删除设备拒绝 query token",
			route:   "/api/devices/:device_id",
			reqPath: "/api/devices/dev1?token=abc",
			wantTok: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{}
			var got string

			r := gin.New()
			r.GET(tc.route, func(c *gin.Context) {
				got = s.requestSessionToken(c)
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, tc.reqPath, nil)
			r.ServeHTTP(httptest.NewRecorder(), req)

			if got != tc.wantTok {
				t.Fatalf("requestSessionToken = %q, want %q", got, tc.wantTok)
			}
		})
	}
}

// Authorization 头存在时必须优先于 query，且不受白名单影响。
func TestRequestSessionTokenPrefersAuthorizationHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	s := &Server{}
	var got string

	r := gin.New()
	r.GET("/api/logs/stream", func(c *gin.Context) {
		got = s.requestSessionToken(c)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/logs/stream?token=fromquery", nil)
	req.Header.Set("Authorization", "Bearer fromheader")
	r.ServeHTTP(httptest.NewRecorder(), req)

	if got != "fromheader" {
		t.Fatalf("requestSessionToken = %q, want %q", got, "fromheader")
	}
}

// 访问日志绝不能原样记录 token：SSE 端点用 query 传凭证，而 gin 的访问日志
// 默认会把整个查询串写进 stdout（docker logs / journald）。
func TestAccessLogFormatterRedactsToken(t *testing.T) {
	cases := []struct {
		path     string
		wantOmit string
		wantHave string
	}{
		{
			path:     "/api/logs/stream?token=secret123",
			wantOmit: "secret123",
			wantHave: "token=***",
		},
		{
			path:     "/api/devices/d1/overview/stream?level=info&token=secret123",
			wantOmit: "secret123",
			wantHave: "token=***",
		},
	}

	for _, tc := range cases {
		out := accessLogFormatter(gin.LogFormatterParams{
			Method: http.MethodGet,
			Path:   tc.path,
		})

		if strings.Contains(out, tc.wantOmit) {
			t.Fatalf("access log leaked token: %q", out)
		}
		if !strings.Contains(out, tc.wantHave) {
			t.Fatalf("access log = %q, want it to contain %q", out, tc.wantHave)
		}
	}
}

// 未携带 token 的查询串必须原样保留，脱敏不能误伤其它参数。
func TestAccessLogFormatterKeepsOtherQueryParams(t *testing.T) {
	out := accessLogFormatter(gin.LogFormatterParams{
		Method: http.MethodGet,
		Path:   "/api/traffic/analysis?range=day&device_id=d1",
	})

	if !strings.Contains(out, "range=day") || !strings.Contains(out, "device_id=d1") {
		t.Fatalf("access log = %q, want query params preserved", out)
	}
}
