package device

import (
	"context"
	"testing"
	"time"

	"github.com/boa-z/vowifi-go/runtimehost"
	"github.com/boa-z/vowifi-go/runtimehost/voicehost"
	"github.com/emiago/sipgo/sip"
	"github.com/yuanshuai1122/vodoge/internal/config"
	"github.com/yuanshuai1122/vodoge/internal/cscall"
)

type routingVoiceAgent struct {
	byes    chan voicehost.DialogInfo
	cancels chan voicehost.DialogInfo
}

func (a *routingVoiceAgent) EndVoiceCall(_ context.Context, info voicehost.DialogInfo) error {
	a.byes <- info
	return nil
}

func (a *routingVoiceAgent) CancelVoiceCall(_ context.Context, info voicehost.DialogInfo) error {
	a.cancels <- info
	return nil
}

func newDialogRequest(method sip.RequestMethod, callID string) *sip.Request {
	req := sip.NewRequest(method, sip.Uri{Scheme: "sip", User: "device", Host: "vodoge.local"})
	req.AppendHeader(sip.NewHeader("Call-ID", callID))
	return req
}

func newActiveVoWiFiRoutingPool(t *testing.T, deviceID string, gateway *voicehost.Gateway) *Pool {
	t.Helper()
	p := NewPool(&config.Config{})
	t.Cleanup(p.cancel)
	p.SetVoiceGateway(gateway)
	p.voWiFiHost().RuntimeStore().SetInstance(deviceID, &runtimehost.Instance{})
	return p
}

func TestVoWiFiByeRoutesToIMSBeforeCSBridge(t *testing.T) {
	agent := &routingVoiceAgent{byes: make(chan voicehost.DialogInfo, 1), cancels: make(chan voicehost.DialogInfo, 1)}
	gateway := voicehost.NewGateway()
	gateway.RegisterAgent("dev-ims", agent)
	p := newActiveVoWiFiRoutingPool(t, "dev-ims", gateway)
	p.workers["dev-ims"] = &Worker{ID: "dev-ims", CSCallMgr: &cscall.Manager{}}

	callID := "ims-bye-1"
	p.routeClientBye("dev-ims", newDialogRequest(sip.BYE, callID), nil)

	select {
	case dialog := <-agent.byes:
		if dialog.CallID != callID {
			t.Fatalf("IMS BYE call id=%q, want %q", dialog.CallID, callID)
		}
	case <-time.After(time.Second):
		t.Fatal("IMS BYE handler was not called")
	}
}

func TestVoWiFiCancelRoutesToIMSBeforeCSBridge(t *testing.T) {
	agent := &routingVoiceAgent{byes: make(chan voicehost.DialogInfo, 1), cancels: make(chan voicehost.DialogInfo, 1)}
	gateway := voicehost.NewGateway()
	gateway.RegisterAgent("dev-ims", agent)
	p := newActiveVoWiFiRoutingPool(t, "dev-ims", gateway)
	p.workers["dev-ims"] = &Worker{ID: "dev-ims", CSCallMgr: &cscall.Manager{}}

	callID := "ims-cancel-1"
	p.routeClientCancel("dev-ims", newDialogRequest(sip.CANCEL, callID), nil)

	select {
	case dialog := <-agent.cancels:
		if dialog.CallID != callID {
			t.Fatalf("IMS CANCEL call id=%q, want %q", dialog.CallID, callID)
		}
	case <-time.After(time.Second):
		t.Fatal("IMS CANCEL handler was not called")
	}
}

func TestVoWiFiStaleAgentDoesNotReceiveCSDialogRequests(t *testing.T) {
	agent := &routingVoiceAgent{byes: make(chan voicehost.DialogInfo, 1), cancels: make(chan voicehost.DialogInfo, 1)}
	gateway := voicehost.NewGateway()
	gateway.RegisterAgent("dev-cs", agent)
	p := NewPool(&config.Config{})
	t.Cleanup(p.cancel)
	p.SetVoiceGateway(gateway)
	// The worker still has a registered IMS agent, but no active VoWiFi
	// runtime. A CS call must not be handed to that stale agent.
	p.workers["dev-cs"] = &Worker{ID: "dev-cs", CSCallMgr: &cscall.Manager{}}

	p.routeClientBye("dev-cs", newDialogRequest(sip.BYE, "cs-bye-1"), nil)
	p.routeClientCancel("dev-cs", newDialogRequest(sip.CANCEL, "cs-cancel-1"), nil)

	select {
	case <-agent.byes:
		t.Fatal("stale IMS agent received CS BYE")
	case <-agent.cancels:
		t.Fatal("stale IMS agent received CS CANCEL")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestVoWiFiRoutingRequiresActiveRuntime(t *testing.T) {
	gateway := voicehost.NewGateway()
	gateway.RegisterAgent("dev-route", &routingVoiceAgent{byes: make(chan voicehost.DialogInfo, 1), cancels: make(chan voicehost.DialogInfo, 1)})
	p := NewPool(&config.Config{})
	t.Cleanup(p.cancel)
	p.SetVoiceGateway(gateway)

	if p.hasActiveIMSVoiceRoute("dev-route", gateway) {
		t.Fatal("registered but inactive IMS agent should not own SIP routing")
	}
	p.voWiFiHost().RuntimeStore().SetInstance("dev-route", &runtimehost.Instance{})
	if !p.hasActiveIMSVoiceRoute("dev-route", gateway) {
		t.Fatal("active VoWiFi runtime with an IMS agent should own SIP routing")
	}
}
