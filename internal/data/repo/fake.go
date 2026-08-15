package repo

import (
	"time"

	"github.com/yuanshuai1122/vodoge/internal/db"
)

// 供测试注入的假实现。
//
// 放在生产包而非 _test.go 里，是因为使用方在 internal/api：Go 的测试文件不跨包共享。
// 代价是一点点二进制体积，换来的是 API 层的 handler 终于能在没有数据库的情况下测。
//
// 每个字段是可选的钩子，留 nil 就返回零值。测试只需要给它关心的那一两个赋值，
// 不必为了测一个 handler 去实现整个接口。

type FakeCardPolicy struct {
	GetFn     func(iccid string) (db.CardPolicy, error)
	ResolveFn func(iccid string) (db.CardPolicy, error)
	UpsertFn  func(p db.CardPolicy) error
	ListFn    func() ([]db.CardPolicy, error)

	// Upserted 记录所有写入，断言副作用时用
	Upserted []db.CardPolicy
}

func (f *FakeCardPolicy) Get(iccid string) (db.CardPolicy, error) {
	if f.GetFn != nil {
		return f.GetFn(iccid)
	}
	return db.CardPolicy{}, db.ErrCardPolicyNotFound
}

func (f *FakeCardPolicy) Resolve(iccid string) (db.CardPolicy, error) {
	if f.ResolveFn != nil {
		return f.ResolveFn(iccid)
	}
	return db.DefaultCardPolicy(iccid), nil
}

func (f *FakeCardPolicy) Upsert(p db.CardPolicy) error {
	f.Upserted = append(f.Upserted, p)
	if f.UpsertFn != nil {
		return f.UpsertFn(p)
	}
	return nil
}

func (f *FakeCardPolicy) List() ([]db.CardPolicy, error) {
	if f.ListFn != nil {
		return f.ListFn()
	}
	return nil, nil
}

type FakeSIM struct {
	PhoneByIMSIFn           func(imsi string) (string, error)
	PhoneByIMSIOrICCIDFn    func(imsi, iccid string) (string, error)
	PhonesByIMSIFn          func() (map[string]string, error)
	SetVoWiFiPhoneFn        func(imsi, phone string) error
	SetModemPhoneFn         func(imsi, phone string) error
	ICCIDForIMSIFn          func(imsi string) string
	CurrentICCIDForDeviceFn func(deviceID string) string
}

func (f *FakeSIM) PhoneByIMSI(imsi string) (string, error) {
	if f.PhoneByIMSIFn != nil {
		return f.PhoneByIMSIFn(imsi)
	}
	return "", nil
}

func (f *FakeSIM) PhoneByIMSIOrICCID(imsi, iccid string) (string, error) {
	if f.PhoneByIMSIOrICCIDFn != nil {
		return f.PhoneByIMSIOrICCIDFn(imsi, iccid)
	}
	return "", nil
}

func (f *FakeSIM) PhonesByIMSI() (map[string]string, error) {
	if f.PhonesByIMSIFn != nil {
		return f.PhonesByIMSIFn()
	}
	return map[string]string{}, nil
}

func (f *FakeSIM) SetVoWiFiPhone(imsi, phone string) error {
	if f.SetVoWiFiPhoneFn != nil {
		return f.SetVoWiFiPhoneFn(imsi, phone)
	}
	return nil
}

func (f *FakeSIM) SetModemPhone(imsi, phone string) error {
	if f.SetModemPhoneFn != nil {
		return f.SetModemPhoneFn(imsi, phone)
	}
	return nil
}

func (f *FakeSIM) ICCIDForIMSI(imsi string) string {
	if f.ICCIDForIMSIFn != nil {
		return f.ICCIDForIMSIFn(imsi)
	}
	return ""
}

func (f *FakeSIM) CurrentICCIDForDevice(deviceID string) string {
	if f.CurrentICCIDForDeviceFn != nil {
		return f.CurrentICCIDForDeviceFn(deviceID)
	}
	return ""
}

type FakeSMS struct {
	SaveFn                func(imsi, localPhone, sender, recipient, content string, smsType, status int, timestamp time.Time) error
	ContactsFn            func(limit int, beforeTs *time.Time, beforePeer string) ([]db.SMSContact, error)
	ContactsByICCIDFn     func(iccid string, limit int, beforeTs *time.Time, beforePeer string) ([]db.SMSContact, error)
	ByICCIDFn             func(iccid string, limit int) ([]db.SMS, error)
	ThreadByICCIDFn       func(iccid, peer string, limit int, beforeTs *time.Time, beforeID uint) ([]db.SMS, error)
	RecentFn              func(limit int) ([]db.SMS, error)
	DeleteByIDFn          func(id uint) (bool, string, string, error)
	DeleteThreadByICCIDFn func(iccid, peer string) (int64, error)
	ReserveSendFn         func(limit int, deviceID, recipient string) (db.SMSRateStatus, error)
	RateStatusFn          func(limit int) (db.SMSRateStatus, error)

	// Saved 记录所有落库的短信
	Saved []db.SMS
	// Reserved 记录占额调用（含限额检查本身）
	Reserved []SMSReserveCall
}

