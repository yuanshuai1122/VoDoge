package device

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/boa-z/quectel-qmi-go/pkg/qmi"
	"github.com/yuanshuai1122/vodoge/internal/backend"
	"github.com/yuanshuai1122/vodoge/internal/config"
)

// cellAwareController 在基础假控制器上补出可选的第二信源。
//
// 真机复现的场景：EC20-CE 驻在电信 46011 的 LTE 小区上，AT+CEREG 报已注册、
// RSRP -72 dBm，而 NAS Get Serving System 一直返回 not-registered-searching
// 且 Radio interfaces 为 'none'。
type cellAwareController struct {
	qmiRegistrationTestController

	cellInfo  *qmi.CellLocationInfo
	cellErr   error
	cellCalls int
}

func (c *cellAwareController) GetCellLocationInfo(ctx context.Context) (*qmi.CellLocationInfo, error) {
	c.cellCalls++
	if c.cellErr != nil {
		return nil, c.cellErr
	}
	return c.cellInfo, nil
}

func campedLTE() *qmi.CellLocationInfo {
	return &qmi.CellLocationInfo{
		LTE: &qmi.LTECellLocationInfo{
			UEInIdle:     true,
			MCC:          "460",
			MNC:          "11",
			TAC:          6401,
			GlobalCellID: 4945529,
			EARFCN:       1650,
		},
	}
}

// 核心回归：serving system 说搜网，小区信息说已 camp，必须判为已驻网，
// 且**一个升级动作都不许发**——force network search 会打断当前搜网，
// radio cycle 会把一个正常工作的模组射频重启一遍。
func TestEnsureQMIRegistrationTrustsCellLocationOverServingSystem(t *testing.T) {
	ctrl := &cellAwareController{
		qmiRegistrationTestController: qmiRegistrationTestController{
			simStatuses: []qmi.SIMStatus{qmi.SIMReady},
			servingSeq: []*backend.ServingSystem{
				{RegStatus: 2, RegStatusText: "搜索中"},
			},
		},
		cellInfo: campedLTE(),
	}

	err := ensureQMIRegistration(context.Background(), "dev-lte", config.DeviceConfig{}, ctrl, ctrl, qmiRegistrationOptions{
		PollInterval: time.Nanosecond,
		MaxAttempts:  8,
	})
	if err != nil {
		t.Fatalf("ensureQMIRegistration() error = %v，已驻 LTE 时不该失败", err)
	}
	if ctrl.cellCalls == 0 {
		t.Fatal("没有查询小区信息，第二信源未生效")
	}
	if ctrl.registerCalls != 0 {
		t.Errorf("registerCalls=%d want 0：模组已驻网，不该再发 NAS 注册唤醒", ctrl.registerCalls)
	}
	if ctrl.forceNetworkSearchCalls != 0 {
		t.Errorf("forceNetworkSearchCalls=%d want 0：force search 会打断已有驻网", ctrl.forceNetworkSearchCalls)
	}
	if len(ctrl.setModeCalls) != 0 {
		t.Errorf("setModeCalls=%v want 空：不该对正常工作的模组做 radio cycle", ctrl.setModeCalls)
	}
}

func TestEnsureQMIRegistrationAcceptsBackendCellCampedFlag(t *testing.T) {
	ctrl := &qmiRegistrationTestController{
		simStatuses: []qmi.SIMStatus{qmi.SIMReady},
		servingSeq: []*backend.ServingSystem{{
			RegStatus:     2,
			RegStatusText: "已驻 LTE（数据未附着）",
			CellCamped:    true,
		}},
	}

	err := ensureQMIRegistration(context.Background(), "dev-lte", config.DeviceConfig{}, ctrl, ctrl, qmiRegistrationOptions{
		PollInterval: time.Nanosecond,
		MaxAttempts:  4,
	})
	if err != nil {
		t.Fatalf("ensureQMIRegistration() error = %v，后端已确认 cell camped 时不该失败", err)
	}
	if ctrl.registerCalls != 0 || ctrl.forceNetworkSearchCalls != 0 || len(ctrl.setModeCalls) != 0 {
		t.Fatalf("cell camped 状态不应触发恢复动作: register=%d force=%d radio=%v", ctrl.registerCalls, ctrl.forceNetworkSearchCalls, ctrl.setModeCalls)
	}
}

