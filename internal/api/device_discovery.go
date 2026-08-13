// 硬件发现：枚举 QMI/MBIM 设备，供添加设备时选择。
package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yuanshuai1122/vohive/internal/config"
	"github.com/yuanshuai1122/vohive/internal/device"

	"github.com/gin-gonic/gin"
)

type discoveredDevice struct {
	DiscoveryKey   string   `json:"discovery_key"`
	ControlPath    string   `json:"control_path"`
	NetInterface   string   `json:"net_interface"`
	USBPath        string   `json:"usb_path"`
	IMEI           string   `json:"imei,omitempty"`
	VendorID       uint16   `json:"vendor_id"`
	ProductID      uint16   `json:"product_id"`
	DriverName     string   `json:"driver_name"`
	ATPorts        []string `json:"at_ports"`
	ATPort         string   `json:"at_port"`
	AudioDevice    string   `json:"audio_device,omitempty"`
	Mode           string   `json:"mode,omitempty"`  // qmi/mbim/ecm/rndis/ncm/unknown
	NetworkCapable bool     `json:"network_capable"` // 是否可由 QMI Core 接管
	Configured     bool     `json:"configured"`
	ConfiguredID   string   `json:"configured_id,omitempty"`
	Degraded       bool     `json:"degraded,omitempty"` // 探不到 IMEI,无法确立身份,不可直接添加
}

// hardwareProbe 是硬件枚举与探测的边界。
//
// 这几件事都要真的去摸 /dev 下的 QMI/MBIM 控制设备，本机与 CI 都没有，
// 因此必须可替换。此前的做法是五个包级 `var xxxFn = device.Xxx`，测试直接赋值
// 再在 defer 里还原——那是全局可变状态：包内测试不能并行，忘了还原就会污染
// 后面的用例，而且从 Server 的定义里完全看不出它依赖硬件。
//
// 现在依赖挂在 Server 上，显式且各测试互不影响。
type hardwareProbe interface {
	DiscoverQMI() ([]device.QMIDevice, error)
	CompatibleModemsFromQMI(qmiList []device.QMIDevice) ([]device.CompatibleModem, error)
	EnrichCompatibleModem(dev device.CompatibleModem, opts device.CompatibleModemEnrichOptions) (device.CompatibleModem, string)
	// ProbeIMEIViaQMI / ViaMBIM 是两条独立通路：QMI 探不到时回退 MBIM，
	// 两者都失败才判定为 degraded（无法确立身份，不可直接添加）。
	ProbeIMEIViaQMI(controlPath string) (string, error)
	ProbeIMEIViaMBIM(controlPath string) (string, error)
}

// realHardwareProbe 直连 internal/device 的实现。
type realHardwareProbe struct{}

func (realHardwareProbe) DiscoverQMI() ([]device.QMIDevice, error) {
	return device.DiscoverQMIDevices()
}

func (realHardwareProbe) CompatibleModemsFromQMI(qmiList []device.QMIDevice) ([]device.CompatibleModem, error) {
	return device.DiscoverCompatibleModemsFromQMI(qmiList)
}

func (realHardwareProbe) EnrichCompatibleModem(dev device.CompatibleModem, opts device.CompatibleModemEnrichOptions) (device.CompatibleModem, string) {
	return device.EnrichDiscoveredCompatibleModem(dev, opts)
}

func (realHardwareProbe) ProbeIMEIViaQMI(controlPath string) (string, error) {
	return device.ProbeIMEIViaQMI(controlPath)
}

func (realHardwareProbe) ProbeIMEIViaMBIM(controlPath string) (string, error) {
	return device.ProbeIMEIViaMBIM(controlPath)
}

// hw 返回本次请求使用的硬件探测实现；未注入时用真实实现。
func (s *Server) hw() hardwareProbe {
	if s.hardware != nil {
		return s.hardware
	}
	return realHardwareProbe{}
}

func ensureAddDeviceIMEI(cfg config.DeviceConfig, probe func(string) (string, error)) (config.DeviceConfig, error) {
	if strings.TrimSpace(cfg.ControlDevice) == "" || config.NormalizeIMEI(cfg.ModemIMEI) != "" {
		return cfg, nil
	}
	probed, err := probe(cfg.ControlDevice)
	if err != nil || strings.TrimSpace(probed) == "" {
		return cfg, fmt.Errorf("IMEI 探测失败，请重新插拔设备或稍后重试")
	}
	cfg.ModemIMEI = strings.TrimSpace(probed)
	return cfg, nil
}

