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
	"sync"
	"time"
	"unsafe"

	"github.com/judwhiteneck/qoe-test-harness/core/clock"
	cnet "github.com/judwhiteneck/qoe-test-harness/core/net"
	"github.com/judwhiteneck/qoe-test-harness/core/protocol"
	"golang.org/x/sys/unix"
)

const (
	// loadReportInterval bounds how often the server replies to an upstream load
	// flow with a running byte tally. Coarse enough that the reply stream is
	// negligible next to the inbound load (no amplification), fine enough for a
	// responsive rate read.
	loadReportInterval = 50 * time.Millisecond

	// Downstream-flow bounds. A valid cookie authorizes a flow, but these caps
	// ensure a single Start still cannot be turned into an unbounded blast at the
	// (proven-reachable) client — defense in depth behind the cookie gate.
	maxDownRateBps    = 1_000_000_000 // 1 Gbps ceiling
	maxDownDurationMs = 30_000        // 30 s ceiling; the client renews for longer
	downPktSize       = 1200
	downTick          = time.Millisecond
)

// loadCounter is the per-source running tally for an upstream load flow.
type loadCounter struct {
	bytes uint64
	last  time.Time
}

// downFlow is a live downstream flow; the pointer identity lets a finishing flow
// avoid deleting a newer flow that replaced it for the same source.
type downFlow struct {
	cancel context.CancelFunc
}

// Server handles probe/echo, the handshake, upstream load sinking, and (behind
// the cookie gate) downstream load generation on a UDP socket.
type Server struct {
	secret []byte
	conn   *stdnet.UDPConn

	lmu   sync.Mutex
	loads map[string]*loadCounter

	dmu   sync.Mutex
	downs map[string]*downFlow // per-source downstream flows
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
	s := &Server{
		secret: secret,
		conn:   c,
		loads:  make(map[string]*loadCounter),
		downs:  make(map[string]*downFlow),
	}
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

// Close cancels any in-flight downstream flows and closes the socket.
func (s *Server) Close() error {
	s.dmu.Lock()
	for key, df := range s.downs {
		df.cancel()
		delete(s.downs, key)
	}
	s.dmu.Unlock()
	return s.conn.Close()
}

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
	case protocol.MsgLoad:
		// Upstream load: count the bytes and periodically report the running tally
		// so the client can read the achieved (bottleneck-limited) rate.
		s.onLoad(len(b), src)
	case protocol.MsgStart:
		s.onStart(b, src)
	case protocol.MsgStop:
		s.onStop(h, b, src)
	}
}

// onStart begins a paced downstream flow, but ONLY after the cookie proves the
// requester actually receives at src.IP (spec G1, anti-amplification). Without a
// valid cookie this is a no-op: the server never sends bulk traffic to an
// unverified address, so it cannot be used to flood a spoofed victim.
func (s *Server) onStart(b []byte, src *stdnet.UDPAddr) {
	st, err := protocol.UnmarshalStart(b)
	if err != nil {
		return
	}
	if !protocol.VerifyCookie(s.secret, st.Session, src.IP, st.Cookie[:]) {
		return // no cookie, no flow
	}
	rate := st.RateBps
	if rate > maxDownRateBps {
		rate = maxDownRateBps
	}
	dur := time.Duration(st.DurationMs) * time.Millisecond
	if dur <= 0 || dur > maxDownDurationMs*time.Millisecond {
		dur = maxDownDurationMs * time.Millisecond
	}
	s.startDown(src, rate, dur, st.Marking)
}

// onStop cancels the caller's downstream flow. It requires the same cookie so a
// stranger cannot cancel another tester's flow.
func (s *Server) onStop(h protocol.Header, b []byte, src *stdnet.UDPAddr) {
	if len(b) < protocol.HeaderSize+protocol.CookieSize {
		return
	}
	if !protocol.VerifyCookie(s.secret, h.Session, src.IP, b[protocol.HeaderSize:protocol.HeaderSize+protocol.CookieSize]) {
		return
	}
	s.stopDown(src.String())
}