// 小区信息拿不到时必须退回原行为，而不是把「搜网」误判成「已驻网」。
func TestEnsureQMIRegistrationFallsBackWhenCellLocationUnusable(t *testing.T) {
	cases := []struct {
		name string
		info *qmi.CellLocationInfo
		err  error
	}{
		{name: "调用出错", err: errors.New("qmi busy")},
		{name: "返回 nil", info: nil},
		{name: "没有 LTE 段", info: &qmi.CellLocationInfo{}},
		{name: "PLMN 为空", info: &qmi.CellLocationInfo{LTE: &qmi.LTECellLocationInfo{GlobalCellID: 4945529}}},
		{name: "MNC 为空", info: &qmi.CellLocationInfo{LTE: &qmi.LTECellLocationInfo{MCC: "460", GlobalCellID: 4945529}}},
		// 小区 ID 为 0 表示还没 camp 上，只有 PLMN 不足以判定已驻网
		{name: "小区 ID 为零", info: &qmi.CellLocationInfo{LTE: &qmi.LTECellLocationInfo{MCC: "460", MNC: "11"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := &cellAwareController{
				qmiRegistrationTestController: qmiRegistrationTestController{
					simStatuses: []qmi.SIMStatus{qmi.SIMReady},
					servingSeq: []*backend.ServingSystem{
						{RegStatus: 2, RegStatusText: "搜索中"},
						{RegStatus: 1, RegStatusText: "已注册(本地)", PSAttached: true},
					},
				},
				cellInfo: tc.info,
				cellErr:  tc.err,
			}

			err := ensureQMIRegistration(context.Background(), "dev-lte", config.DeviceConfig{}, ctrl, ctrl, qmiRegistrationOptions{
				PollInterval: time.Nanosecond,
				MaxAttempts:  6,
			})
			if err != nil {
				t.Fatalf("ensureQMIRegistration() error = %v", err)
			}
			if ctrl.registerCalls != 1 {
				t.Fatalf("registerCalls=%d want 1：第二信源不可用时必须退回原有的搜网升级路径", ctrl.registerCalls)
			}
		})
	}
}

// 不实现该可选接口的后端（AT / MBIM，以及既有测试里的假实现）必须完全不受影响。
func TestEnsureQMIRegistrationIgnoresControllersWithoutCellLocation(t *testing.T) {
	ctrl := &qmiRegistrationTestController{
		simStatuses: []qmi.SIMStatus{qmi.SIMReady},
		servingSeq: []*backend.ServingSystem{
			{RegStatus: 2, RegStatusText: "搜索中"},
			{RegStatus: 1, RegStatusText: "已注册(本地)", PSAttached: true},
		},
	}

	err := ensureQMIRegistration(context.Background(), "dev-at", config.DeviceConfig{}, ctrl, ctrl, qmiRegistrationOptions{
		PollInterval: time.Nanosecond,
		MaxAttempts:  6,
	})
	if err != nil {
		t.Fatalf("ensureQMIRegistration() error = %v", err)
	}
	if ctrl.registerCalls != 1 {
		t.Fatalf("registerCalls=%d want 1", ctrl.registerCalls)
	}
}

func TestLTECampedOnCell(t *testing.T) {
	ctrl := &cellAwareController{cellInfo: campedLTE()}
	camped, plmn, cellID := lteCampedOnCell(context.Background(), "dev-lte", ctrl)
	if !camped {
		t.Fatal("camped=false，want true")
	}
	if plmn != "46011" {
		t.Errorf("plmn=%q want %q", plmn, "46011")
	}
	if cellID != 4945529 {
		t.Errorf("cellID=%d want 4945529", cellID)
	}

	// 不实现可选接口时静默返回 false，不得 panic
	plain := &qmiRegistrationTestController{}
	if camped, _, _ := lteCampedOnCell(context.Background(), "dev-at", plain); camped {
		t.Error("控制器未实现第二信源时不该判为已驻网")
	}
}
