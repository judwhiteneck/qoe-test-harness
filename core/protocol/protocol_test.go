package protocol

import (
	"bytes"
	"testing"
)

func TestHeaderRoundTrip(t *testing.T) {
	h := Header{Type: MsgProbe, Session: 0xDEADBEEFCAFEBABE, Seq: 42}
	buf := make([]byte, HeaderSize)
	n, err := h.Marshal(buf)
	if err != nil || n != HeaderSize {
		t.Fatalf("Marshal n=%d err=%v", n, err)
	}
	got, err := UnmarshalHeader(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got != h {
		t.Fatalf("round trip: got %+v want %+v", got, h)
	}
}

func TestHeaderErrors(t *testing.T) {
	if _, err := UnmarshalHeader([]byte{1, 2}); err != ErrShort {
		t.Errorf("short: got %v want ErrShort", err)
	}
	bad := make([]byte, HeaderSize)
	bad[0], bad[1] = 0xFF, 0xFF // wrong magic
	if _, err := UnmarshalHeader(bad); err != ErrMagic {
		t.Errorf("magic: got %v want ErrMagic", err)
	}
	wrongVer := make([]byte, HeaderSize)
	_, _ = Header{}.Marshal(wrongVer)
	wrongVer[2] = 99
	if _, err := UnmarshalHeader(wrongVer); err != ErrVersion {
		t.Errorf("version: got %v want ErrVersion", err)
	}
	if _, err := (Header{}).Marshal(make([]byte, 3)); err != ErrShort {
		t.Errorf("marshal short: got %v want ErrShort", err)
	}
}

func TestProbeRoundTrip(t *testing.T) {
	p := Probe{Header: Header{Session: 7, Seq: 9}, TSendNanos: 1_700_000_000_000_000_000, TOSIntended: 0x2D<<2 | 0x01}
	buf := make([]byte, ProbeSize)
	if _, err := p.Marshal(buf); err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalProbe(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != MsgProbe || got.Seq != 9 || got.TSendNanos != p.TSendNanos || got.TOSIntended != p.TOSIntended {
		t.Fatalf("probe round trip mismatch: %+v", got)
	}
	if _, err := UnmarshalProbe(buf[:ProbeSize-1]); err != ErrShort {
		t.Errorf("short probe: got %v want ErrShort", err)
	}
	if _, err := (Probe{}).Marshal(make([]byte, 2)); err != ErrShort {
		t.Errorf("marshal short probe: got %v", err)
	}
}

func TestEchoRoundTrip(t *testing.T) {
	e := Echo{Header: Header{Session: 7, Seq: 9}, TSendNanos: 111, TRecvServerNanos: 222, TOSObserved: 0x01, CE: 1}
	buf := make([]byte, EchoSize)
	if _, err := e.Marshal(buf); err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalEcho(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != MsgEcho || got.TSendNanos != 111 || got.TRecvServerNanos != 222 || got.TOSObserved != 1 || got.CE != 1 {
		t.Fatalf("echo round trip mismatch: %+v", got)
	}
	if _, err := UnmarshalEcho(buf[:EchoSize-1]); err != ErrShort {
		t.Errorf("short echo: got %v want ErrShort", err)
	}
	if _, err := (Echo{}).Marshal(make([]byte, 2)); err != ErrShort {
		t.Errorf("marshal short echo: got %v", err)
	}
}

func TestCookie(t *testing.T) {
	secret := []byte("server-secret-key")
	addr := []byte{203, 0, 113, 7}
	c := MakeCookie(secret, 1234, addr)

	// Deterministic.
	if c2 := MakeCookie(secret, 1234, addr); !bytes.Equal(c[:], c2[:]) {
		t.Fatal("cookie not deterministic")
	}
	// Valid round trip.
	if !VerifyCookie(secret, 1234, addr, c[:]) {
		t.Fatal("valid cookie rejected")
	}
	// Wrong source address (the anti-amplification property: a spoofed source
	// can't reuse a cookie minted for a different address).
	if VerifyCookie(secret, 1234, []byte{198, 51, 100, 9}, c[:]) {
		t.Fatal("cookie accepted for wrong address")
	}
	// Wrong session.
	if VerifyCookie(secret, 9999, addr, c[:]) {
		t.Fatal("cookie accepted for wrong session")
	}
	// Tampered cookie.
	bad := c
	bad[0] ^= 0xFF
	if VerifyCookie(secret, 1234, addr, bad[:]) {
		t.Fatal("tampered cookie accepted")
	}
	// Wrong length.
	if VerifyCookie(secret, 1234, addr, c[:CookieSize-1]) {
		t.Fatal("short cookie accepted")
	}
}
