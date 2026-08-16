package sipgw

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
)

const (
	sipNonceTTL        = 60 * time.Second
	inviteAuthTTL      = 24 * time.Hour
	sipDigestAlgorithm = "MD5"
	sipDigestQOP       = "auth"
)

type sipAuthNonce struct {
	username string
	method   sip.RequestMethod
	uri      string
	expires  time.Time
}

type authenticatedInvite struct {
	username  string
	source    string
	transport string
	fromTag   string
	cseq      uint32
	expires   time.Time
}

func (r *Registrar) currentTime() time.Time {
	if r.clock != nil {
		return r.clock()
	}
	return time.Now()
}

// authenticateInitialRequest challenges requests that create server-side state.
// ACK and CANCEL deliberately do not use this path; RFC 3261 does not permit a
// normal challenge/retry exchange for those methods.
func (r *Registrar) authenticateInitialRequest(req *sip.Request, tx sip.ServerTransaction) (string, bool) {
	username := extractUsername(req.From())
	if username == "" {
		r.respond(tx, req, 400, "Bad Request - Missing From User")
		return "", false
	}

	authHeader := req.GetHeader("Authorization")
	if authHeader == nil || !r.validateSIPDigest(req, username, authHeader.Value()) {
		r.sendAuthChallenge(tx, req, username)
		return "", false
	}
	return username, true
}

func (r *Registrar) sendAuthChallenge(tx sip.ServerTransaction, req *sip.Request, username string) {
	nonce, err := generateSIPNonce()
	if err != nil {
		r.respond(tx, req, 500, "Internal Server Error")
		return
	}
	state := sipAuthNonce{
		username: username,
		method:   req.Method,
		uri:      req.Recipient.String(),
		expires:  r.currentTime().Add(sipNonceTTL),
	}

	r.mu.Lock()
	r.nonces[nonce] = state
	r.mu.Unlock()

	res := sip.NewResponseFromRequest(req, 401, "Unauthorized", nil)
	authValue := fmt.Sprintf(
		`Digest realm="%s", nonce="%s", algorithm=%s, qop="%s"`,
		quoteDigestValue(r.cfg.SIP.Realm), nonce, sipDigestAlgorithm, sipDigestQOP,
	)
	res.AppendHeader(sip.NewHeader("WWW-Authenticate", authValue))
	if err := tx.Respond(res); err != nil {
		return
	}
}

func (r *Registrar) validateSIPDigest(req *sip.Request, username, authValue string) bool {
	params, err := parseSIPDigestAuthorization(authValue)
	if err != nil {
		return false
	}
	actualURI := req.Recipient.String()
	algorithm := params["algorithm"]
	if params["username"] != username ||
		params["realm"] != r.cfg.SIP.Realm ||
		params["uri"] != actualURI ||
		!strings.EqualFold(algorithm, sipDigestAlgorithm) ||
		params["qop"] != sipDigestQOP ||
		params["cnonce"] == "" {
		return false
	}

	ncText := params["nc"]
	if len(ncText) != 8 {
		return false
	}
	nc, err := strconv.ParseUint(ncText, 16, 32)
	if err != nil || nc == 0 {
		return false
	}

	nonce := params["nonce"]
	now := r.currentTime()
	r.mu.Lock()
	nonceState, ok := r.nonces[nonce]
	if ok && !now.Before(nonceState.expires) {
		delete(r.nonces, nonce)
		ok = false
	}
	r.mu.Unlock()
	if !ok || nonceState.username != username || nonceState.method != req.Method || nonceState.uri != actualURI {
		return false
	}

	userCfg := r.findUserConfig(username)
	if userCfg == nil {
		return false
	}
	ha1 := r.md5sum(username + ":" + r.cfg.SIP.Realm + ":" + userCfg.Password)
	ha2 := r.md5sum(string(req.Method) + ":" + actualURI)
	expected := r.md5sum(ha1 + ":" + nonce + ":" + ncText + ":" + params["cnonce"] + ":" + sipDigestQOP + ":" + ha2)
	gotBytes, err := hex.DecodeString(params["response"])
	if err != nil {
		return false
	}
	expectedBytes, _ := hex.DecodeString(expected)
	if subtle.ConstantTimeCompare(gotBytes, expectedBytes) != 1 {
		return false
	}

	// Consume only after the digest is valid. The lock makes two concurrent
	// replays race to a single winner without letting bad guesses burn a nonce.
	r.mu.Lock()
	current, ok := r.nonces[nonce]
	if ok && r.currentTime().Before(current.expires) && current == nonceState {
		delete(r.nonces, nonce)
	} else {
		ok = false
	}
	r.mu.Unlock()
	return ok
}

func (r *Registrar) registeredUserForRequest(username string, req *sip.Request) *RegisteredUser {
	r.mu.RLock()
	user := r.users[username]
	if user == nil || !r.currentTime().Before(user.Expires) {
		r.mu.RUnlock()
		return nil
	}
	copyOfUser := *user
	r.mu.RUnlock()

	if copyOfUser.ContactAddr == nil || copyOfUser.Source == "" ||
		!sameSIPSource(copyOfUser.Source, req.Source()) ||
		(copyOfUser.Transport != "" && !strings.EqualFold(copyOfUser.Transport, req.Transport())) {
		return nil
	}
	return &copyOfUser
}

