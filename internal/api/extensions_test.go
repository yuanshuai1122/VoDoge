package api

import (
	"archive/zip"
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodog/internal/extensions"
)

func testPluginZip(t *testing.T, id string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mf := `{
		"schema_version": 1,
		"id": "` + id + `",
		"name": "Demo",
		"version": "0.1.0",
		"contributions": [
			{"id": "demo-page", "label": "Demo", "location": "sidebar", "entry": "index.html"}
		]
	}`
	w, err := zw.Create("vodog-plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(mf)); err != nil {
		t.Fatal(err)
	}
	w, err = zw.Create("index.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("<html>demo</html>")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtensionsInstallToggleUninstall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mgr, err := extensions.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)
	s := &Server{extensions: mgr}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("package", "demo.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(testPluginZip(t, "hello-lab")); err != nil {
		t.Fatal(err)
	}
	_ = mw.Close()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/extensions/upload", &body)
	c.Request.Header.Set("Content-Type", mw.FormDataContentType())
	s.handleUploadExtension(c)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/extensions", nil)
	s.handleListExtensions(c)
	var list []extensions.Installed
	decodeData(t, rec, &list)
	if len(list) != 1 || list[0].ID != "hello-lab" || !list[0].Enabled {
		t.Fatalf("list=%+v", list)
	}

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: "hello-lab"}}
	c.Request = httptest.NewRequest(http.MethodPut, "/api/extensions/hello-lab", strings.NewReader(`{"enabled":false}`))
	c.Request.Header.Set("Content-Type", "application/json")
	s.handleUpdateExtension(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", rec.Code, rec.Body.String())
	}
	var inst extensions.Installed
	decodeData(t, rec, &inst)
	if inst.Enabled {
		t.Fatal("expected disabled")
	}

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: "hello-lab"}}
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/extensions/hello-lab", nil)
	s.handleDeleteExtension(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(mgr.List()) != 0 {
		t.Fatal("expected uninstall")
	}
}

func TestUploadExtensionRejectsDuplicate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mgr, err := extensions.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)
	s := &Server{extensions: mgr}
	zip := testPluginZip(t, "hello-lab")
	upload := func() *httptest.ResponseRecorder {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		part, _ := mw.CreateFormFile("package", "demo.zip")
		_, _ = part.Write(zip)
		_ = mw.Close()
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/extensions/upload", &body)
		c.Request.Header.Set("Content-Type", mw.FormDataContentType())
		s.handleUploadExtension(c)
		return rec
	}
	if rec := upload(); rec.Code != http.StatusCreated {
		t.Fatalf("first=%d %s", rec.Code, rec.Body.String())
	}
	rec := upload()
	if rec.Code != http.StatusConflict {
		t.Fatalf("second=%d %s", rec.Code, rec.Body.String())
	}
	env := decodeEnvelope(t, rec)
	if env.Error == nil || env.Error.Code != "plugin_already_installed" {
		t.Fatalf("env=%+v", env)
	}
}

func TestInstallExtensionURLRejectsHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mgr, err := extensions.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)
	s := &Server{extensions: mgr}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/extensions/install-url", strings.NewReader(`{"url":"http://example.com/p.zip"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	s.handleInstallExtensionURL(c)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	env := decodeEnvelope(t, rec)
	if env.Error == nil || env.Error.Code != "plugin_url_rejected" {
		t.Fatalf("env=%+v", env)
	}
}

func TestListExtensionsNilManager(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/extensions", nil)
	s.handleListExtensions(c)
	var list []extensions.Installed
	decodeData(t, rec, &list)
	if list == nil {
		t.Fatal("want empty list, not null")
	}
}
