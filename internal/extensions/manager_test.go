package extensions

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func pluginZip(t *testing.T, manifest string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, body string) {
		t.Helper()
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	write(ManifestFilename, manifest)
	for name, body := range files {
		write(name, body)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sidebarManifest(id string) string {
	return fmt.Sprintf(`{
		"schema_version": 1,
		"id": %q,
		"name": "Demo",
		"version": "0.1.0",
		"contributions": [
			{"id": "demo-page", "label": "Demo", "location": "sidebar", "entry": "index.html"}
		]
	}`, id)
}

func TestInstallEnableDisableUninstall(t *testing.T) {
	mgr, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	data := pluginZip(t, sidebarManifest("hello-lab"), map[string]string{
		"index.html": "<html>hi</html>",
	})
	inst, err := mgr.InstallZip(data, "")
	if err != nil {
		t.Fatal(err)
	}
	if !inst.Enabled || inst.ID != "hello-lab" || inst.SHA256 == "" {
		t.Fatalf("%+v", inst)
	}
	list := mgr.List()
	if len(list) != 1 || list[0].ID != "hello-lab" {
		t.Fatalf("list=%+v", list)
	}
	path, err := mgr.AssetPath("hello-lab", "index.html")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "<html>hi</html>" {
		t.Fatalf("asset=%q err=%v", raw, err)
	}
	inst, err = mgr.SetEnabled("hello-lab", false)
	if err != nil || inst.Enabled {
		t.Fatalf("disable: %+v %v", inst, err)
	}
	if _, err := mgr.AssetPath("hello-lab", "index.html"); !errors.Is(err, ErrPluginDisabled) {
		t.Fatalf("disabled assets: %v", err)
	}
	if err := mgr.Uninstall("hello-lab"); err != nil {
		t.Fatal(err)
	}
	if len(mgr.List()) != 0 {
		t.Fatal("expected empty list")
	}
}

func TestCloseConcurrentIsIdempotent(t *testing.T) {
	var cancelCalls atomic.Int32
	mgr := &Manager{
		procs: map[string]*runningBackend{
			"demo": {cancel: func() { cancelCalls.Add(1) }},
		},
	}

	const callers = 32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			mgr.Close()
		}()
	}
	close(start)
	wg.Wait()
	mgr.Close()

	if got := cancelCalls.Load(); got != 1 {
		t.Fatalf("backend cancel calls=%d want=1", got)
	}
	if _, err := mgr.SetEnabled("demo", true); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("SetEnabled after Close error=%v want ErrManagerClosed", err)
	}
}

func TestInstallZipRefusesReplace(t *testing.T) {
	mgr, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	data := pluginZip(t, sidebarManifest("hello-lab"), map[string]string{"index.html": "a"})
	if _, err := mgr.InstallZip(data, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.InstallZip(data, ""); !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("err=%v want ErrAlreadyInstalled", err)
	}
}

