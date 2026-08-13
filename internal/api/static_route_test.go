package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

// Next 静态导出会为每个路由同时产出 `<route>.html` 和一个同名目录
// （目录内只有框架内部文件）。若按请求路径打开时命中目录就直接回退 index.html，
// 浏览器会拿到根路由的 HTML 而 URL 却是 /login，页面白屏。
func TestHandleStaticPrefersRouteHTMLOverSameNamedDirectory(t *testing.T) {
	gin.SetMode(gin.TestMode)

	fsys := fstest.MapFS{
		"index.html":               {Data: []byte("ROOT")},
		"login.html":               {Data: []byte("LOGIN")},
		"login/__next._tree.txt":   {Data: []byte("internal")},
		"devices.html":             {Data: []byte("DEVICES")},
		"devices/__next._tree.txt": {Data: []byte("internal")},
		"_next/static/chunks/a.js": {Data: []byte("JS")},
	}

	s := &Server{fs: http.FS(fsys)}
	r := gin.New()
	r.NoRoute(s.handleStatic)

	cases := []struct {
		path string
		want string
	}{
		{"/", "ROOT"},
		{"/login", "LOGIN"},
		{"/devices", "DEVICES"},
		{"/login.html", "LOGIN"},
		{"/_next/static/chunks/a.js", "JS"},
		// 未知路由仍按 SPA 回退，交给客户端路由
		{"/devices/detail", "ROOT"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			if got := w.Body.String(); got != tc.want {
				t.Fatalf("body = %q, want %q", got, tc.want)
			}
		})
	}
}

// 未挂载静态资源时（纯后端模式），非 API 路径应当 404 而不是 panic。
func TestHandleStaticBackendOnlyReturns404(t *testing.T) {
	gin.SetMode(gin.TestMode)

	s := &Server{}
	r := gin.New()
	r.NoRoute(s.handleStatic)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/login", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// API 路径未匹配到路由时返回 JSON 404，不能落到静态资源回退。
func TestHandleStaticAPIPathReturnsJSON404(t *testing.T) {
	gin.SetMode(gin.TestMode)

	fsys := fstest.MapFS{"index.html": {Data: []byte("ROOT")}}
	s := &Server{fs: http.FS(fsys)}
	r := gin.New()
	r.NoRoute(s.handleStatic)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/nope", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct == "" || ct[:16] != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
}
