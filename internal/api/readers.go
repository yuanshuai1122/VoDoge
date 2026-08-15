package api

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodog/internal/config"
	"github.com/yuanshuai1122/vodog/internal/pcsc"
)

func (s *Server) readersBackend() pcsc.Backend {
	if s.pcsc != nil {
		return s.pcsc
	}
	return pcsc.System()
}

// handleListReaders 列出本机 CCID 读卡器。
// pcscd 没起来也回 200，用 data.daemon=missing 明示，避免对话框把读卡器一行藏掉。
func (s *Server) handleListReaders(c *gin.Context) {
	st := s.readersBackend().Discover(c.Request.Context())
	if st.Readers == nil {
		st.Readers = []pcsc.Reader{}
	}
	claimed := map[string]string{}
	for _, d := range config.ListDevices() {
		if config.IsPCSCBackend(d.DeviceBackend) && strings.TrimSpace(d.ReaderName) != "" {
			claimed[strings.TrimSpace(d.ReaderName)] = d.ID
		}
	}
	for i := range st.Readers {
		if id, ok := claimed[st.Readers[i].Name]; ok {
			st.Readers[i].ClaimedID = id
		}
	}
	respondOK(c, st)
}
