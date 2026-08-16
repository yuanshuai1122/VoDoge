// pcsc-lite 守护进程协议（公开的 MUSCLE/PCSC 协议）。
//
// 直接说 Unix 套接字，避免 CGO / libpcsclite，这样静态 Linux 二进制也能写卡。
// 命令号与结构体布局来自 pcsclite 公开头文件 winscard_msg.h / pcsclite.h。
package pcsc

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

const (
	pcscProtoMajor = 4
	pcscProtoMinor = 6

	cmdEstablishContext     = 0x01
	cmdReleaseContext       = 0x02
	cmdConnect              = 0x04
	cmdDisconnect           = 0x06
	cmdBeginTransaction     = 0x07
	cmdEndTransaction       = 0x08
	cmdTransmit             = 0x09
	cmdVersion              = 0x11
	cmdGetReadersState      = 0x12
	cmdGetReadersStateSize  = 0x16
	cmdGetReadersStateArray = 0x17

	scopeSystem     = 0x0002
	protocolT0      = 0x0001
	protocolT1      = 0x0002
	protocolAny     = protocolT0 | protocolT1
	shareShared     = 0x0002
	leaveCard       = 0x0000
	scardPresent    = uint32(0x0004) // SCardStatus：卡在位（不是 SCARD_STATE_PRESENT）
	readerStateOff  = 132
	scardSuccess    = uint32(0)
	scardNoCard     = uint32(0x8010000C)
	maxReaderName   = 128
	maxATR          = 33
	maxReaders      = 16
	readerStateSize = 184
	ioPCILength     = 8
	maxAPDUResponse = 64 << 10
)

type daemonClient struct {
	conn        net.Conn
	contextID   uint32
	protocol    uint32
	serverMinor uint32
}

func dialDaemon(ctx context.Context, sockets []string) (*daemonClient, error) {
	if len(sockets) == 0 {
		sockets = DefaultSockets
	}
	if env := strings.TrimSpace(os.Getenv("PCSCLITE_CSOCK_NAME")); env != "" {
		sockets = append([]string{env}, sockets...)
	}
	var errs []error
	seen := map[string]bool{}
	for _, path := range sockets {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		d := net.Dialer{Timeout: 5 * time.Second}
		conn, err := d.DialContext(ctx, "unix", path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		c := &daemonClient{conn: conn}
		if err := c.handshake(ctx); err != nil {
			_ = conn.Close()
			errs = append(errs, err)
			continue
		}
		return c, nil
	}
	if len(errs) == 0 {
		return nil, fmt.Errorf("%w: 没有可用的 pcscd 套接字", ErrAPDUUnavailable)
	}
	return nil, fmt.Errorf("%w: %v", ErrAPDUUnavailable, errors.Join(errs...))
}

func (c *daemonClient) handshake(ctx context.Context) error {
	ver := make([]byte, 12)
	binary.LittleEndian.PutUint32(ver[0:4], pcscProtoMajor)
	binary.LittleEndian.PutUint32(ver[4:8], pcscProtoMinor)
	if err := c.exchange(ctx, cmdVersion, ver); err != nil {
		return err
	}
	if rv := binary.LittleEndian.Uint32(ver[8:12]); rv != scardSuccess {
		return fmt.Errorf("pcscd 协议协商失败: 0x%x", rv)
	}
	c.serverMinor = binary.LittleEndian.Uint32(ver[4:8])
	est := make([]byte, 12)
	binary.LittleEndian.PutUint32(est[0:4], scopeSystem)
	if err := c.exchange(ctx, cmdEstablishContext, est); err != nil {
		return err
	}
	if rv := binary.LittleEndian.Uint32(est[8:12]); rv != scardSuccess {
		return fmt.Errorf("SCardEstablishContext 失败: 0x%x", rv)
	}
	c.contextID = binary.LittleEndian.Uint32(est[4:8])
	return nil
}

func (c *daemonClient) close(ctx context.Context) error {
	if c == nil || c.conn == nil {
		return nil
	}
	var errs []error
	if c.contextID != 0 {
		body := make([]byte, 8)
		binary.LittleEndian.PutUint32(body[0:4], c.contextID)
		if err := c.exchange(ctx, cmdReleaseContext, body); err != nil {
			errs = append(errs, fmt.Errorf("SCardReleaseContext: %w", err))
		} else if rv := binary.LittleEndian.Uint32(body[4:8]); rv != scardSuccess {
			errs = append(errs, fmt.Errorf("SCardReleaseContext 失败: 0x%x", rv))
		}
	}
	if err := c.conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		errs = append(errs, err)
	}
	c.conn = nil
	c.contextID = 0
	return errors.Join(errs...)
}

