package pcsc

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestListReadersViaFakeDaemon(t *testing.T) {
	startFakeDaemon(t, []fakeReader{
		{name: "Alcor Micro AU9540 00 00", cardPresent: true},
		{name: "Empty Slot", cardPresent: false},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	readers, err := ListReaders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(readers) != 2 {
		t.Fatalf("readers=%+v", readers)
	}
	if readers[0].Name != "Alcor Micro AU9540 00 00" || !readers[0].CardPresent {
		t.Fatalf("first=%+v", readers[0])
	}
	if readers[1].Name != "Empty Slot" || readers[1].CardPresent {
		t.Fatalf("second=%+v", readers[1])
	}
}

func TestListReadersAllowsEmptyDaemonState(t *testing.T) {
	d := startFakeDaemon(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	readers, err := ListReaders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if readers == nil || len(readers) != 0 {
		t.Fatalf("readers=%+v, want non-nil empty slice", readers)
	}
	if d.saw(cmdGetReadersStateArray) {
		t.Fatal("zero-reader response must not request a state array")
	}
}

func TestTransmitRejectsOversizedDaemonResponse(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	serverDone := make(chan error, 1)
	go func() {
		var frame [8]byte
		if _, err := io.ReadFull(serverConn, frame[:]); err != nil {
			serverDone <- err
			return
		}
		body := make([]byte, binary.LittleEndian.Uint32(frame[0:4]))
		if _, err := io.ReadFull(serverConn, body); err != nil {
			serverDone <- err
			return
		}
		sendLen := binary.LittleEndian.Uint32(body[12:16])
		if _, err := io.CopyN(io.Discard, serverConn, int64(sendLen)); err != nil {
			serverDone <- err
			return
		}
		binary.LittleEndian.PutUint32(body[24:28], uint32(maxAPDUResponse+1))
		binary.LittleEndian.PutUint32(body[28:32], scardSuccess)
		_, err := serverConn.Write(body)
		serverDone <- err
	}()

	c := &daemonClient{conn: clientConn, protocol: protocolT1}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := c.transmit(ctx, 1, []byte{0x00, 0xA4, 0x00, 0x00})
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("err=%v, want ErrResponseTooLarge", err)
	}
	if _, err := c.transmit(ctx, 1, []byte{0x00, 0xA4, 0x00, 0x00}); err == nil {
		t.Fatal("connection remained reusable after an oversized response")
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestConnectRejectsReaderNameThatWouldBeTruncated(t *testing.T) {
	c := &daemonClient{}
	_, _, err := c.connect(context.Background(), strings.Repeat("x", maxReaderName))
	if !errors.Is(err, ErrReaderNameTooLong) {
		t.Fatalf("err=%v, want ErrReaderNameTooLong", err)
	}
}

func TestDialDaemonMissingSocket(t *testing.T) {
	t.Setenv("PCSCLITE_CSOCK_NAME", "/tmp/vodoge-no-pcscd-"+t.Name())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := dialDaemon(ctx, []string{"/tmp/vodoge-no-pcscd-" + t.Name()})
	if !errors.Is(err, ErrAPDUUnavailable) {
		t.Fatalf("err=%v want ErrAPDUUnavailable", err)
	}
}

func TestParseReaderStatesOffset(t *testing.T) {
	buf := make([]byte, readerStateSize)
	copy(buf[:maxReaderName], "Reader X")
	// eventCounter at 128 must not be mistaken for present
	buf[128] = 0x20
	got := parseReaderStates(buf)
	if len(got) != 1 || got[0].CardPresent {
		t.Fatalf("eventCounter must not look like a card: %+v", got)
	}
	buf[readerStateOff] = byte(scardPresent)
	got = parseReaderStates(buf)
	if len(got) != 1 || !got[0].CardPresent || got[0].Name != "Reader X" {
		t.Fatalf("present=%+v", got)
	}
}
