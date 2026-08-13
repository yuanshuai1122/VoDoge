package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleTrafficAnalysis(c *gin.Context) {
	rng := c.Query("range")
	if rng == "" {
		rng = "day"
	}
	deviceID := strings.TrimSpace(c.Query("device_id"))
	now := time.Now()

	buckets, chartData, err := s.data().Traffic.AnalysisWithChart(rng, deviceID, now)
	if err != nil {
		fail(c, http.StatusBadRequest, "", err.Error())
		return
	}

	respondOKWith(c, gin.H{
		"buckets": buckets,
		"chart":   chartData,
	}, gin.H{
		"range": rng,
	})
}
