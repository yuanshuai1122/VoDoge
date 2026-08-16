package api

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuanshuai1122/vodoge/internal/httpsmode"
)

type lifecycleAddr string

func (a lifecycleAddr) Network() string { return "test" }
func (a lifecycleAddr) String() string  { return string(a) }

type lifecycleListener struct {
	closed     chan struct{}
	closeOnce  sync.Once
	closeCalls atomic.Int32
	closeErr   error
}

func (l *lifecycleListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}

func (l *lifecycleListener) Close() error {
	l.closeCalls.Add(1)
	l.closeOnce.Do(func() { close(l.closed) })
	return l.closeErr
}

func (l *lifecycleListener) Addr() net.Addr { return lifecycleAddr("lifecycle") }

type acceptStartedListener struct {
	net.Listener
	started chan struct{}
	once    sync.Once
}

func (l *acceptStartedListener) Accept() (net.Conn, error) {
	l.once.Do(func() { close(l.started) })
	return l.Listener.Accept()
}

func TestServerShutdownConcurrentReturnsCachedResult(t *testing.T) {
	closeErr := errors.New("listener close sentinel")
	base := &lifecycleListener{closed: make(chan struct{}), closeErr: closeErr}
	mux := httpsmode.NewMultiplexer(base, nil)
	s := &Server{httpsMux: mux, shutdownCh: make(chan struct{})}

	const callers = 32
	start := make(chan struct{})
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = s.Shutdown(context.Background())
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if !errors.Is(err, closeErr) {
			t.Fatalf("Shutdown caller %d error=%v want %v", i, err, closeErr)
		}
	}
	if got := base.closeCalls.Load(); got != 1 {
		t.Fatalf("base listener Close calls=%d want=1", got)
	}
	select {
	case <-s.shutdownCh:
	default:
		t.Fatal("shutdown signal was not closed")
	}
}

func startBlockedTLSServer(t *testing.T) (*http.Server, *httpsmode.Multiplexer, <-chan error) {
	t.Helper()
	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := httpsmode.NewMultiplexer(base, nil)
	listener := &acceptStartedListener{Listener: mux.TLS(), started: make(chan struct{})}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	srv := newAPIHTTPServer(base.Addr().String(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.TLSConfig = tlsConfig
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- srv.Serve(tls.NewListener(listener, tlsConfig))
	}()

	select {
	case <-listener.started:
	case <-time.After(time.Second):
		_ = mux.Close()
		t.Fatal("HTTPS server did not enter Accept")
	}
	return srv, mux, serveDone
}

func TestServerShutdownClosesHTTPSMultiplexersBeforeServers(t *testing.T) {
	mainHTTPS, mainMux, mainDone := startBlockedTLSServer(t)
	pluginHTTPS, pluginMux, pluginDone := startBlockedTLSServer(t)
	s := &Server{
		httpsSrv:    mainHTTPS,
		httpsMux:    mainMux,
		pluginHTTPS: pluginHTTPS,
		pluginMux:   pluginMux,
		shutdownCh:  make(chan struct{}),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Shutdown(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		_ = mainMux.Close()
		_ = pluginMux.Close()
		<-done
		t.Fatal("Shutdown blocked while HTTPS listeners were waiting in Accept")
	}

	for name, serveDone := range map[string]<-chan error{"main": mainDone, "plugin": pluginDone} {
		select {
		case err := <-serveDone:
			if !s.isExpectedShutdownError(err) {
				t.Fatalf("%s HTTPS Serve error=%v was not recognized as normal shutdown", name, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s HTTPS Serve did not exit", name)
		}
	}
}
