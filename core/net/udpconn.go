//go:build linux

package net

import (
	"errors"
	stdnet "net"
	"time"

	"github.com/judwhiteneck/qoe-test-harness/core/clock"
	"github.com/judwhiteneck/qoe-test-harness/core/protocol"
	"golang.org/x/sys/unix"
)

// DefaultReadTimeout bounds how long RecvEcho waits before reporting ErrNoEcho
// (the "no more echoes this batch" signal for the real socket).
const DefaultReadTimeout = 200 * time.Millisecond

// UDPConn is the production PacketConn: real connected UDP with per-batch IP TOS
// marking (ECT(1)/NQB) via setsockopt(IP_TOS), and clock-sync-free RTT (the echo
// carries back the client send time). Linux only; IPv6 (IPV6_TCLASS) and other
// platforms are follow-ups.
type UDPConn struct {
	clk         clock.Clock
	conn        *stdnet.UDPConn
	readTimeout time.Duration
	rbuf        []byte
	lastTOS     int
}

// DialUDP opens a connected UDP socket to serverAddr ("host:port").
func DialUDP(clk clock.Clock, serverAddr string) (*UDPConn, error) {
	raddr, err := stdnet.ResolveUDPAddr("udp4", serverAddr)
	if err != nil {
		return nil, err
	}
	c, err := stdnet.DialUDP("udp4", nil, raddr)
	if err != nil {
		return nil, err
	}
	return &UDPConn{clk: clk, conn: c, readTimeout: DefaultReadTimeout, rbuf: make([]byte, 2048), lastTOS: -1}, nil
}

// SetReadTimeout overrides the per-recv drain timeout.
func (u *UDPConn) SetReadTimeout(d time.Duration) { u.readTimeout = d }

func (u *UDPConn) setTOS(tos int) error {
	rc, err := u.conn.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	if cerr := rc.Control(func(fd uintptr) {
		serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_TOS, tos)
	}); cerr != nil {
		return cerr
	}
	return serr
}

// Handshake performs return-routability: sends a padded HELLO and returns the
// server's cookie. The HELLO is padded so the cookie reply is never an
// amplification.
func (u *UDPConn) Handshake(session uint64) ([]byte, error) {
	hello := make([]byte, 64)
	if _, err := (protocol.Header{Type: protocol.MsgHello, Session: session}).Marshal(hello); err != nil {
		return nil, err
	}
	if _, err := u.conn.Write(hello); err != nil {
		return nil, err
	}
	if err := u.conn.SetReadDeadline(time.Now().Add(u.readTimeout)); err != nil {
		return nil, err
	}
	n, err := u.conn.Read(u.rbuf)
	if err != nil {
		return nil, err
	}
	h, err := protocol.UnmarshalHeader(u.rbuf[:n])
	if err != nil {
		return nil, err
	}
	if h.Type != protocol.MsgCookie || n < protocol.HeaderSize+protocol.CookieSize {
		return nil, ErrNoEcho
	}
	cookie := make([]byte, protocol.CookieSize)
	copy(cookie, u.rbuf[protocol.HeaderSize:])
	return cookie, nil
}

// SendProbe marshals and sends a probe, setting the IP TOS to its marking (once
// per change; engine batches share a marking, so this is one syscall per batch).
func (u *UDPConn) SendProbe(p Probe) error {
	if int(p.Marking) != u.lastTOS {
		if err := u.setTOS(int(p.Marking)); err != nil {
			return err
		}
		u.lastTOS = int(p.Marking)
	}
	var buf [protocol.ProbeSize]byte
	pp := protocol.Probe{
		Header:      protocol.Header{Seq: p.Seq},
		TSendNanos:  p.SentAt.UnixNano(),
		TOSIntended: uint8(p.Marking),
	}
	if _, err := pp.Marshal(buf[:]); err != nil {
		return err
	}
	_, err := u.conn.Write(buf[:])
	return err
}

// RecvEcho reads one echo, or returns ErrNoEcho on read timeout (batch drained).
func (u *UDPConn) RecvEcho() (Echo, error) {
	if err := u.conn.SetReadDeadline(time.Now().Add(u.readTimeout)); err != nil {
		return Echo{}, err
	}
	n, err := u.conn.Read(u.rbuf)
	if err != nil {
		var ne stdnet.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return Echo{}, ErrNoEcho
		}
		return Echo{}, err
	}
	recvAt := u.clk.Now()
	e, err := protocol.UnmarshalEcho(u.rbuf[:n])
	if err != nil {
		return Echo{}, err
	}
	return Echo{
		Seq:          e.Seq,
		SentAt:       time.Unix(0, e.TSendNanos),
		RecvAt:       recvAt,
		ServerRecvAt: time.Unix(0, e.TRecvServerNanos),
		TOSObserved:  Marking(e.TOSObserved),
		CESeen:       e.CE != 0,
	}, nil
}

// Close closes the socket.
func (u *UDPConn) Close() error { return u.conn.Close() }

var _ PacketConn = (*UDPConn)(nil)
