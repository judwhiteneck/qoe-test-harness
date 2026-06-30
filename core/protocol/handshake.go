package protocol

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
)

// CookieSize is the length of a return-routability cookie.
const CookieSize = 16

// MakeCookie binds a session to the client's observed source address. The server
// hands this out in response to a HELLO and requires it back (from the same
// source) before sending any high-rate flow — the anti-amplification gate (spec
// G1). Pure: the caller passes the observed source bytes; this never touches a
// socket. The cookie is unforgeable without the server secret.
func MakeCookie(secret []byte, session uint64, clientAddr []byte) [CookieSize]byte {
	m := hmac.New(sha256.New, secret)
	var s [8]byte
	binary.BigEndian.PutUint64(s[:], session)
	_, _ = m.Write(s[:])
	_, _ = m.Write(clientAddr)
	var out [CookieSize]byte
	copy(out[:], m.Sum(nil))
	return out
}

// VerifyCookie reports whether got is the cookie for (session, clientAddr).
// Constant-time to avoid leaking via timing.
func VerifyCookie(secret []byte, session uint64, clientAddr, got []byte) bool {
	if len(got) != CookieSize {
		return false
	}
	want := MakeCookie(secret, session, clientAddr)
	return subtle.ConstantTimeCompare(want[:], got) == 1
}
