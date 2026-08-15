// 短信：收发、会话、联系人。
//
// 发送有两条通路（AT 与 VoWiFi），由设备当前状态决定走哪条；
// 存储与查询统一按 IMSI/ICCID 维度。
package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/boa-z/vowifi-go/runtimehost/messaging"
	"github.com/yuanshuai1122/vodog/internal/config"
	"github.com/yuanshuai1122/vodog/internal/db"
	"github.com/yuanshuai1122/vodog/internal/device"
	"github.com/yuanshuai1122/vodog/pkg/smscodec"

	"github.com/yuanshuai1122/vodog/pkg/logger"

	"github.com/gin-gonic/gin"
)

type SMSWithDevice struct {
	db.SMS
	DeviceName string `json:"device_name"`
}

type smsInboxIMSIReader interface {
	GetCachedIMSI() string
	GetIMSI() string
}

func smsInboxIMSI(source smsInboxIMSIReader, liveRefresh bool) string {
	if source == nil {
		return ""
	}
	imsi := strings.TrimSpace(source.GetCachedIMSI())
	if imsi != "" || !liveRefresh {
		return imsi
	}
	return strings.TrimSpace(source.GetIMSI())
}

func (s *Server) handleSendSMS(c *gin.Context) {
	type SendSMSRequest struct {
		DeviceID string `json:"device_id"`
		IMSI     string `json:"imsi"`
		Phone    string `json:"phone" binding:"required"`
		Message  string `json:"message" binding:"required"`
		Encoding string `json:"encoding"`
	}

	var req SendSMSRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "参数错误: "+err.Error())
		return
	}

	deviceID := strings.TrimSpace(req.DeviceID)
	imsi := strings.TrimSpace(req.IMSI)
	encoding, err := smscodec.NormalizeSMSEncoding(req.Encoding)
	if err != nil {
		fail(c, http.StatusBadRequest, "", "短信编码参数错误: "+err.Error())
		return
	}
	sendOpts := smscodec.SubmitOptions{Encoding: encoding}

	var worker *device.Worker
	if deviceID != "" {
		worker = s.pool.GetWorker(deviceID)
	} else if imsi != "" {
		for _, w := range s.pool.GetAllWorkers() {
			if w != nil && w.GetIMSI() == imsi {
				worker = w
				deviceID = w.ID
				break
			}
		}
	} else {
		workers := s.pool.GetAllWorkers()
		if len(workers) == 1 {
			worker = workers[0]
			if worker != nil {
				deviceID = worker.ID
			}
		}
	}

	if worker == nil {
		msg := "存在多个设备时必须指定 device_id 或 imsi"
		if deviceID != "" {
			msg = "设备未找到: " + deviceID
		} else if imsi != "" {
			msg = "未找到匹配 IMSI 的设备: " + imsi
		}
		fail(c, http.StatusNotFound, "", msg)
		return
	}

	if !s.reserveSMSSend(c, deviceID, req.Phone) {
		return
	}

	// 获取 IMSI 用于入库
	imsi = worker.GetIMSI()
	messageID := ""
	partsTotal := 1
	deliveryState := "acked"
	usedTransport := ""

	lane := ""
	if worker != nil {
		lane = worker.Config.Lane
	}
	vowifiActive := s.pool.IsVoWiFiActive(deviceID)
	plan := planSMSSend(lane, vowifiActive, radioRegistered(worker.GetCachedDeviceStatus().RegStatus))
	logger.Debug("短信通道计划",
		"device", deviceID,
		"lane", lane,
		"primary", plan.Primary,
		"fallback", plan.Fallback,
		"reason", plan.Reason)

	sendIMS := func() error {
		outcome, err := s.pool.SendVoWiFiSMSWithOptions(c.Request.Context(), deviceID, req.Phone, req.Message, sendOpts)
		if outcome.PartsTotal > 0 {
			partsTotal = outcome.PartsTotal
		}
		if strings.TrimSpace(outcome.DeliveryState) != "" {
			deliveryState = strings.TrimSpace(outcome.DeliveryState)
		}
		messageID = strings.TrimSpace(outcome.MessageID)
		return err
	}
	sendCellular := func() error {
		err := worker.SendSMSWithOptions(req.Phone, req.Message, sendOpts)
		if refs := worker.TakeCellularSubmitRefs(); len(refs) > 0 {
			partsTotal = len(refs)
			if id := worker.RecordCellularSubmit(req.Phone, req.Message, refs); id != "" {
				messageID = id
				deliveryState = "pending"
			}
		}
		return err
	}

	try := func(transport string) error {
		switch transport {
		case smsTransportIMS:
			return sendIMS()
		case smsTransportCellular:
			return sendCellular()
		default:
			return fmt.Errorf("未知短信通道: %s", transport)
		}
	}

	err = try(plan.Primary)
	usedTransport = plan.Primary
	if err != nil && plan.Fallback != "" {
		if fallbackErr := try(plan.Fallback); fallbackErr == nil {
			err = nil
			usedTransport = plan.Fallback
		} else {
			err = fmt.Errorf("%s 失败: %v；%s 也失败: %v", plan.Primary, err, plan.Fallback, fallbackErr)
		}
	}

	if err != nil {
		if usedTransport == smsTransportIMS || plan.Primary == smsTransportIMS {
			_ = device.RecordVoWiFiSMSSendFailure(s.pool, deviceID, req.Phone, req.Message, time.Now())
		}
		if imsi != "" && usedTransport != smsTransportIMS {
			_ = s.data().SMS.Save(imsi, "", worker.ID, req.Phone, req.Message, 2, 3, time.Now())
		}
		failWith(c, http.StatusInternalServerError, "", "发送失败: "+err.Error(), gin.H{
			"device":         deviceID,
			"phone":          req.Phone,
			"transport":      usedTransport,
			"message_id":     messageID,
			"parts_total":    partsTotal,
			"delivery_state": deliveryState,
		})
		return
	}

	if usedTransport == smsTransportCellular && imsi != "" {
		_ = s.data().SMS.Save(imsi, "", worker.ID, req.Phone, req.Message, 2, 2, time.Now())
	}

	outcome := "accepted_unconfirmed"
	if usedTransport == smsTransportIMS && deliveryState == "acked" {
		outcome = "delivered"
	}
	if usedTransport == smsTransportCellular && messageID == "" {
		outcome = "accepted_unconfirmed"
	}
	respondOKWith(c, gin.H{
		"device":         deviceID,
		"phone":          req.Phone,
		"message_id":     messageID,
		"parts_total":    partsTotal,
		"delivery_state": deliveryState,
		"transport":      usedTransport,
		"outcome":        outcome,
		"retry_safe":     true,
	}, gin.H{
		"message": "短信发送成功",
	})
}

