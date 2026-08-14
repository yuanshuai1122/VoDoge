// 短信与会话的读写。
//
// 会话表（sms_contacts）是 sms 表的派生视图，每次写入短信时同步维护——
// 会话列表要按最后一条消息排序并显示未读数，实时聚合在有量之后会很慢。
// 派生意味着它可能与 sms 表不一致，RebuildSMSContact 负责重建。
//
// 查询与投递回执分别在 sms_query.go 与 sms_delivery.go。
package db

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrSMSNotFound = errors.New("sms not found")

// SaveSMS 保存短信记录
// 注意：时间戳会被截断到秒精度，确保发送（time.Now() 有纳秒）和接收（SCTS 仅有秒）
// 的消息在同一秒内能通过 id 正确排序
func SaveSMS(imsi, sender, recipient, content string, smsType, status int, timestamp time.Time) error {
	return SaveSMSWithLocalPhone(imsi, "", sender, recipient, content, smsType, status, timestamp)
}

// SaveSMSWithLocalPhone 保存短信记录并显式写入本机号码。
// localPhone 为空时会按方向自动推导，并在必要时回退到订阅手机号。
func SaveSMSWithLocalPhone(imsi, localPhone, sender, recipient, content string, smsType, status int, timestamp time.Time) error {
	if DB == nil {
		return nil
	}
	imsi = strings.TrimSpace(imsi)
	sender = strings.TrimSpace(sender)
	recipient = strings.TrimSpace(recipient)

	peer := normalizeSMSPeer(smsType, sender, recipient)
	localPhone = normalizeSMSLocalPhone(imsi, smsType, localPhone, sender, recipient)
	// 运行时即解析 ICCID（与 P4 回填同一约定：无真实映射回退 "imsi:" 前缀），
	// 否则新短信 iccid 为空，按 ICCID 维度的查询/删除会全部落空。
	sms := SMS{
		IMSI:       imsi,
		ICCID:      GetICCIDForIMSI(imsi),
		Peer:       peer,
		LocalPhone: localPhone,
		Sender:     sender,
		Recipient:  recipient,
		Content:    content,
		Type:       smsType,
		Status:     status,
		Timestamp:  timestamp.Truncate(time.Second),
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&sms).Error; err != nil {
			return err
		}
		if peer == "" {
			return nil
		}
		return upsertSMSContactFromSMS(tx, &sms)
	})
}

