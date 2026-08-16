package cscall

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/yuanshuai1122/vodoge/internal/sipgw"
)

type managerStopController struct {
	events       chan Event
	pcmReady     chan bool
	hangupCalls  int
	stopCalls    int
	hangupCallID string
	hangupOpts   HangupOptions
}

func newManagerStopController() *managerStopController {
	return &managerStopController{
		events:   make(chan Event),
		pcmReady: make(chan bool),
	}
}

func (*managerStopController) Start(context.Context) error { return nil }
func (c *managerStopController) Stop()                     { c.stopCalls++ }
func (*managerStopController) Dial(context.Context, string) (CallRef, error) {
	return CallRef{}, nil
}
func (*managerStopController) Answer(context.Context, string) error { return nil }
func (c *managerStopController) Hangup(_ context.Context, callID string, opts HangupOptions) error {
	c.hangupCalls++
	c.hangupCallID = callID
	c.hangupOpts = opts
	return nil
}
func (*managerStopController) GetCalls(context.Context) ([]CallInfo, error) { return nil, nil }
func (c *managerStopController) Events() <-chan Event                       { return c.events }
func (c *managerStopController) PCMReady() <-chan bool                      { return c.pcmReady }

type blockingDialController struct {
	mu          sync.Mutex
	events      chan Event
	pcmReady    chan bool
	dialStarted chan struct{}
	dialRelease chan struct{}
	hangupIDs   []string
	stopCalls   int
}

type blockingAnswerController struct {
	mu            sync.Mutex
	events        chan Event
	pcmReady      chan bool
	answerStarted chan struct{}
	answerRelease chan struct{}
	hangupIDs     []string
	stopCalls     int
}

func newBlockingAnswerController() *blockingAnswerController {
	return &blockingAnswerController{
		events:        make(chan Event),
		pcmReady:      make(chan bool),
		answerStarted: make(chan struct{}),
		answerRelease: make(chan struct{}),
	}
}

func (*blockingAnswerController) Start(context.Context) error { return nil }
func (c *blockingAnswerController) Stop() {
	c.mu.Lock()
	c.stopCalls++
	c.mu.Unlock()
}
func (*blockingAnswerController) Dial(context.Context, string) (CallRef, error) {
	return CallRef{}, nil
}
func (c *blockingAnswerController) Answer(context.Context, string) error {
	close(c.answerStarted)
	<-c.answerRelease // Deliberately ignores cancellation to exercise compensation.
	return nil
}
func (c *blockingAnswerController) Hangup(_ context.Context, callID string, _ HangupOptions) error {
	c.mu.Lock()
	c.hangupIDs = append(c.hangupIDs, callID)
	c.mu.Unlock()
	return nil
}
func (*blockingAnswerController) GetCalls(context.Context) ([]CallInfo, error) { return nil, nil }
func (c *blockingAnswerController) Events() <-chan Event                       { return c.events }
func (c *blockingAnswerController) PCMReady() <-chan bool                      { return c.pcmReady }
func (c *blockingAnswerController) snapshot() ([]string, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.hangupIDs...), c.stopCalls
}

func newBlockingDialController() *blockingDialController {
	return &blockingDialController{
		events:      make(chan Event),
		pcmReady:    make(chan bool),
		dialStarted: make(chan struct{}),
		dialRelease: make(chan struct{}),
	}
}

func (*blockingDialController) Start(context.Context) error { return nil }
func (c *blockingDialController) Stop() {
	c.mu.Lock()
	c.stopCalls++
	c.mu.Unlock()
}
func (c *blockingDialController) Dial(context.Context, string) (CallRef, error) {
	close(c.dialStarted)
	<-c.dialRelease // Deliberately ignores cancellation to exercise Stop's workflow wait.
	return CallRef{ID: "call-9", Number: "+123"}, nil
}
func (*blockingDialController) Answer(context.Context, string) error { return nil }
func (c *blockingDialController) Hangup(_ context.Context, callID string, _ HangupOptions) error {
	c.mu.Lock()
	c.hangupIDs = append(c.hangupIDs, callID)
	c.mu.Unlock()
	return nil
}
func (*blockingDialController) GetCalls(context.Context) ([]CallInfo, error) { return nil, nil }
func (c *blockingDialController) Events() <-chan Event                       { return c.events }
func (c *blockingDialController) PCMReady() <-chan bool                      { return c.pcmReady }
func (c *blockingDialController) snapshot() ([]string, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.hangupIDs...), c.stopCalls
}

type recordingServerTx struct {
	mu        sync.Mutex
	responses []int
	done      chan struct{}
	once      sync.Once
}

