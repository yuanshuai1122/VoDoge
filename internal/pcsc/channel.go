package pcsc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/damonto/euicc-go/driver"
)

const (
	maxLogicalChannel      = 19
	maxShortAPDUDataLength = 255
	channelTimeout         = 30 * time.Second
)

// Channel 是连到指定读卡器的 APDU 通道，实现 euicc-go 的 SmartCardChannel。
// 走 pcscd Unix 协议，不链接 libpcsclite。
type Channel struct {
	reader  string
	client  *daemonClient
	hCard   int32
	channel byte
	inTxn   bool
	gate    func() func()
	mu      sync.Mutex
}

func NewChannel(reader string) *Channel {
	return &Channel{reader: NormalizeReaderName(reader)}
}

// SetGate 在每次 APDU 前后加读卡器互斥。eSIM 与 VoWiFi AKA 共用。
func (c *Channel) SetGate(gate func() func()) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.gate = gate
	c.mu.Unlock()
}

func (c *Channel) enterGate() func() {
	c.mu.Lock()
	gate := c.gate
	c.mu.Unlock()
	if gate == nil {
		return func() {}
	}
	return gate()
}

var _ driver.SmartCardChannel = (*Channel)(nil)

func (c *Channel) CurrentChannel() byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.channel
}

func (c *Channel) Connect() error {
	unlock := c.enterGate()
	defer unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		return nil
	}
	if c.reader == "" {
		return ErrReaderNameEmpty
	}
	if err := ValidateReaderName(c.reader); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelTimeout)
	defer cancel()
	client, err := dialDaemon(ctx, nil)
	if err != nil {
		return err
	}
	hCard, _, err := client.connect(ctx, c.reader)
	if err != nil {
		_ = client.close(ctx)
		return err
	}
	if err := client.beginTransaction(ctx, hCard); err != nil {
		_ = client.disconnect(ctx, hCard)
		_ = client.close(ctx)
		return err
	}
	c.client = client
	c.hCard = hCard
	c.inTxn = true
	return nil
}

func (c *Channel) Disconnect() error {
	unlock := c.enterGate()
	defer unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var errs []error
	if c.inTxn {
		if err := c.client.endTransaction(ctx, c.hCard); err != nil {
			errs = append(errs, err)
		}
		c.inTxn = false
	}
	if err := c.client.disconnect(ctx, c.hCard); err != nil {
		errs = append(errs, err)
	}
	if err := c.client.close(ctx); err != nil {
		errs = append(errs, err)
	}
	c.client = nil
	c.hCard = 0
	c.channel = 0
	return errors.Join(errs...)
}

func (c *Channel) OpenLogicalChannel(aid []byte) (byte, error) {
	unlock := c.enterGate()
	defer unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		return 0, fmt.Errorf("读卡器尚未 Connect")
	}
	if len(aid) == 0 || len(aid) > maxShortAPDUDataLength {
		return 0, fmt.Errorf("AID 长度非法: %d", len(aid))
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelTimeout)
	defer cancel()
	ch, err := c.openLogicalChannel(ctx)
	if err != nil {
		return 0, err
	}
	if err := c.selectAID(ctx, ch, aid); err != nil {
		_ = c.closeLogicalChannel(ctx, ch)
		return 0, err
	}
	c.channel = ch
	return ch, nil
}

func (c *Channel) Transmit(command []byte) ([]byte, error) {
	unlock := c.enterGate()
	defer unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		return nil, fmt.Errorf("读卡器尚未 Connect")
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelTimeout)
	defer cancel()
	return c.transmitAPDU(ctx, command)
}

func (c *Channel) CloseLogicalChannel(channel byte) error {
	unlock := c.enterGate()
	defer unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelTimeout)
	defer cancel()
	return c.closeLogicalChannel(ctx, channel)
}

func (c *Channel) openLogicalChannel(ctx context.Context) (byte, error) {
	resp, err := c.transmitAPDU(ctx, []byte{0x00, 0x70, 0x00, 0x00, 0x01})
	if err != nil {
		return 0, err
	}
	if len(resp) < 3 || !statusOK(resp) {
		return 0, fmt.Errorf("打开逻辑通道失败: %X", resp)
	}
	ch := resp[0]
	if ch == 0 || ch > maxLogicalChannel {
		return 0, fmt.Errorf("逻辑通道号非法: %d", ch)
	}
	return ch, nil
}

func (c *Channel) selectAID(ctx context.Context, channel byte, aid []byte) error {
	cla, err := classByteForChannel(0x00, channel)
	if err != nil {
		return err
	}
	cmd := make([]byte, 0, 5+len(aid))
	cmd = append(cmd, cla, 0xA4, 0x04, 0x00, byte(len(aid)))
	cmd = append(cmd, aid...)
	resp, err := c.transmitAPDU(ctx, cmd)
	if err != nil {
		return err
	}
	if len(resp) < 2 || (!statusOK(resp) && !statusHasMore(resp)) {
		return fmt.Errorf("选择 AID 失败: %X", resp)
	}
	return nil
}

func (c *Channel) closeLogicalChannel(ctx context.Context, channel byte) error {
	if channel == 0 || channel > maxLogicalChannel {
		return fmt.Errorf("逻辑通道号非法: %d", channel)
	}
	resp, err := c.transmitAPDU(ctx, []byte{0x00, 0x70, 0x80, channel, 0x00})
	if err != nil {
		return err
	}
	if len(resp) < 2 || !statusOK(resp) {
		return fmt.Errorf("关闭逻辑通道失败: %X", resp)
	}
	if c.channel == channel {
		c.channel = 0
	}
	return nil
}

func (c *Channel) transmitAPDU(ctx context.Context, command []byte) ([]byte, error) {
	resp, err := c.client.transmit(ctx, c.hCard, command)
	if err != nil {
		return nil, err
	}
	for i := 0; i < 8; i++ {
		if len(resp) < 2 {
			return resp, nil
		}
		sw1, sw2 := resp[len(resp)-2], resp[len(resp)-1]
		switch sw1 {
		case 0x6C:
			if len(command) < 4 {
				return resp, nil
			}
			retry := append([]byte(nil), command...)
			if len(retry) == 4 {
				retry = append(retry, sw2)
			} else {
				retry[len(retry)-1] = sw2
			}
			resp, err = c.client.transmit(ctx, c.hCard, retry)
			if err != nil {
				return nil, err
			}
		case 0x61:
			getResp := []byte{command[0] & 0xFC, 0xC0, 0x00, 0x00, sw2}
			more, err := c.client.transmit(ctx, c.hCard, getResp)
			if err != nil {
				return nil, err
			}
			resp = append(resp[:len(resp)-2], more...)
		default:
			return resp, nil
		}
	}
	return resp, nil
}

func classByteForChannel(cla, channel byte) (byte, error) {
	if channel < 4 {
		return (cla & 0x9C) | channel, nil
	}
	if channel <= maxLogicalChannel {
		return (cla & 0xB0) | 0x40 | (channel - 4), nil
	}
	return 0, fmt.Errorf("逻辑通道号超出范围: %d", channel)
}

func statusOK(response []byte) bool {
	return len(response) >= 2 && response[len(response)-2] == 0x90 && response[len(response)-1] == 0x00
}

func statusHasMore(response []byte) bool {
	return len(response) >= 2 && response[len(response)-2] == 0x61
}
