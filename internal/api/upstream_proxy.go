package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodog/internal/db"
	"github.com/yuanshuai1122/vodog/internal/upstreamproxy"
)

// ── 前置代理管理 API（主服务） ──

func normalizeUpstreamProxyPayload(existing *db.UpstreamProxy, req db.UpstreamProxy) db.UpstreamProxy {
	out := req
	out.ID = strings.TrimSpace(out.ID)
	out.Name = strings.TrimSpace(out.Name)
	out.Addr = strings.TrimSpace(out.Addr)
	out.Username = strings.TrimSpace(out.Username)
	out.Password = strings.TrimSpace(out.Password)

	if existing != nil {
		out.CreatedAt = existing.CreatedAt
		if out.Password == "" {
			out.Password = existing.Password
		}
	}
	return out
}

func probeUpstreamProxyConfig(c *gin.Context, proxy db.UpstreamProxy) (upstreamproxy.ProbeResult, error) {
	return upstreamproxy.ProbeSOCKS5(c.Request.Context(), upstreamproxy.ProbeConfig{
		ProxyAddr: proxy.Addr,
		Username:  proxy.Username,
		Password:  proxy.Password,
		Timeout:   5 * time.Second,
	})
}

// handleListUpstreamProxies 获取所有前置代理实例
func (s *Server) handleListUpstreamProxies(c *gin.Context) {
	proxies, err := s.data().UpstreamProxy.List()
	if err != nil {
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	// 密码脱敏
	for i := range proxies {
		proxies[i].Password = maskSecret(proxies[i].Password)
	}
	respondOK(c, proxies)
}

// handleCreateUpstreamProxy 创建前置代理实例
func (s *Server) handleCreateUpstreamProxy(c *gin.Context) {
	var req db.UpstreamProxy
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "参数解析失败: "+err.Error())
		return
	}
	req = normalizeUpstreamProxyPayload(nil, req)
	if req.ID == "" {
		fail(c, http.StatusBadRequest, "", "id 不能为空")
		return
	}
	if req.Addr == "" {
		fail(c, http.StatusBadRequest, "", "addr 不能为空")
		return
	}
	result, probeErr := probeUpstreamProxyConfig(c, req)
	if probeErr != nil {
		failWith(c, http.StatusBadGateway, "", "前置代理探测失败: "+result.FailureSummary(), gin.H{
			"result": result,
		})
		return
	}
	if err := s.data().UpstreamProxy.Upsert(req); err != nil {
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	respondOKWith(c, result, gin.H{
		"message": "前置代理已保存，并已通过探测",
	})
}

