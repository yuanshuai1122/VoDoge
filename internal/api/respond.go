package api

import (
	"github.com/gin-gonic/gin"
)

// HTTP 响应的**唯一**结构。
//
//	成功：{"data": <载荷|null>, "meta": {...}?, "request_id": "..."}
//	失败：{"error": {"code","message","details"?}, "request_id": "..."}
//
// 改造前有约 60 种成功形状。真正的麻烦不是键名不齐，而是**元数据与载荷同层**：
// `requires_restart`、`device_limit`、`warning`、`thread_empty`、`space_delta`
// 描述的是这次操作而非返回的资源，却和资源字段平铺在一起——调用方分不清哪些是
// 数据、哪些是关于数据的说明，加新字段还有撞名风险。
//
// 三条不变式：
//  1. data 与 error 互斥且必有其一。判别是结构性的，不再靠 status:"ok" 这种
//     魔法字符串——那个字符串曾经出现在 200 响应里表示失败（日志读不到文件时
//     回 200 + {status:"error"}），自相矛盾且无法防。
//  2. request_id 恒在，与 X-Request-Id 头一致。
//  3. meta 只放"关于这次操作/这批数据"的信息，绝不放资源本身。
//
// 不套信封的三处：SSE 事件帧（是领域事件流，不是 HTTP 响应）、`/ping`
// （`/api` 之外的存活探针，外部监控在用）、websheet 的承载页与代理通道
// （内容由运营商页面决定，本就不是本服务的 JSON 契约）。

// successEnvelope 与 errorEnvelope 刻意分成两个结构体。
// data 在成功响应里**必须出现**（哪怕是 null），在错误响应里**不该出现**；
// 单个结构体加 omitempty 做不到这个区分。
type successEnvelope struct {
	Data      any    `json:"data"`
	Meta      gin.H  `json:"meta,omitempty"`
	RequestID string `json:"request_id"`
}

type errorEnvelope struct {
	Error     errorBody `json:"error"`
	RequestID string    `json:"request_id"`
}

type errorBody struct {
	// Code 是给程序判别用的稳定标识；留空时按 HTTP 状态推导。
	Code string `json:"code"`
	// Message 给人读，不要用于程序分支。
	Message string `json:"message"`
	// Details 放需要客户端据以决策的结构化数据，例如 ESIM_BUSY 的 retry_after_ms。
	Details gin.H `json:"details,omitempty"`
}

// respondOK 返回 200 与载荷。无资源可返回时传 nil。
func respondOK(c *gin.Context, data any) {
	respond(c, 200, data, nil)
}

// respondOKWith 返回 200、载荷与元数据。
func respondOKWith(c *gin.Context, data any, meta gin.H) {
	respond(c, 200, data, meta)
}

// respond 返回指定状态码的成功响应。
func respond(c *gin.Context, status int, data any, meta gin.H) {
	if len(meta) == 0 {
		meta = nil
	}
	c.JSON(status, successEnvelope{
		Data:      data,
		Meta:      meta,
		RequestID: requestID(c),
	})
}

// fail 输出错误响应。
//
// code 留空时按 HTTP 状态推导。绝大多数错误站点并不需要比状态码更细的区分，
// 硬给它们编一个专属 code 只是制造一堆没人分支的字符串；真正需要客户端据以
// 决策的场景（eSIM 并发冲突、E911 前置条件、websheet 会话状态）本来就带着
// 自己的 code。
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

// failWith 输出错误响应并附带结构化细节。
func failWith(c *gin.Context, status int, code, message string, details gin.H) {
	if code == "" {
		code = defaultCodeFor(status)
	}
	if len(details) == 0 {
		details = nil
	}
	c.JSON(status, errorEnvelope{
		Error: errorBody{
			Code:    code,
			Message: message,
			Details: details,
		},
		RequestID: requestID(c),
	})
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
