package pcsc

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeReader struct {
	name        string
	cardPresent bool
}

type fakeDaemon struct {
	t       *testing.T
	socket  string
	ln      net.Listener
	readers []fakeReader
	mu      sync.Mutex
	seen    [][]byte
	cmds    []uint32
}

func startFakeDaemon(t *testing.T, readers []fakeReader) *fakeDaemon {
	t.Helper()
	// macOS sun_path 很短，t.TempDir() 拼出来会 bind: invalid argument。
	socket := filepath.Join(os.TempDir(), fmt.Sprintf("vd-pcsc-%d.comm", time.Now().UnixNano()))
	_ = os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	d := &fakeDaemon{t: t, socket: socket, ln: ln, readers: readers}
	t.Setenv("PCSCLITE_CSOCK_NAME", socket)
	go d.serve()
	t.Cleanup(func() {
		_ = ln.Close()
		_ = os.Remove(socket)
	})
	return d
}

func (d *fakeDaemon) serve() {
	for {
		conn, err := d.ln.Accept()
		if err != nil {
			return
		}
		go d.handle(conn)
	}
}

func (d *fakeDaemon) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	var ctxID uint32 = 7
	var hCard int32 = 3
	var proto uint32 = protocolT1
	for {
		var hdr [8]byte
		if _, err := io.ReadFull(conn, hdr[:]); err != nil {
			return
		}
		size := binary.LittleEndian.Uint32(hdr[0:4])
		cmd := binary.LittleEndian.Uint32(hdr[4:8])
		body := make([]byte, size)
		if size > 0 {
			if _, err := io.ReadFull(conn, body); err != nil {
				return
			}
		}
		d.mu.Lock()
		d.cmds = append(d.cmds, cmd)
		d.mu.Unlock()
		switch cmd {
		case cmdVersion:
			if len(body) < 12 {
				return
			}
			binary.LittleEndian.PutUint32(body[0:4], pcscProtoMajor)
			binary.LittleEndian.PutUint32(body[4:8], pcscProtoMinor)
			binary.LittleEndian.PutUint32(body[8:12], scardSuccess)
			_, _ = conn.Write(body)
		case cmdEstablishContext:
			if len(body) < 12 {
				return
			}
			binary.LittleEndian.PutUint32(body[4:8], ctxID)
			binary.LittleEndian.PutUint32(body[8:12], scardSuccess)
			_, _ = conn.Write(body)
		case cmdReleaseContext:
			if len(body) < 8 {
				return
			}
			binary.LittleEndian.PutUint32(body[4:8], scardSuccess)
			_, _ = conn.Write(body)
		case cmdGetReadersStateSize:
			n := make([]byte, 4)
			binary.LittleEndian.PutUint32(n, uint32(len(d.readers)))
			if len(d.readers) == 0 {
				binary.LittleEndian.PutUint32(n, 1)
			}
			_, _ = conn.Write(n)
		case cmdGetReadersStateArray, cmdGetReadersState:
			count := len(d.readers)
			if count == 0 {
				count = 1
			}
			buf := make([]byte, count*readerStateSize)
			for i, r := range d.readers {
				off := i * readerStateSize
				copy(buf[off:off+maxReaderName], r.name)
				if r.cardPresent {
					binary.LittleEndian.PutUint32(buf[off+readerStateOff:off+readerStateOff+4], scardPresent)
				}
			}
			_, _ = conn.Write(buf)
		case cmdConnect:
			if len(body) < 4+maxReaderName+20 {
				return
			}
			name := cString(body[4 : 4+maxReaderName])
			present := false
			for _, r := range d.readers {
				if r.name == name {
					present = r.cardPresent
					break
				}
			}
			rvOff := len(body) - 4
			if !present {
				binary.LittleEndian.PutUint32(body[rvOff:], scardNoCard)
				_, _ = conn.Write(body)
				continue
			}
			binary.LittleEndian.PutUint32(body[4+maxReaderName+8:], uint32(hCard))
			binary.LittleEndian.PutUint32(body[4+maxReaderName+12:], proto)
			binary.LittleEndian.PutUint32(body[rvOff:], scardSuccess)
			_, _ = conn.Write(body)
		case cmdDisconnect:
			if len(body) < 12 {
				return
			}
			binary.LittleEndian.PutUint32(body[8:12], scardSuccess)
			_, _ = conn.Write(body)
		case cmdBeginTransaction:
			if len(body) < 8 {
				return
			}
			binary.LittleEndian.PutUint32(body[4:8], scardSuccess)
			_, _ = conn.Write(body)
		case cmdEndTransaction:
			if len(body) < 12 {
				return
			}
			binary.LittleEndian.PutUint32(body[8:12], scardSuccess)
			_, _ = conn.Write(body)
		case cmdTransmit:
			if len(body) < 32 {
				return
			}
			sendLen := binary.LittleEndian.Uint32(body[12:16])
			apdu := make([]byte, sendLen)
			if sendLen > 0 {
				if _, err := io.ReadFull(conn, apdu); err != nil {
					return
				}
			}
			d.mu.Lock()
			d.seen = append(d.seen, append([]byte(nil), apdu...))
			d.mu.Unlock()
			resp := fakeAPDU(apdu)
			binary.LittleEndian.PutUint32(body[24:28], uint32(len(resp)))
			binary.LittleEndian.PutUint32(body[28:32], scardSuccess)
			_, _ = conn.Write(body)
			_, _ = conn.Write(resp)
		default:
			return
		}
	}
}

func fakeAPDU(apdu []byte) []byte {
	if len(apdu) >= 4 && apdu[1] == 0x70 && apdu[2] == 0x00 {
		return []byte{0x01, 0x90, 0x00}
	}
	if len(apdu) >= 4 && apdu[1] == 0x70 && apdu[2] == 0x80 {
		return []byte{0x90, 0x00}
	}
	if len(apdu) >= 4 && apdu[1] == 0xA4 {
		return []byte{0x90, 0x00}
	}
	if len(apdu) >= 4 && apdu[1] == 0xC0 {
		return []byte{0x90, 0x00}
	}
	return []byte{0x90, 0x00}
}

func (d *fakeDaemon) saw(cmd uint32) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.cmds {
		if c == cmd {
			return true
		}
	}
	return false
}

func (d *fakeDaemon) lastAPDU() []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.seen) == 0 {
		return nil
	}
	return append([]byte(nil), d.seen[len(d.seen)-1]...)
}
