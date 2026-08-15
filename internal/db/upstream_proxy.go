package db

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yuanshuai1122/vodoge/internal/upstreamproxy"
	"gorm.io/gorm"
)

// UpstreamProxy 前置代理实例（用于代理 VoWiFi 的 ePDG 连接）
// 通过 Socks5 UDP Associate 将 IKE/ESP 流量转发到 ePDG
type UpstreamProxy struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	Name      string    `json:"name"`
	Addr      string    `json:"addr"`               // Socks5 服务器地址 (host:port)
	Username  string    `json:"username"`           // 可选鉴权用户名
	Password  string    `json:"password,omitempty"` // 可选鉴权密码
	Enabled   bool      `json:"enabled"`            // 是否启用
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UpstreamProxyCountryRule 将 SIM home country 路由到指定前置代理。
type UpstreamProxyCountryRule struct {
	CountryCode     string    `gorm:"primaryKey" json:"country_code"`
	UpstreamProxyID string    `gorm:"index" json:"upstream_proxy_id"`
	Enabled         bool      `json:"enabled"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (UpstreamProxyCountryRule) TableName() string {
	return "upstream_proxy_country_rules"
}

// UpstreamProxyProfileBinding 把一张 SIM / 一个 eSIM Profile（ICCID）绑到指定前置代理。
// ICCID 全局唯一，一张卡只能绑一个代理；一个代理可以服务多张卡。
type UpstreamProxyProfileBinding struct {
	ICCID           string    `gorm:"column:iccid;primaryKey" json:"iccid"`
	DeviceID        string    `gorm:"column:device_id;index" json:"device_id"`
	ProfileName     string    `gorm:"column:profile_name" json:"profile_name"`
	UpstreamProxyID string    `gorm:"column:upstream_proxy_id;index" json:"upstream_proxy_id"`
	CreatedAt       time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (UpstreamProxyProfileBinding) TableName() string {
	return "upstream_proxy_profile_bindings"
}

var ErrProfileBindingNotFound = errors.New("profile proxy binding not found")

const (
	profileBindingICCIDMin = 18
	profileBindingICCIDMax = 22
)

func NormalizeProfileBindingICCID(in string) string {
	return strings.TrimSpace(in)
}

func ValidProfileBindingICCID(in string) bool {
	s := NormalizeProfileBindingICCID(in)
	if len(s) < profileBindingICCIDMin || len(s) > profileBindingICCIDMax {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ── UpstreamProxy CRUD ──

// ListUpstreamProxies 列出所有前置代理实例
func ListUpstreamProxies() ([]UpstreamProxy, error) {
	var out []UpstreamProxy
	if err := DB.Order("id asc").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// GetUpstreamProxyByID 根据 ID 获取前置代理
func GetUpstreamProxyByID(id string) (*UpstreamProxy, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("empty id")
	}
	var out UpstreamProxy
	err := DB.First(&out, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

// UpsertUpstreamProxy 创建或更新前置代理
func UpsertUpstreamProxy(p UpstreamProxy) error {
	if strings.TrimSpace(p.ID) == "" {
		return errors.New("empty id")
	}
	if strings.TrimSpace(p.Addr) == "" {
		return errors.New("empty addr")
	}
	return DB.Save(&p).Error
}

// DeleteUpstreamProxy 删除前置代理（同时清理关联的国家规则）
func DeleteUpstreamProxy(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("empty id")
	}
	if err := DB.Delete(&UpstreamProxyCountryRule{}, "upstream_proxy_id = ?", id).Error; err != nil {
		return err
	}
	if err := DB.Delete(&UpstreamProxyProfileBinding{}, "upstream_proxy_id = ?", id).Error; err != nil {
		return err
	}
	return DB.Delete(&UpstreamProxy{}, "id = ?", id).Error
}

// ── UpstreamProxyCountryRule 国家规则管理 ──

func ListUpstreamProxyCountryRules() ([]UpstreamProxyCountryRule, error) {
	var out []UpstreamProxyCountryRule
	if err := DB.Order("country_code asc").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func UpsertUpstreamProxyCountryRule(rule UpstreamProxyCountryRule) error {
	rule.CountryCode = upstreamproxy.NormalizeCountryCode(rule.CountryCode)
	rule.UpstreamProxyID = strings.TrimSpace(rule.UpstreamProxyID)
	if rule.CountryCode == "" {
		return errors.New("empty country_code")
	}
	if rule.UpstreamProxyID == "" {
		return errors.New("empty upstream_proxy_id")
	}
	rule.UpdatedAt = time.Now()
	return DB.Save(&rule).Error
}

func DeleteUpstreamProxyCountryRule(countryCode string) error {
	countryCode = upstreamproxy.NormalizeCountryCode(countryCode)
	if countryCode == "" {
		return errors.New("empty country_code")
	}
	return DB.Delete(&UpstreamProxyCountryRule{}, "country_code = ?", countryCode).Error
}

func GetCountryUpstreamProxy(countryCode string) (*UpstreamProxy, error) {
	countryCode = upstreamproxy.NormalizeCountryCode(countryCode)
	if countryCode == "" {
		return nil, nil
	}
	var rule UpstreamProxyCountryRule
	err := DB.First(&rule, "country_code = ?", countryCode).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if !rule.Enabled || strings.TrimSpace(rule.UpstreamProxyID) == "" {
		return nil, nil
	}
	proxy, err := GetUpstreamProxyByID(rule.UpstreamProxyID)
	if err != nil || proxy == nil || !proxy.Enabled {
		return nil, err
	}
	return proxy, nil
}

func GetHomeMCCUpstreamProxy(homeMCC string) (*UpstreamProxy, string, error) {
	countryCode, ok := upstreamproxy.CountryCodeFromHomeMCC(homeMCC)
	if !ok {
		return nil, "", nil
	}
	proxy, err := GetCountryUpstreamProxy(countryCode)
	return proxy, countryCode, err
}

func ListProfileBindings() ([]UpstreamProxyProfileBinding, error) {
	var out []UpstreamProxyProfileBinding
	if err := DB.Order("device_id asc, profile_name asc, iccid asc").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func GetProfileBinding(iccid string) (*UpstreamProxyProfileBinding, error) {
	iccid = NormalizeProfileBindingICCID(iccid)
	if iccid == "" {
		return nil, errors.New("empty iccid")
	}
	var out UpstreamProxyProfileBinding
	err := DB.First(&out, "iccid = ?", iccid).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func UpsertProfileBinding(b UpstreamProxyProfileBinding) error {
	b.ICCID = NormalizeProfileBindingICCID(b.ICCID)
	b.DeviceID = strings.TrimSpace(b.DeviceID)
	b.ProfileName = strings.TrimSpace(b.ProfileName)
	b.UpstreamProxyID = strings.TrimSpace(b.UpstreamProxyID)
	if !ValidProfileBindingICCID(b.ICCID) {
		return fmt.Errorf("invalid iccid")
	}
	if b.DeviceID == "" {
		return errors.New("empty device_id")
	}
	if b.UpstreamProxyID == "" {
		return errors.New("empty upstream_proxy_id")
	}
	now := time.Now()
	if b.CreatedAt.IsZero() {
		b.CreatedAt = now
	}
	b.UpdatedAt = now
	return DB.Save(&b).Error
}

func DeleteProfileBinding(iccid string) error {
	iccid = NormalizeProfileBindingICCID(iccid)
	if iccid == "" {
		return errors.New("empty iccid")
	}
	res := DB.Delete(&UpstreamProxyProfileBinding{}, "iccid = ?", iccid)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrProfileBindingNotFound
	}
	return nil
}

// GetProfileUpstreamProxy 按 ICCID 取已启用的前置代理；未绑定或代理停用时返回 nil。
func GetProfileUpstreamProxy(iccid string) (*UpstreamProxy, error) {
	iccid = NormalizeProfileBindingICCID(iccid)
	if iccid == "" {
		return nil, nil
	}
	binding, err := GetProfileBinding(iccid)
	if err != nil || binding == nil || strings.TrimSpace(binding.UpstreamProxyID) == "" {
		return nil, err
	}
	proxy, err := GetUpstreamProxyByID(binding.UpstreamProxyID)
	if err != nil || proxy == nil || !proxy.Enabled {
		return nil, err
	}
	return proxy, nil
}
