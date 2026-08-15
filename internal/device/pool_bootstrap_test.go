package device

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yuanshuai1122/vodoge/internal/config"
)

// requireQMIProxy 在缺少 libqmi 的 qmi-proxy 时跳过用例。
//
// 相关用例虽然只关心「按 IMEI 重绑定」的发现逻辑，但走的是完整的
// AddWorkerFromConfig，其中包含启动 QMI Core——那一步需要系统上真实存在
// qmi-proxy。CI 与容器环境通常都没有，此时用例根本到不了它要断言的地方，
// 失败信息也只会指向 QMI 连接而非被测逻辑，因此跳过比报错更诚实。
func requireQMIProxy(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/usr/libexec/qmi-proxy"); err == nil {
		return
	}
	if _, err := exec.LookPath("qmi-proxy"); err == nil {
		return
	}
	t.Skip("需要 libqmi 的 qmi-proxy，当前环境不可用")
}

func TestAddWorkerQMIManagedRebindsByIMEIWhenControlDeviceGone(t *testing.T) {
	requireQMIProxy(t)

	// QMI 托管设备:配置 control_device 指向不存在节点,但配置了正确 IMEI;
	// 注入一块带该 IMEI 的新路径 QMI 硬件。bootstrap 应按 IMEI 取回新路径并采纳。

	originalDiscover := discoverQMIDevicesFn
	defer func() { discoverQMIDevicesFn = originalDiscover }()
	discoverQMIDevicesFn = func() ([]QMIDevice, error) {
		return []QMIDevice{
			{
				ControlPath:  "/dev/cdc-wdm-new-qmi",
				NetInterface: "wwan-new",
				USBPath:      "1-2.3",
				ATPort:       "/dev/ttyUSB-new",
			},
		}, nil
	}

	originalResolveQMI := resolveDiscoveredQMIDeviceFn
	defer func() { resolveDiscoveredQMIDeviceFn = originalResolveQMI }()
	resolveDiscoveredQMIDeviceFn = func(dev QMIDevice, timeout time.Duration, allowProbe bool) (QMIDevice, string) {
		if dev.ControlPath == "/dev/cdc-wdm-new-qmi" {
			return dev, "123456789012345"
		}
		return dev, ""
	}

	// 初始化 Pool
	p := NewPool(&config.Config{})

	devCfg := config.DeviceConfig{
		ID:             "dev-qmi-1",
		DeviceBackend:  "qmi",
		ModemIMEI:      "123456789012345",
		ControlDevice:  "/dev/nonexistent-control-old",
		Interface:      "wwan-old",
		USBPath:        "1-9.9",
		NetworkEnabled: true, // hasManagedQMINetwork 的条件
	}

	// 此时 /dev/nonexistent-control-old 不存在，controlDeviceStatErr != nil。
	// 但 shouldDiscoverQMIManagedBootstrapByIMEI 会返回 true。
	// 它会用 discovery 取回 /dev/cdc-wdm-new-qmi，并用新的 QMI attachment 启动 worker。
	w, err := p.AddWorkerFromConfig(devCfg)
	require.NoError(t, err)
	require.NotNil(t, w)
	t.Cleanup(func() {
		_ = p.RemoveWorker(w.ID)
		_ = p.Shutdown()
	})

	require.Equal(t, "/dev/cdc-wdm-new-qmi", w.Config.ControlDevice)
	require.Equal(t, "/dev/cdc-wdm-new-qmi", w.Config.QMIDevice)
	require.Equal(t, "wwan-new", w.Config.Interface)
	require.Equal(t, "1-2.3", w.Config.USBPath)
	require.Equal(t, "/dev/ttyUSB-new", w.Config.ATPort)
	require.Equal(t, "/dev/ttyUSB-new", w.Config.ManagePort)
	require.NotNil(t, w.QMICore)
	require.Equal(t, "/dev/cdc-wdm-new-qmi", w.QMICore.ControlDevice())
}
