package device

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yuanshuai1122/vodoge/internal/backend"
)

// ctxAwareRadioController 与既有的 qmiRegistrationTestController 有一个关键差别：
// 它**尊重 ctx**。既有假实现的 SetOperatingMode 永远返回 nil，于是 ctx 超时后
// 「恢复 Online」看上去照样成功，真机上的死局在测试里根本复现不出来。
type ctxAwareRadioController struct {
	setModeCalls []backend.OperatingMode
	failOnline   bool
	onlineCalls  int
}

func (c *ctxAwareRadioController) SetOperatingMode(ctx context.Context, mode backend.OperatingMode) error {
	if mode == backend.ModeOnline {
		c.onlineCalls++
		if c.failOnline && c.onlineCalls == 1 {
			return errors.New("modem busy")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.setModeCalls = append(c.setModeCalls, mode)
	return nil
}

func (c *ctxAwareRadioController) GetServingSystem(context.Context) (*backend.ServingSystem, error) {
	return &backend.ServingSystem{RegStatus: 2}, nil
}
func (c *ctxAwareRadioController) NASInitiateNetworkRegister(context.Context, backend.NASRegisterRequest) error {
	return nil
}
func (c *ctxAwareRadioController) NASForceNetworkSearch(context.Context) error          { return nil }
func (c *ctxAwareRadioController) NASSetSystemSelectionAutomatic(context.Context) error { return nil }
func (c *ctxAwareRadioController) NASAttachDetach(context.Context, bool) error          { return nil }
func (c *ctxAwareRadioController) GetOperatingMode(context.Context) (backend.OperatingMode, error) {
	return backend.ModeOnline, nil
}

func (c *ctxAwareRadioController) lastMode() backend.OperatingMode {
	if len(c.setModeCalls) == 0 {
		return backend.ModeOnline
	}
	return c.setModeCalls[len(c.setModeCalls)-1]
}

// 核心回归：ctx 在射频已关闭之后超时，射频**必须**被恢复。
//
// 留在关闭状态是个没人来救的死局——suppressQMIUnhealthyEviction 见到 RFOff 会
// 抑制驱逐，applyNetworkPreference 又只由事件驱动，而射频关着就产生不了事件。
func TestRadioCycleRestoresRadioWhenContextExpires(t *testing.T) {
	ctrl := &ctxAwareRadioController{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := radioCycleQMIForRegistration(ctx, "dev-cycle", ctrl, 2*time.Second)
	if err == nil {
		t.Fatal("ctx 超时时应返回错误")
	}
	if got := ctrl.lastMode(); got != backend.ModeOnline {
		t.Fatalf("最终射频状态=%v want Online：射频被留在关闭状态，设备将永久失联", got)
	}
	if ctrl.onlineCalls == 0 {
		t.Fatal("从未尝试恢复 Online")
	}
}

// 正常路径不该触发兜底恢复，避免多发一次无谓的 SetOperatingMode。
func TestRadioCycleDoesNotDoubleRestoreOnSuccess(t *testing.T) {
	ctrl := &ctxAwareRadioController{}

	if err := radioCycleQMIForRegistration(context.Background(), "dev-cycle", ctrl, time.Millisecond); err != nil {
		t.Fatalf("radioCycleQMIForRegistration() error = %v", err)
	}
	if ctrl.onlineCalls != 1 {
		t.Fatalf("onlineCalls=%d want 1：正常完成时不应重复恢复", ctrl.onlineCalls)
	}
	want := []backend.OperatingMode{backend.ModeRFOff, backend.ModeOnline}
	if len(ctrl.setModeCalls) != len(want) {
		t.Fatalf("setModeCalls=%v want %v", ctrl.setModeCalls, want)
	}
	for i := range want {
		if ctrl.setModeCalls[i] != want[i] {
			t.Fatalf("setModeCalls=%v want %v", ctrl.setModeCalls, want)
		}
	}
}

// 恢复调用本身失败时要重试一次（用独立 ctx），并且错误必须冒出去，
// 不能只在日志里留一行然后当作没事发生。
func TestRadioCycleRetriesRestoreWhenOnlineFails(t *testing.T) {
	ctrl := &ctxAwareRadioController{failOnline: true}

	err := radioCycleQMIForRegistration(context.Background(), "dev-cycle", ctrl, time.Millisecond)
	if err == nil {
		t.Fatal("首次恢复失败时应返回错误")
	}
	if ctrl.onlineCalls != 2 {
		t.Fatalf("onlineCalls=%d want 2：首次恢复失败后应再用独立 ctx 兜底一次", ctrl.onlineCalls)
	}
	if got := ctrl.lastMode(); got != backend.ModeOnline {
		t.Fatalf("最终射频状态=%v want Online", got)
	}
}
