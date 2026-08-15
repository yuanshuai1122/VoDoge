package device

import (
	"fmt"
	"strings"

	"github.com/yuanshuai1122/vodoge/internal/config"
)

const DefaultFreeDeviceLimit = config.DefaultDeviceLimit

func (p *Pool) deviceLimit() int {
	if p == nil {
		return config.DefaultDeviceLimit
	}
	return config.ResolveDeviceLimit(p.cfg)
}

func DeviceLimitReached(count, limit int) bool {
	if limit <= 0 {
		limit = config.DefaultDeviceLimit
	}
	return count >= limit
}

func FreeDeviceLimitReached(count int) bool {
	return DeviceLimitReached(count, DefaultFreeDeviceLimit)
}

func DeviceAddLimitMessage(limit int) string {
	if limit <= 0 {
		limit = config.DefaultDeviceLimit
	}
	return fmt.Sprintf("当前最多只能添加 %d 个设备", limit)
}

func FreeDeviceAddLimitMessage() string {
	return DeviceAddLimitMessage(DefaultFreeDeviceLimit)
}

func DeviceWorkerLimitMessage(limit int) string {
	if limit <= 0 {
		limit = config.DefaultDeviceLimit
	}
	return fmt.Sprintf("当前最多只能启动 %d 个设备", limit)
}

func FreeDeviceWorkerLimitMessage() string {
	return DeviceWorkerLimitMessage(DefaultFreeDeviceLimit)
}

func DeviceLimitAllowsConfiguredDevice(devices []config.DeviceConfig, deviceID string, limit int) bool {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return true
	}
	if limit <= 0 {
		limit = config.DefaultDeviceLimit
	}
	seen := 0
	for _, dev := range devices {
		id := strings.TrimSpace(dev.ID)
		if id == "" {
			continue
		}
		seen++
		if id == deviceID {
			return seen <= limit
		}
	}
	return true
}

func FreeDeviceLimitAllowsConfiguredDevice(devices []config.DeviceConfig, deviceID string) bool {
	return DeviceLimitAllowsConfiguredDevice(devices, deviceID, DefaultFreeDeviceLimit)
}
