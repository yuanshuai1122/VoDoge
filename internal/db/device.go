// 设备表的读写。设备以 IMEI 为主键；ICCID 是可变的当前插卡。
package db

import (
	"time"

	"gorm.io/gorm"
)

// UpsertDevice 更新或插入设备记录
func UpsertDevice(imei, alias, model, port string) error {
	if DB == nil {
		return nil
	}
	var device Device
	result := DB.Where("imei = ?", imei).First(&device)
	if result.Error == gorm.ErrRecordNotFound {
		device = Device{
			IMEI:      imei,
			Alias:     alias,
			Model:     model,
			Port:      port,
			LastSeen:  time.Now(),
			CreatedAt: time.Now(),
		}
		return DB.Create(&device).Error
	}
	// 更新现有设备
	return DB.Model(&device).Updates(map[string]interface{}{
		"alias":     alias,
		"model":     model,
		"port":      port,
		"last_seen": time.Now(),
	}).Error
}

// UpdateDeviceCurrentSIM 更新设备当前插入的 SIM 卡
func UpdateDeviceCurrentSIM(imei string, iccid *string) error {
	return DB.Model(&Device{}).Where("imei = ?", imei).Updates(map[string]interface{}{
		"iccid":     iccid,
		"last_seen": time.Now(),
	}).Error
}

// UpdateDeviceSignal 更新设备信号强度
func UpdateDeviceSignal(imei string, signalDBM int) error {
	return DB.Model(&Device{}).Where("imei = ?", imei).Updates(map[string]interface{}{
		"signal_dbm": signalDBM,
		"last_seen":  time.Now(),
	}).Error
}

// GetAllDevices 获取所有设备
func GetAllDevices() ([]Device, error) {
	var devices []Device
	err := DB.Find(&devices).Error
	return devices, err
}

// GetDevicePublicIP 获取设备当前外网 IP (PublicIP)
func GetDevicePublicIP(imei string) (string, error) {
	var device Device
	if err := DB.Select("public_ip").Where("imei = ?", imei).First(&device).Error; err != nil {
		return "", err
	}
	return device.PublicIP, nil
}

// UpdateDeviceIPs 更新设备当前 IP (PublicIP 和 PrivateIP)
func UpdateDeviceIPs(imei, publicIP, privateIP string) error {
	updates := map[string]interface{}{
		"last_seen": time.Now(),
	}
	if publicIP != "" {
		updates["public_ip"] = publicIP
	}
	if privateIP != "" {
		updates["private_ip"] = privateIP
	}
	return DB.Model(&Device{}).Where("imei = ?", imei).Updates(updates).Error
}

// UpdateDeviceIPsV6 updates v4/v6 public and private addresses; empty values do not overwrite existing fields.
func UpdateDeviceIPsV6(imei, publicV4, publicV6, privateV4, privateV6 string) error {
	updates := map[string]interface{}{
		"last_seen": time.Now(),
	}
	if publicV4 != "" {
		updates["public_ip"] = publicV4
	}
	if publicV6 != "" {
		updates["public_ipv6"] = publicV6
	}
	if privateV4 != "" {
		updates["private_ip"] = privateV4
	}
	if privateV6 != "" {
		updates["private_ipv6"] = privateV6
	}
	return DB.Model(&Device{}).Where("imei = ?", imei).Updates(updates).Error
}

// CurrentICCIDForDevice 尝试通过 alias (即 device id) 或 imei 查询关联的当前 ICCID。
func CurrentICCIDForDevice(deviceID string) string {
	if DB == nil {
		return ""
	}
	var dev Device
	if err := DB.Where("alias = ? OR imei = ?", deviceID, deviceID).First(&dev).Error; err == nil {
		if dev.CurrentICCID != nil {
			return *dev.CurrentICCID
		}
	}
	return ""
}
