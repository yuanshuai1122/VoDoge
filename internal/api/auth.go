// 登录、会话令牌与鉴权中间件。
//
// 令牌是 HMAC(过期时间戳, 登录口令) 的自签名形式，没有服务端会话表——
// 因此改口令会让所有既有令牌立即失效，这是有意的。
package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yuanshuai1122/vodoge/internal/config"

	"github.com/yuanshuai1122/vodoge/pkg/logger"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type loginAttempt struct {
	Count   int
	ResetAt time.Time
}

// checkPassword 验证密码，支持 bcrypt 哈希和明文（向后兼容）
// stored 是存储的密码（可能是哈希或明文），input 是用户输入的明文密码
func checkPassword(stored, input string) bool {
	// 如果存储的密码以 $2a$ 或 $2b$ 开头，说明是 bcrypt 哈希
	if strings.HasPrefix(stored, "$2a$") || strings.HasPrefix(stored, "$2b$") {
		err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(input))
		return err == nil
	}
	// 向后兼容：明文密码对比
	return stored == input
}

func (s *Server) issueSessionToken() (string, time.Time, error) {
	exp := time.Now().Add(30 * 24 * time.Hour) // 有效期 30 天
	expStr := strconv.FormatInt(exp.Unix(), 10)

	h := hmac.New(sha256.New, []byte(s.auth.Password))
	h.Write([]byte(expStr))
	sig := hex.EncodeToString(h.Sum(nil))

	tokenRaw := expStr + "." + sig
	token := base64.StdEncoding.EncodeToString([]byte(tokenRaw))

	return token, exp, nil
}

func (s *Server) allowLoginAttempt(ip string, now time.Time) bool {
	if ip == "" {
		ip = "unknown"
	}
	window := 2 * time.Minute
	limit := 10

	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	cur := s.loginAttempts[ip]
	if cur.ResetAt.IsZero() || now.After(cur.ResetAt) {
		cur = loginAttempt{Count: 0, ResetAt: now.Add(window)}
	}
	if cur.Count >= limit {
		s.loginAttempts[ip] = cur
		return false
	}
	cur.Count++
	s.loginAttempts[ip] = cur
	return true
}

func (s *Server) handleLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "参数错误")
		return
	}

	clientIP := c.ClientIP()
	if !s.allowLoginAttempt(clientIP, time.Now()) {
		fail(c, http.StatusTooManyRequests, "rate_limited", "登录尝试过于频繁，请稍后再试")
		return
	}

	if req.Username == s.auth.Username && checkPassword(s.auth.Password, req.Password) {
		token, exp, err := s.issueSessionToken()
		if err != nil {
			logger.Error("生成登录 token 失败", "err", err)
			fail(c, http.StatusInternalServerError, "internal_error", "登录失败")
			return
		}
		logger.Info("登录成功", "ip", clientIP, "username", req.Username)

		s.attachSessionCookie(c, token)
		respondOK(c, gin.H{
			"token":      token,
			"expires_at": exp.Format(time.RFC3339),
		})
	} else {
		logger.Warn("登录失败", "ip", clientIP, "username", req.Username)
		fail(c, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
	}
}

