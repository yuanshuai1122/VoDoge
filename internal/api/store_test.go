package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vohive/internal/config"
	"github.com/yuanshuai1122/vohive/internal/data/repo"
	"github.com/yuanshuai1122/vohive/internal/db"
	"github.com/yuanshuai1122/vohive/internal/device"
)

// 本文件的全部测试都**不连数据库**——这正是抽 repository 层要换来的东西。
// 在此之前，任何碰到持久化的 handler 测试都得先 db.OpenTestDB(t)：起容器、
// 清空全库、跑十几秒，而且各包共用一个库所以只能 -p 1 串行。
//
// 如果哪天有人把 handler 改回直接调 internal/db，这些测试会因为 DB 为 nil 而失败，
// 那个边界就有了守卫。

func newFakeServer(t *testing.T) (*Server, *repo.FakeCardPolicy, *repo.FakeSIM, *repo.FakeSMS, *repo.FakeUpstreamProxy) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store, cp, sim, sms, _, up := repo.NewFakeStore()
	// 空设备池：这些 handler 会遍历在线设备去补充展示字段，池为 nil 会直接 panic。
	// 无设备正是我们要的场景——断言的是持久化那一路，不是设备那一路。
	return &Server{store: store, pool: device.NewPool(&config.Config{})}, cp, sim, sms, up
}

func TestListCardPoliciesReadsThroughTheStore(t *testing.T) {
	s, cp, _, _, _ := newFakeServer(t)
	cp.ListFn = func() ([]db.CardPolicy, error) {
		return []db.CardPolicy{
			{ICCID: "8986001", NetworkEnabled: true, VoWiFiEnabled: false},
			{ICCID: "8986002", NetworkEnabled: false, VoWiFiEnabled: true},
		}, nil
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/cards/policies", nil)
	s.handleListCardPolicies(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var policies []db.CardPolicy
	decodeData(t, rec, &policies)
	if len(policies) != 2 || policies[0].ICCID != "8986001" {
		t.Fatalf("policies=%+v", policies)
	}
}

// 无策略时必须回 []，不能是 null——前端按数组消费，null 会让 .map 崩掉。
func TestListCardPoliciesReturnsEmptyArrayNotNull(t *testing.T) {
	s, cp, _, _, _ := newFakeServer(t)
	cp.ListFn = func() ([]db.CardPolicy, error) { return nil, nil }

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/cards/policies", nil)
	s.handleListCardPolicies(c)

	if !strings.Contains(rec.Body.String(), `"data":[]`) {
		t.Fatalf("body=%s want data:[]", rec.Body.String())
	}
}

// 未建档的卡返回默认模板而不是 404：这个端点是"给我这张卡当前的有效策略"，
// 没有记录时有效策略就是默认值。读端点保持只读，不会顺手落库。
func TestGetCardPolicyFallsBackToDefaultTemplate(t *testing.T) {
	s, cp, _, _, _ := newFakeServer(t)
	cp.GetFn = func(string) (db.CardPolicy, error) { return db.CardPolicy{}, db.ErrCardPolicyNotFound }

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/cards/nope/policy", nil)
	c.Params = gin.Params{{Key: "iccid", Value: "nope"}}
	s.handleGetCardPolicy(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s want 200", rec.Code, rec.Body.String())
	}
	var got db.CardPolicy
	decodeData(t, rec, &got)
	want := db.DefaultCardPolicy("nope")
	if got.ICCID != want.ICCID || got.Source != want.Source || got.IPVersion != want.IPVersion {
		t.Fatalf("got=%+v want 默认模板 %+v", got, want)
	}
	if len(cp.Upserted) != 0 {
		t.Fatalf("读端点不应落库，却写入了 %d 次", len(cp.Upserted))
	}
}

func TestPutCardPolicyWritesThroughTheStore(t *testing.T) {
	s, cp, _, _, _ := newFakeServer(t)
	cp.GetFn = func(iccid string) (db.CardPolicy, error) {
		return db.CardPolicy{ICCID: iccid, NetworkEnabled: false, VoWiFiEnabled: false}, nil
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/cards/8986001/policy",
		strings.NewReader(`{"network_enabled":true,"apn":"cmnet"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "iccid", Value: "8986001"}}
	s.handlePutCardPolicy(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(cp.Upserted) != 1 {
		t.Fatalf("Upserted=%d want 1 —— 写入没有经过 store", len(cp.Upserted))
	}
	if got := cp.Upserted[0]; got.ICCID != "8986001" || !got.NetworkEnabled || got.APN != "cmnet" {
		t.Fatalf("upserted=%+v", got)
	}
}

func TestUpstreamProxyListReadsThroughTheStore(t *testing.T) {
	s, _, _, _, up := newFakeServer(t)
	up.ListFn = func() ([]db.UpstreamProxy, error) {
		return []db.UpstreamProxy{{ID: "p1", Name: "东京", Addr: "10.0.0.1:1080", Enabled: true}}, nil
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/upstream-proxies", nil)
	s.handleListUpstreamProxies(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"p1"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestDeleteSMSMessageMapsNotFoundTo404(t *testing.T) {
	s, _, _, sms, _ := newFakeServer(t)
	sms.DeleteByIDFn = func(uint) (bool, string, string, error) {
		return false, "", "", db.ErrSMSNotFound
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/sms/messages/9999", nil)
	c.Params = gin.Params{{Key: "id", Value: "9999"}}
	s.handleDeleteSMSMessage(c)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s want 404", rec.Code, rec.Body.String())
	}
}

// 删掉会话里最后一条消息时，响应要告诉前端"这个会话空了"，
// 否则联系人列表会留下一条点进去空白的记录。
func TestDeleteSMSMessageReportsEmptiedThread(t *testing.T) {
	s, _, _, sms, _ := newFakeServer(t)
	sms.DeleteByIDFn = func(uint) (bool, string, string, error) {
		return true, "460010000000001", "10086", nil
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/sms/messages/1", nil)
	c.Params = gin.Params{{Key: "id", Value: "1"}}
	s.handleDeleteSMSMessage(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// 会话是否已空是这次删除的副作用，属于 meta 而不是资源
	if decodeEnvelope(t, rec).Meta["thread_empty"] != true {
		t.Fatalf("body=%s want meta.thread_empty=true", rec.Body.String())
	}
}

func TestSMSContactsReadThroughTheStore(t *testing.T) {
	s, _, _, sms, _ := newFakeServer(t)
	ts := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	sms.ContactsFn = func(int, *time.Time, string) ([]db.SMSContact, error) {
		return []db.SMSContact{{
			IMSI: "460010000000001", ICCID: "8986001", Peer: "10086",
			LastContent: "余额提醒", LastTimestamp: ts, UnreadCount: 2,
		}}, nil
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/sms/contacts", nil)
	s.handleGetSMSContacts(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "10086") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}
