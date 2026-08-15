package pcsc

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

type fakeFileInfo struct{}

func (fakeFileInfo) Name() string       { return "pcscd.comm" }
func (fakeFileInfo) Size() int64        { return 0 }
func (fakeFileInfo) Mode() os.FileMode  { return os.ModeSocket }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeFileInfo) IsDir() bool        { return false }
func (fakeFileInfo) Sys() any           { return nil }

func TestDiscoverMissingDaemon(t *testing.T) {
	b := &SystemBackend{
		Sockets: []string{"/tmp/vodog-no-such-pcscd.comm"},
		Stat: func(string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}
	st := b.Discover(context.Background())
	if st.Daemon != DaemonMissing {
		t.Fatalf("daemon=%q want missing", st.Daemon)
	}
	if st.Message == "" {
		t.Fatal("missing pcscd must explain itself, not hide")
	}
	if st.Readers == nil || len(st.Readers) != 0 {
		t.Fatalf("readers must be empty list, got %#v", st.Readers)
	}
}

func TestDiscoverRunningWithReaders(t *testing.T) {
	b := &SystemBackend{
		Sockets: []string{"/run/pcscd/pcscd.comm"},
		Stat: func(string) (os.FileInfo, error) {
			return fakeFileInfo{}, nil
		},
		ListReaders: func(context.Context) ([]Reader, error) {
			return nil, errors.New("force scan fallback")
		},
		LookPath: func(string) (string, error) { return "/usr/bin/pcsc_scan", nil },
		RunPCSC: func(context.Context, string) (string, error) {
			return "PC/SC device scanner\nReader 0: Alcor Micro AU9540 00 00\n", nil
		},
	}
	st := b.Discover(context.Background())
	if st.Daemon != DaemonRunning {
		t.Fatalf("daemon=%q want running", st.Daemon)
	}
	if st.Socket != "/run/pcscd/pcscd.comm" {
		t.Fatalf("socket=%q", st.Socket)
	}
	if len(st.Readers) != 1 || st.Readers[0].Name != "Alcor Micro AU9540 00 00" {
		t.Fatalf("readers=%+v", st.Readers)
	}
}

func TestDiscoverRunningButScanFails(t *testing.T) {
	b := &SystemBackend{
		Sockets:     []string{"/run/pcscd/pcscd.comm"},
		Stat:        func(string) (os.FileInfo, error) { return fakeFileInfo{}, nil },
		ListReaders: func(context.Context) ([]Reader, error) { return nil, errors.New("force scan fallback") },
		LookPath:    func(string) (string, error) { return "/usr/bin/pcsc_scan", nil },
		RunPCSC: func(context.Context, string) (string, error) {
			return "", errors.New("exit 1")
		},
	}
	st := b.Discover(context.Background())
	if st.Daemon != DaemonError {
		t.Fatalf("daemon=%q want error", st.Daemon)
	}
	if st.Message == "" {
		t.Fatal("error must include a message")
	}
}

func TestParsePCSCScanReadersIgnoresNoise(t *testing.T) {
	out := parsePCSCScanReaders("Waiting for the first reader...\nReader 0: Foo Bar\nReader 0: Foo Bar\n")
	if len(out) != 1 || out[0].Name != "Foo Bar" {
		t.Fatalf("got %+v", out)
	}
}
