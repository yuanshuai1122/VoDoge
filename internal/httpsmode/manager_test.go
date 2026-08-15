package httpsmode

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGenerateAndReloadCertificate(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir, "127.0.0.1:7575", true)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	st := m.State("127.0.0.1:7575")
	if !st.Enabled || st.Fingerprint == "" || !strings.Contains(st.Fingerprint, ":") {
		t.Fatalf("state=%+v", st)
	}
	if st.NotAfter.Before(time.Now().Add(24 * time.Hour)) {
		t.Fatalf("cert expires too soon: %s", st.NotAfter)
	}
	pem, err := m.CertificatePEM()
	if err != nil || !strings.Contains(string(pem), "BEGIN CERTIFICATE") {
		t.Fatalf("pem err=%v body=%q", err, pem)
	}

	reloaded, err := New(dir, "127.0.0.1:7575", true)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.State("").Fingerprint != st.Fingerprint {
		t.Fatal("reloaded cert fingerprint changed")
	}
}

func TestSetEnabledToggle(t *testing.T) {
	m, err := New(t.TempDir(), ":7575", false)
	if err != nil {
		t.Fatal(err)
	}
	if m.Enabled() {
		t.Fatal("default off")
	}
	if _, err := m.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if !m.Enabled() {
		t.Fatal("want enabled")
	}
	if _, err := m.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if m.Enabled() {
		t.Fatal("want disabled")
	}
}

func TestMultiplexerSplitsHTTPAndTLS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(t.TempDir(), ln.Addr().String(), true)
	if err != nil {
		t.Fatal(err)
	}
	mux := NewMultiplexer(ln, m)
	defer mux.Close()

	plainSrv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "plain")
	})}
	tlsSrv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "secure")
		}),
		TLSConfig: m.TLSConfig(),
	}
	go func() { _ = plainSrv.Serve(mux.Plain()) }()
	go func() { _ = tlsSrv.Serve(tls.NewListener(mux.TLS(), m.TLSConfig())) }()
	t.Cleanup(func() {
		_ = plainSrv.Close()
		_ = tlsSrv.Close()
	})

	base := "http://" + ln.Addr().String() + "/"
	resp, err := http.Get(base)
	if err != nil {
		t.Fatalf("http get: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "plain" {
		t.Fatalf("http body=%q", body)
	}

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
	sec, err := client.Get("https://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatalf("https get: %v", err)
	}
	secBody, _ := io.ReadAll(sec.Body)
	_ = sec.Body.Close()
	if string(secBody) != "secure" {
		t.Fatalf("https body=%q", secBody)
	}
}

func TestMultiplexerDropsTLSWhenDisabled(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(t.TempDir(), ln.Addr().String(), false)
	if err != nil {
		t.Fatal(err)
	}
	mux := NewMultiplexer(ln, m)
	defer mux.Close()
	go func() { _, _ = mux.TLS().Accept() }()

	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{InsecureSkipVerify: true})
	if err == nil {
		_ = conn.Close()
		t.Fatal("tls dial should fail when https is off")
	}
}

func TestIsTLSClientHello(t *testing.T) {
	if !IsTLSClientHello(0x16) || IsTLSClientHello('G') {
		t.Fatal("hello classification")
	}
}
