package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func rawBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是 JSON 对象: %v\n%s", err, rec.Body.String())
	}
	return body
}

// data 与 error 互斥且必有其一——判别是结构性的，不再靠 status:"ok" 字符串。
// 那个字符串曾经出现在 200 响应里表示失败，自相矛盾且无法防。
func TestSuccessAndErrorEnvelopesAreDisjoint(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	respondOK(c, gin.H{"id": "dev1"})

	body := rawBody(t, rec)
	if _, hasErr := body["error"]; hasErr {
		t.Fatalf("成功响应里出现了 error: %v", body)
	}
	if _, hasData := body["data"]; !hasData {
		t.Fatalf("成功响应缺少 data: %v", body)
	}

	rec2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(rec2)
	fail(c2, http.StatusNotFound, "", "设备未找到")

	body2 := rawBody(t, rec2)
	if _, hasData := body2["data"]; hasData {
		t.Fatalf("错误响应里出现了 data: %v", body2)
	}
	if _, hasErr := body2["error"]; !hasErr {
		t.Fatalf("错误响应缺少 error: %v", body2)
	}
}

// 无资源可返回时 data 必须显式为 null，而不是整个字段消失——
// 调用方靠 "data" in body 判别成功，字段缺失会让判别失效。
func TestSuccessEnvelopeKeepsNullData(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	respondOK(c, nil)

	body := rawBody(t, rec)
	v, ok := body["data"]
	if !ok {
		t.Fatalf("data 字段消失了: %s", rec.Body.String())
	}
	if v != nil {
		t.Fatalf("data=%v want null", v)
	}
}

func TestMetaIsOmittedWhenEmpty(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	respondOKWith(c, nil, gin.H{})

	if _, ok := rawBody(t, rec)["meta"]; ok {
		t.Fatalf("空 meta 不应出现: %s", rec.Body.String())
	}
}

// meta 只放"关于这次操作"的信息，与载荷分开。以前它们平铺在一起，
// 调用方分不清哪些是数据、哪些是关于数据的说明。
func TestMetaStaysOutOfData(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	respondOKWith(c, []string{"a", "b"}, gin.H{"device_limit": 3})

	body := rawBody(t, rec)
	data, ok := body["data"].([]any)
	if !ok || len(data) != 2 {
		t.Fatalf("data=%v want 两个元素的数组", body["data"])
	}
	meta, ok := body["meta"].(map[string]any)
	if !ok || meta["device_limit"] != float64(3) {
		t.Fatalf("meta=%v want device_limit=3", body["meta"])
	}
}

// request_id 是错误统一的主要收益：用户报上来的错误要能在服务端日志里搜到。
func TestRequestIDPresentOnBothOutcomes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	s := &Server{}
	router := gin.New()
	router.Use(s.requestIDMiddleware())
	router.GET("/ok", func(c *gin.Context) { respondOK(c, gin.H{"x": 1}) })
	router.GET("/boom", func(c *gin.Context) { fail(c, http.StatusNotFound, "", "没了") })

	for _, path := range []string{"/ok", "/boom"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if id, _ := rawBody(t, rec)["request_id"].(string); id == "" {
			t.Fatalf("%s 缺少 request_id: %s", path, rec.Body.String())
		}
	}
}

func TestFailDerivesCodeFromStatusWhenNotGiven(t *testing.T) {
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

		env := decodeEnvelope(t, rec)
		if env.Error == nil || env.Error.Code != tc.want {
			t.Fatalf("status %d -> %+v want code %q", tc.status, env.Error, tc.want)
		}
	}
}

func TestFailKeepsExplicitCode(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	fail(c, http.StatusConflict, "ESIM_BUSY", "eSIM 操作正忙")

	env := decodeEnvelope(t, rec)
	if env.Error.Code != "ESIM_BUSY" {
		t.Fatalf("code=%q want ESIM_BUSY —— 专属码不能被通用码盖掉", env.Error.Code)
	}
}

// 需要客户端据以决策的数据放 error.details，与给人读的 message 分开。
func TestFailWithCarriesStructuredDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	failWith(c, http.StatusConflict, "ESIM_DOWNLOAD_IN_PROGRESS", "已有进行中的下载", gin.H{
		"busy":    true,
		"task_id": "abc123",
	})

	env := decodeEnvelope(t, rec)
	if env.Error.Details["busy"] != true || env.Error.Details["task_id"] != "abc123" {
		t.Fatalf("details=%v", env.Error.Details)
	}
}

func TestFailWithOmitsEmptyDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	failWith(c, http.StatusBadRequest, "", "参数错误", gin.H{})

	body := rawBody(t, rec)
	errObj := body["error"].(map[string]any)
	if _, ok := errObj["details"]; ok {
		t.Fatalf("空 details 不应出现: %s", rec.Body.String())
	}
}

func TestFailErrFallsBackWhenErrorIsNil(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	failErr(c, http.StatusInternalServerError, "", nil, "操作失败")

	if got := decodeEnvelope(t, rec).Error.Message; got != "操作失败" {
		t.Fatalf("message=%q want 兜底文案而不是 <nil>", got)
	}
}

// eSIM 并发冲突要告诉客户端等多久。retryAfterMs 这个 camelCase 遗留拼写
// 随本次破坏性变更一并删除，只保留 snake_case。
func TestEsimBusyResponseCarriesRetryHint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/devices/dev1/esim/actions/switch", nil)
	respondEsimBusy(c, "switch_profile", errEsimBusyForTest{})

	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d want 409", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if env.Error.Code != "ESIM_BUSY" {
		t.Fatalf("code=%q", env.Error.Code)
	}
	d := env.Error.Details
	if d["busy"] != true || d["reason"] != "switch_profile" || d["retry_after_ms"] == nil {
		t.Fatalf("details=%v", d)
	}
	if _, stale := d["retryAfterMs"]; stale {
		t.Fatal("camelCase 的 retryAfterMs 应已删除")
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("缺少 Retry-After 头")
	}
}

type errEsimBusyForTest struct{}

func (errEsimBusyForTest) Error() string { return "eSIM 操作正忙" }
