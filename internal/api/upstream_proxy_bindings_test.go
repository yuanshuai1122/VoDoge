package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodog/internal/data/repo"
	"github.com/yuanshuai1122/vodog/internal/db"
)

const testBindingICCID = "89441000400128014257"

func newBindingTestServer(t *testing.T) (*Server, *repo.FakeUpstreamProxy) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store, _, _, _, _, up := repo.NewFakeStore()
	up.GetFn = func(id string) (*db.UpstreamProxy, error) {
		switch id {
		case "route-1":
			return &db.UpstreamProxy{ID: "route-1", Addr: "127.0.0.1:1080", Enabled: true}, nil
		case "route-2":
			return &db.UpstreamProxy{ID: "route-2", Addr: "127.0.0.1:1081", Enabled: true}, nil
		case "route-off":
			return &db.UpstreamProxy{ID: "route-off", Addr: "127.0.0.1:1082", Enabled: false}, nil
		default:
			return nil, nil
		}
	}
	return &Server{store: store}, up
}

func TestProfileProxyBindingPersistsAndRejectsRebind(t *testing.T) {
	s, up := newBindingTestServer(t)
	body := `{"upstream_proxy_id":"route-1","bindings":[{"device_id":"ec20","iccid":"89441000400128014257","profile_name":"Vodafone UK"}]}`
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/upstream-proxy-profile-bindings", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	s.handleCreateProfileProxyBindings(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(up.ProfileBindings) != 1 || up.ProfileBindings[0].UpstreamProxyID != "route-1" {
		t.Fatalf("bindings=%+v", up.ProfileBindings)
	}

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/upstream-proxy-profile-bindings", nil)
	s.handleListProfileProxyBindings(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status=%d", rec.Code)
	}
	var listed []db.UpstreamProxyProfileBinding
	decodeData(t, rec, &listed)
	if len(listed) != 1 || listed[0].ICCID != testBindingICCID {
		t.Fatalf("listed=%+v", listed)
	}

	conflict := `{"upstream_proxy_id":"route-2","bindings":[{"device_id":"ec20","iccid":"89441000400128014257","profile_name":"Vodafone UK"}]}`
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/upstream-proxy-profile-bindings", bytes.NewBufferString(conflict))
	c.Request.Header.Set("Content-Type", "application/json")
	s.handleCreateProfileProxyBindings(c)
	if rec.Code != http.StatusConflict {
		t.Fatalf("rebind status=%d want 409 body=%s", rec.Code, rec.Body.String())
	}
	env := decodeEnvelope(t, rec)
	if env.Error == nil || env.Error.Code != "profile_already_bound" {
		t.Fatalf("error=%+v", env.Error)
	}
}

func TestProfileProxyBindingRejectsDisabledProxyAndBadICCID(t *testing.T) {
	s, _ := newBindingTestServer(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/upstream-proxy-profile-bindings",
		bytes.NewBufferString(`{"upstream_proxy_id":"route-off","bindings":[{"device_id":"ec20","iccid":"89441000400128014257"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	s.handleCreateProfileProxyBindings(c)
	if rec.Code != http.StatusConflict {
		t.Fatalf("disabled status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/upstream-proxy-profile-bindings",
		bytes.NewBufferString(`{"upstream_proxy_id":"route-1","bindings":[{"device_id":"ec20","iccid":"123"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	s.handleCreateProfileProxyBindings(c)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad iccid status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestProfileProxyBindingDelete(t *testing.T) {
	s, up := newBindingTestServer(t)
	up.ProfileBindings = []db.UpstreamProxyProfileBinding{{
		ICCID: testBindingICCID, DeviceID: "ec20", UpstreamProxyID: "route-1",
	}}
	payload, _ := json.Marshal(map[string]any{
		"upstream_proxy_id": "route-1",
		"iccids":            []string{testBindingICCID},
	})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/upstream-proxy-profile-bindings", bytes.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	s.handleDeleteProfileProxyBindings(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(up.ProfileBindings) != 0 {
		t.Fatalf("remaining=%+v", up.ProfileBindings)
	}
}
