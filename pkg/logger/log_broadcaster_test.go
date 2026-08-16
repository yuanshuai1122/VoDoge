package logger

import (
	"sync"
	"testing"
)

func TestBroadcasterUnsubscribeIsConcurrentAndIdempotent(t *testing.T) {
	b := NewBroadcaster(1)
	ch := b.Subscribe()

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Unsubscribe(ch)
		}()
	}
	wg.Wait()

	if got := b.ClientCount(); got != 0 {
		t.Fatalf("ClientCount() = %d, want 0", got)
	}
	if _, ok := <-ch; ok {
		t.Fatal("subscriber channel remains open after unsubscribe")
	}
}
