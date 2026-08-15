// GORM 模型定义。
//
// 表结构的唯一来源——启动时 AutoMigrate 按这些结构体建表（见 migrate.go）。
// 改字段就是改表结构，没有单独的 DDL 脚本。
package db

import (
	"time"
)

// Device 模块设备表 (主键: IMEI)
type Device struct {
	IMEI         string    `gorm:"primaryKey" json:"imei"`
	Alias        string    `json:"alias"`
	Model        string    `json:"model"`
	Firmware     string    `json:"firmware"`
	Port         string    `json:"port"`
	PublicIP     string    `json:"public_ip"`  // 当前公网IP
	PrivateIP    string    `json:"private_ip"` // 当前内网IP
	PublicIPv6   string    `json:"public_ipv6"`
	PrivateIPv6  string    `json:"private_ipv6"`
	CurrentICCID *string   `gorm:"column:iccid" json:"current_iccid"`
	SimInserted  bool      `json:"sim_inserted"`
	SignalDBM    int       `json:"signal_dbm"`
	SignalRSRQ   int       `json:"signal_rsrq"`
	SignalRSRP   int       `json:"signal_rsrp"`
	LastSeen     time.Time `json:"last_seen"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// SIMCard SIM卡表 (主键: ICCID)
type SIMCard struct {
	ICCID         string    `gorm:"column:iccid;primaryKey" json:"iccid"`
	IMSI          string    `json:"imsi"`
	Operator      string    `json:"operator"`        // 运营商
	CurrentIMEI   *string   `json:"current_imei"`    // 当前所在的设备
	RegStatus     int       `json:"reg_status"`      // 网络注册状态 (0-5)
	RegStatusText string    `json:"reg_status_text"` // 注册状态文本
	LAC           string    `json:"lac"`             // 位置区代码
	CellID        string    `json:"cell_id"`         // 小区 ID
	APN           string    `json:"apn"`             // 接入点
	IMSStatus     int       `json:"ims_status"`      // IMS 注册状态
	LastSeen      time.Time `json:"last_seen"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type SIMSubscription struct {
	IMSI              string    `gorm:"column:imsi;primaryKey" json:"imsi"`
	CurrentICCID      string    `gorm:"column:current_iccid;index" json:"current_iccid"`
	PhoneNumber       string    `gorm:"column:phone_number" json:"phone_number"`
	ModemPhoneNumber  string    `gorm:"column:modem_phone_number" json:"modem_phone_number"`
	VowifiPhoneNumber string    `gorm:"column:vowifi_phone_number" json:"vowifi_phone_number"`
	Operator          string    `gorm:"column:operator" json:"operator"`
	LastSeen          time.Time `gorm:"column:last_seen" json:"last_seen"`
	CreatedAt         time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt         time.Time `gorm:"column:updated_at" json:"updated_at"`
}

// PendingPhoneNumber 在 IMSI 未知时按 ICCID 暂存本机号码，IMSI 到位后迁移进 sim_subscriptions。
type PendingPhoneNumber struct {
	ICCID             string    `gorm:"column:iccid;primaryKey" json:"iccid"`
	PhoneNumber       string    `gorm:"column:phone_number" json:"phone_number"`
	ModemPhoneNumber  string    `gorm:"column:modem_phone_number" json:"modem_phone_number"`
	VowifiPhoneNumber string    `gorm:"column:vowifi_phone_number" json:"vowifi_phone_number"`
	CreatedAt         time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt         time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (PendingPhoneNumber) TableName() string { return "pending_phone_numbers" }

// SMS 短信表 (关联 IMSI)
type SMS struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	IMSI       string    `gorm:"column:imsi;index:idx_sms_imsi_peer_ts,priority:1;index:idx_sms_imsi_ts,priority:1" json:"imsi"`
	ICCID      string    `gorm:"column:iccid;index" json:"iccid"`
	Peer       string    `gorm:"column:peer;index:idx_sms_imsi_peer_ts,priority:2" json:"peer"`
	LocalPhone string    `gorm:"column:local_phone;index" json:"local_phone"`
	Sender     string    `json:"sender"`
	Recipient  string    `json:"recipient"`
	Content    string    `json:"content"`
	Type       int       `json:"type"`   // 1: 接收, 2: 发送
	Status     int       `json:"status"` // 0: 未读, 1: 已读, 2: 发送成功, 3: 发送失败
	Timestamp  time.Time `gorm:"index:idx_sms_imsi_peer_ts,priority:3,sort:desc;index:idx_sms_ts,sort:desc;index:idx_sms_imsi_ts,priority:2,sort:desc" json:"timestamp"`
	CreatedAt  time.Time `json:"created_at"`
}

type SMSContact struct {
	IMSI          string    `gorm:"column:imsi;primaryKey;index:idx_sms_contact_imsi_last_ts,priority:1" json:"imsi"`
	ICCID         string    `gorm:"column:iccid;index" json:"iccid"`
	Peer          string    `gorm:"column:peer;primaryKey" json:"peer"`
	LastSMSID     uint      `gorm:"column:last_sms_id" json:"last_sms_id"`
	LastTimestamp time.Time `gorm:"column:last_timestamp;index:idx_sms_contact_imsi_last_ts,priority:2,sort:desc;index:idx_sms_contact_last_ts,sort:desc" json:"last_timestamp"`
	LastContent   string    `gorm:"column:last_content" json:"last_content"`
	LastType      int       `gorm:"column:last_type" json:"last_type"`
	UnreadCount   int       `gorm:"column:unread_count" json:"unread_count"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// SMSSendAttempt 是发送限额的独立计数表。
//
// 不跟 sms 历史混用：删会话/改状态不能回补额度。所有设备共用一条滚动窗口。
type SMSSendAttempt struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	DeviceID  string    `gorm:"column:device_id;index" json:"device_id"`
	Recipient string    `gorm:"column:recipient" json:"recipient"`
	CreatedAt time.Time `gorm:"column:created_at;index:idx_sms_send_attempts_created,sort:desc" json:"created_at"`
}

func (SMSSendAttempt) TableName() string { return "sms_send_attempts" }
