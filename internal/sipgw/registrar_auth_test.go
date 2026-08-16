package sipgw

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
)

const (
	testSIPSource      = "192.0.2.10:5062"
	testSIPOtherSource = "192.0.2.99:5090"
	testSIPPassword    = "correct horse battery staple"
)

func TestUnauthenticatedInviteIsChallenged(t *testing.T) {
	r := newAuthTestRegistrar(t)
	seedRegisteredUser(t, r)
	called := make(chan struct{}, 1)
	r.SetOnInvite(func(string, *sip.Request, sip.ServerTransaction) {
		called <- struct{}{}
	})

	req := newAuthTestRequest(sip.INVITE, "alice", "10086", 1)
	challengeNonce(t, req, r.handleInvite)
	assertNoSignal(t, called, "unauthenticated INVITE reached call callback")
}

func TestUnauthenticatedPublishIsChallenged(t *testing.T) {
	r := newAuthTestRegistrar(t)
	seedRegisteredUser(t, r)

	req := newAuthTestRequest(sip.PUBLISH, "alice", "alice", 1)
	challengeNonce(t, req, r.handlePublish)
}

func TestSIPDigestRejectsMismatchedRequestBindings(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]string
	}{
		{name: "method", overrides: map[string]string{"method": string(sip.REGISTER)}},
		{name: "request URI", overrides: map[string]string{"uri": "sip:other@vodoge.local"}},
		{name: "username", overrides: map[string]string{"username": "bob"}},
		{name: "realm", overrides: map[string]string{"realm": "other-realm"}},
		{name: "algorithm", overrides: map[string]string{"algorithm": "SHA-256"}},
		{name: "qop", overrides: map[string]string{"qop": "auth-int"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := newAuthTestRegistrar(t)
			seedRegisteredUser(t, r)
			called := make(chan struct{}, 1)
			r.SetOnInvite(func(string, *sip.Request, sip.ServerTransaction) {
				called <- struct{}{}
			})

			challengeReq := newAuthTestRequest(sip.INVITE, "alice", "10086", 1)
			nonce := challengeNonce(t, challengeReq, r.handleInvite)
			authReq := newAuthTestRequest(sip.INVITE, "alice", "10086", 2)
			addDigestAuthorization(r, authReq, nonce, test.overrides)
			tx := siptest.NewServerTxRecorder(authReq)
			r.handleInvite(authReq, tx)

			requireSingleResponseStatus(t, transactionResponses(tx), 401)
			assertNoSignal(t, called, "invalid Digest INVITE reached call callback")
		})
	}
}

func TestValidRegisterAuthenticatesAndBindsSource(t *testing.T) {
	r := newAuthTestRegistrar(t)

	challengeReq := newAuthTestRequest(sip.REGISTER, "alice", "alice", 1)
	nonce := challengeNonce(t, challengeReq, r.handleRegister)
	authReq := newAuthTestRequest(sip.REGISTER, "alice", "alice", 2)
	authReq.AppendHeader(&sip.ContactHeader{
		Address: sip.Uri{User: "alice", Host: "192.0.2.10", Port: 5062},
	})
	authReq.AppendHeader(sip.NewHeader("Expires", "3600"))
	addDigestAuthorization(r, authReq, nonce, nil)
	tx := siptest.NewServerTxRecorder(authReq)
	r.handleRegister(authReq, tx)

	requireSingleResponseStatus(t, transactionResponses(tx), 200)
	user := r.GetUserByUsername("alice")
	if user == nil {
		t.Fatal("authenticated REGISTER did not create registration")
	}
	if user.Source != testSIPSource || user.Transport != "UDP" {
		t.Fatalf("registration source=%q transport=%q, want %q UDP", user.Source, user.Transport, testSIPSource)
	}
}

func TestRegisteredUserQueriesReturnDetachedSnapshots(t *testing.T) {
	r := newAuthTestRegistrar(t)
	seedRegisteredUser(t, r)

	user := r.GetUserByUsername("alice")
	if user == nil || user.ContactAddr == nil {
		t.Fatal("registered user snapshot is missing")
	}
	user.Source = "modified:1"
	user.ContactAddr.IP[0] = 203

	stored := r.GetUserByUsername("alice")
	if stored == nil || stored.ContactAddr == nil {
		t.Fatal("stored registered user is missing")
	}
	if stored.Source != testSIPSource {
		t.Fatalf("stored source=%q, want %q", stored.Source, testSIPSource)
	}
	if got := stored.ContactAddr.IP.String(); got != "192.0.2.10" {
		t.Fatalf("stored contact IP=%q, want 192.0.2.10", got)
	}
}

func TestValidInitialInviteReachesCallback(t *testing.T) {
	r := newAuthTestRegistrar(t)
	seedRegisteredUser(t, r)
	completeAuthenticatedInvite(t, r)
}