// ListReaders 向本机 pcscd 要当前读卡器名单。套接字不在或握手失败时返回 ErrAPDUUnavailable。
func ListReaders(ctx context.Context) ([]Reader, error) {
	c, err := dialDaemon(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.close(ctx) }()
	return c.listReaders(ctx)
}

func (c *daemonClient) listReaders(ctx context.Context) ([]Reader, error) {
	var buf []byte
	if c.serverMinor >= 6 {
		if err := c.send(ctx, cmdGetReadersStateSize, nil); err != nil {
			return nil, err
		}
		var nbuf [4]byte
		if err := c.readFull(nbuf[:]); err != nil {
			return nil, err
		}
		n := int(int32(binary.LittleEndian.Uint32(nbuf[:])))
		if n < 0 || n > maxReaders {
			return nil, fmt.Errorf("pcscd 读卡器数量异常: %d", n)
		}
		if n == 0 {
			return []Reader{}, nil
		}
		if err := c.send(ctx, cmdGetReadersStateArray, nil); err != nil {
			return nil, err
		}
		buf = make([]byte, n*readerStateSize)
		if err := c.readFull(buf); err != nil {
			return nil, err
		}
	} else {
		if err := c.send(ctx, cmdGetReadersState, nil); err != nil {
			return nil, err
		}
		buf = make([]byte, maxReaders*readerStateSize)
		if err := c.readFull(buf); err != nil {
			return nil, err
		}
	}
	return parseReaderStates(buf), nil
}

func parseReaderStates(buf []byte) []Reader {
	var out []Reader
	for i := 0; i+readerStateSize <= len(buf); i += readerStateSize {
		name := cString(buf[i : i+maxReaderName])
		if name == "" {
			continue
		}
		state := binary.LittleEndian.Uint32(buf[i+readerStateOff : i+readerStateOff+4])
		out = append(out, Reader{Name: name, CardPresent: state&scardPresent != 0})
	}
	return out
}

func (c *daemonClient) connect(ctx context.Context, reader string) (int32, uint32, error) {
	if err := ValidateReaderName(reader); err != nil {
		return 0, 0, err
	}
	body := make([]byte, 4+maxReaderName+4+4+4+4+4)
	binary.LittleEndian.PutUint32(body[0:4], c.contextID)
	copy(body[4:4+maxReaderName], reader)
	binary.LittleEndian.PutUint32(body[4+maxReaderName:], shareShared)
	binary.LittleEndian.PutUint32(body[4+maxReaderName+4:], protocolAny)
	if err := c.exchange(ctx, cmdConnect, body); err != nil {
		return 0, 0, err
	}
	rvOff := len(body) - 4
	rv := binary.LittleEndian.Uint32(body[rvOff:])
	if rv == scardNoCard {
		return 0, 0, ErrNoCard
	}
	if rv != scardSuccess {
		return 0, 0, fmt.Errorf("SCardConnect %q 失败: 0x%x", reader, rv)
	}
	hCard := int32(binary.LittleEndian.Uint32(body[4+maxReaderName+8:]))
	proto := binary.LittleEndian.Uint32(body[4+maxReaderName+12:])
	if proto == 0 {
		proto = protocolT1
	}
	c.protocol = proto
	return hCard, proto, nil
}

func (c *daemonClient) disconnect(ctx context.Context, hCard int32) error {
	body := make([]byte, 12)
	binary.LittleEndian.PutUint32(body[0:4], uint32(hCard))
	binary.LittleEndian.PutUint32(body[4:8], leaveCard)
	if err := c.exchange(ctx, cmdDisconnect, body); err != nil {
		return err
	}
	if rv := binary.LittleEndian.Uint32(body[8:12]); rv != scardSuccess {
		return fmt.Errorf("SCardDisconnect 失败: 0x%x", rv)
	}
	return nil
}

