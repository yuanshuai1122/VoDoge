// 全局短信发送限额：滚动 1 小时窗口，所有设备共用。
//
// 计数走独立表 sms_send_attempts，不读 sms 历史——删会话不能回补额度。
// 接收不占用额度。limit<=0 表示不限制，但仍会记一条 attempt 方便设置页显示已用条数。
package db

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	SMSRateWindow = time.Hour
	// smsSendRateLockKey 把同一事务里的计数+插入串起来，避免并发超发。
	smsSendRateLockKey int64 = 75750001
)

// SMSRateStatus 是当前滚动窗口的用量快照。
type SMSRateStatus struct {
	HourlyLimit   int  `json:"hourly_limit"`
	Used          int  `json:"used"`
	Remaining     int  `json:"remaining"`
	WindowSeconds int  `json:"window_seconds"`
	Unlimited     bool `json:"unlimited"`
}

// SMSRateLimitedError 表示本窗口额度已用完。
type SMSRateLimitedError struct {
	SMSRateStatus
	RetryAfterSeconds int `json:"retry_after_seconds"`
}

func (e *SMSRateLimitedError) Error() string {
	if e == nil {
		return "短信发送已达每小时上限"
	}
	return fmt.Sprintf("短信发送已达每小时上限 %d 条，请 %d 秒后再试", e.HourlyLimit, e.RetryAfterSeconds)
}

func IsSMSRateLimited(err error) bool {
	var e *SMSRateLimitedError
	return errors.As(err, &e)
}

func NewSMSRateStatus(limit, used int) SMSRateStatus {
	if limit < 0 {
		limit = 0
	}
	if used < 0 {
		used = 0
	}
	unlimited := limit == 0
	remaining := 0
	if !unlimited {
		remaining = limit - used
		if remaining < 0 {
			remaining = 0
		}
	}
	return SMSRateStatus{
		HourlyLimit:   limit,
		Used:          used,
		Remaining:     remaining,
		WindowSeconds: int(SMSRateWindow.Seconds()),
		Unlimited:     unlimited,
	}
}

func retryAfterSeconds(oldest, now time.Time) int {
	if oldest.IsZero() {
		return int(SMSRateWindow.Seconds())
	}
	until := oldest.Add(SMSRateWindow).Sub(now)
	sec := int((until + time.Second - 1) / time.Second)
	if sec < 1 {
		return 1
	}
	return sec
}

// GetSMSRateStatus 只读当前窗口用量，不占额度。
func GetSMSRateStatus(limit int) (SMSRateStatus, error) {
	if limit < 0 {
		limit = 0
	}
	if DB == nil {
		return NewSMSRateStatus(limit, 0), nil
	}
	since := time.Now().Add(-SMSRateWindow)
	var count int64
	if err := DB.Model(&SMSSendAttempt{}).Where("created_at > ?", since).Count(&count).Error; err != nil {
		return SMSRateStatus{}, err
	}
	return NewSMSRateStatus(limit, int(count)), nil
}

// ReserveSMSSend 在窗口内占 1 条额度。超限返回 *SMSRateLimitedError。
func ReserveSMSSend(limit int, deviceID, recipient string) (SMSRateStatus, error) {
	if limit < 0 {
		limit = 0
	}
	if DB == nil {
		return NewSMSRateStatus(limit, 0), nil
	}
	var status SMSRateStatus
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", smsSendRateLockKey).Error; err != nil {
			return err
		}
		now := time.Now()
		since := now.Add(-SMSRateWindow)
		var count int64
		if err := tx.Model(&SMSSendAttempt{}).Where("created_at > ?", since).Count(&count).Error; err != nil {
			return err
		}
		if limit > 0 && count >= int64(limit) {
			var oldest time.Time
			if err := tx.Model(&SMSSendAttempt{}).
				Where("created_at > ?", since).
				Select("COALESCE(MIN(created_at), ?)", since).
				Scan(&oldest).Error; err != nil {
				return err
			}
			return &SMSRateLimitedError{
				SMSRateStatus:     NewSMSRateStatus(limit, int(count)),
				RetryAfterSeconds: retryAfterSeconds(oldest, now),
			}
		}
		attempt := SMSSendAttempt{
			DeviceID:  strings.TrimSpace(deviceID),
			Recipient: strings.TrimSpace(recipient),
			CreatedAt: now,
		}
		if err := tx.Create(&attempt).Error; err != nil {
			return err
		}
		_ = tx.Where("created_at < ?", now.Add(-2*SMSRateWindow)).Delete(&SMSSendAttempt{})
		status = NewSMSRateStatus(limit, int(count)+1)
		return nil
	})
	return status, err
}
