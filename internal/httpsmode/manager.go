// 本机自签 HTTPS：给个人部署提供可安装 PWA 的 TLS，不依赖反代。
//
// 证书写在本地目录（默认 data/tls），开关落在配置文件。
// 开启后与 HTTP 共用同一端口：握手第一个字节 0x16 走 TLS，其余走明文。
package httpsmode

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	certFileName = "tls.crt"
	keyFileName  = "tls.key"
	renewIfLeft  = 30 * 24 * time.Hour
)

type State struct {
	Enabled     bool      `json:"enabled"`
	HTTPURL     string    `json:"http_url"`
	HTTPSURL    string    `json:"https_url"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	NotAfter    time.Time `json:"not_after,omitempty"`
}

type Manager struct {
	dir     string
	address string
	enabled atomic.Bool
	mu      sync.RWMutex
	cert    *tls.Certificate
}

func New(dir, listenAddr string, enabled bool) (*Manager, error) {
	m := &Manager{dir: dir, address: listenAddr}
	if enabled {
		if err := m.ensureCertificate(); err != nil {
			return nil, err
		}
		m.enabled.Store(true)
	}
	return m, nil
}

func (m *Manager) Enabled() bool {
	return m != nil && m.enabled.Load()
}

func (m *Manager) SetEnabled(enabled bool) (State, error) {
	if m == nil {
		return State{}, errors.New("https manager is nil")
	}
	if enabled {
		if err := m.ensureCertificate(); err != nil {
			return State{}, err
		}
	}
	m.enabled.Store(enabled)
	return m.State(""), nil
}

func (m *Manager) State(host string) State {
	if m == nil {
		return State{}
	}
	host = strings.TrimSpace(host)
	if host == "" {
		host = displayHost(m.address)
	}
	st := State{
		Enabled:  m.Enabled(),
		HTTPURL:  "http://" + host,
		HTTPSURL: "https://" + host,
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cert != nil && m.cert.Leaf != nil {
		st.Fingerprint = fingerprint(m.cert.Leaf.Raw)
		st.NotAfter = m.cert.Leaf.NotAfter
	}
	return st
}

func (m *Manager) TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			if m == nil {
				return nil, errors.New("https manager is nil")
			}
			m.mu.RLock()
			defer m.mu.RUnlock()
			if m.cert == nil {
				return nil, errors.New("self-signed certificate is unavailable")
			}
			return m.cert, nil
		},
	}
}

func (m *Manager) CertificatePEM() ([]byte, error) {
	if m == nil {
		return nil, errors.New("https manager is nil")
	}
	if err := m.ensureCertificate(); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(m.dir, certFileName))
}

func (m *Manager) ensureCertificate() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cert != nil && m.cert.Leaf != nil && time.Until(m.cert.Leaf.NotAfter) > renewIfLeft {
		return nil
	}
	if err := os.MkdirAll(m.dir, 0o750); err != nil {
		return fmt.Errorf("create tls dir: %w", err)
	}
	certPath := filepath.Join(m.dir, certFileName)
	keyPath := filepath.Join(m.dir, keyFileName)
	if cert, err := loadCertificate(certPath, keyPath); err == nil && time.Until(cert.Leaf.NotAfter) > renewIfLeft {
		m.cert = cert
		return nil
	}
	certPEM, keyPEM, err := generateCertificate(m.address)
	if err != nil {
		return err
	}
	if err := writeAtomic(keyPath, keyPEM, 0o600); err != nil {
		return err
	}
	if err := writeAtomic(certPath, certPEM, 0o644); err != nil {
		return err
	}
	cert, err := loadCertificate(certPath, keyPath)
	if err != nil {
		return err
	}
	m.cert = cert
	return nil
}

func loadCertificate(certPath, keyPath string) (*tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	if len(cert.Certificate) == 0 {
		return nil, errors.New("empty certificate")
	}
	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

func generateCertificate(listenAddr string) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "VoDog local", Organization: []string{"VoDog"}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	if hostname, err := os.Hostname(); err == nil {
		if h := strings.TrimSpace(hostname); h != "" {
			tpl.DNSNames = append(tpl.DNSNames, h)
		}
	}
	if host, _, err := net.SplitHostPort(listenAddr); err == nil {
		if ip := net.ParseIP(host); ip != nil && !ip.IsUnspecified() {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else if host != "" && host != "0.0.0.0" && host != "::" {
			tpl.DNSNames = append(tpl.DNSNames, host)
		}
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, item := range addrs {
			text := item.String()
			if i := strings.IndexByte(text, '/'); i >= 0 {
				text = text[:i]
			}
			if ip := net.ParseIP(strings.TrimSpace(text)); ip != nil && !ip.IsUnspecified() {
				tpl.IPAddresses = append(tpl.IPAddresses, ip)
			}
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), nil
}

func fingerprint(raw []byte) string {
	sum := sha256.Sum256(raw)
	hexed := strings.ToUpper(hex.EncodeToString(sum[:]))
	parts := make([]string, 0, len(hexed)/2)
	for i := 0; i+2 <= len(hexed); i += 2 {
		parts = append(parts, hexed[i:i+2])
	}
	return strings.Join(parts, ":")
}

func displayHost(listenAddr string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listenAddr))
	if err != nil {
		return strings.TrimPrefix(listenAddr, ":")
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return net.JoinHostPort(host, port)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tls-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