func (s *Server) handleSMSDelivery(c *gin.Context) {
	messageID := strings.TrimSpace(c.Param("message_id"))
	if messageID == "" {
		fail(c, http.StatusBadRequest, "", "message_id 不能为空")
		return
	}
	if s.pool == nil {
		fail(c, http.StatusServiceUnavailable, "", "服务未就绪")
		return
	}
	services := s.pool.GetAllVoWiFiApps()
	for _, svc := range services {
		if svc == nil {
			continue
		}
		status, err := svc.GetSMSDeliveryStatus(messageID)
		if err != nil {
			continue
		}
		respondOK(c, status)
		return
	}
	fail(c, http.StatusNotFound, "", "未找到对应短信投递记录")
}

func (s *Server) handleVoWiFiSMSStatus(c *gin.Context) {
	if s.pool == nil {
		respondOK(c, gin.H{
			"enabled": false,
			"status":  "no_pool",
		})
		return
	}
	svc := s.pool.GetVoWiFiApp()
	if svc == nil {
		respondOK(c, gin.H{
			"enabled": false,
			"status":  "not_running",
		})
		return
	}
	respondOK(c, svc.Status())
}

func (s *Server) handleVoWiFiSendSMS(c *gin.Context) {
	type SendSMSRequest struct {
		To       string `json:"to" binding:"required"`
		Text     string `json:"text" binding:"required"`
		Encoding string `json:"encoding"`
	}

	var req SendSMSRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "参数错误: "+err.Error())
		return
	}

	if s.pool == nil {
		fail(c, http.StatusServiceUnavailable, "", "服务未就绪")
		return
	}
	encoding, err := smscodec.NormalizeSMSEncoding(req.Encoding)
	if err != nil {
		fail(c, http.StatusBadRequest, "", "短信编码参数错误: "+err.Error())
		return
	}
	svc := s.pool.GetVoWiFiApp()
	if svc == nil {
		fail(c, http.StatusServiceUnavailable, "", "IMS Core 未启动")
		return
	}

	outcome, err := svc.SendSMSWithOptions(c.Request.Context(), req.To, req.Text, messaging.SendOptions{Encoding: string(encoding)})
	if err != nil {
		failWith(c, http.StatusInternalServerError, "", "发送失败: "+err.Error(), gin.H{
			"message_id":     strings.TrimSpace(outcome.MessageID),
			"parts_total":    outcome.PartsTotal,
			"delivery_state": strings.TrimSpace(outcome.DeliveryState),
		})
		return
	}

	respondOKWith(c, gin.H{
		"message_id":     strings.TrimSpace(outcome.MessageID),
		"parts_total":    outcome.PartsTotal,
		"delivery_state": strings.TrimSpace(outcome.DeliveryState),
	}, gin.H{
		"message": "IMS 短信发送成功",
	})
}