func TestInstallPersistenceFailureRollsBackMemoryAndFiles(t *testing.T) {
	root := t.TempDir()
	mgr, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	statePath := filepath.Join(root, stateFilename)
	if err := os.Mkdir(statePath, 0o755); err != nil {
		t.Fatal(err)
	}
	data := pluginZip(t, sidebarManifest("rollback-install"), map[string]string{"index.html": "x"})
	if _, err := mgr.InstallZip(data, ""); err == nil {
		t.Fatal("InstallZip should fail when state cannot be replaced")
	}
	if got := mgr.List(); len(got) != 0 {
		t.Fatalf("memory state committed after persistence failure: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, "rollback-install")); !os.IsNotExist(err) {
		t.Fatalf("plugin directory remains after persistence failure: %v", err)
	}
}

func TestMutationPersistenceFailureLeavesInstalledPluginUnchanged(t *testing.T) {
	root := t.TempDir()
	mgr, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	data := pluginZip(t, sidebarManifest("stable-state"), map[string]string{"index.html": "x"})
	if _, err := mgr.InstallZip(data, ""); err != nil {
		t.Fatal(err)
	}

	statePath := filepath.Join(root, stateFilename)
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(statePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.SetEnabled("stable-state", false); err == nil {
		t.Fatal("SetEnabled should fail when state cannot be replaced")
	}
	list := mgr.List()
	if len(list) != 1 || !list[0].Enabled {
		t.Fatalf("enable state changed after persistence failure: %+v", list)
	}
	if err := mgr.Uninstall("stable-state"); err == nil {
		t.Fatal("Uninstall should fail when state cannot be replaced")
	}
	if _, err := mgr.AssetPath("stable-state", "index.html"); err != nil {
		t.Fatalf("plugin files changed after uninstall persistence failure: %v", err)
	}
}

func TestLockedBufferConcurrentAccess(t *testing.T) {
	var buf lockedBuffer
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 1000 {
				_, _ = buf.Write([]byte("stderr\n"))
				_ = buf.String()
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := buf.String(); got == "" {
		t.Fatal("buffer unexpectedly empty")
	}
}

func TestListSortsByName(t *testing.T) {
	mgr, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	if _, err := mgr.InstallZip(pluginZip(t, sidebarManifest("zeta-one"), map[string]string{"index.html": "z"}), ""); err != nil {
		t.Fatal(err)
	}
	mf := `{
		"schema_version": 1,
		"id": "alpha-two",
		"name": "Alpha",
		"version": "1.0.0",
		"contributions": [{"id": "page-a", "label": "P", "location": "sidebar", "entry": "index.html"}]
	}`
	if _, err := mgr.InstallZip(pluginZip(t, mf, map[string]string{"index.html": "a"}), ""); err != nil {
		t.Fatal(err)
	}
	list := mgr.List()
	if len(list) != 2 || list[0].Name != "Alpha" || list[1].Name != "Demo" {
		t.Fatalf("list=%+v", list)
	}
}

func TestInstallURLUsesFetcherAndRejectsChecksum(t *testing.T) {
	mgr, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	data := pluginZip(t, sidebarManifest("from-url"), map[string]string{"index.html": "x"})
	mgr.fetch = func(context.Context, string) ([]byte, error) { return data, nil }
	if _, err := mgr.InstallURL(context.Background(), "https://example.com/p.zip", "deadbeef"); !errors.Is(err, ErrChecksum) {
		t.Fatalf("err=%v", err)
	}
	inst, err := mgr.InstallURL(context.Background(), "https://example.com/p.zip", "")
	if err != nil || inst.ID != "from-url" {
		t.Fatalf("%+v %v", inst, err)
	}
}

func TestInstallRejectsUnknownManifestName(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("plugin.json")
	_, _ = w.Write([]byte(sidebarManifest("unknown-manifest")))
	w, _ = zw.Create("index.html")
	_, _ = w.Write([]byte("ok"))
	_ = zw.Close()
	mgr, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	if _, err := mgr.InstallZip(buf.Bytes(), ""); err == nil {
		t.Fatal("zip without vodoge-plugin.json must be rejected")
	}
}

func TestAssetPathRejectsTraversal(t *testing.T) {
	mgr, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	_, err = mgr.InstallZip(pluginZip(t, sidebarManifest("safe-one"), map[string]string{"index.html": "x"}), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.AssetPath("safe-one", "../state.json"); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("err=%v", err)
	}
}

func TestBackendProcessAndAddr(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain required to build plugin backend")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	body := `package main
import (
	"net/http"
	"os"
)
func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pong"))
	})
	_ = http.ListenAndServe(os.Getenv("VODOGE_PLUGIN_LISTEN"), nil)
}
`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	binName := "plugin-backend"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	bin := filepath.Join(dir, binName)
	cmd := exec.Command("go", "build", "-o", bin, src)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	rawBin, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	rel := "bin/" + runtime.GOOS + "-" + runtime.GOARCH
	mf := fmt.Sprintf(`{
		"schema_version": 1,
		"id": "with-backend",
		"name": "Backend",
		"version": "1.0.0",
		"contributions": [{"id": "page", "label": "P", "location": "sidebar", "entry": "index.html"}],
		"backend": {"commands": {%q: %q}}
	}`, runtime.GOOS+"/"+runtime.GOARCH, rel)
	w, _ := zw.Create(ManifestFilename)
	_, _ = w.Write([]byte(mf))
	w, _ = zw.Create("index.html")
	_, _ = w.Write([]byte("ui"))
	bw, err := zw.Create(rel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bw.Write(rawBin); err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()

	mgr, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	inst, err := mgr.InstallZip(buf.Bytes(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !inst.BackendAvailable || !inst.BackendRunning {
		t.Fatalf("backend not running: %+v", inst)
	}
	addr, err := mgr.BackendAddr("with-backend")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := net.ResolveTCPAddr("tcp", addr); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if _, err := mgr.SetEnabled("with-backend", false); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.BackendAddr("with-backend"); !errors.Is(err, ErrPluginDisabled) && !errors.Is(err, ErrBackendDown) {
		t.Fatalf("after disable: %v", err)
	}
}