// handleChangePassword 处理修改密码请求
func (s *Server) handleChangePassword(c *gin.Context) {
	var req struct {
		OldPassword     string `json:"old_password"`
		NewPassword     string `json:"new_password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "参数错误")
		return
	}

	// 校验当前密码
	if !checkPassword(s.auth.Password, req.OldPassword) {
		fail(c, http.StatusUnauthorized, "invalid_password", "当前密码错误")
		return
	}

	// 校验新密码与确认密码一致
	if req.NewPassword != req.ConfirmPassword {
		fail(c, http.StatusBadRequest, "password_mismatch", "两次输入的新密码不一致")
		return
	}

	// 校验新密码不能为空
	if strings.TrimSpace(req.NewPassword) == "" {
		fail(c, http.StatusBadRequest, "empty_password", "新密码不能为空")
		return
	}

	// 生成 bcrypt 哈希
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		logger.Error("生成密码哈希失败", "err", err)
		fail(c, http.StatusInternalServerError, "", "密码处理失败")
		return
	}
	hashedPassword := string(hashed)

	// 持久化到配置文件
	if err := config.UpdateWebCredentialsInFile(s.configPath, s.auth.Username, hashedPassword); err != nil {
		logger.Error("更新密码配置失败", "err", err)
		fail(c, http.StatusInternalServerError, "", "保存配置失败: "+err.Error())
		return
	}

	// 更新内存中的密码（已哈希）
	s.auth.Password = hashedPassword

	logger.Info("密码已更新", "username", s.auth.Username, "ip", c.ClientIP())
	respondOKWith(c, nil, gin.H{"message": "密码已更新"})
}

func (s *Server) authorizeRotate(c *gin.Context, username string, password string, now time.Time) bool {
	token := strings.TrimSpace(c.GetHeader("Authorization"))
	if token != "" {
		token = strings.TrimPrefix(token, "Bearer ")
		token = strings.TrimSpace(token)
		if token != "" && s.isSessionTokenValid(token, now) {
			return true
		}
	}

	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" && password == "" {
		fail(c, http.StatusUnauthorized, "unauthorized", "未授权")
		return false
	}
	if c.Request.Method != http.MethodPost {
		fail(c, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 POST 表单/JSON 认证")
		return false
	}

	clientIP := c.ClientIP()
	if !s.allowLoginAttempt(clientIP, now) {
		fail(c, http.StatusTooManyRequests, "rate_limited", "请求过于频繁，请稍后再试")
		return false
	}

	if username == s.auth.Username && checkPassword(s.auth.Password, password) {
		return true
	}

	fail(c, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
	return false
}

func (s *Server) isSessionTokenValid(token string, now time.Time) bool {
	decodedBytes, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return false
	}
	parts := strings.SplitN(string(decodedBytes), ".", 2)
	if len(parts) != 2 {
		return false
	}
	expStr, sig := parts[0], parts[1]

	expInt, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return false
	}
	if now.After(time.Unix(expInt, 0)) {
		return false
	}

	h := hmac.New(sha256.New, []byte(s.auth.Password))
	h.Write([]byte(expStr))
	expectedSig := hex.EncodeToString(h.Sum(nil))

	return hmac.Equal([]byte(sig), []byte(expectedSig))
}

func (s *Server) requestSessionToken(c *gin.Context) string {
	token := strings.TrimSpace(c.GetHeader("Authorization"))
	if token != "" {
		token = strings.TrimPrefix(token, "Bearer ")
		return strings.TrimSpace(token)
	}

	// 只有路由表里标了 sseToken 的流式端点才接受 query 凭证。
	// 白名单从路由表派生（见 routes.go），不再是一份需要同步维护的清单。
	if _, ok := s.sseTokenPaths()[c.FullPath()]; ok {
		return strings.TrimSpace(c.Query("token"))
	}

	// 插件 iframe 和 /plugin-assets 带不了 Authorization，登录时写下的 cookie 补这一段。
	if ck, err := c.Request.Cookie(sessionCookieName); err == nil {
		return strings.TrimSpace(ck.Value)
	}
	return ""
}

const sessionCookieName = "vodoge_session"

func (s *Server) attachSessionCookie(c *gin.Context, token string) {
	if c == nil || strings.TrimSpace(token) == "" {
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, token, int((30 * 24 * time.Hour).Seconds()), "/", "", c.Request.TLS != nil, true)
}

// sseTokenPaths 惰性求值一次并缓存：每个请求都要查它，不能每次重建路由表。
func (s *Server) sseTokenPaths() map[string]struct{} {
	s.sseTokenOnce.Do(func() {
		s.sseTokenCache = s.sseTokenQueryPaths()
	})
	return s.sseTokenCache
}

func (s *Server) isAuthenticatedRequest(c *gin.Context, now time.Time) bool {
	token := s.requestSessionToken(c)
	return token != "" && s.isSessionTokenValid(token, now)
}

func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.requestSessionToken(c) == "" {
			fail(c, http.StatusUnauthorized, "unauthorized", "未授权")
			c.Abort()
			return
		}

		if s.isAuthenticatedRequest(c, time.Now()) {
			s.attachSessionCookie(c, s.requestSessionToken(c))
			c.Next()
			return
		}

		fail(c, http.StatusUnauthorized, "unauthorized", "未授权")
		c.Abort()
	}
}