func (s *Server) handleGetSMSInbox(c *gin.Context) {
	deviceID := c.Query("device_id")
	limitStr := c.DefaultQuery("limit", "20")
	var limit int
	fmt.Sscanf(limitStr, "%d", &limit)
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// 如果未指定设备 ID 且只有一个设备，默认使用该设备
	// 如果未指定设备 ID，则返回全局最近短信
	if deviceID == "" || deviceID == "all" {
		smsList, err := s.data().SMS.Recent(limit)
		if err != nil {
			fail(c, http.StatusInternalServerError, "", "查询数据库失败: "+err.Error())
			return
		}

		cfgByID := map[string]config.DeviceConfig{}
		{
			managed := config.ListDevices()
			for _, d := range managed {
				cfgByID[d.ID] = d
			}
		}

		iccidToName := map[string]string{}
		enrichedList := make([]SMSWithDevice, 0, len(smsList))
		for _, w := range s.pool.GetAllWorkers() {
			if w == nil || w.Modem == nil {
				continue
			}
			iccid := w.CurrentICCID()
			if strings.TrimSpace(iccid) == "" {
				continue
			}
			name := ""
			if v, ok := cfgByID[w.ID]; ok {
				name = v.Name
			} else {
				name = w.Config.Name
			}
			if name == "" {
				name = w.ID
			}
			iccidToName[iccid] = name
		}

		for _, sms := range smsList {
			devName := iccidToName[sms.ICCID]
			enrichedList = append(enrichedList, SMSWithDevice{
				SMS:        sms,
				DeviceName: devName,
			})
		}

		respondOK(c, enrichedList)
		return
	}

	worker := s.pool.GetWorker(deviceID)
	if worker == nil {
		fail(c, http.StatusNotFound, "", "设备未找到: "+deviceID)
		return
	}

	iccid := worker.CurrentICCID()
	logger.Debug("查询指定设备短信", "device_id", deviceID, "iccid", iccid)
	if iccid == "" {
		fail(c, http.StatusBadRequest, "", "该设备未识别到 SIM 卡 ICCID")
		return
	}

	smsList, err := s.data().SMS.ByICCID(iccid, limit)
	if err != nil {
		logger.Error("查询数据库短信失败", "err", err, "iccid", iccid)
		fail(c, http.StatusInternalServerError, "", "查询数据库失败: "+err.Error())
		return
	}

	enrichedList := make([]SMSWithDevice, 0, len(smsList))
	devName := worker.Config.Name
	if devName == "" {
		devName = worker.ID
	}

	for _, sms := range smsList {
		enrichedList = append(enrichedList, SMSWithDevice{
			SMS:        sms,
			DeviceName: devName,
		})
	}

	respondOK(c, enrichedList)
}

type SMSContactWithDevice struct {
	db.SMSContact
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	LocalPhone string `json:"local_phone"` // 本机号码（收件人手机号），来自订阅手机号
	Lane       string `json:"lane,omitempty"`
}

func (s *Server) resolveSMSIMSI(deviceID, imsi string) (string, int, string) {
	deviceID = strings.TrimSpace(deviceID)
	imsi = strings.TrimSpace(imsi)
	if deviceID == "" || deviceID == "all" {
		if imsi == "" {
			return "", http.StatusBadRequest, "缺少 imsi 参数（device_id=all 时必须指定）"
		}
		return imsi, 0, ""
	}

	worker := s.pool.GetWorker(deviceID)
	if worker == nil {
		return "", http.StatusNotFound, "设备未找到: " + deviceID
	}
	imsi = strings.TrimSpace(worker.GetCachedIMSI())
	if imsi == "" {
		return "", http.StatusBadRequest, "该设备未识别到 SIM 卡 IMSI"
	}
	return imsi, 0, ""
}

