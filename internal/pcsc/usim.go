package pcsc

import (
	"encoding/hex"
	"fmt"
	"strings"
)

var (
	fidMF     = []byte{0x3F, 0x00}
	fidICCID  = []byte{0x2F, 0xE2}
	fidIMSI   = []byte{0x6F, 0x07}
	aidUSIM   = []byte{0xA0, 0x00, 0x00, 0x00, 0x87, 0x10, 0x02}
	aidUSIM16 = []byte{0xA0, 0x00, 0x00, 0x00, 0x87, 0x10, 0x02, 0xFF, 0x86, 0xFF, 0xFF, 0x89, 0xFF, 0xFF, 0xFF, 0xFF}
)

// ReadICCID 从当前已连接的卡读 EF_ICCID。
func (c *Channel) ReadICCID() (string, error) {
	if err := c.selectFID(fidMF); err != nil {
		return "", err
	}
	if err := c.selectFID(fidICCID); err != nil {
		return "", err
	}
	raw, err := c.readBinary(10)
	if err != nil {
		return "", err
	}
	return decodeICCID(raw), nil
}

// ReadIMSI 选择 USIM 后读 EF_IMSI。
func (c *Channel) ReadIMSI() (string, error) {
	if _, err := c.OpenLogicalChannel(aidUSIM16); err != nil {
		if _, err2 := c.OpenLogicalChannel(aidUSIM); err2 != nil {
			return "", fmt.Errorf("选择 USIM 失败: %v / %v", err, err2)
		}
	}
	defer func() {
		if ch := c.CurrentChannel(); ch != 0 {
			_ = c.CloseLogicalChannel(ch)
		}
	}()
	if err := c.selectFID(fidIMSI); err != nil {
		return "", err
	}
	raw, err := c.readBinary(9)
	if err != nil {
		return "", err
	}
	return decodeIMSI(raw)
}

func (c *Channel) selectFID(fid []byte) error {
	cmd := append([]byte{0x00, 0xA4, 0x00, 0x04, byte(len(fid))}, fid...)
	resp, err := c.Transmit(cmd)
	if err != nil {
		return err
	}
	if !statusOK(resp) && !statusHasMore(resp) {
		return fmt.Errorf("选择文件 %X 失败: %X", fid, resp)
	}
	return nil
}

func (c *Channel) readBinary(le byte) ([]byte, error) {
	resp, err := c.Transmit([]byte{0x00, 0xB0, 0x00, 0x00, le})
	if err != nil {
		return nil, err
	}
	if len(resp) < 2 {
		return nil, fmt.Errorf("READ BINARY 响应过短")
	}
	if !statusOK(resp) && !statusHasMore(resp) {
		return nil, fmt.Errorf("READ BINARY 失败: %X", resp)
	}
	return resp[:len(resp)-2], nil
}

func decodeICCID(raw []byte) string {
	var b strings.Builder
	for _, v := range raw {
		lo, hi := v&0x0F, v>>4
		if lo <= 9 {
			b.WriteByte('0' + lo)
		}
		if hi <= 9 {
			b.WriteByte('0' + hi)
		}
	}
	return strings.TrimRight(b.String(), "F")
}

func decodeIMSI(raw []byte) (string, error) {
	if len(raw) < 2 {
		return "", fmt.Errorf("EF_IMSI 过短")
	}
	// byte0 = length; byte1 low nibble is parity, high nibble first IMSI digit
	var b strings.Builder
	b.WriteByte('0' + (raw[1] >> 4))
	for _, v := range raw[2:] {
		lo, hi := v&0x0F, v>>4
		if lo <= 9 {
			b.WriteByte('0' + lo)
		}
		if hi <= 9 {
			b.WriteByte('0' + hi)
		}
	}
	s := b.String()
	if len(s) < 5 {
		return "", fmt.Errorf("IMSI 过短: %s", s)
	}
	return s, nil
}

// DerivedIMEI 从读卡器名生成 15 位数字（含 Luhn），VoWiFi 需要 IMEI 而读卡器没有模组号。
func DerivedIMEI(reader string) string {
	sum := 0
	h := []byte(strings.TrimSpace(reader))
	if len(h) == 0 {
		h = []byte("vodoge-reader")
	}
	// 用名字字节摊成 14 位再补 Luhn
	digits := make([]byte, 14)
	for i := 0; i < 14; i++ {
		digits[i] = '0' + h[i%len(h)]%10
	}
	for i, d := range digits {
		n := int(d - '0')
		if (len(digits)-i)%2 == 0 {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
	}
	check := (10 - sum%10) % 10
	return string(digits) + string(rune('0'+check))
}

// DecodeHexAID 把十六进制 AID 转成字节。
func DecodeHexAID(aid string) ([]byte, error) {
	aid = strings.TrimSpace(aid)
	if aid == "" {
		return nil, fmt.Errorf("AID 为空")
	}
	return hex.DecodeString(aid)
}
