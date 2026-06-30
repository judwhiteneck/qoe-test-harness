//go:build linux

// Package server is the VPS-side UDP endpoint: it answers the return-routability
// handshake, echoes probes with the TOS byte it actually observed (the marking-
// survival signal), and validates a cookie before any high-rate flow. The marked
// downstream load generator is a later milestone; this is the probe/echo +
// handshake core. Logic lives here (testable over a real loopback socket); the
// binary is a thin wrapper in cmd/qoe-server. Linux only (uses IP_RECVTOS cmsgs).
package server

import (
	"context"
	"errors"
	stdnet "net"
	"time"

	"github.com/judwhiteneck/qoe-test-harness/core/protocol"
	"golang.org/x/sys/unix"
)

// Server handles probe/echo and the handshake on a UDP socket.
type Server struct {
	secret []byte
	conn   *stdnet.UDPConn
}

// Listen binds addr ("host:port"; empty host = all) and enables IP_RECVTOS so the
// echo can report the TOS that actually arrived.
func Listen(addr string, secret []byte) (*Server, error) {
	uaddr, err := stdnet.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return nil, err
	}
	c, err := stdnet.ListenUDP("udp4", uaddr)
	if err != nil {
		return nil, err
	}
	s := &Server{secret: secret, conn: c}
	_ = s.enableRecvTOS() // best-effort; TOSObserved is 0 if unsupported
	return s, nil
}

func (s *Server) enableRecvTOS() error {
	rc, err := s.conn.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	if cerr := rc.Control(func(fd uintptr) {
		serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_RECVTOS, 1)
	}); cerr != nil {
		return cerr
	}
	return serr
}

// Addr returns the bound local address (useful when binding to :0 in tests).
func (s *Server) Addr() stdnet.Addr { return s.conn.LocalAddr() }

// Close closes the socket.
func (s *Server) Close() error { return s.conn.Close() }

// Serve reads and handles packets until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	buf := make([]byte, 2048)
	oob := make([]byte, 256)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := s.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
			return err
		}
		n, oobn, _, src, err := s.conn.ReadMsgUDP(buf, oob)
		if err != nil {
			var ne stdnet.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		s.handle(buf[:n], parseTOS(oob[:oobn]), src)
	}
}

// parseTOS extracts the IP TOS byte from received control messages.
func parseTOS(oob []byte) int {
	msgs, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return 0
	}
	for _, m := range msgs {
		if m.Header.Level == unix.IPPROTO_IP &&
			(m.Header.Type == unix.IP_TOS || m.Header.Type == unix.IP_RECVTOS) &&
			len(m.Data) >= 1 {
			return int(m.Data[0])
		}
	}
	return 0
}

func (s *Server) handle(b []byte, tos int, src *stdnet.UDPAddr) {
	h, err := protocol.UnmarshalHeader(b)
	if err != nil {
		return // drop malformed; never trust the wire
	}
	switch h.Type {
	case protocol.MsgHello:
		s.sendCookie(h.Session, src)
	case protocol.MsgProbe:
		s.echoProbe(b, tos, src)
	case protocol.MsgStart:
		// Anti-amplification: a high-rate flow starts only if the cookie is valid
		// for this source. The load generator is a later milestone; we validate here.
		_ = protocol.VerifyCookie(s.secret, h.Session, src.IP, b[protocol.HeaderSize:])
	}
}

func (s *Server) sendCookie(session uint64, src *stdnet.UDPAddr) {
	cookie := protocol.MakeCookie(s.secret, session, src.IP)
	out := make([]byte, protocol.HeaderSize+protocol.CookieSize)
	if _, err := (protocol.Header{Type: protocol.MsgCookie, Session: session}).Marshal(out); err != nil {
		return
	}
	copy(out[protocol.HeaderSize:], cookie[:])
	_, _ = s.conn.WriteToUDP(out, src)
}

func (s *Server) echoProbe(b []byte, tos int, src *stdnet.UDPAddr) {
	p, err := protocol.UnmarshalProbe(b)
	if err != nil {
		return
	}
	echo := protocol.Echo{
		Header:           protocol.Header{Session: p.Session, Seq: p.Seq},
		TSendNanos:       p.TSendNanos,
		TRecvServerNanos: time.Now().UnixNano(),
		TOSObserved:      uint8(tos),
		CE:               ceBit(tos),
	}
	out := make([]byte, protocol.EchoSize) // EchoSize <= ProbeSize: never an amplification
	if _, err := echo.Marshal(out); err != nil {
		return
	}
	_, _ = s.conn.WriteToUDP(out, src)
}

// ceBit reports ECN Congestion-Experienced (the low two bits == 0b11).
func ceBit(tos int) uint8 {
	if tos&0x03 == 0x03 {
		return 1
	}
	return 0
}
