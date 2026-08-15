package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodog/internal/config"
	"github.com/yuanshuai1122/vodog/internal/device"
)

// 不同 IMEI 落在已配置设备的旧路径上,不再是"冲突":新模组就是一台可正常添加的设备,
// 旧配置离线(身份锚定后路径不再有否决权)。
func TestHandleDeviceMgmtDiscoveredDifferentIMEIIsPlainAddable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := writeDeviceMgmtDiscoveryConfig(t, `
server:
  port: ":7575"
devices:
  - id: old-device
    modem_imei: "111111111111111"
    control_device: /dev/cdc-wdm0
    interface: wwan0
    usb_path: /sys/bus/usb/devices/1-1
    at_port: /dev/ttyUSB2
    device_backend: qmi
`)
	if err := config.InitGlobalManager(path); err != nil {
		t.Fatalf("InitGlobalManager() error = %v", err)
	}
	hw := &fakeHardwareProbe{}
	hw.discoverQMI = func() ([]device.QMIDevice, error) { return nil, nil }
	hw.compatibleModems = func([]device.QMIDevice) ([]device.CompatibleModem, error) {
		return []device.CompatibleModem{{
			ControlPath:    "/dev/cdc-wdm0",
			NetInterface:   "wwan0",
			USBPath:        "/sys/bus/usb/devices/1-1",
			ATPorts:        []string{"/dev/ttyUSB2"},
			ATPort:         "/dev/ttyUSB2",
			Mode:           "qmi",
			NetworkCapable: true,
		}}, nil
	}
	hw.enrich = func(dev device.CompatibleModem, opts device.CompatibleModemEnrichOptions) (device.CompatibleModem, string) {
		dev.IMEI = "222222222222222"
		return dev, "222222222222222"
	}

	got := requestDiscoveredDevices(t, &Server{pool: device.NewPool(&config.Config{}), configPath: path, hardware: hw})
	if len(got.Devices) != 1 {
		t.Fatalf("devices len = %d, want 1", len(got.Devices))
	}
	d := got.Devices[0]
	if d.Configured {
		t.Fatalf("Configured = true, want false: %+v", d)
	}
	if d.Degraded {
		t.Fatalf("Degraded = true, want false (IMEI is readable): %+v", d)
	}
	if d.IMEI != "222222222222222" {
		t.Fatalf("IMEI = %q, want new device IMEI", d.IMEI)
	}
}

// 探不到 IMEI 的硬件(如 MBIM 挂死返回垃圾)归 degraded:不可直接添加,UI 需提示。
func TestHandleDeviceMgmtDiscoveredMarksDegradedWhenIMEIUnreadable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := writeDeviceMgmtDiscoveryConfig(t, `
server:
  port: ":7575"
devices: []
`)
	if err := config.InitGlobalManager(path); err != nil {
		t.Fatalf("InitGlobalManager() error = %v", err)
	}
	hw := &fakeHardwareProbe{}
	hw.discoverQMI = func() ([]device.QMIDevice, error) { return nil, nil }
	hw.compatibleModems = func([]device.QMIDevice) ([]device.CompatibleModem, error) {
		return []device.CompatibleModem{{
			ControlPath:  "/dev/cdc-wdm1",
			NetInterface: "wwan3",
			USBPath:      "/sys/bus/usb/devices/1-9",
			Mode:         "mbim",
		}}, nil
	}
	hw.enrich = func(dev device.CompatibleModem, opts device.CompatibleModemEnrichOptions) (device.CompatibleModem, string) {
		return dev, "" // AT/QMI 探不到 IMEI
	}
	hw.probeMBIM = func(string) (string, error) { return "", fmt.Errorf("mbim hung") } // MBIM 也读不到

	got := requestDiscoveredDevices(t, &Server{pool: device.NewPool(&config.Config{}), configPath: path, hardware: hw})
	if len(got.Devices) != 1 {
		t.Fatalf("devices len = %d, want 1", len(got.Devices))
	}
	d := got.Devices[0]
	if d.Configured || !d.Degraded {
		t.Fatalf("want Configured=false Degraded=true, got %+v", d)
	}
}

func TestHandleDeviceMgmtDiscoveredMarksConfiguredForSameIMEI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := writeDeviceMgmtDiscoveryConfig(t, `
server:
  port: ":7575"
devices:
  - id: old-device
    modem_imei: "111111111111111"
    control_device: /dev/cdc-wdm0
    interface: wwan0
    usb_path: /sys/bus/usb/devices/1-1
    at_port: /dev/ttyUSB2
    device_backend: qmi
`)
	if err := config.InitGlobalManager(path); err != nil {
		t.Fatalf("InitGlobalManager() error = %v", err)
	}
	hw := &fakeHardwareProbe{}
	hw.discoverQMI = func() ([]device.QMIDevice, error) { return nil, nil }
	hw.compatibleModems = func([]device.QMIDevice) ([]device.CompatibleModem, error) {
		return []device.CompatibleModem{{
			ControlPath:  "/dev/cdc-wdm0",
			NetInterface: "wwan0",
			USBPath:      "/sys/bus/usb/devices/1-1",
			ATPorts:      []string{"/dev/ttyUSB2"},
			ATPort:       "/dev/ttyUSB2",
			Mode:         "qmi",
		}}, nil
	}
	hw.enrich = func(dev device.CompatibleModem, opts device.CompatibleModemEnrichOptions) (device.CompatibleModem, string) {
		dev.IMEI = "111111111111111"
		return dev, "111111111111111"
	}

	got := requestDiscoveredDevices(t, &Server{pool: device.NewPool(&config.Config{}), configPath: path, hardware: hw})
	d := got.Devices[0]
	if !d.Configured || d.ConfiguredID != "old-device" {
		t.Fatalf("configured fields = %+v, want configured old-device", d)
	}
	if d.Degraded {
		t.Fatalf("Degraded = true, want false: %+v", d)
	}
}

