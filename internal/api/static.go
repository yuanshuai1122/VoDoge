// 内嵌前端的静态文件服务（SPA 回退）。
//
// 前端是 Next.js 静态导出，由 //go:embed 打进二进制；未命中的路径回落到
// index.html，交给客户端路由。API 路径不参与回退，否则 404 会变成一张 HTML。
package api

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleStatic(c *gin.Context) {
	requestPath := c.Request.URL.Path

	// 如果是 API 请求但未匹配到路由，返回 404
	if strings.HasPrefix(requestPath, "/api") {
		fail(c, http.StatusNotFound, "", "API 不存在")
		return
	}

	// 纯后端模式：未启用静态文件系统时，非 API 路径统一返回 404。
	if s.fs == nil {
		c.String(http.StatusNotFound, "Not Found")
		return
	}

	filePath := strings.TrimPrefix(requestPath, "/")
	if filePath == "" {
		filePath = "index.html"
	}

	// 打开候选文件；若命中目录则视为未找到，交给下面的 .html 兜底。
	//
	// Next 静态导出会为每个路由同时生成 `<route>.html` 与一个同名目录
	// （目录里只放框架内部文件）。直接按请求路径打开 `/login` 会命中那个目录，
	// 若此时就回退到 index.html，浏览器拿到的是根路由的 HTML，
	// 与当前 URL 不匹配，页面会白屏。因此必须先试 `<route>.html`。
	open := func(name string) (http.File, bool) {
		file, oerr := s.fs.Open(name)
		if oerr != nil {
			return nil, false
		}
		if st, serr := file.Stat(); serr != nil || st.IsDir() {
			file.Close()
			return nil, false
		}
		return file, true
	}

	f, ok := open(filePath)
	if !ok {
		if candidate := filePath + ".html"; !strings.HasSuffix(filePath, ".html") {
			if file, found := open(candidate); found {
				f, ok, filePath = file, true, candidate
			}
		}
	}
	if !ok {
		// 仍未命中：按 SPA 回退到 index.html，交由客户端路由处理
		filePath = "index.html"
		file, found := open(filePath)
		if !found {
			c.String(http.StatusNotFound, "Not Found")
			return
		}
		f = file
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	// 设置缓存头
	if filePath == "index.html" || filePath == "sw.js" {
		c.Header("Cache-Control", "no-cache")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
	} else if strings.HasPrefix(filePath, "_next/static/") || strings.HasPrefix(filePath, "assets/") {
		// _next/static: Next 静态导出的哈希资源；assets: 旧 Vite 构建产物
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		c.Header("Cache-Control", "public, max-age=3600")
	}

	// 设置 Content-Type
	contentType := "application/octet-stream"
	if strings.HasSuffix(filePath, ".html") {
		contentType = "text/html; charset=utf-8"
	} else if strings.HasSuffix(filePath, ".css") {
		contentType = "text/css; charset=utf-8"
	} else if strings.HasSuffix(filePath, ".js") {
		contentType = "application/javascript; charset=utf-8"
	} else if strings.HasSuffix(filePath, ".json") {
		contentType = "application/json; charset=utf-8"
	} else if strings.HasSuffix(filePath, ".webmanifest") {
		contentType = "application/manifest+json; charset=utf-8"
	} else if strings.HasSuffix(filePath, ".svg") {
		contentType = "image/svg+xml"
	} else if strings.HasSuffix(filePath, ".png") {
		contentType = "image/png"
	} else if strings.HasSuffix(filePath, ".ico") {
		contentType = "image/x-icon"
	} else if strings.HasSuffix(filePath, ".woff") {
		contentType = "font/woff"
	} else if strings.HasSuffix(filePath, ".woff2") {
		contentType = "font/woff2"
	}
	c.Header("Content-Type", contentType)

	// 使用 http.ServeContent 直接响应文件内容
	http.ServeContent(c.Writer, c.Request, filePath, stat.ModTime(), f.(io.ReadSeeker))
}