func (s *Server) handleDeviceMgmtDiscovered(c *gin.Context) {
	discoveredQMI, err := s.hw().DiscoverQMI()
	if err != nil {
		discoveredQMI = nil
	}

	list, err := s.hw().CompatibleModemsFromQMI(discoveredQMI)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"devices": []discoveredDevice{}})
		return
	}

	withIMEI := strings.TrimSpace(c.Query("with_imei")) == "1"
	managedIndex := device.BuildWorkerDiscoveryIndex(s.pool.GetAllWorkers(), withIMEI)

	managed := config.ListDevices()

	// 第一阶段:并行补全每块硬件(AT/IMEI探测),不做任何身份判定。
	enriched := make([]device.CompatibleModem, len(list))
	var wg sync.WaitGroup

	for i, d := range list {
		wg.Add(1)
		go func(idx int, dev device.CompatibleModem) {
			defer wg.Done()

			resolvedATPort := strings.TrimSpace(dev.ATPort)
			imei := strings.TrimSpace(dev.IMEI)

			if withIMEI {
				managedMatch, hasManaged := managedIndex.Lookup(strings.TrimSpace(dev.ControlPath), strings.TrimSpace(dev.USBPath), strings.TrimSpace(dev.NetInterface))
				if hasManaged {
					if containsDiscoveredATPort(dev.ATPorts, managedMatch.ATPort) {
						resolvedATPort = managedMatch.ATPort
					}
					if imei == "" {
						imei = managedMatch.IMEI
					}
				} else {
					probed, discoveredIMEI := s.hw().EnrichCompatibleModem(dev, device.CompatibleModemEnrichOptions{
						EnableATProbe:      true,
						ATProbeTimeout:     900 * time.Millisecond,
						EnableQMIIMEIProbe: strings.TrimSpace(dev.ControlPath) != "" && dev.Mode != "mbim",
					})
					dev = probed
					if resolved := strings.TrimSpace(probed.ATPort); resolved != "" {
						resolvedATPort = resolved
					}
					if imei == "" {
						imei = discoveredIMEI
					}
					// MBIM 设备没有 AT 端口也不支持 QMI，使用 MBIM DeviceCaps 探测 IMEI
					if imei == "" && dev.Mode == "mbim" && strings.TrimSpace(dev.ControlPath) != "" {
						if mbimIMEI, err := s.hw().ProbeIMEIViaMBIM(dev.ControlPath); err == nil && mbimIMEI != "" {
							imei = mbimIMEI
						}
					}
				}
			}

			mode := strings.ToLower(strings.TrimSpace(dev.Mode))
			networkCapable := dev.NetworkCapable

			if mode == "" {
				mode = "unknown"
			}

			dev.IMEI = imei
			dev.ATPort = resolvedATPort
			dev.Mode = mode
			dev.NetworkCapable = networkCapable
			enriched[idx] = dev
		}(i, d)
	}
	wg.Wait()

	// 第二阶段:统一身份解析(按 IMEI),路径不再是身份。
	resolved := device.ResolveDeviceIdentities(enriched, managed)

	out := make([]discoveredDevice, 0, len(enriched))
	for _, pair := range resolved.Matched {
		out = append(out, buildDiscoveredDevice(pair.Hardware, true, pair.Config.ID, false))
	}
	for _, hw := range resolved.Unmatched {
		out = append(out, buildDiscoveredDevice(hw, false, "", false))
	}
	for _, hw := range resolved.Degraded {
		out = append(out, buildDiscoveredDevice(hw, false, "", true))
	}

	c.JSON(http.StatusOK, gin.H{"devices": out})
}

func buildDiscoveredDevice(hw device.CompatibleModem, configured bool, configuredID string, degraded bool) discoveredDevice {
	return discoveredDevice{
		DiscoveryKey:   hw.DiscoveryKey(),
		ControlPath:    hw.ControlPath,
		NetInterface:   hw.NetInterface,
		USBPath:        hw.USBPath,
		IMEI:           strings.TrimSpace(hw.IMEI),
		VendorID:       hw.VendorID,
		ProductID:      hw.ProductID,
		DriverName:     hw.DriverName,
		ATPorts:        hw.ATPorts,
		ATPort:         hw.ATPort,
		AudioDevice:    hw.AudioDevice,
		Mode:           hw.Mode,
		NetworkCapable: hw.NetworkCapable,
		Configured:     configured,
		ConfiguredID:   configuredID,
		Degraded:       degraded,
	}
}

func containsDiscoveredATPort(ports []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, port := range ports {
		if strings.TrimSpace(port) == target {
			return true
		}
	}
	return false
}