func (r *Registrar) dialogUser(req *sip.Request) *RegisteredUser {
	if !hasToTag(req) {
		return nil
	}
	return r.registeredUserForRequest(extractUsername(req.From()), req)
}

func (r *Registrar) rememberAuthenticatedInvite(req *sip.Request, username string) {
	callID := requestCallID(req)
	fromTag := requestFromTag(req)
	cseq := req.CSeq()
	if callID == "" || fromTag == "" || cseq == nil {
		return
	}
	r.mu.Lock()
	r.authenticatedInvites[inviteAuthKey(username, callID)] = authenticatedInvite{
		username:  username,
		source:    req.Source(),
		transport: req.Transport(),
		fromTag:   fromTag,
		cseq:      cseq.SeqNo,
		expires:   r.currentTime().Add(inviteAuthTTL),
	}
	r.mu.Unlock()
}

func (r *Registrar) inviteRelatedUser(req *sip.Request) *RegisteredUser {
	username := extractUsername(req.From())
	user := r.registeredUserForRequest(username, req)
	if user == nil {
		return nil
	}
	callID := requestCallID(req)
	cseq := req.CSeq()
	if callID == "" || cseq == nil {
		return nil
	}

	key := inviteAuthKey(username, callID)
	r.mu.Lock()
	state, ok := r.authenticatedInvites[key]
	if ok && !r.currentTime().Before(state.expires) {
		delete(r.authenticatedInvites, key)
		ok = false
	}
	r.mu.Unlock()
	if !ok || state.username != username || state.fromTag != requestFromTag(req) ||
		state.cseq != cseq.SeqNo || !sameSIPSource(state.source, req.Source()) ||
		!strings.EqualFold(state.transport, req.Transport()) {
		return nil
	}
	return user
}

func (r *Registrar) forgetAuthenticatedInvite(req *sip.Request) {
	key := inviteAuthKey(extractUsername(req.From()), requestCallID(req))
	r.mu.Lock()
	delete(r.authenticatedInvites, key)
	r.mu.Unlock()
}

func hasToTag(req *sip.Request) bool {
	if req == nil || req.To() == nil {
		return false
	}
	tag, ok := req.To().Params.Get("tag")
	return ok && strings.TrimSpace(tag) != ""
}

func requestFromTag(req *sip.Request) string {
	if req == nil || req.From() == nil {
		return ""
	}
	tag, _ := req.From().Params.Get("tag")
	return strings.TrimSpace(tag)
}

func requestCallID(req *sip.Request) string {
	if req == nil || req.CallID() == nil {
		return ""
	}
	return strings.TrimSpace(req.CallID().Value())
}

func inviteAuthKey(username, callID string) string {
	return username + "\x00" + callID
}

func sameSIPSource(want, got string) bool {
	wantAddr, err := netip.ParseAddrPort(strings.TrimSpace(want))
	if err != nil {
		return false
	}
	gotAddr, err := netip.ParseAddrPort(strings.TrimSpace(got))
	if err != nil {
		return false
	}
	return wantAddr.Addr().Unmap() == gotAddr.Addr().Unmap() && wantAddr.Port() == gotAddr.Port()
}

func generateSIPNonce() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate SIP digest nonce: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func quoteDigestValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func parseSIPDigestAuthorization(value string) (map[string]string, error) {
	value = strings.TrimSpace(value)
	space := strings.IndexAny(value, " \t")
	if space < 0 || !strings.EqualFold(value[:space], "Digest") {
		return nil, fmt.Errorf("invalid digest authorization scheme")
	}
	value = strings.TrimSpace(value[space+1:])
	params := make(map[string]string)

	for len(value) > 0 {
		value = strings.TrimLeft(value, " \t,")
		if value == "" {
			break
		}
		eq := strings.IndexByte(value, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("invalid digest parameter")
		}
		key := strings.ToLower(strings.TrimSpace(value[:eq]))
		if key == "" || strings.ContainsAny(key, " \t,") {
			return nil, fmt.Errorf("invalid digest parameter name")
		}
		if _, duplicate := params[key]; duplicate {
			return nil, fmt.Errorf("duplicate digest parameter %q", key)
		}
		value = strings.TrimLeft(value[eq+1:], " \t")

		var parsed string
		if strings.HasPrefix(value, `"`) {
			var out strings.Builder
			closed := false
			for i := 1; i < len(value); i++ {
				switch value[i] {
				case '\\':
					if i+1 >= len(value) {
						return nil, fmt.Errorf("invalid digest quoted escape")
					}
					i++
					out.WriteByte(value[i])
				case '"':
					parsed = out.String()
					value = strings.TrimLeft(value[i+1:], " \t")
					closed = true
				default:
					out.WriteByte(value[i])
				}
				if closed {
					break
				}
			}
			if !closed {
				return nil, fmt.Errorf("unterminated digest quoted value")
			}
			if value != "" && value[0] != ',' {
				return nil, fmt.Errorf("invalid digest parameter separator")
			}
		} else {
			comma := strings.IndexByte(value, ',')
			if comma < 0 {
				parsed = strings.TrimSpace(value)
				value = ""
			} else {
				parsed = strings.TrimSpace(value[:comma])
				value = value[comma:]
			}
		}
		if parsed == "" {
			return nil, fmt.Errorf("empty digest parameter %q", key)
		}
		params[key] = parsed
	}
	return params, nil
}