func newRecordingServerTx() *recordingServerTx {
	return &recordingServerTx{done: make(chan struct{})}
}

func (tx *recordingServerTx) Terminate()                      { tx.once.Do(func() { close(tx.done) }) }
func (*recordingServerTx) OnTerminate(sip.FnTxTerminate) bool { return false }
func (tx *recordingServerTx) Done() <-chan struct{}           { return tx.done }
func (*recordingServerTx) Err() error                         { return nil }
func (tx *recordingServerTx) Respond(res *sip.Response) error {
	tx.mu.Lock()
	tx.responses = append(tx.responses, res.StatusCode)
	tx.mu.Unlock()
	return nil
}
func (*recordingServerTx) Acks() <-chan *sip.Request { return nil }
func (*recordingServerTx) OnCancel(sip.FnTxCancel) bool {
	return false
}
func (tx *recordingServerTx) responseCodes() []int {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	return append([]int(nil), tx.responses...)
}

func outboundInviteRequest() *sip.Request {
	req := sip.NewRequest(sip.INVITE, sip.Uri{Scheme: "sip", User: "+123", Host: "vodoge.local"})
	req.AppendHeader(sip.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:5060;branch=z9hG4bK-test"))
	req.AppendHeader(sip.NewHeader("From", "<sip:alice@vodoge.local>;tag=from-test"))
	req.AppendHeader(sip.NewHeader("To", "<sip:+123@vodoge.local>"))
	req.AppendHeader(sip.NewHeader("Call-ID", "outbound-stop-test"))
	req.AppendHeader(sip.NewHeader("CSeq", "1 INVITE"))
	return req
}

func TestHasConnectedCallMatchesEmptyOrExactID(t *testing.T) {
	calls := []CallInfo{{ID: "7", State: CallStateConnected}}
	if !hasConnectedCall(calls, "7") {
		t.Fatal("hasConnectedCall() false for exact connected call")
	}
	if !hasConnectedCall(calls, "") {
		t.Fatal("hasConnectedCall() false for empty desired call")
	}
	if hasConnectedCall(calls, "8") {
		t.Fatal("hasConnectedCall() true for different call id")
	}
}

func TestManagerBeginIncomingCallSetsRingingState(t *testing.T) {
	mgr := &Manager{deviceID: "dev-1", state: CallStateIdle}
	sipCallID, shouldStart := mgr.beginIncomingCall("at", "+123")
	if !shouldStart {
		t.Fatal("shouldStart=false want true")
	}
	if sipCallID == "" {
		t.Fatal("sipCallID is empty")
	}
	if mgr.state != CallStateRinging {
		t.Fatalf("state=%v want ringing", mgr.state)
	}
	if mgr.callerID != "+123" {
		t.Fatalf("callerID=%q want +123", mgr.callerID)
	}
	if mgr.controllerCallID != "at" {
		t.Fatalf("controllerCallID=%q want at", mgr.controllerCallID)
	}
}

func TestManagerStopEndsActiveCallAndReleasesAudio(t *testing.T) {
	rtpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	rtpAddr := rtpConn.LocalAddr().(*net.UDPAddr)
	audio := &AudioBridge{rtpConn: rtpConn, stop: make(chan struct{})}
	controller := newManagerStopController()
	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	callCanceled := false
	mgr := &Manager{
		deviceID:         "dev-stop",
		controller:       controller,
		state:            CallStateConnected,
		controllerCallID: "call-7",
		currentCall: &CSCall{
			audio:      audio,
			cancelFunc: func() { callCanceled = true },
		},
		monitorCtx:    monitorCtx,
		monitorCancel: monitorCancel,
	}

	const stopCallers = 16
	var stopWG sync.WaitGroup
	for i := 0; i < stopCallers; i++ {
		stopWG.Add(1)
		go func() {
			defer stopWG.Done()
			mgr.Stop()
		}()
	}
	stopWG.Wait()

	if mgr.state != CallStateIdle || mgr.currentCall != nil {
		t.Fatalf("call state after Stop = %v, currentCall=%v", mgr.state, mgr.currentCall)
	}
	if !callCanceled {
		t.Fatal("active call context was not canceled")
	}
	select {
	case <-audio.stop:
	default:
		t.Fatal("AudioBridge was not stopped")
	}
	if _, err := rtpConn.WriteToUDP([]byte{0}, rtpAddr); err == nil {
		t.Fatal("RTP socket remained writable after Stop")
	}
	select {
	case <-monitorCtx.Done():
	default:
		t.Fatal("manager monitor context was not canceled")
	}
	if controller.hangupCalls != 1 || controller.hangupCallID != "call-7" || !controller.hangupOpts.SendModemSignal {
		t.Fatalf("Hangup calls=%d callID=%q opts=%+v", controller.hangupCalls, controller.hangupCallID, controller.hangupOpts)
	}
	if controller.stopCalls != 1 {
		t.Fatalf("controller Stop calls=%d want 1", controller.stopCalls)
	}
	if sipCallID, ok := mgr.beginIncomingCall("late-call", "+123"); ok || sipCallID != "" {
		t.Fatalf("late incoming call accepted after Stop: callID=%q ok=%v", sipCallID, ok)
	}
	mgr.handleControllerEvent(Event{Type: EventConnected, CallID: "late-call"})
	if mgr.state != CallStateIdle || mgr.currentCall != nil {
		t.Fatalf("late controller event changed stopped manager: state=%v currentCall=%v", mgr.state, mgr.currentCall)
	}
}

func TestManagerStopWaitsForBlockedOutboundWorkflow(t *testing.T) {
	controller := newBlockingDialController()
	registrar, err := sipgw.NewRegistrar(sipgw.Config{SIP: sipgw.SIPConfig{ExternalIP: "127.0.0.1"}})
	if err != nil {
		t.Fatalf("NewRegistrar() error = %v", err)
	}
	defer registrar.Stop()
	mgr := NewManagerWithController("dev-blocked-dial", "hw:test", controller, registrar)
	tx := newRecordingServerTx()
	req := outboundInviteRequest()

	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		mgr.HandleOutboundInvite("dev-blocked-dial", req, tx)
	}()
	select {
	case <-controller.dialStarted:
	case <-time.After(time.Second):
		t.Fatal("outbound workflow did not reach blocked Dial")
	}

	stopDone := make(chan struct{})
	go func() {
		mgr.Stop()
		close(stopDone)
	}()
	select {
	case <-stopDone:
		t.Fatal("Stop returned while Dial workflow was still blocked")
	case <-time.After(20 * time.Millisecond):
	}

	close(controller.dialRelease)
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("outbound handler did not finish after Dial was released")
	}
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish after outbound workflow settled")
	}

	for _, code := range tx.responseCodes() {
		if code == 180 || code == 200 {
			t.Fatalf("response %d was emitted after shutdown began", code)
		}
	}
	if got := tx.responseCodes(); len(got) != 1 || got[0] != 503 {
		t.Fatalf("responses during blocked Dial shutdown=%v, want exactly [503]", got)
	}
	hangupIDs, stopCalls := controller.snapshot()
	if stopCalls != 1 {
		t.Fatalf("controller Stop calls=%d want 1", stopCalls)
	}
	foundDialHangup := false
	for _, callID := range hangupIDs {
		if callID == "call-9" {
			foundDialHangup = true
		}
	}
	if !foundDialHangup {
		t.Fatalf("Dial returned after shutdown without compensating hangup: IDs=%v", hangupIDs)
	}

	responsesBefore := len(tx.responseCodes())
	mgr.HandleOutboundInvite("dev-blocked-dial", outboundInviteRequest(), tx)
	responsesAfter := tx.responseCodes()
	if got := len(responsesAfter); got != responsesBefore+1 || responsesAfter[got-1] != 503 {
		t.Fatalf("stopped manager responses=%v, want one final 503", responsesAfter)
	}
}

