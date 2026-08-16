package device

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuanshuai1122/vodoge/internal/backend"
	"github.com/yuanshuai1122/vodoge/internal/config"
)

type lifecycleBackendStub struct {
	workerStatusBackendStub
	pool       *Pool
	closeCalls atomic.Int32
	closeErr   error
	lockErr    error
}

func (s *lifecycleBackendStub) Close() error {
	s.closeCalls.Add(1)
	if s.pool != nil {
		if !s.pool.mu.TryLock() {
			return errors.Join(s.closeErr, s.lockErr)
		}
		s.pool.mu.Unlock()
	}
	return s.closeErr
}

type lifecycleESIMTransportStub struct {
	stopCalls atomic.Int32
	stopErr   error
}

func (*lifecycleESIMTransportStub) ControlDevice() string { return "test" }
func (*lifecycleESIMTransportStub) Start() error          { return nil }
func (s *lifecycleESIMTransportStub) Stop() error {
	s.stopCalls.Add(1)
	return s.stopErr
}
func (*lifecycleESIMTransportStub) OpenEUICCLogicalChannel(context.Context, byte, []byte) (byte, error) {
	return 1, nil
}
func (*lifecycleESIMTransportStub) CloseEUICCLogicalChannel(context.Context, byte, byte) error {
	return nil
}
func (*lifecycleESIMTransportStub) TransmitEUICCAPDU(context.Context, byte, byte, []byte) ([]byte, error) {
	return nil, nil
}

func TestPoolShutdownConcurrentIdempotentAndCleansWorkerResources(t *testing.T) {
	backendErr := errors.New("backend close failed")
	transportErr := errors.New("transport stop failed")
	poolLockErr := errors.New("pool lock held during backend close")

	p := NewPool(&config.Config{})
	backendStub := &lifecycleBackendStub{
		workerStatusBackendStub: workerStatusBackendStub{mode: backend.BackendAT},
		pool:                    p,
		closeErr:                backendErr,
		lockErr:                 poolLockErr,
	}
	transportStub := &lifecycleESIMTransportStub{stopErr: transportErr}
	var operatorCancels atomic.Int32
	worker := &Worker{
		ID:                  "dev-lifecycle",
		Backend:             backendStub,
		ESIMQMITransport:    transportStub,
		Pool:                p,
		stop:                make(chan struct{}),
		publicIPRetryTimer:  time.AfterFunc(time.Hour, func() {}),
		operatorScanActive:  true,
		operatorScanCancel:  func() { operatorCancels.Add(1) },
		operatorScanCurrent: OperatorScanResult{Status: OperatorScanStatusRunning},
	}
	p.workers[worker.ID] = worker

	p.backgroundWG.Add(1)
	go func() {
		defer p.backgroundWG.Done()
		<-p.ctx.Done()
	}()

	const callers = 16
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- p.Shutdown()
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if !errors.Is(err, backendErr) {
			t.Fatalf("Shutdown() error %v does not include backend error", err)
		}
		if !errors.Is(err, transportErr) {
			t.Fatalf("Shutdown() error %v does not include transport error", err)
		}
		if errors.Is(err, poolLockErr) {
			t.Fatalf("Shutdown() closed a worker while holding Pool.mu: %v", err)
		}
	}
	if got := backendStub.closeCalls.Load(); got != 1 {
		t.Fatalf("backend close calls = %d, want 1", got)
	}
	if got := transportStub.stopCalls.Load(); got != 1 {
		t.Fatalf("transport stop calls = %d, want 1", got)
	}
	if got := operatorCancels.Load(); got != 1 {
		t.Fatalf("operator scan cancel calls = %d, want 1", got)
	}
	select {
	case <-worker.stop:
	default:
		t.Fatal("worker stop channel was not closed")
	}
	worker.publicIPRetryMu.Lock()
	if worker.publicIPRetryTimer != nil {
		worker.publicIPRetryMu.Unlock()
		t.Fatal("public IP retry timer was not cleared")
	}
	worker.publicIPRetryMu.Unlock()
	if worker.IsOperatorScanActive() {
		t.Fatal("operator scan remained active after shutdown")
	}
	if got := p.GetAllWorkers(); len(got) != 0 {
		t.Fatalf("workers after shutdown = %d, want 0", len(got))
	}
	select {
	case <-p.ctx.Done():
	default:
		t.Fatal("pool context was not canceled")
	}
}

func TestPoolRejectsStartAndRegistrationAfterShutdown(t *testing.T) {
	p := NewPool(&config.Config{})
	if err := p.Shutdown(); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := p.StartAll(); !errors.Is(err, ErrPoolShuttingDown) {
		t.Fatalf("StartAll() error = %v, want ErrPoolShuttingDown", err)
	}
	if _, err := p.AddWorkerFromConfig(config.DeviceConfig{ID: "late-worker"}); !errors.Is(err, ErrPoolShuttingDown) {
		t.Fatalf("AddWorkerFromConfig() error = %v, want ErrPoolShuttingDown", err)
	}
	lateWorker := &Worker{ID: "late-registration", stop: make(chan struct{})}
	if err := p.registerWorkerStarting(lateWorker); !errors.Is(err, ErrPoolShuttingDown) {
		t.Fatalf("registerWorkerStarting() error = %v, want ErrPoolShuttingDown", err)
	}
	if got := p.GetAllWorkers(); len(got) != 0 {
		t.Fatalf("workers after rejected registration = %d, want 0", len(got))
	}
}

