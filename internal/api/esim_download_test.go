package api

import (
	"testing"
	"time"
)

func TestEsimDownloadRegistryRejectsConcurrentTaskForSameDevice(t *testing.T) {
	reg := newEsimDownloadRegistry()

	first, created := reg.begin("dev-1")
	if !created || first == nil {
		t.Fatalf("begin() first=%v created=%v want a new task", first, created)
	}

	second, created := reg.begin("dev-1")
	if created {
		t.Fatalf("begin() created a second task while the first is running")
	}
	if second != first {
		t.Fatalf("begin() second=%v want the running task so the client can subscribe to it", second)
	}

	// 另一台设备不受影响：串行化只针对单台设备的 APDU 通道。
	other, created := reg.begin("dev-2")
	if !created || other == first {
		t.Fatalf("begin(dev-2) other=%v created=%v want an independent task", other, created)
	}
}

func TestEsimDownloadRegistryAllowsNewTaskAfterCompletion(t *testing.T) {
	reg := newEsimDownloadRegistry()

	first, _ := reg.begin("dev-1")
	first.publish(esimDownloadEvent{Step: "done", Msg: "Profile 下载完成", Pct: 100})

	second, created := reg.begin("dev-1")
	if !created || second == first {
		t.Fatalf("begin() second=%v created=%v want a fresh task once the previous one finished", second, created)
	}
}

// 订阅晚于事件产生是常态：客户端要先拿到 POST 的响应才能发起 SSE 连接，
// 这期间的进度事件必须补发，否则进度条会从中途开始。
func TestEsimDownloadTaskSubscribeReplaysHistory(t *testing.T) {
	reg := newEsimDownloadRegistry()
	task, _ := reg.begin("dev-1")

	task.publish(esimDownloadEvent{Step: "preflight", Msg: "检查空间", Pct: 10})
	task.publish(esimDownloadEvent{Step: "auth_client", Msg: "认证", Pct: 30})

	history, ch := task.subscribe()
	if len(history) != 2 || history[0].Step != "preflight" || history[1].Step != "auth_client" {
		t.Fatalf("history=%v want both earlier events replayed in order", history)
	}
	if ch == nil {
		t.Fatalf("subscribe() ch=nil want a live channel for an unfinished task")
	}

	task.publish(esimDownloadEvent{Step: "install", Msg: "安装", Pct: 80})
	select {
	case ev := <-ch:
		if ev.Step != "install" {
			t.Fatalf("ev=%v want the event published after subscribing", ev)
		}
	case <-time.After(time.Second):
		t.Fatalf("subscriber did not receive the event published after subscribing")
	}

	// 终态事件后通道关闭，SSE handler 借此结束响应。
	task.publish(esimDownloadEvent{Step: "done", Msg: "Profile 下载完成", Pct: 100})
	for {
		ev, open := <-ch
		if !open {
			break
		}
		if ev.Step == "done" {
			continue
		}
		t.Fatalf("ev=%v want only the done event before the channel closes", ev)
	}
}

// 任务结束后订阅仍应拿到完整历史——断线重连的客户端靠这个取回最终结果。
func TestEsimDownloadTaskSubscribeAfterCompletionReturnsHistoryOnly(t *testing.T) {
	reg := newEsimDownloadRegistry()
	task, _ := reg.begin("dev-1")

	task.publish(esimDownloadEvent{Step: "install", Msg: "安装", Pct: 80})
	task.publish(esimDownloadEvent{Step: "error", Msg: "下载失败: network down", Pct: -1})

	history, ch := task.subscribe()
	if ch != nil {
		t.Fatalf("subscribe() ch=%v want nil for a finished task", ch)
	}
	if len(history) != 2 || history[1].Step != "error" {
		t.Fatalf("history=%v want the full event list including the terminal event", history)
	}
}

func TestEsimDownloadRegistryExpiresFinishedTasks(t *testing.T) {
	reg := newEsimDownloadRegistry()
	task, _ := reg.begin("dev-1")
	task.publish(esimDownloadEvent{Step: "done", Msg: "Profile 下载完成", Pct: 100})

	if reg.get(task.ID) == nil {
		t.Fatalf("get(%q)=nil want the task retained for late subscribers", task.ID)
	}

	// 把完成时间推到 TTL 之外，模拟保留期结束。
	task.mu.Lock()
	task.finishedAt = time.Now().Add(-esimDownloadTaskTTL - time.Minute)
	task.mu.Unlock()

	reg.begin("dev-2") // begin 顺带做 GC
	if reg.get(task.ID) != nil {
		t.Fatalf("get(%q) want nil after the retention window elapsed", task.ID)
	}
}
