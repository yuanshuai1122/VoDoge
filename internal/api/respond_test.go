package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是 JSON 对象: %v\n%s", err, rec.Body.String())
	}
	return body
}

// request_id 是这次统一的主要收益：裸 {error:"..."} 那一支从来没带过它，
// 用户报上来的错误信息在服务端日志里搜不到对应请求。
func TestFailAlwaysCarriesRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	s := &Server{}
	router := gin.New()
	router.Use(s.requestIDMiddleware())
	router.GET("/boom", func(c *gin.Context) {
		fail(c, http.StatusNotFound, "", "设备未找到")
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404", rec.Code)
	}
	body := decodeBody(t, rec)
	if body["status"] != "error" || body["message"] != "设备未找到" {
		t.Fatalf("body=%v", body)
	}
	if id, _ := body["request_id"].(string); id == "" {
		t.Fatalf("body=%v want a non-empty request_id", body)
	}
}

func TestFailDerivesCodeFromStatusWhenNotGiven(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		status int
		want   string
	}{
		{http.StatusBadRequest, "bad_request"},
		{http.StatusNotFound, "not_found"},
		{http.StatusConflict, "conflict"},
		{http.StatusGone, "gone"},
		{http.StatusInternalServerError, "internal_error"},
		{http.StatusBadGateway, "bad_gateway"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		fail(c, tc.status, "", "boom")
		if got := decodeBody(t, rec)["code"]; got != tc.want {
			t.Fatalf("status %d -> code=%v want %q", tc.status, got, tc.want)
		}
	}
}

func TestFailKeepsExplicitCode(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	fail(c, http.StatusConflict, "ESIM_BUSY", "eSIM 操作正忙")

	if got := decodeBody(t, rec)["code"]; got != "ESIM_BUSY" {
		t.Fatalf("code=%v want ESIM_BUSY —— 专属码不能被通用码盖掉", got)
	}
}

// 附加字段平铺在同一层级，调用方的读法不变。
func TestFailWithSurfacesExtraFieldsAtTopLevel(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	failWith(c, http.StatusConflict, "ESIM_DOWNLOAD_IN_PROGRESS", "已有进行中的下载", gin.H{
		"busy":    true,
		"task_id": "abc123",
	})

	body := decodeBody(t, rec)
	if body["busy"] != true || body["task_id"] != "abc123" {
		t.Fatalf("body=%v want busy/task_id 平铺在顶层", body)
	}
}

// 固定字段是调用方判别的依据，形状必须稳定；extra 不能把它们顶掉。
func TestFailWithCannotOverrideTheFixedFields(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	failWith(c, http.StatusBadRequest, "real_code", "真实消息", gin.H{
		"status":     "ok",
		"code":       "hijacked",
		"message":    "被顶掉的消息",
		"request_id": "伪造的",
	})

	body := decodeBody(t, rec)
	if body["status"] != "error" || body["code"] != "real_code" || body["message"] != "真实消息" {
		t.Fatalf("body=%v want extra 无法覆盖固定字段", body)
	}
}

func TestFailErrFallsBackWhenErrorIsNil(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	failErr(c, http.StatusInternalServerError, "", nil, "操作失败")

	if got := decodeBody(t, rec)["message"]; got != "操作失败" {
		t.Fatalf("message=%v want 兜底文案而不是 <nil>", got)
	}
}

// eSIM 并发冲突是唯一带 camelCase 字段的响应。改名是破坏性的，
// 所以两个名字一起给；这里盯住两者都在。
func TestEsimBusyResponseCarriesBothRetryAfterSpellings(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/devices/dev1/esim/actions/switch", nil)
	respondEsimBusy(c, "switch_profile", errEsimBusyForTest{})

	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d want 409", rec.Code)
	}
	body := decodeBody(t, rec)
	if body["code"] != "ESIM_BUSY" || body["busy"] != true || body["reason"] != "switch_profile" {
		t.Fatalf("body=%v", body)
	}
	if body["retryAfterMs"] == nil || body["retry_after_ms"] == nil {
		t.Fatalf("body=%v want 新旧两种拼写同时存在", body)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("缺少 Retry-After 头")
	}
}

type errEsimBusyForTest struct{}

func (errEsimBusyForTest) Error() string { return "eSIM 操作正忙" }