// resolveSMSICCID 将 device_id 或 imsi 查询参数解析为 ICCID，供 ICCID 维度的 SMS 查询使用。
// 对于 ?imsi= 路径，通过 sim_cards 映射转换为 ICCID（无映射时使用 "imsi:" 前缀合成键）。
func (s *Server) resolveSMSICCID(deviceID, imsi string) (string, int, string) {
	deviceID = strings.TrimSpace(deviceID)
	imsi = strings.TrimSpace(imsi)
	if deviceID == "" || deviceID == "all" {
		if imsi == "" {
			return "", http.StatusBadRequest, "缺少 imsi 参数（device_id=all 时必须指定）"
		}
		return s.data().SIM.ICCIDForIMSI(imsi), 0, ""
	}

	worker := s.pool.GetWorker(deviceID)
	if worker == nil {
		return "", http.StatusNotFound, "设备未找到: " + deviceID
	}
	iccid := worker.CurrentICCID()
	if iccid == "" {
		return "", http.StatusBadRequest, "该设备未识别到 SIM 卡 ICCID"
	}
	return iccid, 0, ""
}

func (s *Server) handleGetSMSContacts(c *gin.Context) {
	deviceID := c.Query("device_id")
	imsi := c.Query("imsi")
	lane := strings.TrimSpace(c.Query("lane"))
	if err := config.ValidateLane(lane); err != nil {
		fail(c, http.StatusBadRequest, "", "无效的 lane，允许 cn|intl 或空")
		return
	}
	lane = config.NormalizeLane(lane)

	limitStr := c.DefaultQuery("limit", "50")
	var limit int
	fmt.Sscanf(limitStr, "%d", &limit)

	var beforeTs *time.Time
	if v := strings.TrimSpace(c.Query("before_ts")); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			beforeTs = &t
		}
	}
	beforePeer := strings.TrimSpace(c.Query("before_peer"))

	var iccid string
	if strings.TrimSpace(deviceID) != "" {
		resolved, status, msg := s.resolveSMSICCID(deviceID, imsi)
		if status != 0 {
			fail(c, status, "", msg)
			return
		}
		iccid = resolved
	}

	var contacts []db.SMSContact
	var err error
	if iccid != "" {
		contacts, err = s.data().SMS.ContactsByICCID(iccid, limit, beforeTs, beforePeer)
	} else {
		contacts, err = s.data().SMS.Contacts(limit, beforeTs, beforePeer)
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "", "查询数据库失败: "+err.Error())
		return
	}

	iccidDevice := make(map[string]struct {
		id   string
		name string
	})
	cfgByID := map[string]config.DeviceConfig{}
	{
		managed := config.ListDevices()
		for _, d := range managed {
			cfgByID[d.ID] = d
		}
	}
	workers := s.pool.GetAllWorkers()
	for _, w := range workers {
		wICCID := w.CurrentICCID()
		if wICCID == "" {
			continue
		}
		name := ""
		if v, ok := cfgByID[w.ID]; ok {
			name = v.Name
		} else {
			name = w.Config.Name
		}
		if name == "" {
			name = w.ID
		}
		iccidDevice[wICCID] = struct {
			id   string
			name string
		}{id: w.ID, name: name}
	}

	// 手机号仍通过 IMSI 从 sim_subscriptions 查询（sim_subscriptions 主键尚为 IMSI）。
	imsiPhone := make(map[string]string)
	if phones, err := s.data().SIM.PhonesByIMSI(); err == nil {
		imsiPhone = phones
	}

	enriched := make([]SMSContactWithDevice, 0, len(contacts))
	for _, ct := range contacts {
		info := iccidDevice[ct.ICCID]
		laneVal := ""
		if cfg, ok := cfgByID[info.id]; ok {
			laneVal = config.NormalizeLane(cfg.Lane)
		}
		enriched = append(enriched, SMSContactWithDevice{
			SMSContact: ct,
			DeviceID:   info.id,
			DeviceName: info.name,
			LocalPhone: imsiPhone[ct.IMSI],
			Lane:       laneVal,
		})
	}

	if lane != "" {
		enriched = filterSMSContactsByLane(enriched, lane, cfgByID)
	}

	respondOK(c, enriched)
}

