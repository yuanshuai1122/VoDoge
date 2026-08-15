// 持久化访问的接口边界。
//
// 起因：`db.DB` 是包级全局变量，11 个 API 文件直接调 `db.XxxFunc()` 查库。
// 代价不是"不好看"，是三件具体的事：
//
//  1. handler 与表结构直接耦合，改一列要翻遍 API 层；
//  2. **API 包的测试必须连真 PostgreSQL**——跑十几秒、要先起容器、
//     还得 `-p 1` 串行（各包共用一个库，一个包的 truncate 会清掉另一个包的数据）；
//  3. `db.OpenTestDB` 会清空目标库全部表，测试指错 DSN 就是一次事故（KI-002）。
//
// 现在 handler 只认这里的接口，测试注入假实现即可，不需要数据库。
//
// **本层刻意只做接口与转发**：实现直接委托给 `internal/db` 里已有的函数，
// 不重写查询。那些函数有真库测试覆盖，重写它们等于把风险从"没有边界"
// 换成"边界后面的实现是新的"。全局 `db.DB` 因此仍然存在，只是 API 层够不着它了
// ——把 `*gorm.DB` 一路下推、彻底干掉全局是下一步的事，那要连 device / notify /
// proxy 三层一起改，属于硬件路径，等现场验证之后再动。
package repo

import (
	"time"

	"github.com/yuanshuai1122/vodoge/internal/db"
)

// Store 聚合各域仓储，作为 API 层的唯一持久化入口。
type Store struct {
	CardPolicy    CardPolicyRepository
	SIM           SIMRepository
	SMS           SMSRepository
	Traffic       TrafficRepository
	UpstreamProxy UpstreamProxyRepository
	ProxyInstance ProxyInstanceRepository
}

// NewStore 返回接到全局 gorm 句柄上的实现。
func NewStore() *Store {
	return &Store{
		CardPolicy:    cardPolicyRepo{},
		SIM:           simRepo{},
		SMS:           smsRepo{},
		Traffic:       trafficRepo{},
		UpstreamProxy: upstreamProxyRepo{},
		ProxyInstance: NewDBRepo(),
	}
}

// CardPolicyRepository 卡策略。策略以 ICCID 为键，是"用户对这张卡的意图"，
// 与设备运行时状态分离。
type CardPolicyRepository interface {
	Get(iccid string) (db.CardPolicy, error)
	// Resolve 在无记录时返回默认策略，不报 ErrCardPolicyNotFound。
	Resolve(iccid string) (db.CardPolicy, error)
	Upsert(p db.CardPolicy) error
	List() ([]db.CardPolicy, error)
}

// SIMRepository SIM 卡身份与本机号码。
//
// 号码有两个来源且优先级固定：vowifi > modem。IMSI 未知时按 ICCID 暂存，
// IMSI 到位后迁移——这套规则在 db 层，接口只暴露入口。
type SIMRepository interface {
	PhoneByIMSI(imsi string) (string, error)
	PhoneByIMSIOrICCID(imsi, iccid string) (string, error)
	PhonesByIMSI() (map[string]string, error)
	SetVoWiFiPhone(imsi, phone string) error
	SetModemPhone(imsi, phone string) error
	ICCIDForIMSI(imsi string) string
	CurrentICCIDForDevice(deviceID string) string
}

// SMSRepository 短信与会话。
//
// 分页是游标式（时间戳 + 次键），不是页码；后端不返回总数。
type SMSRepository interface {
	Save(imsi, localPhone, sender, recipient, content string, smsType, status int, timestamp time.Time) error
	Contacts(limit int, beforeTs *time.Time, beforePeer string) ([]db.SMSContact, error)
	ContactsByICCID(iccid string, limit int, beforeTs *time.Time, beforePeer string) ([]db.SMSContact, error)
	ByICCID(iccid string, limit int) ([]db.SMS, error)
	ThreadByICCID(iccid, peer string, limit int, beforeTs *time.Time, beforeID uint) ([]db.SMS, error)
	Recent(limit int) ([]db.SMS, error)
	// DeleteByID 返回 (会话是否已空, imsi, peer, err)——会话空了前端要把它从列表里去掉。
	DeleteByID(id uint) (bool, string, string, error)
	DeleteThreadByICCID(iccid, peer string) (int64, error)
	// ReserveSend 在滚动 1 小时窗口里占 1 条全局发送额度。超限返回 *db.SMSRateLimitedError。
	ReserveSend(limit int, deviceID, recipient string) (db.SMSRateStatus, error)
	RateStatus(limit int) (db.SMSRateStatus, error)
}