func TestPoolShutdownStuckUnrelatedBootstrapDoesNotDelayWorkerCleanup(t *testing.T) {
	oldTimeout := poolShutdownTimeout
	poolShutdownTimeout = 20 * time.Millisecond
	defer func() { poolShutdownTimeout = oldTimeout }()

	p := NewPool(&config.Config{})
	backendStub := &lifecycleBackendStub{}
	worker := &Worker{
		ID:      "dev-timeout-cleanup",
		Backend: backendStub,
		Pool:    p,
		stop:    make(chan struct{}),
	}
	p.workers[worker.ID] = worker
	p.bootstrapWG.Add(1)
	defer p.bootstrapWG.Done()

	err := p.Shutdown()
	if err == nil || !strings.Contains(err.Error(), "关闭超时") {
		t.Fatalf("Shutdown() error = %v, want timeout", err)
	}
	if got := backendStub.closeCalls.Load(); got != 1 {
		t.Fatalf("backend close calls while unrelated bootstrap is stuck = %d, want 1", got)
	}
}

func TestPoolShutdownWaitsOnlyForCapturedWorkerBootstrap(t *testing.T) {
	oldTimeout := poolShutdownTimeout
	poolShutdownTimeout = time.Second
	defer func() { poolShutdownTimeout = oldTimeout }()

	p := NewPool(&config.Config{})
	backendStub := &lifecycleBackendStub{}
	bootstrapDone := make(chan struct{})
	worker := &Worker{
		ID:            "dev-active-bootstrap",
		Backend:       backendStub,
		Pool:          p,
		stop:          make(chan struct{}),
		bootstrapDone: bootstrapDone,
	}
	p.workers[worker.ID] = worker
	p.bootstrapWG.Add(1)

	shutdownErr := make(chan error, 1)
	go func() {
		shutdownErr <- p.Shutdown()
	}()

	select {
	case <-worker.stop:
	case <-time.After(time.Second):
		t.Fatal("Shutdown() did not capture and prepare the worker")
	}
	if got := backendStub.closeCalls.Load(); got != 0 {
		t.Fatalf("backend closed before its own bootstrap settled: calls=%d", got)
	}
	close(bootstrapDone)
	p.bootstrapWG.Done()

	select {
	case err := <-shutdownErr:
		if err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Shutdown() did not finish after worker bootstrap settled")
	}
	if got := backendStub.closeCalls.Load(); got != 1 {
		t.Fatalf("backend close calls = %d, want 1", got)
	}
}

func TestRemoveWorkerRacesPublicIPRetryWithoutLeavingTimer(t *testing.T) {
	for i := 0; i < 20; i++ {
		p := NewPool(&config.Config{})
		worker := &Worker{
			ID:      "dev-retry",
			Backend: &lifecycleBackendStub{},
			Pool:    p,
			stop:    make(chan struct{}),
		}
		p.workers[worker.ID] = worker

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			p.schedulePublicIPRetry(worker)
		}()
		go func() {
			defer wg.Done()
			<-start
			if err := p.RemoveWorker(worker.ID); err != nil {
				t.Errorf("RemoveWorker() error = %v", err)
			}
		}()
		close(start)
		wg.Wait()

		worker.publicIPRetryMu.Lock()
		timer := worker.publicIPRetryTimer
		worker.publicIPRetryMu.Unlock()
		if timer != nil {
			t.Fatalf("iteration %d left a public IP retry timer", i)
		}
	}
}

func TestSuppressQMIUnhealthyEvictionDuringLifecycleRecovery(t *testing.T) {
	pool := NewPool(&config.Config{})
	worker := &Worker{
		ID: "dev1",
		Config: config.DeviceConfig{
			ID:            "dev1",
			DeviceBackend: backend.BackendQMI,
			ControlDevice: "/dev/cdc-wdm0",
		},
		Backend: &workerStatusBackendStub{mode: backend.BackendQMI, opModeErr: errBackendUnavailable{}},
	}
	pool.workers["dev1"] = worker
	pool.lifecycle.BeginRecovery("dev1", LifecyclePhaseQMIStarting, "modem_reboot", qmiLifecycleRecoveryTTL)

	suppressed, reason := pool.suppressQMIUnhealthyEviction(worker)
	if !suppressed {
		t.Fatal("expected lifecycle recovery to suppress eviction")
	}
	if !strings.Contains(reason, "lifecycle_qmi_starting") {
		t.Fatalf("reason=%q want contains lifecycle_qmi_starting", reason)
	}
}

func TestSuppressQMIUnhealthyEvictionAfterLifecycleDeadline(t *testing.T) {
	pool := NewPool(&config.Config{})
	worker := &Worker{
		ID: "dev1",
		Config: config.DeviceConfig{
			ID:            "dev1",
			DeviceBackend: backend.BackendQMI,
			ControlDevice: "/dev/cdc-wdm0",
		},
		Backend: &workerStatusBackendStub{mode: backend.BackendQMI, opModeErr: errBackendUnavailable{}},
	}
	now := time.Now().Add(-2 * qmiLifecycleRecoveryTTL)
	pool.lifecycle.BeginRecoveryAt("dev1", LifecyclePhaseRecovering, "modem_reboot", now, time.Second)

	suppressed, reason := pool.suppressQMIUnhealthyEviction(worker)
	if suppressed {
		t.Fatalf("suppressed=true want false reason=%q", reason)
	}
}

type errBackendUnavailable struct{}

func (errBackendUnavailable) Error() string { return "backend unavailable" }
