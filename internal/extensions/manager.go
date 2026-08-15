package extensions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/yuanshuai1122/vodog/pkg/logger"
)

const (
	maxPackageBytes = 64 << 20
	stateFilename   = "state.json"
)

var (
	ErrNotFound         = errors.New("插件未安装")
	ErrChecksum         = errors.New("插件 SHA-256 不匹配")
	ErrBackendDown      = errors.New("插件后端未运行")
	ErrPluginDisabled   = errors.New("插件已禁用")
	ErrAssetNotFound    = errors.New("插件资源不存在")
	ErrAlreadyInstalled = errors.New("插件已安装，请先卸载再覆盖")
)

type Installed struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Version          string         `json:"version"`
	Description      string         `json:"description,omitempty"`
	Author           string         `json:"author,omitempty"`
	Homepage         string         `json:"homepage,omitempty"`
	Permissions      []string       `json:"permissions,omitempty"`
	Contributions    []Contribution `json:"contributions"`
	Enabled          bool           `json:"enabled"`
	BackendAvailable bool           `json:"backend_available"`
	BackendRunning   bool           `json:"backend_running"`
	BackendError     string         `json:"backend_error,omitempty"`
	InstalledAt      string         `json:"installed_at"`
	SHA256           string         `json:"sha256"`
}

type record struct {
	ID          string `json:"id"`
	Enabled     bool   `json:"enabled"`
	InstalledAt string `json:"installed_at"`
	SHA256      string `json:"sha256"`
}

type stateFile struct {
	Plugins []record `json:"plugins"`
}

type runningBackend struct {
	cmd    *exec.Cmd
	addr   string
	err    string
	cancel context.CancelFunc
}

type Manager struct {
	root  string
	fetch func(ctx context.Context, rawURL string) ([]byte, error)

	mu      sync.Mutex
	state   stateFile
	procs   map[string]*runningBackend
	started bool
}

func Open(root string) (*Manager, error) {
	if strings.TrimSpace(root) == "" {
		root = filepath.Join("data", "plugins")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	m := &Manager{
		root: root,
		fetch: func(ctx context.Context, rawURL string) ([]byte, error) {
			return fetchHTTPS(ctx, rawURL, maxPackageBytes)
		},
		procs: map[string]*runningBackend{},
	}
	if err := m.loadState(); err != nil {
		return nil, err
	}
	m.startEnabledLocked()
	m.started = true
	return m, nil
}

func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.procs {
		m.stopBackendLocked(id)
	}
}

