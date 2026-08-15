package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodoge/internal/db"
)

// patchCardPolicyForDevice 解析设备当前 ICCID，对 card_policies 行执行原地修改并落库。
// mutate 在 resolve 后的副本上改字段（source 会被强制为 "user"）。
// applied=false 且 err=nil 表示设备当前无 ICCID（离线/未识别），跳过落库。
func (s *Server) patchCardPolicyForDevice(deviceID string, mutate func(*db.CardPolicy)) (iccid string, applied bool, err error) {
	worker := s.pool.GetWorker(deviceID)
	if worker == nil {
		return "", false, fmt.Errorf("设备未找到")
	}
	iccid = worker.CurrentICCID()
	if iccid == "" {
		return "", false, nil
	}
	p, err := s.data().CardPolicy.Resolve(iccid)
	if err != nil {
		return iccid, false, fmt.Errorf("获取卡策略失败: %w", err)
	}
	mutate(&p)
	p.Source = "user"
	db.NormalizeCardPolicy(&p)
	if err := s.data().CardPolicy.Upsert(p); err != nil {
		return iccid, false, fmt.Errorf("保存卡策略失败: %w", err)
	}
	return iccid, true, nil
}

func (s *Server) handleGetCardPolicy(c *gin.Context) {
	iccid := c.Param("iccid")
	pol, err := s.data().CardPolicy.Get(iccid)
	if errors.Is(err, db.ErrCardPolicyNotFound) {
		// 未建档则返回默认模板（不落库，读端点保持只读语义）
		respondOK(c, db.DefaultCardPolicy(iccid))
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	respondOK(c, pol)
}

func (s *Server) handleListCardPolicies(c *gin.Context) {
	out, err := s.data().CardPolicy.List()
	if err != nil {
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	if out == nil {
		// 前端按数组消费，null 会让 .map 崩掉
		out = []db.CardPolicy{}
	}
	respondOK(c, out)
}

func (s *Server) handlePutCardPolicy(c *gin.Context) {
	iccid := c.Param("iccid")
	var req struct {
		NetworkEnabled *bool  `json:"network_enabled"`
		VoWiFiEnabled  *bool  `json:"vowifi_enabled"`
		IPVersion      string `json:"ip_version"`
		APN            string `json:"apn"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", err.Error())
		return
	}

	// 查出当前策略（查不到则用默认值）
	pol, err := s.data().CardPolicy.Get(iccid)
	if err != nil {
		pol = db.DefaultCardPolicy(iccid)
	}

	if req.NetworkEnabled != nil {
		pol.NetworkEnabled = *req.NetworkEnabled
	}
	if req.VoWiFiEnabled != nil {
		pol.VoWiFiEnabled = *req.VoWiFiEnabled
	}
	if req.IPVersion != "" {
		pol.IPVersion = req.IPVersion
	}
	if req.APN != "" {
		pol.APN = req.APN
	}
	pol.Source = "user"

	if err := s.data().CardPolicy.Upsert(pol); err != nil {
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}

	respondOK(c, pol)
}