func TestManagerOutboundInviteAdmittedBeforeStopResponds503(t *testing.T) {
	mgr := NewManagerWithController("dev-admitted-stop", "hw:test", nil, nil)
	if !mgr.beginCallWorkflow() {
		t.Fatal("failed to admit outbound workflow")
	}

	stopDone := make(chan struct{})
	go func() {
		mgr.Stop()
		close(stopDone)
	}()
	deadline := time.Now().Add(time.Second)
	for {
		mgr.mu.Lock()
		stopped := mgr.stopped
		mgr.mu.Unlock()
		if stopped {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Stop did not seal manager")
		}
		time.Sleep(time.Millisecond)
	}

	tx := newRecordingServerTx()
	mgr.handleOutboundInvite(outboundInviteRequest(), tx)
	if got := tx.responseCodes(); len(got) != 1 || got[0] != 503 {
		t.Fatalf("admitted workflow responses=%v, want exactly [503]", got)
	}
	select {
	case <-stopDone:
		t.Fatal("Stop returned before admitted workflow was released")
	default:
	}

	mgr.workflowWG.Done()
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish after admitted workflow settled")
	}
}

func TestManagerStopResponds503WhileAudioBridgeCreationIsBlocked(t *testing.T) {
	controller := newManagerStopController()
	registrar, err := sipgw.NewRegistrar(sipgw.Config{SIP: sipgw.SIPConfig{ExternalIP: "127.0.0.1"}})
	if err != nil {
		t.Fatalf("NewRegistrar() error = %v", err)
	}
	defer registrar.Stop()
	mgr := NewManagerWithController("dev-blocked-audio", "hw:test", controller, registrar)
	factoryStarted := make(chan struct{})
	factoryRelease := make(chan struct{})
	mgr.newAudio = func(string, string) (*AudioBridge, error) {
		close(factoryStarted)
		<-factoryRelease
		return nil, errors.New("audio factory released after shutdown")
	}
	tx := newRecordingServerTx()

	handlerDone := make(chan struct{})
	go func() {
		mgr.HandleOutboundInvite("dev-blocked-audio", outboundInviteRequest(), tx)
		close(handlerDone)
	}()
	select {
	case <-factoryStarted:
	case <-time.After(time.Second):
		t.Fatal("outbound workflow did not reach blocked AudioBridge creation")
	}
	mgr.mu.Lock()
	state, call := mgr.state, mgr.currentCall
	mgr.mu.Unlock()
	if state != CallStateDialing || call == nil || call.serverTx != tx {
		t.Fatalf("pending call was not atomically attached: state=%v call=%v", state, call)
	}

	stopDone := make(chan struct{})
	go func() {
		mgr.Stop()
		close(stopDone)
	}()
	deadline := time.Now().Add(time.Second)
	for len(tx.responseCodes()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := tx.responseCodes(); len(got) != 1 || got[0] != 503 {
		t.Fatalf("blocked AudioBridge shutdown responses=%v, want exactly [503]", got)
	}
	select {
	case <-tx.Done():
	case <-time.After(time.Second):
		t.Fatal("Stop did not terminate pending server transaction")
	}
	select {
	case <-stopDone:
		t.Fatal("Stop returned while AudioBridge creation was still blocked")
	default:
	}

	close(factoryRelease)
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("outbound handler did not finish after AudioBridge factory release")
	}
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish after AudioBridge workflow settled")
	}
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if mgr.state != CallStateIdle || mgr.currentCall != nil {
		t.Fatalf("call state after Stop = %v, currentCall=%v", mgr.state, mgr.currentCall)
	}
}

