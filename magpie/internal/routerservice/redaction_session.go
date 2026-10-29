package routerservice

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"time"
)

const redactionSessionHeader = "X-Router-Redaction-Session"

// A body session_id or a history prefix proves no ownership. This opaque handle
// is signed by this service instance and bound to the authenticated application.
// Restart invalidates it; clients that do not echo it use the uncached path.
func (r *RedactionService) requestSession(app, presented string) SessionInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return SessionInfo{}
	}
	if r.sessionKey == nil {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return SessionInfo{}
		}
		r.sessionKey = key
	}
	sign := func(payload []byte) []byte {
		mac := hmac.New(sha256.New, r.sessionKey)
		mac.Write([]byte(app))
		mac.Write([]byte{0})
		mac.Write(payload)
		return mac.Sum(nil)
	}
	now := time.Now().Unix()
	if data, err := base64.RawURLEncoding.DecodeString(presented); err == nil && len(data) == 64 {
		issued := int64(binary.BigEndian.Uint64(data[:8]))
		if issued <= now && now-issued < 900 && hmac.Equal(data[32:], sign(data[:32])) {
			return SessionInfo{ID: presented, Verified: true}
		}
	}
	payload := make([]byte, 32)
	binary.BigEndian.PutUint64(payload, uint64(now))
	if _, err := rand.Read(payload[8:]); err != nil {
		return SessionInfo{}
	}
	return SessionInfo{ID: base64.RawURLEncoding.EncodeToString(append(payload, sign(payload)...)), Verified: true}
}