func TestValidPublishIsAccepted(t *testing.T) {
	r := newAuthTestRegistrar(t)
	seedRegisteredUser(t, r)

	challengeReq := newAuthTestRequest(sip.PUBLISH, "alice", "alice", 1)
	nonce := challengeNonce(t, challengeReq, r.handlePublish)
	authReq := newAuthTestRequest(sip.PUBLISH, "alice", "alice", 2)
	addDigestAuthorization(r, authReq, nonce, nil)
	tx := siptest.NewServerTxRecorder(authReq)
	r.handlePublish(authReq, tx)

	requireSingleResponseStatus(t, transactionResponses(tx), 200)
}

func TestSIPDigestNonceCannotBeReplayed(t *testing.T) {
	r := newAuthTestRegistrar(t)

	challengeReq := newAuthTestRequest(sip.REGISTER, "alice", "alice", 1)
	nonce := challengeNonce(t, challengeReq, r.handleRegister)
	authReq := newAuthTestRequest(sip.REGISTER, "alice", "alice", 2)
	authValue := digestAuthorization(r, authReq, nonce, nil)
	authReq.AppendHeader(sip.NewHeader("Authorization", authValue))
	tx := siptest.NewServerTxRecorder(authReq)
	r.handleRegister(authReq, tx)
	requireSingleResponseStatus(t, transactionResponses(tx), 200)

	replayReq := newAuthTestRequest(sip.REGISTER, "alice", "alice", 3)
	replayReq.AppendHeader(sip.NewHeader("Authorization", authValue))
	replayTx := siptest.NewServerTxRecorder(replayReq)
	r.handleRegister(replayReq, replayTx)
	requireSingleResponseStatus(t, transactionResponses(replayTx), 401)
}

func TestSIPDigestNonceExpires(t *testing.T) {
	r := newAuthTestRegistrar(t)
	now := time.Unix(1_700_000_000, 0)
	r.clock = func() time.Time { return now }

	challengeReq := newAuthTestRequest(sip.REGISTER, "alice", "alice", 1)
	nonce := challengeNonce(t, challengeReq, r.handleRegister)
	now = now.Add(sipNonceTTL + time.Second)
	authReq := newAuthTestRequest(sip.REGISTER, "alice", "alice", 2)
	addDigestAuthorization(r, authReq, nonce, nil)
	tx := siptest.NewServerTxRecorder(authReq)
	r.handleRegister(authReq, tx)

	requireSingleResponseStatus(t, transactionResponses(tx), 401)
}

func TestAuthenticatedInviteRequiresRegisteredSource(t *testing.T) {
	r := newAuthTestRegistrar(t)
	seedRegisteredUser(t, r)
	called := make(chan struct{}, 1)
	r.SetOnInvite(func(string, *sip.Request, sip.ServerTransaction) {
		called <- struct{}{}
	})

	challengeReq := newAuthTestRequest(sip.INVITE, "alice", "10086", 1)
	challengeReq.SetSource(testSIPOtherSource)
	nonce := challengeNonce(t, challengeReq, r.handleInvite)
	authReq := newAuthTestRequest(sip.INVITE, "alice", "10086", 2)
	authReq.SetSource(testSIPOtherSource)
	addDigestAuthorization(r, authReq, nonce, nil)
	tx := siptest.NewServerTxRecorder(authReq)
	r.handleInvite(authReq, tx)

	requireSingleResponseStatus(t, transactionResponses(tx), 403)
	assertNoSignal(t, called, "wrong-source INVITE reached call callback")
}

func TestForgedSourceDialogRequestIsRejected(t *testing.T) {
	r := newAuthTestRegistrar(t)
	seedRegisteredUser(t, r)
	completeAuthenticatedInvite(t, r)
	called := make(chan struct{}, 1)
	r.SetOnBye(func(string, *sip.Request, sip.ServerTransaction) {
		called <- struct{}{}
	})

	bye := newAuthTestRequest(sip.BYE, "alice", "10086", 3)
	addToTag(bye)
	bye.SetSource(testSIPOtherSource)
	tx := siptest.NewServerTxRecorder(bye)
	r.handleBye(bye, tx)

	requireSingleResponseStatus(t, transactionResponses(tx), 481)
	assertNoSignal(t, called, "wrong-source BYE reached dialog callback")
}

