package httpsmode

import (
	"bufio"
	"net"
	"sync"
	"time"
)

// tlsHandshakeRecord 是 TLS 记录层的 handshake 类型。
const tlsHandshakeRecord byte = 0x16

type peekedConn struct {
	net.Conn
	bufr *bufio.Reader
}

func (c *peekedConn) Read(p []byte) (int, error) { return c.bufr.Read(p) }

type queuedListener struct {
	addr net.Addr
	ch   chan net.Conn
	done <-chan struct{}
}

func (l *queuedListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.ch:
		if conn == nil {
			return nil, net.ErrClosed
		}
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *queuedListener) Close() error   { return nil }
func (l *queuedListener) Addr() net.Addr { return l.addr }

// Multiplexer 在同一 TCP 口上拆出明文 HTTP 与 TLS。
type Multiplexer struct {
	base   net.Listener
	mgr    *Manager
	plain  *queuedListener
	secure *queuedListener
	done   chan struct{}
	once   sync.Once
}

func NewMultiplexer(base net.Listener, mgr *Manager) *Multiplexer {
	done := make(chan struct{})
	mux := &Multiplexer{
		base: base,
		mgr:  mgr,
		done: done,
		plain: &queuedListener{
			addr: base.Addr(),
			ch:   make(chan net.Conn, 32),
			done: done,
		},
		secure: &queuedListener{
			addr: base.Addr(),
			ch:   make(chan net.Conn, 32),
			done: done,
		},
	}
	go mux.loop()
	return mux
}

func (m *Multiplexer) Plain() net.Listener { return m.plain }
func (m *Multiplexer) TLS() net.Listener   { return m.secure }

func (m *Multiplexer) Close() error {
	var err error
	m.once.Do(func() {
		close(m.done)
		err = m.base.Close()
	})
	return err
}

func (m *Multiplexer) loop() {
	for {
		conn, err := m.base.Accept()
		if err != nil {
			_ = m.Close()
			return
		}
		go m.dispatch(conn)
	}
}

func (m *Multiplexer) dispatch(conn net.Conn) {
	bufr := bufio.NewReaderSize(conn, 4096)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	head, err := bufr.Peek(1)
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		_ = conn.Close()
		return
	}
	wrapped := &peekedConn{Conn: conn, bufr: bufr}
	dest := m.plain
	if len(head) > 0 && head[0] == tlsHandshakeRecord {
		if m.mgr == nil || !m.mgr.Enabled() {
			_ = conn.Close()
			return
		}
		dest = m.secure
	}
	select {
	case dest.ch <- wrapped:
	case <-m.done:
		_ = conn.Close()
	}
}

func IsTLSClientHello(first byte) bool { return first == tlsHandshakeRecord }