func (c *daemonClient) beginTransaction(ctx context.Context, hCard int32) error {
	body := make([]byte, 8)
	binary.LittleEndian.PutUint32(body[0:4], uint32(hCard))
	if err := c.exchange(ctx, cmdBeginTransaction, body); err != nil {
		return err
	}
	if rv := binary.LittleEndian.Uint32(body[4:8]); rv != scardSuccess {
		return fmt.Errorf("SCardBeginTransaction 失败: 0x%x", rv)
	}
	return nil
}

func (c *daemonClient) endTransaction(ctx context.Context, hCard int32) error {
	body := make([]byte, 12)
	binary.LittleEndian.PutUint32(body[0:4], uint32(hCard))
	binary.LittleEndian.PutUint32(body[4:8], leaveCard)
	if err := c.exchange(ctx, cmdEndTransaction, body); err != nil {
		return err
	}
	if rv := binary.LittleEndian.Uint32(body[8:12]); rv != scardSuccess {
		return fmt.Errorf("SCardEndTransaction 失败: 0x%x", rv)
	}
	return nil
}

func (c *daemonClient) transmit(ctx context.Context, hCard int32, apdu []byte) ([]byte, error) {
	hdr := make([]byte, 32)
	binary.LittleEndian.PutUint32(hdr[0:4], uint32(hCard))
	proto := c.protocol
	if proto == 0 {
		proto = protocolT1
	}
	binary.LittleEndian.PutUint32(hdr[4:8], proto)
	binary.LittleEndian.PutUint32(hdr[8:12], ioPCILength)
	binary.LittleEndian.PutUint32(hdr[12:16], uint32(len(apdu)))
	binary.LittleEndian.PutUint32(hdr[16:20], proto)
	binary.LittleEndian.PutUint32(hdr[20:24], ioPCILength)
	binary.LittleEndian.PutUint32(hdr[24:28], 65536)
	if err := c.send(ctx, cmdTransmit, hdr); err != nil {
		return nil, err
	}
	if err := c.writeFull(apdu); err != nil {
		return nil, err
	}
	if err := c.readFull(hdr); err != nil {
		return nil, err
	}
	rv := binary.LittleEndian.Uint32(hdr[28:32])
	if rv != scardSuccess {
		if rv == scardNoCard {
			return nil, ErrNoCard
		}
		return nil, fmt.Errorf("SCardTransmit 失败: 0x%x", rv)
	}
	n := binary.LittleEndian.Uint32(hdr[24:28])
	if n == 0 {
		return nil, nil
	}
	if n > maxAPDUResponse {
		// The response body is still queued on the socket. Reusing this connection
		// would parse those bytes as the next command response, so fail it closed.
		_ = c.conn.Close()
		return nil, fmt.Errorf("%w: %d bytes (max %d)", ErrResponseTooLarge, n, maxAPDUResponse)
	}
	out := make([]byte, n)
	if err := c.readFull(out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *daemonClient) send(ctx context.Context, cmd uint32, body []byte) error {
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetDeadline(deadline)
	} else {
		_ = c.conn.SetDeadline(time.Now().Add(30 * time.Second))
	}
	var hdr [8]byte
	binary.LittleEndian.PutUint32(hdr[0:4], uint32(len(body)))
	binary.LittleEndian.PutUint32(hdr[4:8], cmd)
	if err := c.writeFull(hdr[:]); err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	return c.writeFull(body)
}

func (c *daemonClient) exchange(ctx context.Context, cmd uint32, body []byte) error {
	if err := c.send(ctx, cmd, body); err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	return c.readFull(body)
}

func (c *daemonClient) writeFull(p []byte) error {
	for len(p) > 0 {
		n, err := c.conn.Write(p)
		if err != nil {
			return err
		}
		p = p[n:]
	}
	return nil
}

func (c *daemonClient) readFull(p []byte) error {
	_, err := io.ReadFull(c.conn, p)
	return err
}

func cString(b []byte) string {
	if i := indexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(string(b))
}

func indexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}
