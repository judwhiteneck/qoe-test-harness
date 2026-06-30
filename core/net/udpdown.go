//go:build linux

package net

import (
	"context"
	stdnet "net"
	"sync"
	"time"

	"github.com/judwhiteneck/qoe-test-harness/core/clock"
	"github.com/judwhiteneck/qoe-test-harness/core/protocol"
)

const (
	// downAchievedWindow is the sliding window over which the client averages the
	// received downstream rate.
	downAchievedWindow = 200 * time.Millisecond
	// downHandshakeTimeout bounds the wait for the cookie reply.
	downHandshakeTimeout = 500 * time.Millisecond
	// downDefaultDurationMs is the flow lifetime requested per Start; the server
	// caps it, and a long run renews. It outlives a typical phase.
	downDefaultDurationMs = 20_000
)

// UDPDownLoad is the client side of downstream (server-paced) load. It proves
// return-routability via the handshake, asks the server to pace a bulk flow back
// with the cookie attached, and measures the achieved rate from the bytes it
// actually receives (the client is the receiver, so no server report is needed).
// It satisfies LoadController. Linux only, to sit beside the rest of core/net.
type UDPDownLoad struct {
	clk     clock.Clock
	conn    *stdnet.UDPConn
	session uint64

	cookie    [protocol.CookieSize]byte
	haveToken bool

	mu       sync.Mutex
	achieved uint64
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// DialUDPDownLoad opens a socket to serverAddr for a downstream flow under session.
func DialUDPDownLoad(clk clock.Clock, serverAddr string, session uint64) (*UDPDownLoad, error) {
	raddr, err := stdnet.ResolveUDPAddr("udp4", serverAddr)
	if err != nil {
		return nil, err
	}
	c, err := stdnet.DialUDP("udp4", nil, raddr)
	if err != nil {
		return nil, err
	}
	return &UDPDownLoad{clk: clk, conn: c, session: session}, nil
}

// handshake fetches a cookie (return-routability) once and caches it.
func (d *UDPDownLoad) handshake() error {
	if d.haveToken {
		return nil
	}
	hello := make([]byte, 64) // padded: the cookie reply is never an amplification
	if _, err := (protocol.Header{Type: protocol.MsgHello, Session: d.session}).Marshal(hello); err != nil {
		return err
	}
	if _, err := d.conn.Write(hello); err != nil {
		return err
	}
	if err := d.conn.SetReadDeadline(time.Now().Add(downHandshakeTimeout)); err != nil {
		return err
	}
	rbuf := make([]byte, 2048)
	n, err := d.conn.Read(rbuf)
	if err != nil {
		return err
	}
	h, err := protocol.UnmarshalHeader(rbuf[:n])
	if err != nil {
		return err
	}
	if h.Type != protocol.MsgCookie || n < protocol.HeaderSize+protocol.CookieSize {
		return ErrNoEcho
	}
	copy(d.cookie[:], rbuf[protocol.HeaderSize:])
	d.haveToken = true
	return nil
}

// SetRateBps requests a downstream flow at the target rate (0 stops it). The
// server only honors it after the cookie verifies for this client's address.
func (d *UDPDownLoad) SetRateBps(bps uint64) error {
	d.Stop()
	if bps == 0 {
		return nil
	}
	if err := d.handshake(); err != nil {
		return err
	}
	start := protocol.Start{
		Header:     protocol.Header{Session: d.session},
		Cookie:     d.cookie,
		RateBps:    bps,
		DurationMs: downDefaultDurationMs,
	}
	out := make([]byte, protocol.StartSize)
	if _, err := start.Marshal(out); err != nil {
		return err
	}
	if _, err := d.conn.Write(out); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	d.mu.Lock()
	d.cancel = cancel
	d.achieved = 0
	d.mu.Unlock()
	d.wg.Add(1)
	go d.recvLoop(ctx)
	return nil
}

func (d *UDPDownLoad) recvLoop(ctx context.Context) {
	defer d.wg.Done()
	rbuf := make([]byte, 2048)
	meter := NewMeter(d.clk)
	lastReset := d.clk.Now()
	for {
		if ctx.Err() != nil {
			return
		}
		_ = d.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, err := d.conn.Read(rbuf)
		if err != nil {
			continue // timeout: loop and re-check ctx
		}
		meter.Add(n)
		if d.clk.Since(lastReset) >= downAchievedWindow {
			d.mu.Lock()
			d.achieved = meter.Bps()
			d.mu.Unlock()
			meter.Reset()
			lastReset = d.clk.Now()
		}
	}
}

// AchievedBps returns the most recent received downstream throughput.
func (d *UDPDownLoad) AchievedBps() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.achieved
}

// Stop asks the server to end the flow and stops measuring.
func (d *UDPDownLoad) Stop() {
	d.mu.Lock()
	cancel := d.cancel
	d.cancel = nil
	d.mu.Unlock()
	if cancel == nil {
		return
	}
	if d.haveToken {
		stop := make([]byte, protocol.HeaderSize+protocol.CookieSize)
		if _, err := (protocol.Header{Type: protocol.MsgStop, Session: d.session}).Marshal(stop); err == nil {
			copy(stop[protocol.HeaderSize:], d.cookie[:])
			_, _ = d.conn.Write(stop)
		}
	}
	cancel()
	d.wg.Wait()
}

// Close stops the flow and closes the socket.
func (d *UDPDownLoad) Close() error {
	d.Stop()
	return d.conn.Close()
}

var _ LoadController = (*UDPDownLoad)(nil)