// handleUpdateUpstreamProxy 更新前置代理实例
func (s *Server) handleUpdateUpstreamProxy(c *gin.Context) {
	id := upstreamProxyIDParam(c)
	var req db.UpstreamProxy
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "参数解析失败: "+err.Error())
		return
	}
	existing, err := s.data().UpstreamProxy.Get(id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	if existing == nil {
		fail(c, http.StatusNotFound, "", "前置代理不存在")
		return
	}
	req.ID = id
	req = normalizeUpstreamProxyPayload(existing, req)
	if req.Addr == "" {
		fail(c, http.StatusBadRequest, "", "addr 不能为空")
		return
	}
	result, probeErr := probeUpstreamProxyConfig(c, req)
	if probeErr != nil {
		failWith(c, http.StatusBadGateway, "", "前置代理探测失败: "+result.FailureSummary(), gin.H{
			"result": result,
		})
		return
	}
	if err := s.data().UpstreamProxy.Upsert(req); err != nil {
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	respondOKWith(c, result, gin.H{
		"message": "前置代理已更新，并已通过探测",
	})
}

// handleDeleteUpstreamProxy 删除前置代理实例
func (s *Server) handleDeleteUpstreamProxy(c *gin.Context) {
	id := upstreamProxyIDParam(c)
	if err := s.data().UpstreamProxy.Delete(id); err != nil {
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	respondOKWith(c, nil, gin.H{"message": "前置代理已删除"})
}

// handleProbeUpstreamProxy 探测前置代理是否支持标准 Socks5 + UDP Associate。
func (s *Server) handleProbeUpstreamProxy(c *gin.Context) {
	id := upstreamProxyIDParam(c)
	proxy, err := s.data().UpstreamProxy.Get(id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	if proxy == nil {
		fail(c, http.StatusNotFound, "", "前置代理不存在")
		return
	}

	result, probeErr := upstreamproxy.ProbeSOCKS5(c.Request.Context(), upstreamproxy.ProbeConfig{
		ProxyAddr: proxy.Addr,
		Username:  proxy.Username,
		Password:  proxy.Password,
		Timeout:   5 * time.Second,
	})
	if probeErr != nil {
		failWith(c, http.StatusBadGateway, "", "前置代理探测失败: "+result.FailureSummary(), gin.H{
			"result": result,
		})
		return
	}

	respondOKWith(c, result, gin.H{
		"message": "前置代理探测成功",
	})
}

type upstreamProxyCountryRuleResponse struct {
	CountryCode     string    `json:"country_code"`
	CountryName     string    `json:"country_name"`
	MCCs            []string  `json:"mccs"`
	UpstreamProxyID string    `json:"upstream_proxy_id"`
	Enabled         bool      `json:"enabled"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func buildUpstreamProxyCountryRuleResponse(rule db.UpstreamProxyCountryRule) upstreamProxyCountryRuleResponse {
	display := upstreamproxy.CountryRuleDisplay(rule.CountryCode)
	return upstreamProxyCountryRuleResponse{
		CountryCode:     display.CountryCode,
		CountryName:     display.CountryName,
		MCCs:            display.MCCs,
		UpstreamProxyID: strings.TrimSpace(rule.UpstreamProxyID),
		Enabled:         rule.Enabled,
		UpdatedAt:       rule.UpdatedAt,
	}
}

func (s *Server) handleListUpstreamProxyCountries(c *gin.Context) {
	if !upstreamproxy.CountryTableReady() {
		fail(c, http.StatusServiceUnavailable, "", "mcc_mnc_table_unavailable")
		return
	}
	respondOK(c, upstreamproxy.ListCountryDisplays())
}

func (s *Server) handleListUpstreamProxyCountryRules(c *gin.Context) {
	rules, err := s.data().UpstreamProxy.ListCountryRules()
	if err != nil {
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	out := make([]upstreamProxyCountryRuleResponse, 0, len(rules))
	for _, rule := range rules {
		out = append(out, buildUpstreamProxyCountryRuleResponse(rule))
	}
	respondOK(c, out)
}

func (s *Server) handleUpsertUpstreamProxyCountryRule(c *gin.Context) {
	if !upstreamproxy.CountryTableReady() {
		fail(c, http.StatusServiceUnavailable, "", "mcc_mnc_table_unavailable")
		return
	}
	countryCode := upstreamproxy.NormalizeCountryCode(countryCodeParam(c))
	if _, ok := upstreamproxy.MCCsForCountryCode(countryCode); !ok {
		fail(c, http.StatusBadRequest, "", "国家代码不在 MCC/MNC 表中")
		return
	}
	var req struct {
		UpstreamProxyID string `json:"upstream_proxy_id"`
		Enabled         bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "参数解析失败: "+err.Error())
		return
	}
	proxy, err := s.data().UpstreamProxy.Get(req.UpstreamProxyID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	if proxy == nil {
		fail(c, http.StatusNotFound, "", "前置代理不存在")
		return
	}
	rule := db.UpstreamProxyCountryRule{
		CountryCode:     countryCode,
		UpstreamProxyID: proxy.ID,
		Enabled:         req.Enabled,
	}
	if err := s.data().UpstreamProxy.UpsertCountryRule(rule); err != nil {
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	rule.UpstreamProxyID = proxy.ID
	rule.CountryCode = countryCode
	respondOK(c, buildUpstreamProxyCountryRuleResponse(rule))
}

func (s *Server) handleDeleteUpstreamProxyCountryRule(c *gin.Context) {
	countryCode := upstreamproxy.NormalizeCountryCode(countryCodeParam(c))
	if err := s.data().UpstreamProxy.DeleteCountryRule(countryCode); err != nil {
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	respondOK(c, nil)
}

// maskSecret 将密码脱敏为 **** 格式
func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	return "****"
}
