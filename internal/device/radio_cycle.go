package device

import (
	"context"
	"fmt"
	"time"

	"github.com/yuanshuai1122/vodoge/internal/backend"
	"github.com/yuanshuai1122/vodoge/pkg/logger"
)

// operatingModeSetter 是 radio cycle 唯一需要的能力。
// QMI 与 MBIM 两个控制器接口都满足它，因此两条路径可以共用同一份实现。
type operatingModeSetter interface {
	SetOperatingMode(ctx context.Context, mode backend.OperatingMode) error
}

// radioCycleForRegistration 关一下射频再打开，逼模组重新搜网。
//
// **关掉射频之后，无论走哪条路径都必须把它开回来。** 这不是洁癖，是因为
// 「射频留在关闭状态」在本项目里是个没人会来救的死局：
//
//  1. suppressQMIUnhealthyEviction 看到 ModeRFOff 会主动**抑制驱逐**
//     （那本是给 VoWiFi / 切卡准备的豁免），于是 worker 不会被重建；
//  2. applyNetworkPreference 只由事件驱动——worker 启动、SIM 状态变化、
//     策略变更——**没有周期性定时器**；
//  3. 射频关着就收不到任何 SIM/网络 indication，也就产生不了能触发上面
//     那些事件的信号。
//
// 三条叠起来：设备一直是黑的，界面显示未注册，而日志里只有一行
// 「radio cycle 失败，继续等待模组自主驻网」。
//
// 原实现在 RFOff 之后直接 `return err`，ctx 在那段 sleep 上超时就再也走不到
// 恢复那一步；更糟的是恢复用的是同一个 ctx，ctx 一旦 Done，恢复调用必然也失败。
// 两个失败是**相关**的，而不是独立的小概率事件——ctx 超时恰恰发生在 QMI 变慢的
// 时候，而 QMI 变慢正是触发 radio cycle 的前提。
func radioCycleForRegistration(ctx context.Context, deviceID, transport string, ctrl operatingModeSetter, wait time.Duration) (err error) {
	if ctrl == nil {
		return fmt.Errorf("%s registration controller unavailable", transport)
	}
	if wait <= 0 {
		wait = 2 * time.Second
	}
	logger.Info(fmt.Sprintf("%s 搜网持续未恢复，执行 radio flight-mode cycle 重新触发搜网", transport), "device", deviceID)

	if err := ctrl.SetOperatingMode(ctx, backend.ModeRFOff); err != nil {
		return fmt.Errorf("设置 RFOff 失败: %w", err)
	}

	restored := false
	defer func() {
		if restored {
			return
		}
		// 独立 context：此刻传进来的 ctx 多半已经 Done，用它去恢复必然失败。
		restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), qmiRadioRestoreTimeout)
		defer cancel()
		if restoreErr := ctrl.SetOperatingMode(restoreCtx, backend.ModeOnline); restoreErr != nil {
			logger.Error("radio cycle 中断且射频恢复失败，模组可能仍处于射频关闭状态",
				"device", deviceID, "transport", transport, "err", restoreErr)
			if err == nil {
				err = fmt.Errorf("恢复 Online 失败: %w", restoreErr)
			}
			return
		}
		logger.Warn("radio cycle 被中断，已强制恢复射频 Online", "device", deviceID, "transport", transport)
	}()

	if err := sleepQMIRegistrationPoll(ctx, wait); err != nil {
		return err
	}
	if err := ctrl.SetOperatingMode(ctx, backend.ModeOnline); err != nil {
		return fmt.Errorf("恢复 Online 失败: %w", err)
	}
	restored = true
	if err := sleepQMIRegistrationPoll(ctx, wait); err != nil {
		return err
	}
	return nil
}
