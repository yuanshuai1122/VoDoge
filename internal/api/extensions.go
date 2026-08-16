package api

import (
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodoge/internal/extensions"
)

func (s *Server) plugins() *extensions.Manager {
	if s == nil {
		return nil
	}
	return s.extensions
}

func (s *Server) handleListExtensions(c *gin.Context) {
	mgr := s.plugins()
	if mgr == nil {
		respondOK(c, []extensions.Installed{})
		return
	}
	respondOK(c, mgr.List())
}

func (s *Server) handleInstallExtensionURL(c *gin.Context) {
	mgr := s.plugins()
	if mgr == nil {
		fail(c, http.StatusServiceUnavailable, "plugins_unavailable", "插件系统不可用")
		return
	}
	var req struct {
		URL    string `json:"url"`
		SHA256 string `json:"sha256"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.URL) == "" {
		fail(c, http.StatusBadRequest, "", "参数错误: 需要 url")
		return
	}
	inst, err := mgr.InstallURL(c.Request.Context(), req.URL, req.SHA256)
	if err != nil {
		failPlugin(c, err)
		return
	}
	respond(c, http.StatusCreated, inst, gin.H{"message": "插件已安装并启用"})
}

func (s *Server) handleUploadExtension(c *gin.Context) {
	mgr := s.plugins()
	if mgr == nil {
		fail(c, http.StatusServiceUnavailable, "plugins_unavailable", "插件系统不可用")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, extensionsMaxUpload)
	file, err := c.FormFile("package")
	if err != nil {
		fail(c, http.StatusBadRequest, "", "请上传字段名为 package 的插件包")
		return
	}
	f, err := file.Open()
	if err != nil {
		fail(c, http.StatusBadRequest, "", "读取插件包失败")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, extensionsMaxUpload+1))
	if err != nil {
		fail(c, http.StatusBadRequest, "", "读取插件包失败")
		return
	}
	inst, err := mgr.InstallZip(data, strings.TrimSpace(c.PostForm("sha256")))
	if err != nil {
		failPlugin(c, err)
		return
	}
	respond(c, http.StatusCreated, inst, gin.H{"message": "插件已安装并启用"})
}

func (s *Server) handleUpdateExtension(c *gin.Context) {
	mgr := s.plugins()
	if mgr == nil {
		fail(c, http.StatusServiceUnavailable, "plugins_unavailable", "插件系统不可用")
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		fail(c, http.StatusBadRequest, "", "参数错误: 需要 enabled")
		return
	}
	inst, err := mgr.SetEnabled(c.Param("id"), *req.Enabled)
	if err != nil {
		failPlugin(c, err)
		return
	}
	respondOK(c, inst)
}

func (s *Server) handleDeleteExtension(c *gin.Context) {
	mgr := s.plugins()
	if mgr == nil {
		fail(c, http.StatusServiceUnavailable, "plugins_unavailable", "插件系统不可用")
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if err := mgr.Uninstall(id); err != nil {
		failPlugin(c, err)
		return
	}
	respondOK(c, gin.H{"id": id, "removed": true})
}

func (s *Server) handlePluginAsset(c *gin.Context) {
	mgr := s.plugins()
	if mgr == nil {
		fail(c, http.StatusServiceUnavailable, "plugins_unavailable", "插件系统不可用")
		return
	}
	rel := strings.TrimPrefix(c.Param("filepath"), "/")
	path, err := mgr.AssetPath(c.Param("id"), rel)
	if err != nil {
		failPlugin(c, err)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			failPlugin(c, extensions.ErrAssetNotFound)
			return
		}
		fail(c, http.StatusInternalServerError, "plugin_asset_unavailable", "读取插件资源失败")
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || stat.IsDir() {
		failPlugin(c, extensions.ErrAssetNotFound)
		return
	}
	http.ServeContent(c.Writer, c.Request, filepath.Base(path), stat.ModTime(), file)
}

func (s *Server) handleExtensionBackend(c *gin.Context) {
	mgr := s.plugins()
	if mgr == nil {
		fail(c, http.StatusServiceUnavailable, "plugins_unavailable", "插件系统不可用")
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	addr, err := mgr.BackendAddr(id)
	if err != nil {
		failPlugin(c, err)
		return
	}
	target, err := url.Parse("http://" + addr)
	if err != nil {
		fail(c, http.StatusBadGateway, "plugin_backend_bad_addr", "插件后端地址无效")
		return
	}
	rest := c.Param("filepath")
	if rest == "" {
		rest = "/"
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = func(r *http.Request) {
		r.URL.Scheme = target.Scheme
		r.URL.Host = target.Host
		r.URL.Path = rest
		r.URL.RawPath = rest
		r.Host = target.Host
		r.Header.Del("Authorization")
		r.Header.Del("Cookie")
		r.Header.Del("X-CSRF-Token")
		if id != "" {
			r.Header.Set("X-VoDoge-Plugin-ID", id)
		}
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Del("Set-Cookie")
		return nil
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}

const extensionsMaxUpload = 64<<20 + 512<<10

func failPlugin(c *gin.Context, err error) {
	switch {
	case errors.Is(err, extensions.ErrAlreadyInstalled):
		fail(c, http.StatusConflict, "plugin_already_installed", err.Error())
	case errors.Is(err, extensions.ErrNotFound):
		fail(c, http.StatusNotFound, "plugin_not_found", err.Error())
	case errors.Is(err, extensions.ErrPluginDisabled):
		fail(c, http.StatusConflict, "plugin_disabled", err.Error())
	case errors.Is(err, extensions.ErrBackendDown):
		fail(c, http.StatusServiceUnavailable, "plugin_backend_unavailable", err.Error())
	case errors.Is(err, extensions.ErrChecksum):
		fail(c, http.StatusBadRequest, "plugin_checksum_mismatch", err.Error())
	case errors.Is(err, extensions.ErrTooLarge):
		fail(c, http.StatusBadRequest, "plugin_too_large", err.Error())
	case errors.Is(err, extensions.ErrURLRejected):
		fail(c, http.StatusBadRequest, "plugin_url_rejected", err.Error())
	case errors.Is(err, extensions.ErrUnsafePath), errors.Is(err, extensions.ErrInvalidManifest):
		fail(c, http.StatusBadRequest, "plugin_invalid", err.Error())
	case errors.Is(err, extensions.ErrAssetNotFound):
		fail(c, http.StatusNotFound, "plugin_asset_not_found", err.Error())
	default:
		fail(c, http.StatusBadRequest, "plugin_invalid", err.Error())
	}
}
