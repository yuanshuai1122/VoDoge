package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// 测试侧解包信封的共用助手。
//
// 所有 JSON 响应都是 {"data":…,"meta":…,"request_id":…} 或
// {"error":{…},"request_id":…}，测试不该各写各的解析。

type envelopeBody struct {
	Data      json.RawMessage `json:"data"`
	Meta      map[string]any  `json:"meta"`
	Error     *errorBody      `json:"error"`
	RequestID string          `json:"request_id"`
}

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) envelopeBody {
	t.Helper()
	var env envelopeBody
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("响应不是信封结构: %v\n%s", err, rec.Body.String())
	}
	return env
}

// decodeData 解包 data 到 out；响应带 error 时直接失败并打出错误码，
// 比让调用方拿到零值再断言"字段为空"好定位得多。
func decodeData(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	env := decodeEnvelope(t, rec)
	if env.Error != nil {
		t.Fatalf("期望成功，得到错误 %s: %s", env.Error.Code, env.Error.Message)
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		t.Fatalf("data 为空: %s", rec.Body.String())
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		t.Fatalf("解析 data 失败: %v\n%s", err, string(env.Data))
	}
}