// HasDuplicateReceivedSMS 检查在指定时间窗口内是否已存在内容相同的下行接收短信。
func HasDuplicateReceivedSMS(imsi, localPhone, sender, recipient, content string, timestamp time.Time, window time.Duration) (bool, error) {
	if DB == nil {
		return false, nil
	}
	imsi = strings.TrimSpace(imsi)
	sender = strings.TrimSpace(sender)
	recipient = strings.TrimSpace(recipient)
	content = strings.TrimSpace(content)
	if imsi == "" || sender == "" || content == "" {
		return false, nil
	}
	if window <= 0 {
		window = 5 * time.Minute
	}
	ts := timestamp.Truncate(time.Second)
	start := ts.Add(-window)
	end := ts.Add(window)

	var count int64
	err := DB.Model(&SMS{}).
		Where("imsi = ? AND type = ? AND sender = ? AND recipient = ? AND content = ? AND timestamp BETWEEN ? AND ?",
			imsi, 1, sender, strings.TrimSpace(recipient), content, start, end).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func normalizeSMSPeer(smsType int, sender, recipient string) string {
	if smsType == 2 {
		p := strings.TrimSpace(recipient)
		if p != "" {
			return p
		}
	}
	return strings.TrimSpace(sender)
}

func normalizeSMSLocalPhone(imsi string, smsType int, localPhone, sender, recipient string) string {
	trimPhone := canonicalLocalPhone(localPhone)
	if looksLikePhoneNumber(trimPhone) {
		return trimPhone
	}

	var candidate string
	switch smsType {
	case 1:
		candidate = canonicalLocalPhone(recipient)
	case 2:
		candidate = canonicalLocalPhone(sender)
	}
	if looksLikePhoneNumber(candidate) {
		return candidate
	}

	if imsi != "" {
		if learned, err := GetSIMCardPhoneNumberByIMSI(imsi); err == nil {
			learned = canonicalLocalPhone(learned)
			if looksLikePhoneNumber(learned) {
				return learned
			}
		}
	}

	return strings.TrimSpace(trimPhone)
}

func upsertSMSContactFromSMS(tx *gorm.DB, sms *SMS) error {
	contact := SMSContact{
		IMSI:          sms.IMSI,
		ICCID:         sms.ICCID,
		Peer:          sms.Peer,
		LastSMSID:     sms.ID,
		LastTimestamp: sms.Timestamp,
		LastContent:   sms.Content,
		LastType:      sms.Type,
		UnreadCount:   0,
	}
	isIncomingUnread := sms.Type == 1 && sms.Status == 0
	if isIncomingUnread {
		contact.UnreadCount = 1
	}

	doUpdates := clause.AssignmentColumns([]string{"iccid", "last_sms_id", "last_timestamp", "last_content", "last_type", "updated_at"})
	onConflict := clause.OnConflict{
		Columns:   []clause.Column{{Name: "imsi"}, {Name: "peer"}},
		DoUpdates: doUpdates,
	}

	if isIncomingUnread {
		onConflict.DoUpdates = clause.Assignments(map[string]any{
			"iccid":          sms.ICCID,
			"last_sms_id":    sms.ID,
			"last_timestamp": sms.Timestamp,
			"last_content":   sms.Content,
			"last_type":      sms.Type,
			// 必须限定表名：PostgreSQL 的 ON CONFLICT DO UPDATE 中，
			// 裸 unread_count 会与 EXCLUDED 的同名列产生歧义（SQLSTATE 42702）。
			"unread_count": gorm.Expr("sms_contacts.unread_count + 1"),
			"updated_at":   time.Now(),
		})
	}

	return tx.Clauses(onConflict).Create(&contact).Error
}

func BackfillSMSPeerAndContacts(batchSize int) error {
	if batchSize <= 0 {
		batchSize = 500
	}

	need, err := NeedBackfillSMSContacts()
	if err != nil {
		return err
	}
	if !need {
		return nil
	}

	if err := DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&SMSContact{}).Error; err != nil {
		return err
	}

	lastTs := time.Time{}
	var lastID uint = 0

	for {
		var batch []SMS
		query := DB.Order("timestamp asc, id asc").Limit(batchSize)
		if !lastTs.IsZero() || lastID != 0 {
			query = query.Where("timestamp > ? OR (timestamp = ? AND id > ?)", lastTs, lastTs, lastID)
		}
		if err := query.Find(&batch).Error; err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}

		if err := DB.Transaction(func(tx *gorm.DB) error {
			for i := range batch {
				sms := &batch[i]
				if strings.TrimSpace(sms.Peer) == "" {
					sms.Peer = normalizeSMSPeer(sms.Type, sms.Sender, sms.Recipient)
					if sms.Peer != "" {
						if err := tx.Model(&SMS{}).Where("id = ?", sms.ID).Update("peer", sms.Peer).Error; err != nil {
							return err
						}
					}
				}
				if sms.Peer != "" {
					if err := upsertSMSContactFromSMS(tx, sms); err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
			return err
		}

		last := batch[len(batch)-1]
		lastTs = last.Timestamp
		lastID = last.ID
	}
}

func NeedBackfillSMSContacts() (bool, error) {
	var smsCount int64
	if err := DB.Model(&SMS{}).Count(&smsCount).Error; err != nil {
		return false, err
	}
	if smsCount == 0 {
		return false, nil
	}

	var contactCount int64
	if err := DB.Model(&SMSContact{}).Count(&contactCount).Error; err != nil {
		return false, err
	}

	var missingPeer int64
	if err := DB.Model(&SMS{}).Where("peer = '' OR peer IS NULL").Count(&missingPeer).Error; err != nil {
		return false, err
	}

	return contactCount == 0 || missingPeer > 0, nil
}

// GetSMSByIMSI 获取指定 IMSI 的短信列表
func GetSMSByIMSI(imsi string, limit int) ([]SMS, error) {
	var smsList []SMS
	err := DB.Where("imsi = ?", imsi).Order("timestamp desc").Limit(limit).Find(&smsList).Error
	return smsList, err
}

// GetSMSByICCID 获取指定 ICCID 的短信列表（P4 ICCID 维度读取）。
func GetSMSByICCID(iccid string, limit int) ([]SMS, error) {
	var smsList []SMS
	err := DB.Where("iccid = ?", iccid).Order("timestamp desc").Limit(limit).Find(&smsList).Error
	return smsList, err
}

// GetRecentSMS 获取所有 SIM 卡的最近短信列表
func GetRecentSMS(limit int) ([]SMS, error) {
	var smsList []SMS
	err := DB.Order("timestamp desc").Limit(limit).Find(&smsList).Error
	return smsList, err
}

func rebuildSMSContactTx(tx *gorm.DB, imsi, peer string) (bool, error) {
	imsi = strings.TrimSpace(imsi)
	peer = strings.TrimSpace(peer)
	if imsi == "" || peer == "" {
		return true, nil
	}

	var latest SMS
	err := tx.Where("imsi = ? AND peer = ?", imsi, peer).
		Order("timestamp desc, id desc").
		First(&latest).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Where("imsi = ? AND peer = ?", imsi, peer).Delete(&SMSContact{}).Error; err != nil {
				return true, err
			}
			return true, nil
		}
		return false, err
	}

	var unreadCount int64
	if err := tx.Model(&SMS{}).
		Where("imsi = ? AND peer = ? AND type = ? AND status = ?", imsi, peer, 1, 0).
		Count(&unreadCount).Error; err != nil {
		return false, err
	}

	now := time.Now()
	contact := SMSContact{
		IMSI:          imsi,
		ICCID:         latest.ICCID,
		Peer:          peer,
		LastSMSID:     latest.ID,
		LastTimestamp: latest.Timestamp,
		LastContent:   latest.Content,
		LastType:      latest.Type,
		UnreadCount:   int(unreadCount),
		UpdatedAt:     now,
	}
	return false, tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "imsi"}, {Name: "peer"}},
		DoUpdates: clause.Assignments(map[string]any{
			"iccid":          contact.ICCID,
			"last_sms_id":    contact.LastSMSID,
			"last_timestamp": contact.LastTimestamp,
			"last_content":   contact.LastContent,
			"last_type":      contact.LastType,
			"unread_count":   contact.UnreadCount,
			"updated_at":     now,
		}),
	}).Create(&contact).Error
}

