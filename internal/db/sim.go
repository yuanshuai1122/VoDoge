// SIM 卡身份与本机号码。
//
// 三张表分工：sim_cards 以 ICCID 为键记卡本身，sim_subscriptions 以 IMSI 为键
// 记号码与运营商，pending_phone_numbers 在 IMSI 尚未读到时按 ICCID 暂存号码，
// IMSI 到位后迁移过去。
//
// 号码有两个来源，优先级固定：vowifi > modem。
package db

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UpsertSIMCard 更新或插入 SIM 卡记录
func UpsertSIMCard(iccid, imsi, phoneNumber, operator string, currentIMEI *string) error {
	if err := UpsertSIMCardIdentity(iccid, imsi, operator, currentIMEI); err != nil {
		return err
	}
	if strings.TrimSpace(imsi) != "" {
		if err := migratePendingPhoneToSubscription(imsi, iccid); err != nil {
			return err
		}
	}
	if normalized := normalizeSIMPhoneNumber(phoneNumber); normalized != "" {
		return UpdateSIMCardModemPhoneNumberByIMSI(imsi, normalized)
	}
	return nil
}

func UpsertSIMCardIdentity(iccid, imsi, operator string, currentIMEI *string) error {
	if DB == nil {
		return nil
	}
	iccid = strings.TrimSpace(iccid)
	imsi = strings.TrimSpace(imsi)
	operator = strings.TrimSpace(operator)
	if iccid == "" {
		return nil
	}
	now := time.Now()
	sim := SIMCard{
		ICCID:       iccid,
		IMSI:        imsi,
		Operator:    operator,
		CurrentIMEI: currentIMEI,
		LastSeen:    now,
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "iccid"}},
			DoUpdates: clause.Assignments(map[string]any{
				"imsi":         imsi,
				"operator":     operator,
				"current_imei": currentIMEI,
				"last_seen":    now,
				"updated_at":   now,
			}),
		}).Create(&sim).Error; err != nil {
			return err
		}
		if imsi == "" {
			return nil
		}
		return upsertSIMSubscriptionIdentity(tx, imsi, iccid, operator, now)
	})
}