func (s *Server) startDown(src *stdnet.UDPAddr, rate uint64, dur time.Duration, marking uint8) {
	key := src.String()
	ctx, cancel := context.WithTimeout(context.Background(), dur)
	df := &downFlow{cancel: cancel}
	s.dmu.Lock()
	if old := s.downs[key]; old != nil {
		old.cancel() // replace any existing flow for this source
	}
	s.downs[key] = df
	s.dmu.Unlock()
	go s.downLoop(ctx, key, df, src, rate, marking)
}

func (s *Server) stopDown(key string) {
	s.dmu.Lock()
	if df := s.downs[key]; df != nil {
		df.cancel()
		delete(s.downs, key)
	}
	s.dmu.Unlock()
}

// downLoop paces bulk packets to src until the context's deadline or a cancel.
// The flow is currently classic (unmarked): its job is to congest the bottleneck.
// Setting ECT(1)/NQB on the downstream bytes is applied and verified on the wire
// in M0/S2 on hardware; the carried Marking field reserves the format for it.
func (s *Server) downLoop(ctx context.Context, key string, df *downFlow, src *stdnet.UDPAddr, rate uint64, marking uint8) {
	defer func() {
		// Only retire the map entry if it is still ours; a newer flow may have
		// replaced us for this source.
		s.dmu.Lock()
		if s.downs[key] == df {
			delete(s.downs, key)
		}
		s.dmu.Unlock()
	}()
	pacer := cnet.NewPacer(clock.System{}, downPktSize, rate)
	buf := make([]byte, downPktSize)
	_, _ = (protocol.Header{Type: protocol.MsgLoad}).Marshal(buf) // header prefix; rest padding
	var oob []byte
	if marking != 0 {
		oob = tosOOB(int(marking))
	}
	ticker := time.NewTicker(downTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			due := pacer.Due()
			for i := 0; i < due; i++ {
				var werr error
				if oob != nil {
					_, _, werr = s.conn.WriteMsgUDP(buf, oob, src)
				} else {
					_, werr = s.conn.WriteToUDP(buf, src)
				}
				if werr != nil {
					break
				}
			}
			pacer.MarkSent(due)
		}
	}
}

// onLoad tallies a received load packet for src and, at most every
// loadReportInterval, replies with the cumulative byte count and server time.
func (s *Server) onLoad(n int, src *stdnet.UDPAddr) {
	now := time.Now()
	key := src.String()

	s.lmu.Lock()
	lc := s.loads[key]
	if lc == nil {
		lc = &loadCounter{last: now}
		s.loads[key] = lc
	}
	lc.bytes += uint64(n)
	bytes := lc.bytes
	report := now.Sub(lc.last) >= loadReportInterval
	if report {
		lc.last = now
	}
	s.lmu.Unlock()

	if !report {
		return
	}
	stat := protocol.LoadStat{BytesRecv: bytes, TServerNanos: now.UnixNano()}
	out := make([]byte, protocol.LoadStatSize)
	if _, err := stat.Marshal(out); err == nil {
		_, _ = s.conn.WriteToUDP(out, src)
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
	// Reflect the intended mark onto the RETURN (downstream) leg so the echo
	// exercises downstream L4S treatment. Per-packet via cmsg so the shared
	// socket's concurrent (classic) downstream load stays unmarked.
	_, _, _ = s.conn.WriteMsgUDP(out, tosOOB(int(p.TOSIntended)), src)
}

// ceBit reports ECN Congestion-Experienced (the low two bits == 0b11).
func ceBit(tos int) uint8 {
	if tos&0x03 == 0x03 {
		return 1
	}
	return 0
}

// tosOOB builds an IP_TOS ancillary-data block so one WriteMsgUDP sets the TOS
// byte on just that datagram (not the whole shared socket).
func tosOOB(tos int) []byte {
	const dataLen = 4
	b := make([]byte, unix.CmsgSpace(dataLen))
	h := (*unix.Cmsghdr)(unsafe.Pointer(&b[0]))
	h.Level = unix.IPPROTO_IP
	h.Type = unix.IP_TOS
	h.SetLen(unix.CmsgLen(dataLen))
	*(*uint32)(unsafe.Pointer(&b[unix.CmsgLen(0)])) = uint32(tos)
	return b
}