func RebuildSMSContact(imsi, peer string) (bool, error) {
	if DB == nil {
		return true, nil
	}
	var threadEmpty bool
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		threadEmpty, err = rebuildSMSContactTx(tx, imsi, peer)
		return err
	})
	return threadEmpty, err
}

func DeleteSMSByID(id uint) (bool, string, string, error) {
	if DB == nil {
		return true, "", "", ErrSMSNotFound
	}

	var (
		threadEmpty bool
		imsi        string
		peer        string
	)
	err := DB.Transaction(func(tx *gorm.DB) error {
		var sms SMS
		if err := tx.Where("id = ?", id).First(&sms).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSMSNotFound
			}
			return err
		}
		imsi = strings.TrimSpace(sms.IMSI)
		peer = strings.TrimSpace(sms.Peer)
		if err := tx.Delete(&SMS{}, id).Error; err != nil {
			return err
		}
		var err error
		threadEmpty, err = rebuildSMSContactTx(tx, imsi, peer)
		return err
	})
	return threadEmpty, imsi, peer, err
}

func DeleteSMSByIMSIAndPeer(imsi, peer string) (int64, error) {
	if DB == nil {
		return 0, ErrSMSNotFound
	}
	imsi = strings.TrimSpace(imsi)
	peer = strings.TrimSpace(peer)
	if imsi == "" || peer == "" {
		return 0, ErrSMSNotFound
	}

	var deleted int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Where("imsi = ? AND peer = ?", imsi, peer).Delete(&SMS{})
		if res.Error != nil {
			return res.Error
		}
		deleted = res.RowsAffected
		if deleted == 0 {
			return ErrSMSNotFound
		}
		return tx.Where("imsi = ? AND peer = ?", imsi, peer).Delete(&SMSContact{}).Error
	})
	return deleted, err
}

// DeleteSMSByICCIDAndPeer 按 ICCID 删除会话（P4 ICCID 维度写入）。
func DeleteSMSByICCIDAndPeer(iccid, peer string) (int64, error) {
	if DB == nil {
		return 0, ErrSMSNotFound
	}
	iccid = strings.TrimSpace(iccid)
	peer = strings.TrimSpace(peer)
	if iccid == "" || peer == "" {
		return 0, ErrSMSNotFound
	}

	var deleted int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Where("iccid = ? AND peer = ?", iccid, peer).Delete(&SMS{})
		if res.Error != nil {
			return res.Error
		}
		deleted = res.RowsAffected
		if deleted == 0 {
			return ErrSMSNotFound
		}
		return tx.Where("iccid = ? AND peer = ?", iccid, peer).Delete(&SMSContact{}).Error
	})
	return deleted, err
}
