package api

import (
	"github.com/gin-gonic/gin"
)

// 错误响应的**唯一**形状：
//
//	{"status":"error", "code":"...", "message":"...", "request_id":"..."}
//
// 此前有三种：主流的 {status,message}、eSIM 与 card_policy 用的裸 {error:"..."}，
// 以及 eSIM 并发冲突的 {error, busy, code, reason, retryAfterMs}。
// 前端因此得在**每个请求**上同时准备三套解析——它无从预知会拿到哪一种。
// 更实际的损失是裸 {error:"..."} 丢掉了 request_id：用户报上来的错误信息，
// 在服务端日志里搜不到对应的那次请求。
//
// 附加字段（busy / retryAfterMs / task_id 等）用 failWith 挂在同一层级，
// 调用方的读法不变。

// fail 输出一个错误响应。
//
// code 是给程序判别用的稳定标识；**留空时按 HTTP 状态推导**一个通用码。
// 绝大多数错误站点并不需要比状态码更细的区分，硬给它们编一个专属 code 只是
// 制造一堆没人会去分支的字符串。真正需要客户端据以决策的场景（eSIM 并发冲突、
// E911 各类前置条件、websheet 会话状态）本来就带着自己的 code。
func fail(c *gin.Context, status int, code, message string) {
	failWith(c, status, code, message, nil)
}

// failErr 与 fail 相同，但 message 取自 err。
// err 为 nil 时退化为 fallback，避免出现 "操作失败: <nil>" 这种响应。
func failErr(c *gin.Context, status int, code string, err error, fallback string) {
	msg := fallback
	if err != nil {
		msg = err.Error()
	}
	failWith(c, status, code, msg, nil)
}

func defaultCodeFor(status int) string {
	switch status {
	case 400:
		return "bad_request"
	case 401:
		return "unauthorized"
	case 403:
		return "forbidden"
	case 404:
		return "not_found"
	case 405:
		return "method_not_allowed"
	case 409:
		return "conflict"
	case 410:
		return "gone"
	case 429:
		return "too_many_requests"
	case 501:
		return "not_implemented"
	case 502:
		return "bad_gateway"
	case 503:
		return "service_unavailable"
	case 504:
		return "gateway_timeout"
	}
	if status >= 500 {
		return "internal_error"
	}
	return "request_failed"
}

// failWith 输出错误响应并附带额外字段。
//
// 额外字段用于调用方需要据以决策的数据——例如 eSIM 并发冲突的 retryAfterMs、
// 下载任务已存在时的 task_id。它们平铺在同一层级，不额外包一层。
func failWith(c *gin.Context, status int, code, message string, extra gin.H) {
	if code == "" {
		code = defaultCodeFor(status)
	}
	body := gin.H{
		"status":     "error",
		"code":       code,
		"message":    message,
		"request_id": requestID(c),
	}
	for k, v := range extra {
		// 固定字段不允许被 extra 覆盖：调用方靠它们判别，形状必须是稳定的
		switch k {
		case "status", "code", "message", "request_id":
			continue
		}
		body[k] = v
	}
	c.JSON(status, body)
}