func upsertSIMSubscriptionIdentity(tx *gorm.DB, imsi, iccid, operator string, now time.Time) error {
	updates := map[string]any{
		"current_iccid": iccid,
		"operator":      operator,
		"last_seen":     now,
		"updated_at":    now,
	}
	row := SIMSubscription{
		IMSI:         imsi,
		CurrentICCID: iccid,
		Operator:     operator,
		LastSeen:     now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "imsi"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(&row).Error
}

// UpdateSIMCardPhoneNumberByIMSI 通过 IMSI 写入/更新手机号。
// 在收到短信/注册成功时从协议层学习本机号码后调用。
// 若尚无订阅行，会自动补建最小订阅记录。
func UpdateSIMCardPhoneNumberByIMSI(imsi, phone string) error {
	return UpdateSIMCardVoWiFiPhoneNumberByIMSI(imsi, phone)
}

func UpdateSIMCardModemPhoneNumberByIMSI(imsi, phone string) error {
	return updateSIMCardPhoneNumberByIMSI(imsi, phone, "modem")
}

func UpdateSIMCardVoWiFiPhoneNumberByIMSI(imsi, phone string) error {
	return updateSIMCardPhoneNumberByIMSI(imsi, phone, "vowifi")
}

func updateSIMCardPhoneNumberByIMSI(imsi, phone, source string) error {
	imsi = strings.TrimSpace(imsi)
	if imsi == "" || DB == nil {
		return nil
	}

	normalized := normalizeSIMPhoneNumber(phone)
	if normalized == "" {
		return nil
	}
	if phoneDigitsEqualIMSI(normalized, imsi) {
		return nil
	}

	now := time.Now()
	column := "modem_phone_number"
	if source == "vowifi" {
		column = "vowifi_phone_number"
	}
	finalPhone := normalized
	if source == "modem" {
		var latest SIMSubscription
		err := DB.Select("vowifi_phone_number").
			Where("imsi = ?", imsi).
			Limit(1).
			First(&latest).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if higherPriority := normalizeSIMPhoneNumber(latest.VowifiPhoneNumber); higherPriority != "" {
			finalPhone = higherPriority
		}
	}

	updates := map[string]interface{}{
		column:         normalized,
		"phone_number": finalPhone,
		"last_seen":    now,
		"updated_at":   now,
	}

	row := SIMSubscription{
		IMSI:        imsi,
		PhoneNumber: finalPhone,
		LastSeen:    now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if source == "modem" {
		row.ModemPhoneNumber = normalized
	} else {
		row.VowifiPhoneNumber = normalized
	}
	return DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "imsi"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(&row).Error
}

// updatePendingPhoneByICCID 与 updateSIMCardPhoneNumberByIMSI 同构，但按 ICCID 暂存。
// 优先级同样 vowifi > modem。
func updatePendingPhoneByICCID(iccid, phone, source string) error {
	iccid = strings.TrimSpace(iccid)
	if iccid == "" || DB == nil {
		return nil
	}
	normalized := normalizeSIMPhoneNumber(phone)
	if normalized == "" {
		return nil
	}
	now := time.Now()
	column := "modem_phone_number"
	if source == "vowifi" {
		column = "vowifi_phone_number"
	}
	finalPhone := normalized
	if source == "modem" {
		var latest PendingPhoneNumber
		err := DB.Select("vowifi_phone_number").
			Where("iccid = ?", iccid).Limit(1).First(&latest).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if hp := normalizeSIMPhoneNumber(latest.VowifiPhoneNumber); hp != "" {
			finalPhone = hp
		}
	}
	updates := map[string]interface{}{
		column:         normalized,
		"phone_number": finalPhone,
		"updated_at":   now,
	}
	row := PendingPhoneNumber{ICCID: iccid, PhoneNumber: finalPhone, CreatedAt: now, UpdatedAt: now}
	if source == "modem" {
		row.ModemPhoneNumber = normalized
	} else {
		row.VowifiPhoneNumber = normalized
	}
	return DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "iccid"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(&row).Error
}

// RecordModemPhoneNumber 路由：IMSI 已知写 sim_subscriptions，否则按 ICCID 暂存。
func RecordModemPhoneNumber(imsi, iccid, phone string) error {
	if strings.TrimSpace(imsi) != "" {
		return UpdateSIMCardModemPhoneNumberByIMSI(imsi, phone)
	}
	return updatePendingPhoneByICCID(iccid, phone, "modem")
}

// RecordVoWiFiPhoneNumber 路由：IMSI 已知写 sim_subscriptions，否则按 ICCID 暂存。
func RecordVoWiFiPhoneNumber(imsi, iccid, phone string) error {
	if strings.TrimSpace(imsi) != "" {
		return UpdateSIMCardVoWiFiPhoneNumberByIMSI(imsi, phone)
	}
	return updatePendingPhoneByICCID(iccid, phone, "vowifi")
}

// migratePendingPhoneToSubscription 在 IMSI 到位后，把 ICCID 暂存的号码迁移进 sim_subscriptions，
// 复用 updateSIMCardPhoneNumberByIMSI（自带 IMSI 等值守卫），随后删除 staging 行。
func migratePendingPhoneToSubscription(imsi, iccid string) error {
	imsi = strings.TrimSpace(imsi)
	iccid = strings.TrimSpace(iccid)
	if imsi == "" || iccid == "" || DB == nil {
		return nil
	}
	var pending PendingPhoneNumber
	err := DB.Where("iccid = ?", iccid).Limit(1).First(&pending).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if m := normalizeSIMPhoneNumber(pending.ModemPhoneNumber); m != "" {
		if err := updateSIMCardPhoneNumberByIMSI(imsi, m, "modem"); err != nil {
			return err
		}
	}
	if v := normalizeSIMPhoneNumber(pending.VowifiPhoneNumber); v != "" {
		if err := updateSIMCardPhoneNumberByIMSI(imsi, v, "vowifi"); err != nil {
			return err
		}
	}
	return DB.Where("iccid = ?", iccid).Delete(&PendingPhoneNumber{}).Error
}

// GetAllSIMCards 获取所有 SIM 卡
func GetAllSIMCards() ([]SIMCard, error) {
	var sims []SIMCard
	err := DB.Find(&sims).Error
	return sims, err
}

// GetSIMCardPhoneNumberByIMSI 获取 IMSI 对应的最近手机号（无则返回空字符串）。
func GetSIMCardPhoneNumberByIMSI(imsi string) (string, error) {
	imsi = strings.TrimSpace(imsi)
	if DB == nil || imsi == "" {
		return "", nil
	}

	var sub SIMSubscription
	err := DB.Select("phone_number").
		Where("imsi = ? AND COALESCE(phone_number, '') <> ''", imsi).
		Limit(1).
		First(&sub).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(sub.PhoneNumber), nil
}

// GetPhoneNumberByIMSIOrICCID 先按 IMSI 查 sim_subscriptions，空则按 ICCID 查 staging。
func GetPhoneNumberByIMSIOrICCID(imsi, iccid string) (string, error) {
	if phone, err := GetSIMCardPhoneNumberByIMSI(imsi); err != nil {
		return "", err
	} else if strings.TrimSpace(phone) != "" {
		return strings.TrimSpace(phone), nil
	}
	iccid = strings.TrimSpace(iccid)
	if DB == nil || iccid == "" {
		return "", nil
	}
	var pending PendingPhoneNumber
	err := DB.Select("phone_number").
		Where("iccid = ? AND COALESCE(phone_number, '') <> ''", iccid).
		Limit(1).First(&pending).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(pending.PhoneNumber), nil
}

func GetSIMPhoneNumbersByIMSI() (map[string]string, error) {
	out := map[string]string{}
	if DB == nil {
		return out, nil
	}
	var rows []SIMSubscription
	if err := DB.Select("imsi", "phone_number").
		Where("COALESCE(imsi, '') <> '' AND COALESCE(phone_number, '') <> ''").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		imsi := strings.TrimSpace(row.IMSI)
		phone := strings.TrimSpace(row.PhoneNumber)
		if imsi != "" && phone != "" {
			out[imsi] = phone
		}
	}
	return out, nil
}

// GetICCIDForIMSI 从 sim_cards 查 IMSI 对应的真实 ICCID；
// 无映射或为 reader-imsi- 合成 ICCID 时返回 "imsi:" 前缀合成键，与 P4 回填逻辑对齐。
func GetICCIDForIMSI(imsi string) string {
	imsi = strings.TrimSpace(imsi)
	if imsi == "" {
		return ""
	}
	if DB == nil {
		return "imsi:" + imsi
	}
	type row struct {
		ICCID string `gorm:"column:iccid"`
	}
	var r row
	err := DB.Table("sim_cards").Select("iccid").Where("imsi = ?", imsi).First(&r).Error
	if err != nil || strings.TrimSpace(r.ICCID) == "" || strings.HasPrefix(r.ICCID, "reader-imsi-") {
		return "imsi:" + imsi
	}
	return strings.TrimSpace(r.ICCID)
}
