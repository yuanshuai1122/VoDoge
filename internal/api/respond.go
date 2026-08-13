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

// fail 输出一个错误响应。code 是给程序判别用的稳定标识，message 是给人看的。
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

// failWith 输出错误响应并附带额外字段。
//
// 额外字段用于调用方需要据以决策的数据——例如 eSIM 并发冲突的 retryAfterMs、
// 下载任务已存在时的 task_id。它们平铺在同一层级，不额外包一层。
func failWith(c *gin.Context, status int, code, message string, extra gin.H) {
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
