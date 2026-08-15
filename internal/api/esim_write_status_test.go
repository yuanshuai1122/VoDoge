package api

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/yuanshuai1122/vodoge/internal/apduarbiter"
	"github.com/yuanshuai1122/vodoge/internal/esim"
	"github.com/yuanshuai1122/vodoge/internal/pcsc"
)

func TestEsimWriteHTTPStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"busy", esim.ErrOperationInProgress, http.StatusConflict},
		{"apdu busy", apduarbiter.ErrAPDUBusy, http.StatusConflict},
		{"not found", esim.NewDeleteProfileError(esim.DeleteProfileErrorProfileNotFound, "nope", nil), http.StatusNotFound},
		{"invalid iccid", fmt.Errorf("无效的 ICCID %q: bad", "xx"), http.StatusBadRequest},
		{"internal", errors.New("modem timeout"), http.StatusInternalServerError},
		{"apdu not wired", fmt.Errorf("open: %w", pcsc.ErrAPDUUnavailable), http.StatusServiceUnavailable},
		{"in use", fmt.Errorf("%w", pcsc.ErrInUse), http.StatusConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := esimWriteHTTPStatus(tt.err); got != tt.want {
				t.Fatalf("esimWriteHTTPStatus()=%d want %d", got, tt.want)
			}
		})
	}
}

func TestEsimDeleteHTTPStatusRejectsEnabled(t *testing.T) {
	err := esim.NewDeleteProfileError(esim.DeleteProfileErrorProfileEnabled, "当前启用的 Profile 不能删除", nil)
	if got := esimDeleteHTTPStatus(err); got != http.StatusConflict {
		t.Fatalf("status=%d want 409", got)
	}
	if !esim.IsProfileEnabled(err) {
		t.Fatal("IsProfileEnabled()=false")
	}
}
