package pcsc

import (
	"context"
	"errors"
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