func TestManagerStopCompensatesBlockedAnswerWithoutHoldingManagerLock(t *testing.T) {
	rtpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	rtpAddr := rtpConn.LocalAddr().(*net.UDPAddr)
	audio := &AudioBridge{rtpConn: rtpConn, stop: make(chan struct{})}
	controller := newBlockingAnswerController()
	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	call := &CSCall{audio: audio}
	mgr := &Manager{
		deviceID:         "dev-blocked-answer",
		controller:       controller,
		state:            CallStateConnected,
		controllerCallID: "call-11",
		currentCall:      call,
		monitorCtx:       monitorCtx,
		monitorCancel:    monitorCancel,
	}
	if !mgr.beginCallWorkflow() {
		t.Fatal("failed to register answer workflow")
	}
	answerErr := make(chan error, 1)
	go func() {
		defer mgr.workflowWG.Done()
		answerErr <- mgr.answerCallController(call)
	}()
	select {
	case <-controller.answerStarted:
	case <-time.After(time.Second):
		t.Fatal("answer workflow did not reach blocked controller call")
	}

	stopDone := make(chan struct{})
	go func() {
		mgr.Stop()
		close(stopDone)
	}()
	select {
	case <-audio.stop:
	case <-time.After(time.Second):
		t.Fatal("Stop could not seal the manager and release audio while Answer was blocked")
	}
	select {
	case <-stopDone:
		t.Fatal("Stop returned before the blocked Answer workflow settled")
	default:
	}
	if _, err := rtpConn.WriteToUDP([]byte{0}, rtpAddr); err == nil {
		t.Fatal("RTP socket remained writable while Stop waited for Answer")
	}

	close(controller.answerRelease)
	select {
	case err := <-answerErr:
		if err != context.Canceled {
			t.Fatalf("answer workflow error=%v want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("answer workflow did not finish after release")
	}
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish after Answer compensation")
	}

	hangupIDs, stopCalls := controller.snapshot()
	if stopCalls != 1 {
		t.Fatalf("controller Stop calls=%d want 1", stopCalls)
	}
	hangupsForCall := 0
	for _, callID := range hangupIDs {
		if callID == "call-11" {
			hangupsForCall++
		}
	}
	if hangupsForCall < 2 {
		t.Fatalf("blocked Answer was not compensated after shutdown: IDs=%v", hangupIDs)
	}
}
