package pcsc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestChannelOpenLogicalChannelAndTransmit(t *testing.T) {
	d := startFakeDaemon(t, []fakeReader{{name: "Alcor Micro AU9540 00 00", cardPresent: true}})
	ch := NewChannel("Alcor Micro AU9540 00 00")
	if err := ch.Connect(); err != nil {
		t.Fatal(err)
	}
	defer ch.Disconnect()

	aid := []byte{0xA0, 0x00, 0x00, 0x05, 0x59, 0x10, 0x10, 0xFF}
	n, err := ch.OpenLogicalChannel(aid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("channel=%d want 1", n)
	}
	resp, err := ch.Transmit([]byte{0x80, 0xE2, 0x91, 0x00, 0x00})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(resp, []byte{0x90, 0x00}) {
		t.Fatalf("resp=%X", resp)
	}
	if err := ch.CloseLogicalChannel(1); err != nil {
		t.Fatal(err)
	}
	if err := ch.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if d.lastAPDU() == nil {
		t.Fatal("expected APDU to reach fake pcscd")
	}
	if !d.saw(cmdBeginTransaction) || !d.saw(cmdEndTransaction) {
		t.Fatal("session must BeginTransaction after Connect and EndTransaction before Disconnect")
	}
}

func TestChannelDisconnectUsesGateAndReportsCleanupErrors(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		defer serverConn.Close()
		_, _ = io.CopyN(io.Discard, serverConn, 20) // end-transaction frame
	}()

	ch := &Channel{
		client: &daemonClient{conn: clientConn, contextID: 7},
		hCard:  3,
		inTxn:  true,
	}
	var entered atomic.Int32
	var exited atomic.Int32
	ch.SetGate(func() func() {
		entered.Add(1)
		return func() { exited.Add(1) }
	})

	err := ch.Disconnect()
	if err == nil {
		t.Fatal("Disconnect() error=nil, want cleanup error")
	}
	if entered.Load() != 1 || exited.Load() != 1 {
		t.Fatalf("gate entered=%d exited=%d, want 1/1", entered.Load(), exited.Load())
	}
	if ch.client != nil || ch.hCard != 0 || ch.inTxn {
		t.Fatalf("channel state not cleared: client=%v hCard=%d inTxn=%v", ch.client, ch.hCard, ch.inTxn)
	}
	<-serverDone
}

func TestChannelConnectNoCard(t *testing.T) {
	startFakeDaemon(t, []fakeReader{{name: "Empty", cardPresent: false}})
	ch := NewChannel("Empty")
	err := ch.Connect()
	if !errors.Is(err, ErrNoCard) {
		t.Fatalf("err=%v want ErrNoCard", err)
	}
}

func TestDiscoverPrefersProtocolList(t *testing.T) {
	b := &SystemBackend{
		Sockets: []string{"/run/pcscd/pcscd.comm"},
		Stat:    func(string) (os.FileInfo, error) { return fakeFileInfo{}, nil },
		ListReaders: func(context.Context) ([]Reader, error) {
			return []Reader{{Name: "FromProtocol", CardPresent: true}}, nil
		},
		LookPath: func(string) (string, error) { return "/usr/bin/pcsc_scan", nil },
		RunPCSC: func(context.Context, string) (string, error) {
			t.Fatal("pcsc_scan must not run when protocol list works")
			return "", nil
		},
	}
	st := b.Discover(context.Background())
	if st.Daemon != DaemonRunning || len(st.Readers) != 1 || st.Readers[0].Name != "FromProtocol" {
		t.Fatalf("status=%+v", st)
	}
}

func TestListReadersTimeoutContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	_, err := ListReaders(ctx)
	if err == nil {
		t.Fatal("expected timeout or unavailable")
	}
}
