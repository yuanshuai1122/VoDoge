// 日志：SSE 实时流与历史查询。
//
// SSE 的 CORS 头也在这里：开发期前端直连 :7575，绕开会缓冲 SSE 的 Next dev 代理。
package api

import (
	"bytes"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yuanshuai1122/vohive/pkg/logger"

	"github.com/gin-gonic/gin"
)

// handleLogStream SSE 实时日志流
func (s *Server) handleLogStream(c *gin.Context) {
	// 设置 SSE 响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	s.setSSECORSHeaders(c)

	// 订阅日志流
	logChan := logger.GlobalBroadcaster.Subscribe()
	defer logger.GlobalBroadcaster.Unsubscribe(logChan)

	// 获取日志级别过滤参数
	levelFilter := c.Query("level") // debug, info, warn, error

	// 客户端连接上下文
	clientGone := c.Request.Context().Done()

	// 发送初始连接成功事件
	c.SSEvent("connected", gin.H{"message": "已连接日志流"})
	c.Writer.Flush()

	for {
		select {
		case <-clientGone:
			return
		case <-s.shutdownCh:
			return
		case entry, ok := <-logChan:
			if !ok {
				return
			}

			// 日志级别过滤
			if levelFilter != "" {
				entryLevel := strings.ToLower(entry.Level)
				filterLevel := strings.ToLower(levelFilter)
				if !matchLogLevel(entryLevel, filterLevel) {
					continue
				}
			}

			// 发送日志条目
			c.SSEvent("log", entry)
			c.Writer.Flush()
		}
	}
}

func (s *Server) handleLogStreamOptions(c *gin.Context) {
	s.setSSECORSHeaders(c)
	c.Status(http.StatusNoContent)
}

func (s *Server) setSSECORSHeaders(c *gin.Context) {
	origin := strings.TrimSpace(c.GetHeader("Origin"))
	if origin == "" {
		return
	}

	if s.isAllowedSSEOrigin(origin, c.Request.Host) {
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		c.Header("Access-Control-Max-Age", "600")
		c.Header("Vary", "Origin")
	}
}

func (s *Server) isAllowedSSEOrigin(origin, host string) bool {
	if origin == "" {
		return false
	}

	host = strings.TrimSpace(host)
	if host != "" {
		if origin == "http://"+host || origin == "https://"+host {
			return true
		}
	}

	if !s.cfg.Debug {
		return false
	}

	if strings.HasPrefix(origin, "http://localhost:") || origin == "http://localhost" {
		return true
	}
	if strings.HasPrefix(origin, "http://127.0.0.1:") || origin == "http://127.0.0.1" {
		return true
	}
	if strings.HasPrefix(origin, "http://[::1]:") || origin == "http://[::1]" {
		return true
	}
	return false
}

// matchLogLevel 判断日志级别是否符合过滤条件
// filterLevel: debug 显示所有，info 显示 info/warn/error，warn 显示 warn/error，error 只显示 error
func matchLogLevel(entryLevel, filterLevel string) bool {
	levels := map[string]int{
		"debug": 0,
		"info":  1,
		"warn":  2,
		"error": 3,
		"fatal": 4,
	}
	entryLvl, entryOk := levels[entryLevel]
	filterLvl, filterOk := levels[filterLevel]
	if !entryOk || !filterOk {
		return true
	}
	return entryLvl >= filterLvl
}

// handleLogHistory 获取历史日志
func (s *Server) handleLogHistory(c *gin.Context) {
	// 读取参数
	lines := 500 // 默认返回最近 500 行
	if n := c.Query("lines"); n != "" {
		if v, err := strconv.Atoi(n); err == nil && v > 0 && v <= 2000 {
			lines = v
		}
	}

	// 日志文件路径（使用 logger 的默认路径）
	logFile := "logs/app.log"

	recentLines, err := readLastLines(logFile, lines, 1<<20)
	if err != nil {
		// 读不到日志文件不是错误——刚启动、或日志只往 stdout 走时就是这样。
		// 这里以前回的是 200 + {status:"error"}，一个自相矛盾的形状：
		// 前端按成功路径取 logs 拿到空数组，按错误路径又会抛。
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"logs":   []logger.LogEntry{},
			"note":   "日志文件不可读：" + err.Error(),
		})
		return
	}

	// 解析日志行
	entries := make([]logger.LogEntry, 0, len(recentLines))
	for _, line := range recentLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entry := parseLogLine(line)
		if entry.Message != "" {
			entries = append(entries, entry)
		}
	}

	c.JSON(http.StatusOK, gin.H{"logs": entries})
}

func readLastLines(path string, maxLines int, maxBytes int64) ([]string, error) {
	if maxLines <= 0 {
		return []string{}, nil
	}
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	if size <= 0 {
		return []string{}, nil
	}

	const blockSize int64 = 32 * 1024
	var offset = size
	var readBytes int64
	var newlines int
	chunks := make([][]byte, 0, 8)

	for offset > 0 && readBytes < maxBytes && newlines <= maxLines {
		step := blockSize
		if offset < step {
			step = offset
		}
		offset -= step

		b := make([]byte, step)
		n, rerr := f.ReadAt(b, offset)
		if rerr != nil && n == 0 {
			return nil, rerr
		}
		b = b[:n]

		chunks = append(chunks, b)
		readBytes += int64(n)
		newlines += bytes.Count(b, []byte{'\n'})
	}

	var buf bytes.Buffer
	for i := len(chunks) - 1; i >= 0; i-- {
		buf.Write(chunks[i])
	}

	all := strings.Split(buf.String(), "\n")
	for len(all) > 0 && strings.TrimSpace(all[len(all)-1]) == "" {
		all = all[:len(all)-1]
	}
	if len(all) <= maxLines {
		return all, nil
	}
	return all[len(all)-maxLines:], nil
}

// parseLogLine 解析日志行
// 格式: [2026-02-06 15:04:05] LEVEL  caller  message {fields}
func parseLogLine(line string) logger.LogEntry {
	entry := logger.LogEntry{}

	// 提取时间 [2026-02-06 15:04:05]
	if len(line) < 22 || line[0] != '[' {
		entry.Message = line
		return entry
	}

	timeEnd := strings.Index(line, "]")
	if timeEnd < 0 {
		entry.Message = line
		return entry
	}

	timeStr := line[1:timeEnd]
	// 使用 ParseInLocation 确保日志时间继承系统本地时区，避免被默认解析为 UTC
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", timeStr, time.Local); err == nil {
		entry.Time = t.Format(time.RFC3339)
	} else {
		entry.Time = timeStr
	}

	rest := strings.TrimSpace(line[timeEnd+1:])

	// 使用 Fields 按任意空白字符分割
	fields := strings.Fields(rest)
	if len(fields) >= 1 {
		entry.Level = fields[0]
	}
	if len(fields) >= 2 {
		entry.Caller = fields[1]
	}
	if len(fields) >= 3 {
		// message 是剩余部分，需要从原字符串中找到正确位置
		// 找到 caller 在 rest 中的位置
		callerIdx := strings.Index(rest, entry.Caller)
		if callerIdx >= 0 {
			msgStart := callerIdx + len(entry.Caller)
			entry.Message = strings.TrimSpace(rest[msgStart:])
		} else {
			entry.Message = strings.Join(fields[2:], " ")
		}
	}

	return entry
}