// filterSMSContactsByLane 留下属于该线路设备的会话。
// 优先看 enrich 后的 Lane；否则用 DeviceID 对照设备配置。
func filterSMSContactsByLane(contacts []SMSContactWithDevice, lane string, cfgByID map[string]config.DeviceConfig) []SMSContactWithDevice {
	lane = config.NormalizeLane(lane)
	if lane == "" {
		return contacts
	}
	allowedID := make(map[string]struct{}, len(cfgByID))
	for id, cfg := range cfgByID {
		if config.NormalizeLane(cfg.Lane) == lane {
			allowedID[id] = struct{}{}
		}
	}
	out := make([]SMSContactWithDevice, 0, len(contacts))
	for _, ct := range contacts {
		if config.NormalizeLane(ct.Lane) == lane {
			out = append(out, ct)
			continue
		}
		if ct.DeviceID != "" {
			if _, ok := allowedID[ct.DeviceID]; ok {
				out = append(out, ct)
			}
		}
	}
	return out
}

func (s *Server) handleGetSMSThread(c *gin.Context) {
	deviceID := c.Query("device_id")
	imsi := c.Query("imsi")
	peer := strings.TrimSpace(c.Query("peer"))
	if peer == "" {
		fail(c, http.StatusBadRequest, "", "缺少 peer 参数")
		return
	}

	limitStr := c.DefaultQuery("limit", "50")
	var limit int
	fmt.Sscanf(limitStr, "%d", &limit)

	var beforeTs *time.Time
	if v := strings.TrimSpace(c.Query("before_ts")); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			beforeTs = &t
		}
	}
	var beforeID uint
	if v := strings.TrimSpace(c.Query("before_id")); v != "" {
		var parsed uint64
		if _, err := fmt.Sscanf(v, "%d", &parsed); err == nil {
			beforeID = uint(parsed)
		}
	}

	var iccid string
	if strings.TrimSpace(deviceID) != "" || strings.TrimSpace(imsi) != "" {
		resolved, status, msg := s.resolveSMSICCID(deviceID, imsi)
		if status != 0 {
			fail(c, status, "", msg)
			return
		}
		iccid = resolved
	}

	list, err := s.data().SMS.ThreadByICCID(iccid, peer, limit, beforeTs, beforeID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "", "查询数据库失败: "+err.Error())
		return
	}

	devName := ""
	cfgByID := map[string]config.DeviceConfig{}
	{
		managed := config.ListDevices()
		for _, d := range managed {
			cfgByID[d.ID] = d
		}
	}
	workers := s.pool.GetAllWorkers()
	for _, w := range workers {
		if w.CurrentICCID() == iccid {
			if v, ok := cfgByID[w.ID]; ok {
				devName = v.Name
			} else {
				devName = w.Config.Name
			}
			if devName == "" {
				devName = w.ID
			}
			break
		}
	}

	enriched := make([]SMSWithDevice, 0, len(list))
	for _, sms := range list {
		enriched = append(enriched, SMSWithDevice{
			SMS:        sms,
			DeviceName: devName,
		})
	}

	respondOK(c, enriched)
}

func (s *Server) handleDeleteSMSMessage(c *gin.Context) {
	id64, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id64 == 0 {
		fail(c, http.StatusBadRequest, "", "无效的短信 id")
		return
	}

	threadEmpty, imsi, peer, err := s.data().SMS.DeleteByID(uint(id64))
	if err != nil {
		if errors.Is(err, db.ErrSMSNotFound) {
			fail(c, http.StatusNotFound, "", "短信不存在")
			return
		}
		fail(c, http.StatusInternalServerError, "", "删除短信失败: "+err.Error())
		return
	}

	respondOKWith(c, gin.H{
		"imsi": imsi,
		"peer": peer,
	}, gin.H{
		"thread_empty": threadEmpty,
	})
}

func (s *Server) handleDeleteSMSThread(c *gin.Context) {
	deviceID := c.Query("device_id")
	imsi := c.Query("imsi")
	peer := strings.TrimSpace(c.Query("peer"))
	if peer == "" {
		fail(c, http.StatusBadRequest, "", "缺少 peer 参数")
		return
	}

	resolved, status, msg := s.resolveSMSICCID(deviceID, imsi)
	if status != 0 {
		fail(c, status, "", msg)
		return
	}

	deleted, err := s.data().SMS.DeleteThreadByICCID(resolved, peer)
	if err != nil {
		if errors.Is(err, db.ErrSMSNotFound) {
			fail(c, http.StatusNotFound, "", "短信会话不存在")
			return
		}
		fail(c, http.StatusInternalServerError, "", "删除短信会话失败: "+err.Error())
		return
	}

	respondOKWith(c, gin.H{
		"iccid": resolved,
		"peer":  peer,
	}, gin.H{
		"deleted": deleted,
	})
}