func TestInDialogInviteDoesNotRequireFreshDigest(t *testing.T) {
	r := newAuthTestRegistrar(t)
	seedRegisteredUser(t, r)
	completeAuthenticatedInvite(t, r)
	called := make(chan string, 1)
	r.SetOnInvite(func(deviceID string, _ *sip.Request, _ sip.ServerTransaction) {
		called <- deviceID
	})

	reinvite := newAuthTestRequest(sip.INVITE, "alice", "10086", 3)
	addToTag(reinvite)
	tx := siptest.NewServerTxRecorder(reinvite)
	r.handleInvite(reinvite, tx)

	requireSingleResponseStatus(t, transactionResponses(tx), 100)
	if deviceID := waitSignal(t, called, "in-dialog INVITE callback"); deviceID != "device-1" {
		t.Fatalf("callback device=%q, want device-1", deviceID)
	}
}

func TestInvalidAckAndCancelAreNotChallenged(t *testing.T) {
	r := newAuthTestRegistrar(t)
	seedRegisteredUser(t, r)

	ack := newAuthTestRequest(sip.ACK, "alice", "10086", 1)
	ackTx := siptest.NewServerTxRecorder(ack)
	r.handleAck(ack, ackTx)
	if responses := transactionResponses(ackTx); len(responses) != 0 {
		t.Fatalf("invalid ACK responses=%v, want no response", responseCodes(responses))
	}

	cancel := newAuthTestRequest(sip.CANCEL, "alice", "10086", 1)
	cancelTx := siptest.NewServerTxRecorder(cancel)
	r.handleCancel(cancel, cancelTx)
	responses := transactionResponses(cancelTx)
	requireSingleResponseStatus(t, responses, 481)
	if responses[0].GetHeader("WWW-Authenticate") != nil {
		t.Fatal("invalid CANCEL was incorrectly challenged")
	}
}

func TestAckAndCancelInheritAuthenticatedInvite(t *testing.T) {
	r := newAuthTestRegistrar(t)
	seedRegisteredUser(t, r)
	invite := completeAuthenticatedInvite(t, r)
	cancelCalled := make(chan string, 1)
	ackCalled := make(chan string, 1)
	r.SetOnCancel(func(deviceID string, req *sip.Request, tx sip.ServerTransaction) {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
		cancelCalled <- deviceID
	})
	r.SetOnAck(func(deviceID string, _ *sip.Request, _ sip.ServerTransaction) {
		ackCalled <- deviceID
	})

	cancel := newAuthTestRequest(sip.CANCEL, "alice", "10086", invite.CSeq().SeqNo)
	cancelTx := siptest.NewServerTxRecorder(cancel)
	r.handleCancel(cancel, cancelTx)
	if deviceID := waitSignal(t, cancelCalled, "CANCEL callback"); deviceID != "device-1" {
		t.Fatalf("CANCEL callback device=%q, want device-1", deviceID)
	}
	requireSingleResponseStatus(t, transactionResponses(cancelTx), 200)

	ack := newAuthTestRequest(sip.ACK, "alice", "10086", invite.CSeq().SeqNo)
	ackTx := siptest.NewServerTxRecorder(ack)
	r.handleAck(ack, ackTx)
	if deviceID := waitSignal(t, ackCalled, "ACK callback"); deviceID != "device-1" {
		t.Fatalf("ACK callback device=%q, want device-1", deviceID)
	}
	if responses := transactionResponses(ackTx); len(responses) != 0 {
		t.Fatalf("valid ACK responses=%v, want no response", responseCodes(responses))
	}
}

func completeAuthenticatedInvite(t *testing.T, r *Registrar) *sip.Request {
	t.Helper()
	called := make(chan string, 1)
	r.SetOnInvite(func(deviceID string, _ *sip.Request, _ sip.ServerTransaction) {
		called <- deviceID
	})

	challengeReq := newAuthTestRequest(sip.INVITE, "alice", "10086", 1)
	nonce := challengeNonce(t, challengeReq, r.handleInvite)
	authReq := newAuthTestRequest(sip.INVITE, "alice", "10086", 2)
	addDigestAuthorization(r, authReq, nonce, nil)
	tx := siptest.NewServerTxRecorder(authReq)
	r.handleInvite(authReq, tx)

	requireSingleResponseStatus(t, transactionResponses(tx), 100)
	if deviceID := waitSignal(t, called, "authenticated INVITE callback"); deviceID != "device-1" {
		t.Fatalf("callback device=%q, want device-1", deviceID)
	}
	return authReq
}

func challengeNonce(t *testing.T, req *sip.Request, handler func(*sip.Request, sip.ServerTransaction)) string {
	t.Helper()
	tx := siptest.NewServerTxRecorder(req)
	handler(req, tx)
	responses := transactionResponses(tx)
	requireSingleResponseStatus(t, responses, 401)
	header := responses[0].GetHeader("WWW-Authenticate")
	if header == nil {
		t.Fatal("401 response is missing WWW-Authenticate")
	}
	params, err := parseSIPDigestAuthorization(header.Value())
	if err != nil {
		t.Fatalf("parse challenge: %v", err)
	}
	if params["algorithm"] != sipDigestAlgorithm || params["qop"] != sipDigestQOP {
		t.Fatalf("challenge algorithm=%q qop=%q, want %s/%s", params["algorithm"], params["qop"], sipDigestAlgorithm, sipDigestQOP)
	}
	if params["nonce"] == "" {
		t.Fatal("401 challenge has empty nonce")
	}
	return params["nonce"]
}