// TestHandleDeviceMgmtDiscoveredLegacyPathConfigDegrades verifies that after the
// zero-path migration, a device configured with only path fields (no IMEI) can no
// longer be matched by legacyPathMatch because the migration scrubs those keys from
// disk on Load(). The discovered device appears as Degraded — this is the accepted
// behavioral risk of Option A (unconditional deprecation).
func TestHandleDeviceMgmtDiscoveredLegacyPathConfigDegrades(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := writeDeviceMgmtDiscoveryConfig(t, `
server:
  port: ":7575"
devices:
  - id: legacy-device
    control_device: /dev/cdc-wdm0
    interface: wwan0
    usb_path: /sys/bus/usb/devices/1-1
    at_port: /dev/ttyUSB2
    device_backend: qmi
`)
	if err := config.InitGlobalManager(path); err != nil {
		t.Fatalf("InitGlobalManager() error = %v", err)
	}
	hw := &fakeHardwareProbe{}
	hw.discoverQMI = func() ([]device.QMIDevice, error) { return nil, nil }
	hw.compatibleModems = func([]device.QMIDevice) ([]device.CompatibleModem, error) {
		return []device.CompatibleModem{{
			ControlPath:  "/dev/cdc-wdm0",
			NetInterface: "wwan0",
			USBPath:      "/sys/bus/usb/devices/1-1",
			ATPorts:      []string{"/dev/ttyUSB2"},
			ATPort:       "/dev/ttyUSB2",
			Mode:         "qmi",
		}}, nil
	}
	hw.enrich = func(dev device.CompatibleModem, opts device.CompatibleModemEnrichOptions) (device.CompatibleModem, string) {
		return dev, ""
	}

	got := requestDiscoveredDevices(t, &Server{pool: device.NewPool(&config.Config{}), configPath: path, hardware: hw})
	d := got.Devices[0]
	// 零路径迁移后:磁盘路径键已删,legacyPathMatch 无法命中,设备归入 degraded。
	if d.Configured {
		t.Fatalf("device should not be matched after path migration, got: %+v", d)
	}
	if !d.Degraded {
		t.Fatalf("device should be degraded (no IMEI, no matched config), got: %+v", d)
	}
}

type discoveredDevicesResponse struct {
	Devices []discoveredDevice `json:"devices"`
}

func requestDiscoveredDevices(t *testing.T, srv *Server) discoveredDevicesResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/devices/discovered?with_imei=1", nil)

	srv.handleDeviceMgmtDiscovered(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", recorder.Code, recorder.Body.String())
	}
	var devices []discoveredDevice
	decodeData(t, recorder, &devices)
	return discoveredDevicesResponse{Devices: devices}
}

func writeDeviceMgmtDiscoveryConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

// fakeHardwareProbe 替代此前那五个包级 `var xxxFn`。
//
// 差别不只是形式：那些是全局变量，每个用例都要 defer 还原，忘了就污染后面的用例，
// 也让包内测试无法并行。现在每个用例持有自己的一份，互不影响。
//
// 未赋值的钩子返回"什么都没发现"——多数用例只关心其中一两条通路。
type fakeHardwareProbe struct {
	discoverQMI      func() ([]device.QMIDevice, error)
	compatibleModems func([]device.QMIDevice) ([]device.CompatibleModem, error)
	enrich           func(device.CompatibleModem, device.CompatibleModemEnrichOptions) (device.CompatibleModem, string)
	probeQMI         func(string) (string, error)
	probeMBIM        func(string) (string, error)
}

func (f *fakeHardwareProbe) DiscoverQMI() ([]device.QMIDevice, error) {
	if f.discoverQMI != nil {
		return f.discoverQMI()
	}
	return nil, nil
}

func (f *fakeHardwareProbe) CompatibleModemsFromQMI(qmiList []device.QMIDevice) ([]device.CompatibleModem, error) {
	if f.compatibleModems != nil {
		return f.compatibleModems(qmiList)
	}
	return nil, nil
}

func (f *fakeHardwareProbe) EnrichCompatibleModem(dev device.CompatibleModem, opts device.CompatibleModemEnrichOptions) (device.CompatibleModem, string) {
	if f.enrich != nil {
		return f.enrich(dev, opts)
	}
	return dev, ""
}

func (f *fakeHardwareProbe) ProbeIMEIViaQMI(controlPath string) (string, error) {
	if f.probeQMI != nil {
		return f.probeQMI(controlPath)
	}
	return "", nil
}

func (f *fakeHardwareProbe) ProbeIMEIViaMBIM(controlPath string) (string, error) {
	if f.probeMBIM != nil {
		return f.probeMBIM(controlPath)
	}
	return "", nil
}

var _ hardwareProbe = (*fakeHardwareProbe)(nil)