type SMSReserveCall struct {
	Limit     int
	DeviceID  string
	Recipient string
}

func (f *FakeSMS) Save(imsi, localPhone, sender, recipient, content string, smsType, status int, timestamp time.Time) error {
	f.Saved = append(f.Saved, db.SMS{
		IMSI: imsi, LocalPhone: localPhone, Sender: sender,
		Recipient: recipient, Content: content, Type: smsType,
		Status: status, Timestamp: timestamp,
	})
	if f.SaveFn != nil {
		return f.SaveFn(imsi, localPhone, sender, recipient, content, smsType, status, timestamp)
	}
	return nil
}

func (f *FakeSMS) Contacts(limit int, beforeTs *time.Time, beforePeer string) ([]db.SMSContact, error) {
	if f.ContactsFn != nil {
		return f.ContactsFn(limit, beforeTs, beforePeer)
	}
	return nil, nil
}

func (f *FakeSMS) ContactsByICCID(iccid string, limit int, beforeTs *time.Time, beforePeer string) ([]db.SMSContact, error) {
	if f.ContactsByICCIDFn != nil {
		return f.ContactsByICCIDFn(iccid, limit, beforeTs, beforePeer)
	}
	return nil, nil
}

func (f *FakeSMS) ByICCID(iccid string, limit int) ([]db.SMS, error) {
	if f.ByICCIDFn != nil {
		return f.ByICCIDFn(iccid, limit)
	}
	return nil, nil
}

func (f *FakeSMS) ThreadByICCID(iccid, peer string, limit int, beforeTs *time.Time, beforeID uint) ([]db.SMS, error) {
	if f.ThreadByICCIDFn != nil {
		return f.ThreadByICCIDFn(iccid, peer, limit, beforeTs, beforeID)
	}
	return nil, nil
}

func (f *FakeSMS) Recent(limit int) ([]db.SMS, error) {
	if f.RecentFn != nil {
		return f.RecentFn(limit)
	}
	return nil, nil
}

func (f *FakeSMS) DeleteByID(id uint) (bool, string, string, error) {
	if f.DeleteByIDFn != nil {
		return f.DeleteByIDFn(id)
	}
	return false, "", "", db.ErrSMSNotFound
}

func (f *FakeSMS) DeleteThreadByICCID(iccid, peer string) (int64, error) {
	if f.DeleteThreadByICCIDFn != nil {
		return f.DeleteThreadByICCIDFn(iccid, peer)
	}
	return 0, db.ErrSMSNotFound
}

func (f *FakeSMS) ReserveSend(limit int, deviceID, recipient string) (db.SMSRateStatus, error) {
	f.Reserved = append(f.Reserved, SMSReserveCall{Limit: limit, DeviceID: deviceID, Recipient: recipient})
	if f.ReserveSendFn != nil {
		return f.ReserveSendFn(limit, deviceID, recipient)
	}
	return db.NewSMSRateStatus(limit, 0), nil
}

func (f *FakeSMS) RateStatus(limit int) (db.SMSRateStatus, error) {
	if f.RateStatusFn != nil {
		return f.RateStatusFn(limit)
	}
	return db.NewSMSRateStatus(limit, 0), nil
}

type FakeTraffic struct {
	LatestMinuteDeltasFn      func(resource, tag string) (time.Time, int64, int64, error)
	LatestMinuteDeltasBatchFn func(resource string, tags []string) (map[string]db.LatestMinuteDeltas, error)
	AnalysisWithChartFn       func(rangeName, deviceID string, now time.Time) ([]db.TrafficBucket, *db.TrafficChartData, error)
}

func (f *FakeTraffic) LatestMinuteDeltas(resource, tag string) (time.Time, int64, int64, error) {
	if f.LatestMinuteDeltasFn != nil {
		return f.LatestMinuteDeltasFn(resource, tag)
	}
	return time.Time{}, 0, 0, nil
}

func (f *FakeTraffic) LatestMinuteDeltasBatch(resource string, tags []string) (map[string]db.LatestMinuteDeltas, error) {
	if f.LatestMinuteDeltasBatchFn != nil {
		return f.LatestMinuteDeltasBatchFn(resource, tags)
	}
	return map[string]db.LatestMinuteDeltas{}, nil
}

func (f *FakeTraffic) AnalysisWithChart(rangeName, deviceID string, now time.Time) ([]db.TrafficBucket, *db.TrafficChartData, error) {
	if f.AnalysisWithChartFn != nil {
		return f.AnalysisWithChartFn(rangeName, deviceID, now)
	}
	return nil, nil, nil
}