func digestAuthorization(r *Registrar, req *sip.Request, nonce string, overrides map[string]string) string {
	values := map[string]string{
		"username":  extractUsername(req.From()),
		"realm":     r.cfg.SIP.Realm,
		"uri":       req.Recipient.String(),
		"algorithm": sipDigestAlgorithm,
		"qop":       sipDigestQOP,
		"nc":        "00000001",
		"cnonce":    "test-cnonce",
		"method":    string(req.Method),
	}
	for key, value := range overrides {
		values[key] = value
	}

	ha1 := r.md5sum(values["username"] + ":" + values["realm"] + ":" + testSIPPassword)
	ha2 := r.md5sum(values["method"] + ":" + values["uri"])
	response := r.md5sum(ha1 + ":" + nonce + ":" + values["nc"] + ":" + values["cnonce"] + ":" + values["qop"] + ":" + ha2)
	return fmt.Sprintf(
		`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s", algorithm=%s, qop=%s, nc=%s, cnonce="%s"`,
		quoteDigestValue(values["username"]),
		quoteDigestValue(values["realm"]),
		quoteDigestValue(nonce),
		quoteDigestValue(values["uri"]),
		response,
		values["algorithm"],
		values["qop"],
		values["nc"],
		quoteDigestValue(values["cnonce"]),
	)
}

func addDigestAuthorization(r *Registrar, req *sip.Request, nonce string, overrides map[string]string) {
	req.AppendHeader(sip.NewHeader("Authorization", digestAuthorization(r, req, nonce, overrides)))
}

func seedRegisteredUser(t *testing.T, r *Registrar) {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", testSIPSource)
	if err != nil {
		t.Fatal(err)
	}
	r.registerUser("alice", "device-1", "Alice", "sip:alice@192.0.2.10:5062", addr, testSIPSource, "UDP", "Linphone", 3600, "", "", "", "", "")
}

func newAuthTestRegistrar(t *testing.T) *Registrar {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Users = []UserConfig{{
		Username: "alice",
		Password: testSIPPassword,
		DeviceID: "device-1",
	}}
	r, err := NewRegistrar(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.ua.Close() })
	return r
}

func newAuthTestRequest(method sip.RequestMethod, fromUser, toUser string, cseq uint32) *sip.Request {
	req := sip.NewRequest(method, sip.Uri{User: toUser, Host: "vodoge.local"})
	viaParams := sip.NewParams()
	viaParams.Add("branch", sip.GenerateBranch())
	req.AppendHeader(&sip.ViaHeader{
		ProtocolName:    "SIP",
		ProtocolVersion: "2.0",
		Transport:       "UDP",
		Host:            "192.0.2.10",
		Port:            5062,
		Params:          viaParams,
	})
	fromParams := sip.NewParams()
	fromParams.Add("tag", "from-tag")
	req.AppendHeader(&sip.FromHeader{
		Address: sip.Uri{User: fromUser, Host: "vodoge.local"},
		Params:  fromParams,
	})
	req.AppendHeader(&sip.ToHeader{Address: sip.Uri{User: toUser, Host: "vodoge.local"}})
	callID := sip.CallIDHeader("auth-test-call")
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: cseq, MethodName: method})
	req.SetSource(testSIPSource)
	req.SetTransport("UDP")
	return req
}

func addToTag(req *sip.Request) {
	params := sip.NewParams()
	params.Add("tag", "to-tag")
	req.To().Params = params
}

func transactionResponses(tx *siptest.ServerTxRecorder) []*sip.Response {
	tx.Terminate()
	return tx.Result()
}

func requireSingleResponseStatus(t *testing.T, responses []*sip.Response, want int) {
	t.Helper()
	if len(responses) != 1 || responses[0].StatusCode != want {
		t.Fatalf("responses=%v, want one %d response", responseCodes(responses), want)
	}
}

func responseCodes(responses []*sip.Response) []int {
	codes := make([]int, 0, len(responses))
	for _, response := range responses {
		codes = append(codes, response.StatusCode)
	}
	return codes
}

func waitSignal[T any](t *testing.T, ch <-chan T, name string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		var zero T
		t.Fatalf("timed out waiting for %s", name)
		return zero
	}
}

func assertNoSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal(message)
	case <-time.After(20 * time.Millisecond):
	}
}
