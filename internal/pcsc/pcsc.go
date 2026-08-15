// Package pcsc 发现本机 USB CCID 读卡器。
//
// 第 3 期才写 profile。这里只回答两件事：pcscd 在不在、有哪些读卡器。
// 不引入 libpcsclite / CGO，避免把生产构建绑到 PC/SC 头文件上。
package pcsc

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	DaemonRunning = "running"
	DaemonMissing = "missing"
	DaemonError   = "error"
)

type Reader struct {
	Name        string `json:"name"`
	ClaimedID   string `json:"claimed_id,omitempty"`
	CardPresent bool   `json:"card_present,omitempty"`
}

type Status struct {
	Daemon  string   `json:"daemon"` // running|missing|error
	Message string   `json:"message"`
	Socket  string   `json:"socket,omitempty"`
	Readers []Reader `json:"readers"`
}

type Backend interface {
	Discover(ctx context.Context) Status
}

// DefaultSockets 是 pcsclite 常见的 UNIX 套接字。
var DefaultSockets = []string{
	"/run/pcscd/pcscd.comm",
	"/var/run/pcscd/pcscd.comm",
}

type SystemBackend struct {
	Sockets     []string
	LookPath    func(string) (string, error)
	RunPCSC     func(ctx context.Context, bin string) (string, error)
	Stat        func(string) (os.FileInfo, error)
	ListReaders func(ctx context.Context) ([]Reader, error)
}

func System() *SystemBackend {
	return &SystemBackend{
		Sockets:  append([]string(nil), DefaultSockets...),
		LookPath: exec.LookPath,
		Stat:     os.Stat,
		RunPCSC: func(ctx context.Context, bin string) (string, error) {
			cmd := exec.CommandContext(ctx, bin, "-n")
			out, err := cmd.CombinedOutput()
			return string(out), err
		},
	}
}

func (b *SystemBackend) Discover(ctx context.Context) Status {
	if b == nil {
		b = System()
	}
	statFn := b.Stat
	if statFn == nil {
		statFn = os.Stat
	}
	look := b.LookPath
	if look == nil {
		look = exec.LookPath
	}

	socket := firstExistingSocket(b.Sockets, statFn)
	if socket == "" {
		return Status{
			Daemon:  DaemonMissing,
			Message: "未检测到 pcscd（PC/SC 智能卡服务未运行）。读卡器写卡需要先安装并启动 pcscd。",
			Readers: []Reader{},
		}
	}

	st := Status{
		Daemon:  DaemonRunning,
		Message: "pcscd 在运行",
		Socket:  socket,
		Readers: []Reader{},
	}

	list := b.ListReaders
	if list == nil {
		list = ListReaders
	}
	if readers, err := list(ctx); err == nil {
		if readers == nil {
			readers = []Reader{}
		}
		st.Readers = readers
		if len(readers) == 0 {
			st.Message = "pcscd 在运行，当前没有读卡器"
		}
		return st
	}

	bin, err := look("pcsc_scan")
	if err != nil || strings.TrimSpace(bin) == "" {
		st.Message = "pcscd 在运行，但列举读卡器失败，且本机没有 pcsc_scan"
		return st
	}

	run := b.RunPCSC
	if run == nil {
		return st
	}
	scanCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := run(scanCtx, bin)
	if err != nil && strings.TrimSpace(out) == "" {
		st.Daemon = DaemonError
		st.Message = "pcscd 套接字存在，但列举读卡器失败: " + err.Error()
		return st
	}
	st.Readers = parsePCSCScanReaders(out)
	if len(st.Readers) == 0 {
		st.Message = "pcscd 在运行，当前没有读卡器"
	}
	return st
}

func firstExistingSocket(paths []string, statFn func(string) (os.FileInfo, error)) string {
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, err := statFn(p); err == nil {
			return p
		}
	}
	return ""
}

// parsePCSCScanReaders 从 `pcsc_scan -n` 输出里抽出读卡器名。
// 常见行形如：`Reader 0: Alcor Micro AU9540 00 00`
func parsePCSCScanReaders(out string) []Reader {
	seen := map[string]struct{}{}
	var readers []Reader
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "reader") {
			continue
		}
		name := line
		if i := strings.Index(line, ":"); i >= 0 {
			name = strings.TrimSpace(line[i+1:])
		}
		name = strings.TrimSpace(name)
		if name == "" || strings.EqualFold(name, "reader") {
			continue
		}
		if strings.Contains(strings.ToLower(name), "waiting for the first reader") {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		readers = append(readers, Reader{Name: name})
	}
	return readers
}