type FakeUpstreamProxy struct {
	ListFn                 func() ([]db.UpstreamProxy, error)
	GetFn                  func(id string) (*db.UpstreamProxy, error)
	UpsertFn               func(p db.UpstreamProxy) error
	DeleteFn               func(id string) error
	ListCountryRulesFn     func() ([]db.UpstreamProxyCountryRule, error)
	UpsertCountryRuleFn    func(rule db.UpstreamProxyCountryRule) error
	DeleteCountryRuleFn    func(countryCode string) error
	ListProfileBindingsFn  func() ([]db.UpstreamProxyProfileBinding, error)
	GetProfileBindingFn    func(iccid string) (*db.UpstreamProxyProfileBinding, error)
	UpsertProfileBindingFn func(b db.UpstreamProxyProfileBinding) error
	DeleteProfileBindingFn func(iccid string) error

	ProfileBindings []db.UpstreamProxyProfileBinding
}

func (f *FakeUpstreamProxy) List() ([]db.UpstreamProxy, error) {
	if f.ListFn != nil {
		return f.ListFn()
	}
	return nil, nil
}

func (f *FakeUpstreamProxy) Get(id string) (*db.UpstreamProxy, error) {
	if f.GetFn != nil {
		return f.GetFn(id)
	}
	return nil, nil
}

func (f *FakeUpstreamProxy) Upsert(p db.UpstreamProxy) error {
	if f.UpsertFn != nil {
		return f.UpsertFn(p)
	}
	return nil
}

func (f *FakeUpstreamProxy) Delete(id string) error {
	if f.DeleteFn != nil {
		return f.DeleteFn(id)
	}
	return nil
}

func (f *FakeUpstreamProxy) ListCountryRules() ([]db.UpstreamProxyCountryRule, error) {
	if f.ListCountryRulesFn != nil {
		return f.ListCountryRulesFn()
	}
	return nil, nil
}

func (f *FakeUpstreamProxy) UpsertCountryRule(rule db.UpstreamProxyCountryRule) error {
	if f.UpsertCountryRuleFn != nil {
		return f.UpsertCountryRuleFn(rule)
	}
	return nil
}

func (f *FakeUpstreamProxy) DeleteCountryRule(countryCode string) error {
	if f.DeleteCountryRuleFn != nil {
		return f.DeleteCountryRuleFn(countryCode)
	}
	return nil
}

func (f *FakeUpstreamProxy) ListProfileBindings() ([]db.UpstreamProxyProfileBinding, error) {
	if f.ListProfileBindingsFn != nil {
		return f.ListProfileBindingsFn()
	}
	out := make([]db.UpstreamProxyProfileBinding, len(f.ProfileBindings))
	copy(out, f.ProfileBindings)
	return out, nil
}

func (f *FakeUpstreamProxy) GetProfileBinding(iccid string) (*db.UpstreamProxyProfileBinding, error) {
	if f.GetProfileBindingFn != nil {
		return f.GetProfileBindingFn(iccid)
	}
	for i := range f.ProfileBindings {
		if f.ProfileBindings[i].ICCID == iccid {
			b := f.ProfileBindings[i]
			return &b, nil
		}
	}
	return nil, nil
}

func (f *FakeUpstreamProxy) UpsertProfileBinding(b db.UpstreamProxyProfileBinding) error {
	if f.UpsertProfileBindingFn != nil {
		return f.UpsertProfileBindingFn(b)
	}
	for i := range f.ProfileBindings {
		if f.ProfileBindings[i].ICCID == b.ICCID {
			f.ProfileBindings[i] = b
			return nil
		}
	}
	f.ProfileBindings = append(f.ProfileBindings, b)
	return nil
}

func (f *FakeUpstreamProxy) DeleteProfileBinding(iccid string) error {
	if f.DeleteProfileBindingFn != nil {
		return f.DeleteProfileBindingFn(iccid)
	}
	for i := range f.ProfileBindings {
		if f.ProfileBindings[i].ICCID == iccid {
			f.ProfileBindings = append(f.ProfileBindings[:i], f.ProfileBindings[i+1:]...)
			return nil
		}
	}
	return db.ErrProfileBindingNotFound
}

// NewFakeStore 返回一个各域都是假实现的 Store。
// 返回具体类型以便测试直接赋钩子、读记录。
func NewFakeStore() (*Store, *FakeCardPolicy, *FakeSIM, *FakeSMS, *FakeTraffic, *FakeUpstreamProxy) {
	cp := &FakeCardPolicy{}
	sim := &FakeSIM{}
	sms := &FakeSMS{}
	traffic := &FakeTraffic{}
	up := &FakeUpstreamProxy{}
	return &Store{
		CardPolicy:    cp,
		SIM:           sim,
		SMS:           sms,
		Traffic:       traffic,
		UpstreamProxy: up,
	}, cp, sim, sms, traffic, up
}

var (
	_ CardPolicyRepository    = (*FakeCardPolicy)(nil)
	_ SIMRepository           = (*FakeSIM)(nil)
	_ SMSRepository           = (*FakeSMS)(nil)
	_ TrafficRepository       = (*FakeTraffic)(nil)
	_ UpstreamProxyRepository = (*FakeUpstreamProxy)(nil)
)