// TrafficRepository 流量采样与聚合。
type TrafficRepository interface {
	LatestMinuteDeltas(resource, tag string) (time.Time, int64, int64, error)
	LatestMinuteDeltasBatch(resource string, tags []string) (map[string]db.LatestMinuteDeltas, error)
	AnalysisWithChart(rangeName, deviceID string, now time.Time) ([]db.TrafficBucket, *db.TrafficChartData, error)
}

// UpstreamProxyRepository 前置代理及其国家规则。
type UpstreamProxyRepository interface {
	List() ([]db.UpstreamProxy, error)
	Get(id string) (*db.UpstreamProxy, error)
	Upsert(p db.UpstreamProxy) error
	Delete(id string) error
	ListCountryRules() ([]db.UpstreamProxyCountryRule, error)
	UpsertCountryRule(rule db.UpstreamProxyCountryRule) error
	DeleteCountryRule(countryCode string) error
	ListProfileBindings() ([]db.UpstreamProxyProfileBinding, error)
	GetProfileBinding(iccid string) (*db.UpstreamProxyProfileBinding, error)
	UpsertProfileBinding(b db.UpstreamProxyProfileBinding) error
	DeleteProfileBinding(iccid string) error
}

// ---- 实现：转发到 internal/db ----

type cardPolicyRepo struct{}

func (cardPolicyRepo) Get(iccid string) (db.CardPolicy, error) { return db.GetCardPolicy(iccid) }
func (cardPolicyRepo) Resolve(iccid string) (db.CardPolicy, error) {
	return db.ResolveCardPolicy(iccid)
}
func (cardPolicyRepo) Upsert(p db.CardPolicy) error   { return db.UpsertCardPolicy(p) }
func (cardPolicyRepo) List() ([]db.CardPolicy, error) { return db.ListCardPolicies() }

type simRepo struct{}

func (simRepo) PhoneByIMSI(imsi string) (string, error) {
	return db.GetSIMCardPhoneNumberByIMSI(imsi)
}
func (simRepo) PhoneByIMSIOrICCID(imsi, iccid string) (string, error) {
	return db.GetPhoneNumberByIMSIOrICCID(imsi, iccid)
}
func (simRepo) PhonesByIMSI() (map[string]string, error) { return db.GetSIMPhoneNumbersByIMSI() }
func (simRepo) SetVoWiFiPhone(imsi, phone string) error {
	return db.UpdateSIMCardVoWiFiPhoneNumberByIMSI(imsi, phone)
}
func (simRepo) SetModemPhone(imsi, phone string) error {
	return db.UpdateSIMCardModemPhoneNumberByIMSI(imsi, phone)
}
func (simRepo) ICCIDForIMSI(imsi string) string { return db.GetICCIDForIMSI(imsi) }
func (simRepo) CurrentICCIDForDevice(deviceID string) string {
	return db.CurrentICCIDForDevice(deviceID)
}

type smsRepo struct{}

