package device

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/vishvananda/netlink/nl"
	"github.com/yuanshuai1122/vodoge/pkg/logger"
	"golang.org/x/sys/unix"
)

// UdevWatcher 监听 USB 设备热插拔事件
type UdevWatcher struct {
	pool   *Pool
	stop   chan struct{}
	rescan func() error

	stateMu sync.Mutex
	started bool
	stopped bool
	wg      sync.WaitGroup

	// 防抖相关
	debounce           time.Duration
	pending            bool
	pendingMu          sync.Mutex
	timer              *time.Timer
	debounceGeneration uint64
}

// NewUdevWatcher 创建 udev 监听器
func NewUdevWatcher(pool *Pool) *UdevWatcher {
	w := &UdevWatcher{
		pool:     pool,
		stop:     make(chan struct{}),
		debounce: 3 * time.Second, // 等待设备枚举完成
	}
	if pool != nil {
		w.rescan = pool.RescanAndReconnect
	}
	return w
}

// Start 启动 udev 事件监听
func (w *UdevWatcher) Start() {
	if w == nil {
		return
	}
	w.stateMu.Lock()
	if w.started || w.stopped {
		w.stateMu.Unlock()
		return
	}
	w.started = true
	w.wg.Add(1)
	w.stateMu.Unlock()
	go func() {
		defer w.wg.Done()
		w.loop()
	}()
}

// Stop 停止监听
func (w *UdevWatcher) Stop() {
	if w == nil {
		return
	}
	w.stateMu.Lock()
	if !w.stopped {
		w.stopped = true
		close(w.stop)
	}
	w.stateMu.Unlock()

	w.pendingMu.Lock()
	w.debounceGeneration++
	if w.timer != nil {
		if w.timer.Stop() {
			w.wg.Done()
		}
		w.timer = nil
	}
	w.pending = false
	w.pendingMu.Unlock()
	w.wg.Wait()
}

func (w *UdevWatcher) loop() {
	// 创建 netlink 连接监听内核 uevent
	conn, err := nl.Subscribe(unix.NETLINK_KOBJECT_UEVENT)
	if err != nil {
		logger.Warn("udev 监听器启动失败，热插拔功能不可用", "err", err)
		return
	}
	defer conn.Close()

	logger.Info("udev 设备热插拔监听器已启动")

	for {
		select {
		case <-w.stop:
			logger.Info("udev 监听器已停止")
			return
		default:
		}

		// 设置读取超时，以便定期检查 stop 信号
		tv := unix.NsecToTimeval((1 * time.Second).Nanoseconds())
		_ = conn.SetReceiveTimeout(&tv)

		msgs, _, err := conn.Receive()
		if err != nil {
			// 超时错误是正常的
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
				continue
			}
			// 其他错误记录但继续
			continue
		}

		for _, msg := range msgs {
			if w.isModemEvent(msg.Data) {
				w.scheduleRescan()
				break // 一批事件只触发一次扫描
			}
		}
	}
}

// isModemEvent 检查是否是 USB 调制解调器相关事件
func (w *UdevWatcher) isModemEvent(data []byte) bool {
	s := string(data)

	// 检查 ACTION
	if !strings.Contains(s, "ACTION=add") && !strings.Contains(s, "ACTION=remove") {
		return false
	}

	// 检查 SUBSYSTEM（usb/net/tty/usbmisc/wwan 都可能是调制解调器相关）
	if strings.Contains(s, "SUBSYSTEM=usb") ||
		strings.Contains(s, "SUBSYSTEM=net") ||
		strings.Contains(s, "SUBSYSTEM=tty") ||
		strings.Contains(s, "SUBSYSTEM=usbmisc") ||
		strings.Contains(s, "SUBSYSTEM=wwan") {

		// 进一步过滤：排除无关设备
		// 如果是 net 子系统，只关心 wwan 开头的接口
		if strings.Contains(s, "SUBSYSTEM=net") {
			if !strings.Contains(s, "wwan") {
				return false
			}
		}

		// 如果是 tty 子系统，只关心 ttyUSB
		if strings.Contains(s, "SUBSYSTEM=tty") {
			if !strings.Contains(s, "ttyUSB") {
				return false
			}
		}

		logger.Debug("检测到调制解调器相关 udev 事件", "data_preview", truncateString(s, 200))
		return true
	}

	return false
}

// scheduleRescan 防抖：延迟执行扫描
// 每次事件创建一个新 generation。已经触发的旧 callback 可能无法被 Stop，
// 但它不能再清理新 timer，也不能执行重复扫描。
func (w *UdevWatcher) scheduleRescan() {
	if w == nil {
		return
	}
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()

	w.stateMu.Lock()
	if w.stopped {
		w.stateMu.Unlock()
		return
	}

	if w.timer != nil && w.timer.Stop() {
		// The stopped callback will not run its deferred Done.
		w.wg.Done()
	}

	w.debounceGeneration++
	generation := w.debounceGeneration
	w.pending = true
	w.wg.Add(1)
	w.timer = time.AfterFunc(w.debounce, func() {
		defer w.wg.Done()
		w.runScheduledRescan(generation)
	})
	w.stateMu.Unlock()
}

func (w *UdevWatcher) runScheduledRescan(generation uint64) {
	if w == nil {
		return
	}
	w.pendingMu.Lock()
	if generation != w.debounceGeneration || w.timer == nil {
		w.pendingMu.Unlock()
		return
	}
	w.pending = false
	w.timer = nil
	w.pendingMu.Unlock()

	select {
	case <-w.stop:
		return
	default:
	}
	logger.Info("udev 检测到设备变化，执行重新扫描")
	if w.pool != nil {
		if woken := w.pool.WakeModemRebootRecoveries("udev_modem_event"); woken > 0 {
			logger.Debug("udev 事件已唤醒模组重启恢复流程", "recoveries", woken)
			return
		}
	}
	if w.rescan != nil {
		if err := w.rescan(); err != nil {
			logger.Warn("设备重新扫描失败", "err", err)
		}
	}
}

// truncateString 截断字符串用于日志
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
