// 启动期迁移。
//
// AutoMigrate 建表之后，还有几步数据迁移必须按顺序跑：sim_cards 拆出
// sim_subscriptions、删掉搬走的号码列、ICCID 重键。它们都要幂等——
// 每次启动都会执行一遍。
package db

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func runMigrations(tx *gorm.DB) error {
	if err := tx.AutoMigrate(
		&Device{},
		&CardPolicy{},
		&SIMCard{},
		&SIMSubscription{},
		&PendingPhoneNumber{},
		&ProxyInstance{},
		&UpstreamProxy{},
		&UpstreamProxyCountryRule{},
		&SMS{},
		&SMSContact{},
		&SMSDelivery{},
		&SMSDeliveryPart{},
		&TrafficMinute{},
		&TrafficHour{},
		&TrafficDay{},
		&TrafficWeek{},
		&TrafficMonth{},
	); err != nil {
		return fmt.Errorf("automigrate: %w", err)
	}
	if err := migrateSIMCardsToSubscriptions(tx); err != nil {
		return fmt.Errorf("migrate sim subscriptions: %w", err)
	}
	if err := migrateSIMCardIdentityColumnsOnly(tx); err != nil {
		return fmt.Errorf("migrate sim identity columns: %w", err)
	}
	if err := RunICCIDReKeyMigration(tx); err != nil {
		return fmt.Errorf("iccid rekey migration: %w", err)
	}
	return nil
}

func migrateSIMCardsToSubscriptions(tx *gorm.DB) error {
	if tx == nil || !tx.Migrator().HasTable(&SIMCard{}) {
		return nil
	}
	type legacySIMCardRow struct {
		ICCID             string    `gorm:"column:iccid"`
		IMSI              string    `gorm:"column:imsi"`
		PhoneNumber       string    `gorm:"column:phone_number"`
		ModemPhoneNumber  string    `gorm:"column:modem_phone_number"`
		VowifiPhoneNumber string    `gorm:"column:vowifi_phone_number"`
		Operator          string    `gorm:"column:operator"`
		LastSeen          time.Time `gorm:"column:last_seen"`
	}
	var rows []legacySIMCardRow
	if err := tx.Table("sim_cards").Find(&rows).Error; err != nil {
		return err
	}
	realICCIDByIMSI := map[string]string{}
	for _, row := range rows {
		imsi := strings.TrimSpace(row.IMSI)
		iccid := strings.TrimSpace(row.ICCID)
		if imsi != "" && iccid != "" && !strings.HasPrefix(iccid, "reader-imsi-") {
			realICCIDByIMSI[imsi] = iccid
		}
	}
	subByIMSI := map[string]SIMSubscription{}
	now := time.Now()
	for _, row := range rows {
		imsi := strings.TrimSpace(row.IMSI)
		if imsi == "" {
			continue
		}
		rowICCID := strings.TrimSpace(row.ICCID)
		currentICCID := realICCIDByIMSI[imsi]
		if currentICCID == "" && !strings.HasPrefix(rowICCID, "reader-imsi-") {
			currentICCID = rowICCID
		}
		phone := normalizeSIMPhoneNumber(row.PhoneNumber)
		modemPhone := normalizeSIMPhoneNumber(row.ModemPhoneNumber)
		vowifiPhone := normalizeSIMPhoneNumber(row.VowifiPhoneNumber)
		if phone == "" {
			if vowifiPhone != "" {
				phone = vowifiPhone
			} else {
				phone = modemPhone
			}
		}
		if currentICCID == "" && phone == "" && modemPhone == "" && vowifiPhone == "" {
			continue
		}
		lastSeen := row.LastSeen
		if lastSeen.IsZero() {
			lastSeen = now
		}
		sub := subByIMSI[imsi]
		if sub.IMSI == "" {
			sub = SIMSubscription{
				IMSI:      imsi,
				CreatedAt: now,
				UpdatedAt: now,
			}
		}
		if currentICCID != "" {
			sub.CurrentICCID = currentICCID
		}
		if phone != "" {
			sub.PhoneNumber = phone
		}
		if modemPhone != "" {
			sub.ModemPhoneNumber = modemPhone
		}
		if vowifiPhone != "" {
			sub.VowifiPhoneNumber = vowifiPhone
		}
		if operator := strings.TrimSpace(row.Operator); operator != "" {
			sub.Operator = operator
		}
		if sub.LastSeen.IsZero() || lastSeen.After(sub.LastSeen) {
			sub.LastSeen = lastSeen
		}
		subByIMSI[imsi] = sub
	}
	for _, sub := range subByIMSI {
		var existing SIMSubscription
		err := tx.Where("imsi = ?", sub.IMSI).First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			if strings.TrimSpace(existing.PhoneNumber) != "" {
				sub.PhoneNumber = existing.PhoneNumber
			}
			if strings.TrimSpace(existing.ModemPhoneNumber) != "" {
				sub.ModemPhoneNumber = existing.ModemPhoneNumber
			}
			if strings.TrimSpace(existing.VowifiPhoneNumber) != "" {
				sub.VowifiPhoneNumber = existing.VowifiPhoneNumber
			}
			if strings.TrimSpace(existing.Operator) != "" {
				sub.Operator = existing.Operator
			}
			if strings.TrimSpace(existing.CurrentICCID) != "" {
				sub.CurrentICCID = existing.CurrentICCID
			}
			if sub.LastSeen.IsZero() || existing.LastSeen.After(sub.LastSeen) {
				sub.LastSeen = existing.LastSeen
			}
			if !existing.CreatedAt.IsZero() {
				sub.CreatedAt = existing.CreatedAt
			}
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "imsi"}},
			DoUpdates: clause.Assignments(map[string]any{
				"current_iccid":       sub.CurrentICCID,
				"phone_number":        sub.PhoneNumber,
				"modem_phone_number":  sub.ModemPhoneNumber,
				"vowifi_phone_number": sub.VowifiPhoneNumber,
				"operator":            sub.Operator,
				"last_seen":           sub.LastSeen,
				"updated_at":          sub.UpdatedAt,
			}),
		}).Create(&sub).Error; err != nil {
			return err
		}
	}
	return tx.Where("iccid LIKE ?", "reader-imsi-%").Delete(&SIMCard{}).Error
}

func hasTableColumn(tx *gorm.DB, table string, column string) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("nil db")
	}
	// Prefer GORM Migrator (works on PostgreSQL).
	if tx.Migrator().HasTable(table) {
		return tx.Migrator().HasColumn(table, column), nil
	}
	return false, nil
}

func migrateSIMCardIdentityColumnsOnly(tx *gorm.DB) error {
	if tx == nil || !tx.Migrator().HasTable(&SIMCard{}) {
		return nil
	}
	for _, column := range []string{"phone_number", "modem_phone_number", "vowifi_phone_number"} {
		exists, err := hasTableColumn(tx, "sim_cards", column)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		// Columns may no longer exist on the Go model; drop by name (PostgreSQL).
		if err := tx.Exec(`ALTER TABLE sim_cards DROP COLUMN IF EXISTS ` + column).Error; err != nil {
			return fmt.Errorf("drop column %s: %w", column, err)
		}
	}
	return nil
}
