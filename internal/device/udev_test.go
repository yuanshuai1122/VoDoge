package device

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestUdevWatcherStopIsConcurrentSafeAndCancelsPendingRescan(t *testing.T) {
	w := NewUdevWatcher(nil)
	w.debounce = 5 * time.Second

	const schedulers = 32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < schedulers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			w.scheduleRescan()
		}()
	}
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			<-start
			w.Stop()
		}()
	}
	close(start)
	stopStarted := time.Now()
	wg.Wait()
	if elapsed := time.Since(stopStarted); elapsed > time.Second {
		t.Fatalf("concurrent Stop took %s, want bounded cancellation", elapsed)
	}

	w.pendingMu.Lock()
	pending, timer := w.pending, w.timer
	w.pendingMu.Unlock()
	if pending || timer != nil {
		t.Fatalf("pending rescan after Stop: pending=%v timer=%v", pending, timer)
	}

	// Stop-before-Start must remain stopped and must not create a netlink loop.
	w.Start()
	w.stateMu.Lock()
	started, stopped := w.started, w.stopped
	w.stateMu.Unlock()
	if started || !stopped {
		t.Fatalf("watcher state after Stop then Start: started=%v stopped=%v", started, stopped)
	}
}

func TestUdevWatcherStaleGenerationCannotClearOrRunCurrentTimer(t *testing.T) {
	w := NewUdevWatcher(nil)
	var rescans atomic.Int32
	w.rescan = func() error {
		rescans.Add(1)
		return nil
	}

	currentTimer := new(time.Timer)
	w.pendingMu.Lock()
	w.pending = true
	w.timer = currentTimer
	w.debounceGeneration = 2
	w.pendingMu.Unlock()

	w.runScheduledRescan(1)
	w.pendingMu.Lock()
	pending, timer := w.pending, w.timer
	w.pendingMu.Unlock()
	if !pending || timer != currentTimer {
		t.Fatalf("stale callback changed current debounce state: pending=%v timer=%p want=%p", pending, timer, currentTimer)
	}
	if got := rescans.Load(); got != 0 {
		t.Fatalf("rescans after stale callback = %d, want 0", got)
	}

	w.runScheduledRescan(2)
	w.runScheduledRescan(2)
	w.pendingMu.Lock()
	pending, timer = w.pending, w.timer
	w.pendingMu.Unlock()
	if pending || timer != nil {
		t.Fatalf("current callback did not consume debounce state: pending=%v timer=%v", pending, timer)
	}
	if got := rescans.Load(); got != 1 {
		t.Fatalf("rescans after current callback ran twice = %d, want 1", got)
	}
}

func TestUdevWatcherDebouncesBurstToOneRescan(t *testing.T) {
	w := NewUdevWatcher(nil)
	w.debounce = 20 * time.Millisecond
	var rescans atomic.Int32
	rescanned := make(chan struct{}, 1)
	w.rescan = func() error {
		rescans.Add(1)
		select {
		case rescanned <- struct{}{}:
		default:
		}
		return nil
	}

	for i := 0; i < 8; i++ {
		w.scheduleRescan()
		time.Sleep(2 * time.Millisecond)
	}
	select {
	case <-rescanned:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for debounced rescan")
	}
	time.Sleep(2 * w.debounce)
	if got := rescans.Load(); got != 1 {
		t.Fatalf("rescans after burst = %d, want 1", got)
	}
	if err := stopWatcherWithin(w, time.Second); err != nil {
		t.Fatal(err)
	}
}

func stopWatcherWithin(w *UdevWatcher, timeout time.Duration) error {
	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return &watcherStopTimeout{timeout: timeout}
	}
}

type watcherStopTimeout struct {
	timeout time.Duration
}

func (e *watcherStopTimeout) Error() string {
	return "udev watcher Stop exceeded " + e.timeout.String()
}

func TestUdevWatcherTreatsWWANPortEventsAsModemEvents(t *testing.T) {
	w := NewUdevWatcher(nil)
	event := []byte("add@/devices/platform/soc@0/4080000.remoteproc/wwan/wwan0/wwan0qmi0\x00ACTION=add\x00SUBSYSTEM=wwan\x00DEVTYPE=wwan_port\x00DEVNAME=/dev/wwan0qmi0\x00")

	if !w.isModemEvent(event) {
		t.Fatal("isModemEvent() = false, want true for SUBSYSTEM=wwan QMI port")
	}
}

func TestUdevWatcherKeepsIgnoringNonWWANNetEvents(t *testing.T) {
	w := NewUdevWatcher(nil)
	event := []byte("add@/devices/virtual/net/eth0\x00ACTION=add\x00SUBSYSTEM=net\x00INTERFACE=eth0\x00")

	if w.isModemEvent(event) {
		t.Fatal("isModemEvent() = true, want false for eth0 net event")
	}
}