func (m *Manager) List() []Installed {
	if m == nil {
		return []Installed{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Installed, 0, len(m.state.Plugins))
	for _, rec := range m.state.Plugins {
		inst, err := m.installedLocked(rec)
		if err != nil {
			logger.Warn("跳过损坏的插件", "id", rec.ID, "err", err)
			continue
		}
		out = append(out, inst)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ni, nj := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if ni != nj {
			return ni < nj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (m *Manager) InstallZip(data []byte, wantSHA string) (Installed, error) {
	if m == nil {
		return Installed{}, fmt.Errorf("插件系统不可用")
	}
	if int64(len(data)) > maxPackageBytes {
		return Installed{}, ErrTooLarge
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if want := strings.ToLower(strings.TrimSpace(wantSHA)); want != "" && want != got {
		return Installed{}, ErrChecksum
	}

	tmp, err := os.MkdirTemp(m.root, "unpack-*")
	if err != nil {
		return Installed{}, err
	}
	defer os.RemoveAll(tmp)
	if err := extractPluginZip(data, tmp); err != nil {
		return Installed{}, err
	}
	manifest, err := readManifestFile(tmp)
	if err != nil {
		return Installed{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.indexLocked(manifest.ID) >= 0 {
		return Installed{}, fmt.Errorf("%w: %s", ErrAlreadyInstalled, manifest.ID)
	}
	dest := m.pluginDir(manifest.ID)
	_ = os.RemoveAll(dest)
	if err := os.Rename(tmp, dest); err != nil {
		if err := copyDir(tmp, dest); err != nil {
			return Installed{}, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	rec := record{ID: manifest.ID, Enabled: true, InstalledAt: now, SHA256: got}
	m.state.Plugins = append(m.state.Plugins, rec)
	if err := m.saveStateLocked(); err != nil {
		return Installed{}, err
	}
	if err := m.startBackendLocked(manifest.ID); err != nil {
		logger.Warn("插件后端启动失败", "id", manifest.ID, "err", err)
	}
	return m.installedLocked(rec)
}

func (m *Manager) InstallURL(ctx context.Context, rawURL, wantSHA string) (Installed, error) {
	if m == nil {
		return Installed{}, fmt.Errorf("插件系统不可用")
	}
	fetch := m.fetch
	if fetch == nil {
		return Installed{}, fmt.Errorf("%w: 未配置下载", ErrURLRejected)
	}
	data, err := fetch(ctx, rawURL)
	if err != nil {
		return Installed{}, err
	}
	return m.InstallZip(data, wantSHA)
}

func (m *Manager) SetEnabled(id string, enabled bool) (Installed, error) {
	if m == nil {
		return Installed{}, fmt.Errorf("插件系统不可用")
	}
	id = strings.TrimSpace(id)
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.indexLocked(id)
	if idx < 0 {
		return Installed{}, ErrNotFound
	}
	if m.state.Plugins[idx].Enabled == enabled {
		return m.installedLocked(m.state.Plugins[idx])
	}
	m.state.Plugins[idx].Enabled = enabled
	if err := m.saveStateLocked(); err != nil {
		return Installed{}, err
	}
	if enabled {
		if err := m.startBackendLocked(id); err != nil {
			logger.Warn("插件后端启动失败", "id", id, "err", err)
		}
	} else {
		m.stopBackendLocked(id)
	}
	return m.installedLocked(m.state.Plugins[idx])
}

func (m *Manager) Uninstall(id string) error {
	if m == nil {
		return fmt.Errorf("插件系统不可用")
	}
	id = strings.TrimSpace(id)
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.indexLocked(id)
	if idx < 0 {
		return ErrNotFound
	}
	m.stopBackendLocked(id)
	_ = os.RemoveAll(m.pluginDir(id))
	_ = os.RemoveAll(m.dataDir(id))
	m.state.Plugins = append(m.state.Plugins[:idx], m.state.Plugins[idx+1:]...)
	return m.saveStateLocked()
}

func (m *Manager) AssetPath(id, rel string) (string, error) {
	if m == nil {
		return "", fmt.Errorf("插件系统不可用")
	}
	id = strings.TrimSpace(id)
	rel = strings.TrimPrefix(strings.ReplaceAll(rel, `\`, "/"), "/")
	if rel == "" {
		rel = "index.html"
	}
	if !safeRelativePath(rel) {
		return "", ErrUnsafePath
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.indexLocked(id)
	if idx < 0 {
		return "", ErrNotFound
	}
	if !m.state.Plugins[idx].Enabled {
		return "", ErrPluginDisabled
	}
	root, err := filepath.Abs(m.pluginDir(id))
	if err != nil {
		return "", err
	}
	full, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	if full != root && !strings.HasPrefix(full, root+string(os.PathSeparator)) {
		return "", ErrUnsafePath
	}
	st, err := os.Stat(full)
	if err != nil || st.IsDir() {
		return "", ErrAssetNotFound
	}
	return full, nil
}

func (m *Manager) BackendAddr(id string) (string, error) {
	if m == nil {
		return "", fmt.Errorf("插件系统不可用")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.indexLocked(id)
	if idx < 0 {
		return "", ErrNotFound
	}
	if !m.state.Plugins[idx].Enabled {
		return "", ErrPluginDisabled
	}
	p := m.procs[id]
	if p == nil || p.addr == "" {
		return "", ErrBackendDown
	}
	return p.addr, nil
}

func (m *Manager) pluginDir(id string) string { return filepath.Join(m.root, id) }
func (m *Manager) dataDir(id string) string   { return filepath.Join(m.root, id+"-data") }

func (m *Manager) indexLocked(id string) int {
	for i, rec := range m.state.Plugins {
		if rec.ID == id {
			return i
		}
	}
	return -1
}

func (m *Manager) installedLocked(rec record) (Installed, error) {
	manifest, err := readManifestFile(m.pluginDir(rec.ID))
	if err != nil {
		return Installed{}, err
	}
	cmd, available := manifest.BackendCommand()
	inst := Installed{
		ID:               manifest.ID,
		Name:             manifest.Name,
		Version:          manifest.Version,
		Description:      manifest.Description,
		Author:           manifest.Author,
		Homepage:         manifest.Homepage,
		Permissions:      append([]string(nil), manifest.Permissions...),
		Contributions:    append([]Contribution(nil), manifest.Contributions...),
		Enabled:          rec.Enabled,
		BackendAvailable: available && cmd != "",
		InstalledAt:      rec.InstalledAt,
		SHA256:           rec.SHA256,
	}
	if p := m.procs[rec.ID]; p != nil {
		inst.BackendRunning = p.addr != ""
		inst.BackendError = p.err
	}
	return inst, nil
}

func (m *Manager) loadState() error {
	path := filepath.Join(m.root, stateFilename)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			m.state = stateFile{}
			return nil
		}
		return err
	}
	if err := json.Unmarshal(raw, &m.state); err != nil {
		return err
	}
	return nil
}

func (m *Manager) saveStateLocked() error {
	path := filepath.Join(m.root, stateFilename)
	raw, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func (m *Manager) startEnabledLocked() {
	for _, rec := range m.state.Plugins {
		if !rec.Enabled {
			continue
		}
		if err := m.startBackendLocked(rec.ID); err != nil {
			logger.Warn("插件后端启动失败", "id", rec.ID, "err", err)
		}
	}
}

func (m *Manager) startBackendLocked(id string) error {
	m.stopBackendLocked(id)
	manifest, err := readManifestFile(m.pluginDir(id))
	if err != nil {
		return err
	}
	rel, ok := manifest.BackendCommand()
	if !ok {
		return nil
	}
	bin := filepath.Join(m.pluginDir(id), filepath.FromSlash(rel))
	if st, err := os.Stat(bin); err != nil || st.IsDir() {
		return fmt.Errorf("找不到本平台后端: %s", rel)
	}
	_ = os.Chmod(bin, 0o755)
	addr, err := freeLocalAddr()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.dataDir(id), 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin)
	cmd.Dir = m.pluginDir(id)
	dataDir, _ := filepath.Abs(m.dataDir(id))
	cmd.Env = append(os.Environ(),
		"VODOG_PLUGIN_ID="+id,
		"VODOG_PLUGIN_LISTEN="+addr,
		"VODOG_PLUGIN_DATA_DIR="+dataDir,
		"VOCAT_PLUGIN_ID="+id,
		"VOCAT_PLUGIN_LISTEN="+addr,
		"VOCAT_PLUGIN_DATA_DIR="+dataDir,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return err
	}
	p := &runningBackend{cmd: cmd, cancel: cancel}
	m.procs[id] = p
	go func() {
		waitErr := cmd.Wait()
		m.mu.Lock()
		defer m.mu.Unlock()
		if cur := m.procs[id]; cur != nil && cur.cmd == cmd {
			cur.addr = ""
			msg := strings.TrimSpace(stderr.String())
			if msg == "" && waitErr != nil {
				msg = waitErr.Error()
			}
			if msg == "" {
				msg = "后端已退出"
			}
			cur.err = msg
		}
	}()
	if err := waitTCP(addr, 5*time.Second); err != nil {
		p.err = strings.TrimSpace(stderr.String())
		if p.err == "" {
			p.err = err.Error()
		}
		m.stopBackendLocked(id)
		return err
	}
	p.addr = addr
	return nil
}

func (m *Manager) stopBackendLocked(id string) {
	p := m.procs[id]
	if p == nil {
		return
	}
	delete(m.procs, id)
	if p.cmd != nil && p.cmd.Process != nil {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
	}
	if p.cancel != nil {
		p.cancel()
	}
}

func freeLocalAddr() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr, nil
}

func waitTCP(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		last = err
		time.Sleep(50 * time.Millisecond)
	}
	if last == nil {
		last = fmt.Errorf("timeout")
	}
	return fmt.Errorf("插件后端未监听 %s: %v", addr, last)
}

func copyDir(src, dest string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}
