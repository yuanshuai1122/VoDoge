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
	ErrNoCard          = errors.New("读卡器上没有卡")
	ErrInUse           = errors.New("读卡器或这张卡正被另一路占用")
	ErrAPDUUnavailable = errors.New("读卡器 APDU 连不上 pcscd")
	ErrReaderNameEmpty = errors.New("reader_name 不能为空")
	ErrReaderNotFound  = errors.New("读卡器未找到")
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
}

func NewOccupancy() *Occupancy {
	return &Occupancy{
		byReader: map[string]Holder{},
		byICCID:  map[string]Holder{},
	}
}

func NormalizeReaderName(in string) string {
	return strings.TrimSpace(in)
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
