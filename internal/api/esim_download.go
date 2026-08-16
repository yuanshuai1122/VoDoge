package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodoge/internal/esim"
	"github.com/yuanshuai1122/vodoge/pkg/logger"
)

// eSIM Profile 下载：POST 建任务 + GET 订阅进度。
//
// 早先的实现是单个 GET，把 smdp / matching_id / confirmation_code 放在 query 里，
// 因为 EventSource 只能发 GET。代价是激活码会进入浏览器历史、Referer 与任何
// 中间层的访问日志——激活码通常一次性且可被抢用，这个暴露面不可接受。
//
// 现在拆成两步：敏感参数经 POST body 提交并留在服务端，客户端只拿到一个不敏感的
// task_id，再用它订阅 SSE。附带的好处是下载不再与某一条连接绑定：网络抖动后
// 重新订阅同一个 task_id 即可，已产生的事件会被补发。

const (
	// 任务完成后仍保留一段时间，供断线重连的客户端取回最终结果。
	esimDownloadTaskTTL = 10 * time.Minute
	// 下载本身的上限，与旧实现一致。
	esimDownloadTimeout = 5 * time.Minute
)

type esimDownloadTask struct {
	ID       string
	DeviceID string

	mu     sync.Mutex
	events []esimDownloadEvent
	done   bool
	subs   map[chan esimDownloadEvent]struct{}

	finishedAt time.Time
}

// esimDownloadEvent 是推送给客户端的一帧，与旧实现的 JSON 形状保持一致，
// 前端无需改变解析方式。
type esimDownloadEvent struct {
	Step        string `json:"step"`
	Msg         string `json:"msg"`
	Pct         int    `json:"pct"`
	Code        string `json:"code,omitempty"`
	Details     string `json:"details,omitempty"`
	Warning     string `json:"warning,omitempty"`
	WarningCode string `json:"warning_code,omitempty"`
	SpaceDelta  any    `json:"space_delta,omitempty"`
}

func (t *esimDownloadTask) publish(ev esimDownloadEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.events = append(t.events, ev)
	if ev.Step == "done" || ev.Step == "error" {
		t.done = true
		t.finishedAt = time.Now()
	}
	for ch := range t.subs {
		// 订阅者缓冲区满时丢弃而非阻塞：下载线程不能被慢客户端拖住，
		// 且订阅时会补发全部历史事件，慢客户端重连后仍能拿到完整序列。
		select {
		case ch <- ev:
		default:
		}
	}
	if t.done {
		for ch := range t.subs {
			close(ch)
		}
		t.subs = map[chan esimDownloadEvent]struct{}{}
	}
}

// subscribe 返回已产生的历史事件与后续事件的通道。
// 任务已结束时通道为 nil，调用方只需消费历史事件。
func (t *esimDownloadTask) subscribe() ([]esimDownloadEvent, chan esimDownloadEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	history := append([]esimDownloadEvent(nil), t.events...)
	if t.done {
		return history, nil
	}
	ch := make(chan esimDownloadEvent, 64)
	t.subs[ch] = struct{}{}
	return history, ch
}

func (t *esimDownloadTask) unsubscribe(ch chan esimDownloadEvent) {
	if ch == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.subs[ch]; ok {
		delete(t.subs, ch)
		close(ch)
	}
}

func (t *esimDownloadTask) expired(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.done && now.Sub(t.finishedAt) > esimDownloadTaskTTL
}

type esimDownloadRegistry struct {
	mu    sync.Mutex
	tasks map[string]*esimDownloadTask
	// 同一设备同时只允许一个下载任务：eSIM 操作本就经 APDU 仲裁器串行化，
	// 并发发起只会撞上 ESIM_BUSY，不如在入口挡掉并给出明确的 409。
	active map[string]string // deviceID -> taskID
}

func newEsimDownloadRegistry() *esimDownloadRegistry {
	return &esimDownloadRegistry{
		tasks:  map[string]*esimDownloadTask{},
		active: map[string]string{},
	}
}

func (r *esimDownloadRegistry) get(id string) *esimDownloadTask {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tasks[id]
}

// begin 为设备创建任务；该设备已有进行中的任务时返回其 id 与 false。
func (r *esimDownloadRegistry) begin(deviceID string) (*esimDownloadTask, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.gcLocked()

	if existing, ok := r.active[deviceID]; ok {
		if t := r.tasks[existing]; t != nil && !t.expired(time.Now()) {
			t.mu.Lock()
			running := !t.done
			t.mu.Unlock()
			if running {
				return t, false
			}
		}
	}

	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return nil, false
	}
	task := &esimDownloadTask{
		ID:       hex.EncodeToString(buf),
		DeviceID: deviceID,
		subs:     map[chan esimDownloadEvent]struct{}{},
	}
	r.tasks[task.ID] = task
	r.active[deviceID] = task.ID
	return task, true
}

func (r *esimDownloadRegistry) gcLocked() {
	now := time.Now()
	for id, t := range r.tasks {
		if t.expired(now) {
			delete(r.tasks, id)
			if r.active[t.DeviceID] == id {
				delete(r.active, t.DeviceID)
			}
		}
	}
}

type esimDownloadRequest struct {
	SMDP             string `json:"smdp"`
	MatchingID       string `json:"matching_id"`
	ConfirmationCode string `json:"confirmation_code"`
	AIDHex           string `json:"aid_hex"`
	IMEI             string `json:"imei"`
}

