package extensions

import (
	"strings"
	"testing"
)

func TestDecodeManifestAcceptsVodogSchema(t *testing.T) {
	raw := `{
		"schema_version": 1,
		"id": "hello-lab",
		"name": "Hello",
		"version": "1.0.0",
		"description": "demo",
		"permissions": ["network"],
		"contributions": [
			{"id": "hello-page", "label": "Hello", "label_zh": "你好", "location": "sidebar", "entry": "index.html"}
		],
		"backend": {"commands": {"linux/amd64": "bin/hello"}}
	}`
	m, err := DecodeManifest(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "hello-lab" || m.Contributions[0].Location != "sidebar" {
		t.Fatalf("%+v", m)
	}
}

func TestDecodeManifestRejectsUnknownFieldAndBadID(t *testing.T) {
	if _, err := DecodeManifest(strings.NewReader(`{"schema_version":1,"id":"Hello","name":"x","version":"1","contributions":[]}`)); err == nil {
		t.Fatal("uppercase id must fail")
	}
	if _, err := DecodeManifest(strings.NewReader(`{"schema_version":1,"id":"okid","name":"x","version":"1","contributions":[],"nope":1}`)); err == nil {
		t.Fatal("unknown field must fail")
	}
}

func TestSafeRelativePath(t *testing.T) {
	if !safeRelativePath("index.html") || !safeRelativePath("bin/hello-linux-amd64") {
		t.Fatal("expected safe")
	}
	for _, p := range []string{"/etc/passwd", "../x", "a/../b", "C:foo", ""} {
		if safeRelativePath(p) {
			t.Fatalf("%q should be unsafe", p)
		}
	}
}