func (smsRepo) Save(imsi, localPhone, sender, recipient, content string, smsType, status int, timestamp time.Time) error {
	return db.SaveSMSWithLocalPhone(imsi, localPhone, sender, recipient, content, smsType, status, timestamp)
}
func (smsRepo) Contacts(limit int, beforeTs *time.Time, beforePeer string) ([]db.SMSContact, error) {
	return db.GetSMSContacts(limit, beforeTs, beforePeer)
}
func (smsRepo) ContactsByICCID(iccid string, limit int, beforeTs *time.Time, beforePeer string) ([]db.SMSContact, error) {
	return db.GetSMSContactsByICCID(iccid, limit, beforeTs, beforePeer)
}
func (smsRepo) ByICCID(iccid string, limit int) ([]db.SMS, error) {
	return db.GetSMSByICCID(iccid, limit)
}
func (smsRepo) ThreadByICCID(iccid, peer string, limit int, beforeTs *time.Time, beforeID uint) ([]db.SMS, error) {
	return db.GetSMSByICCIDAndPeer(iccid, peer, limit, beforeTs, beforeID)
}
func (smsRepo) Recent(limit int) ([]db.SMS, error) { return db.GetRecentSMS(limit) }
func (smsRepo) DeleteByID(id uint) (bool, string, string, error) {
	return db.DeleteSMSByID(id)
}
func (smsRepo) DeleteThreadByICCID(iccid, peer string) (int64, error) {
	return db.DeleteSMSByICCIDAndPeer(iccid, peer)
}
func (smsRepo) ReserveSend(limit int, deviceID, recipient string) (db.SMSRateStatus, error) {
	return db.ReserveSMSSend(limit, deviceID, recipient)
}
func (smsRepo) RateStatus(limit int) (db.SMSRateStatus, error) {
	return db.GetSMSRateStatus(limit)
}

type trafficRepo struct{}

func (trafficRepo) LatestMinuteDeltas(resource, tag string) (time.Time, int64, int64, error) {
	return db.GetLatestMinuteDeltas(resource, tag)
}
func (trafficRepo) LatestMinuteDeltasBatch(resource string, tags []string) (map[string]db.LatestMinuteDeltas, error) {
	return db.GetLatestMinuteDeltasBatch(resource, tags)
}
func (trafficRepo) AnalysisWithChart(rangeName, deviceID string, now time.Time) ([]db.TrafficBucket, *db.TrafficChartData, error) {
	return db.GetTrafficAnalysisWithChart(rangeName, deviceID, now)
}

type upstreamProxyRepo struct{}

func (upstreamProxyRepo) List() ([]db.UpstreamProxy, error) { return db.ListUpstreamProxies() }
func (upstreamProxyRepo) Get(id string) (*db.UpstreamProxy, error) {
	return db.GetUpstreamProxyByID(id)
}
func (upstreamProxyRepo) Upsert(p db.UpstreamProxy) error { return db.UpsertUpstreamProxy(p) }
func (upstreamProxyRepo) Delete(id string) error          { return db.DeleteUpstreamProxy(id) }
func (upstreamProxyRepo) ListCountryRules() ([]db.UpstreamProxyCountryRule, error) {
	return db.ListUpstreamProxyCountryRules()
}
func (upstreamProxyRepo) UpsertCountryRule(rule db.UpstreamProxyCountryRule) error {
	return db.UpsertUpstreamProxyCountryRule(rule)
}
func (upstreamProxyRepo) DeleteCountryRule(countryCode string) error {
	return db.DeleteUpstreamProxyCountryRule(countryCode)
}
func (upstreamProxyRepo) ListProfileBindings() ([]db.UpstreamProxyProfileBinding, error) {
	return db.ListProfileBindings()
}
func (upstreamProxyRepo) GetProfileBinding(iccid string) (*db.UpstreamProxyProfileBinding, error) {
	return db.GetProfileBinding(iccid)
}
func (upstreamProxyRepo) UpsertProfileBinding(b db.UpstreamProxyProfileBinding) error {
	return db.UpsertProfileBinding(b)
}
func (upstreamProxyRepo) DeleteProfileBinding(iccid string) error {
	return db.DeleteProfileBinding(iccid)
}

// 确保实现满足接口（编译期检查，改签名时立刻失败而不是运行期）
var (
	_ CardPolicyRepository    = cardPolicyRepo{}
	_ SIMRepository           = simRepo{}
	_ SMSRepository           = smsRepo{}
	_ TrafficRepository       = trafficRepo{}
	_ UpstreamProxyRepository = upstreamProxyRepo{}
	_ ProxyInstanceRepository = (*DBRepo)(nil)
)