// handleEsimDownloadStart 创建下载任务并立即返回 task_id，下载在后台进行。
func (s *Server) handleEsimDownloadStart(c *gin.Context) {
	id := deviceIDParam(c)
	worker := s.pool.GetWorker(id)
	if worker == nil || worker.EsimMgr == nil {
		fail(c, http.StatusNotFound, "", "设备或esim管理器未找到")
		return
	}

	var req esimDownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "参数错误: "+err.Error())
		return
	}
	if strings.TrimSpace(req.SMDP) == "" {
		fail(c, http.StatusBadRequest, "", "smdp 为必填项")
		return
	}

	task, created := s.esimDownloads.begin(id)
	if task == nil {
		fail(c, http.StatusInternalServerError, "", "无法创建下载任务")
		return
	}
	if !created {
		// 复用语义与 eSIM 其它接口一致：409 + 已有任务 id，前端可直接订阅
		failWith(c, http.StatusConflict, "ESIM_DOWNLOAD_IN_PROGRESS", "该设备已有进行中的下载任务", gin.H{
			"busy":    true,
			"task_id": task.ID,
		})
		return
	}

	mgr := worker.EsimMgr
	go func(t *esimDownloadTask, in esimDownloadRequest) {
		// 刻意不绑定请求的 context：客户端断开不应中止已经开始的下载。
		ctx, cancel := context.WithTimeout(context.Background(), esimDownloadTimeout)
		defer cancel()

		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("eSIM 下载任务 panic", "task", t.ID, "err", rec)
				t.publish(esimDownloadEvent{Step: "error", Msg: "下载失败: 内部错误", Pct: -1})
			}
		}()

		result, err := mgr.DownloadProfile(
			ctx, in.AIDHex, in.SMDP, in.MatchingID, in.ConfirmationCode, in.IMEI,
			func(ev esim.DownloadProgressEvent) {
				t.publish(esimDownloadEvent{Step: ev.Step, Msg: ev.Msg, Pct: ev.Pct})
			},
		)
		if err != nil {
			t.publish(esimDownloadErrorEvent(err))
			return
		}
		t.publish(esimDownloadDoneEvent(result))
	}(task, req)

	respond(c, http.StatusAccepted, gin.H{
		"task_id": task.ID,
	}, nil)
}

// handleEsimDownloadStream 按 task_id 订阅下载进度。
// 订阅时会先补发已产生的事件，因此断线重连不会丢失中间过程。
func (s *Server) handleEsimDownloadStream(c *gin.Context) {
	taskID := strings.TrimSpace(c.Query("task_id"))
	if taskID == "" {
		fail(c, http.StatusBadRequest, "", "task_id 为必填项")
		return
	}
	task := s.esimDownloads.get(taskID)
	if task == nil {
		fail(c, http.StatusNotFound, "", "下载任务不存在或已过期")
		return
	}
	if task.DeviceID != deviceIDParam(c) {
		fail(c, http.StatusNotFound, "", "下载任务不属于该设备")
		return
	}

	s.prepareSSE(c)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		fail(c, http.StatusInternalServerError, "", "流式输出不支持")
		return
	}

	write := func(ev esimDownloadEvent) {
		writeEsimDownloadEvent(c.Writer, ev)
		flusher.Flush()
	}

	history, ch := task.subscribe()
	defer task.unsubscribe(ch)

	for _, ev := range history {
		write(ev)
	}
	if ch == nil {
		return // 任务已结束，历史事件即全部内容
	}

	clientGone := c.Request.Context().Done()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-clientGone:
			return
		case <-s.shutdownCh:
			return
		case <-heartbeat.C:
			_, _ = io.WriteString(c.Writer, ": keepalive\n\n")
			flusher.Flush()
		case ev, open := <-ch:
			if !open {
				return
			}
			write(ev)
		}
	}
}

// formatEsimDownloadEvent 序列化为 SSE `data:` 行的载荷。
// 单独成函数是为了让事件形状可以被单测直接断言，而不必架一个 SSE 连接。
func formatEsimDownloadEvent(ev esimDownloadEvent) string {
	payload, err := json.Marshal(ev)
	if err != nil {
		return `{"step":"error","msg":"下载失败: 事件序列化失败","pct":-1}`
	}
	return string(payload)
}

func writeEsimDownloadEvent(w io.Writer, ev esimDownloadEvent) {
	fmt.Fprintf(w, "data: %s\n\n", formatEsimDownloadEvent(ev))
}

func formatEsimDownloadDoneEvent(result esim.DownloadProfileResult) string {
	return formatEsimDownloadEvent(esimDownloadDoneEvent(result))
}

func formatEsimDownloadErrorEvent(err error) string {
	return formatEsimDownloadEvent(esimDownloadErrorEvent(err))
}

func esimDownloadDoneEvent(result esim.DownloadProfileResult) esimDownloadEvent {
	ev := esimDownloadEvent{
		Step: "done",
		Msg:  "Profile 下载完成",
		Pct:  100,
	}
	if w := strings.TrimSpace(result.Warning); w != "" {
		ev.Warning = w
	}
	if wc := strings.TrimSpace(result.WarningCode); wc != "" {
		ev.WarningCode = wc
	}
	if result.SpaceDelta != nil {
		ev.SpaceDelta = result.SpaceDelta
	}
	return ev
}

func esimDownloadErrorEvent(err error) esimDownloadEvent {
	ev := esimDownloadEvent{Step: "error", Pct: -1, Msg: "下载失败"}

	var downloadErr *esim.DownloadProfileError
	if errors.As(err, &downloadErr) && downloadErr != nil {
		if downloadErr.Message != "" {
			ev.Msg += ": " + downloadErr.Message
		} else if err != nil {
			ev.Msg += ": " + err.Error()
		}
		if code := strings.TrimSpace(downloadErr.Code); code != "" {
			ev.Code = code
		}
		if details := strings.TrimSpace(downloadErr.Details); details != "" {
			ev.Details = details
		}
		return ev
	}
	if err != nil {
		ev.Msg += ": " + err.Error()
	}
	return ev
}
