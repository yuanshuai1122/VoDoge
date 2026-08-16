package pcsc

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const (
	KindReader = "reader"
	KindModem  = "modem"
)

var (
	ErrNoCard            = errors.New("读卡器上没有卡")
	ErrInUse             = errors.New("读卡器或这张卡正被另一路占用")
	ErrAPDUUnavailable   = errors.New("读卡器 APDU 连不上 pcscd")
	ErrReaderNameEmpty   = errors.New("reader_name 不能为空")
	ErrReaderNameTooLong = errors.New("reader_name 超过 PC/SC 协议上限")
	ErrReaderNotFound    = errors.New("读卡器未找到")
	ErrResponseTooLarge  = errors.New("pcscd 响应超过协议上限")
)

// Holder 是占用方。读卡器按名字互斥；若双方都知道 ICCID，同一张卡也不能同时给模组用。
type Holder struct {
	DeviceID   string
	Kind       string
	ReaderName string
	ICCID      string
}

type Occupancy struct {
	mu       sync.Mutex
	byReader map[string]Holder
	byICCID  map[string]Holder
	gates    map[string]*sync.Mutex
}

func NewOccupancy() *Occupancy {
	return &Occupancy{
		byReader: map[string]Holder{},
		byICCID:  map[string]Holder{},
		gates:    map[string]*sync.Mutex{},
	}
}

// GuardReader 串行同一把读卡器上的 APDU（eSIM 写卡与 VoWiFi AKA）。
// 与 Acquire 独立：同一设备 ID 的 eSIM / AKA 仍要排队，但不能用 Release 互相拆台。
func (o *Occupancy) GuardReader(name string) func() {
	if o == nil {
		return func() {}
	}
	name = NormalizeReaderName(name)
	if name == "" {
		return func() {}
	}
	o.mu.Lock()
	if o.gates == nil {
		o.gates = map[string]*sync.Mutex{}
	}
	g, ok := o.gates[name]
	if !ok {
		g = &sync.Mutex{}
		o.gates[name] = g
	}
	o.mu.Unlock()
	g.Lock()
	return g.Unlock
}

func NormalizeReaderName(in string) string {
	return strings.TrimSpace(in)
}

func ValidateReaderName(in string) error {
	name := NormalizeReaderName(in)
	if name == "" {
		return ErrReaderNameEmpty
	}
	// pcsc-lite reserves one byte in the fixed-width field for the NUL terminator.
	if len(name) >= maxReaderName {
		return fmt.Errorf("%w: %d bytes (max %d)", ErrReaderNameTooLong, len(name), maxReaderName-1)
	}
	return nil
}

func DeviceIDFromReader(name string) string {
	n := NormalizeReaderName(name)
	if n == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(n))
	return "reader-" + hex.EncodeToString(sum[:8])
}

func (o *Occupancy) Acquire(h Holder) error {
	if o == nil {
		return nil
	}
	h.DeviceID = strings.TrimSpace(h.DeviceID)
	h.ReaderName = NormalizeReaderName(h.ReaderName)
	h.ICCID = strings.TrimSpace(h.ICCID)
	if h.DeviceID == "" {
		return fmt.Errorf("occupancy: empty device id")
	}
	if h.Kind == KindReader && h.ReaderName == "" {
		return ErrReaderNameEmpty
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	if h.ReaderName != "" {
		if cur, ok := o.byReader[h.ReaderName]; ok && cur.DeviceID != h.DeviceID {
			return fmt.Errorf("%w：读卡器 %s 正被设备 %s 占用", ErrInUse, h.ReaderName, cur.DeviceID)
		}
	}
	if h.ICCID != "" {
		if cur, ok := o.byICCID[h.ICCID]; ok && cur.DeviceID != h.DeviceID {
			return fmt.Errorf("%w：ICCID 正被设备 %s（%s）占用", ErrInUse, cur.DeviceID, cur.Kind)
		}
	}

	if h.ReaderName != "" {
		o.byReader[h.ReaderName] = h
	}
	if h.ICCID != "" {
		o.byICCID[h.ICCID] = h
	}
	return nil
}

func (o *Occupancy) Release(deviceID string) {
	if o == nil {
		return
	}
	deviceID = strings.TrimSpace(deviceID)
	o.mu.Lock()
	defer o.mu.Unlock()
	for k, h := range o.byReader {
		if h.DeviceID == deviceID {
			delete(o.byReader, k)
		}
	}
	for k, h := range o.byICCID {
		if h.DeviceID == deviceID {
			delete(o.byICCID, k)
		}
	}
}

func (o *Occupancy) HolderOfReader(name string) (Holder, bool) {
	if o == nil {
		return Holder{}, false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	h, ok := o.byReader[NormalizeReaderName(name)]
	return h, ok
}
