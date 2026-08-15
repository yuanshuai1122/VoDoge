package smscodec

import (
	"strconv"
	"strings"
	"time"

	"github.com/warthog618/sms/encoding/tpdu"
)

// StatusReport 是蜂窝 SMS-STATUS-REPORT（回执）。
type StatusReport struct {
	Recipient string
	MR        int
	Status    byte
	Time      time.Time
}

// DecodeStatusReportTPDU 解析 SMS-STATUS-REPORT。不是回执时 ok=false。
func DecodeStatusReportTPDU(tpduBytes []byte) (StatusReport, bool) {
	if len(tpduBytes) == 0 {
		return StatusReport{}, false
	}
	var t tpdu.TPDU
	if err := t.UnmarshalBinary(tpduBytes); err != nil {
		return StatusReport{}, false
	}
	if t.SmsType() != tpdu.SmsStatusReport {
		return StatusReport{}, false
	}
	out := StatusReport{
		Recipient: t.RA.Number(),
		MR:        int(t.MR),
		Status:    t.ST,
		Time:      t.DT.Time,
	}
	if out.Time.IsZero() {
		out.Time = t.SCTS.Time
	}
	return out, true
}

// StatusReportDelivered 对应 3GPP TP-ST 短消息已由 SME 接收（0x00–0x1F）。
func StatusReportDelivered(status byte) bool {
	return status < 0x20
}

// ParseCMGSReference 从 AT+CMGS 响应里抽出 TP-MR。
func ParseCMGSReference(resp string) (int, bool) {
	for _, line := range strings.Split(resp, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToUpper(line), "+CMGS:") {
			continue
		}
		rest := strings.TrimSpace(line[6:])
		if i := strings.IndexByte(rest, ','); i >= 0 {
			rest = strings.TrimSpace(rest[:i])
		}
		n, err := strconv.Atoi(rest)
		if err == nil {
			return n, true
		}
	}
	return 0, false
}
