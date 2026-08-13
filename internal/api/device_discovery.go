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

var discoverQMIForMgmtFn = device.DiscoverQMIDevices

var discoverCompatibleModemsFromQMIFn = device.DiscoverCompatibleModemsFromQMI

var enrichDiscoveredCompatibleModemFn = device.EnrichDiscoveredCompatibleModem

var probeIMEIForAddFn = device.ProbeIMEIViaQMI

var probeIMEIViaMBIMForMgmtFn = device.ProbeIMEIViaMBIM

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
	discoveredQMI, err := discoverQMIForMgmtFn()
	if err != nil {
		discoveredQMI = nil
	}

	list, err := discoverCompatibleModemsFromQMIFn(discoveredQMI)
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
					probed, discoveredIMEI := enrichDiscoveredCompatibleModemFn(dev, device.CompatibleModemEnrichOptions{
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
						if mbimIMEI, err := probeIMEIViaMBIMForMgmtFn(dev.ControlPath); err == nil && mbimIMEI != "" {
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
