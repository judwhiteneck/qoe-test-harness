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

// LoadPktSize is the on-wire size of each upstream load packet (header + padding).
// Sized near a typical MTU so the load is dominated by payload, not per-packet
// overhead.
const LoadPktSize = 1200

// loadTick is how often the sender wakes to release the packets the pacer says
// are due. Finer than the report interval, coarse enough not to busy-spin.
const loadTick = time.Millisecond

// UDPLoad is the production LoadController for upstream saturation: it paces bulk
// UDP packets to the server on its own socket (separate from the probe socket, so
// load and probes share only the real bottleneck, not the app), and derives the
// achieved (bottleneck-limited) rate from the server's periodic byte-count
// reports. Downstream (server-paced) load is a follow-up and MUST be cookie-gated
// because it is the amplification vector; upstream is not (the server's replies
// are small and rate-limited). Linux only, to sit beside the rest of core/net.
type UDPLoad struct {
	clk  clock.Clock
	conn *stdnet.UDPConn

	mu       sync.Mutex
	achieved uint64
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// DialUDPLoad opens a connected UDP socket to serverAddr for a load flow.
func DialUDPLoad(clk clock.Clock, serverAddr string) (*UDPLoad, error) {
	raddr, err := stdnet.ResolveUDPAddr("udp4", serverAddr)
	if err != nil {
		return nil, err
	}
	c, err := stdnet.DialUDP("udp4", nil, raddr)
	if err != nil {
		return nil, err
	}
	return &UDPLoad{clk: clk, conn: c}, nil
}

// SetRateBps starts (or restarts) the load at the target offered rate. On a real
// path above capacity the achieved rate caps at the bottleneck; passing 0 stops.
func (l *UDPLoad) SetRateBps(bps uint64) error {
	l.Stop()
	if bps == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.mu.Lock()
	l.cancel = cancel
	l.achieved = 0
	l.mu.Unlock()

	l.wg.Add(2)
	go l.sendLoop(ctx, bps)
	go l.recvLoop(ctx)
	return nil
}

func (l *UDPLoad) sendLoop(ctx context.Context, bps uint64) {
	defer l.wg.Done()
	pacer := NewPacer(l.clk, LoadPktSize, bps)
	buf := make([]byte, LoadPktSize)
	_, _ = (protocol.Header{Type: protocol.MsgLoad}).Marshal(buf) // header prefix; rest is padding
	ticker := time.NewTicker(loadTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			due := pacer.Due()
			for i := 0; i < due; i++ {
				if _, err := l.conn.Write(buf); err != nil {
					break // socket gone or buffer full; the pacer catches up next tick
				}
			}
			pacer.MarkSent(due)
		}
	}
}

func (l *UDPLoad) recvLoop(ctx context.Context) {
	defer l.wg.Done()
	rbuf := make([]byte, 2048)
	var lastBytes uint64
	var lastNanos int64
	for {
		if ctx.Err() != nil {
			return
		}
		_ = l.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, err := l.conn.Read(rbuf)
		if err != nil {
			continue // timeout: loop and re-check ctx
		}
		st, err := protocol.UnmarshalLoadStat(rbuf[:n])
		if err != nil {
			continue
		}
		// Compute achieved rate from consecutive cumulative samples using the
		// server's clock for both endpoints (no cross-host clock sync needed).
		if lastNanos != 0 && st.TServerNanos > lastNanos && st.BytesRecv >= lastBytes {
			dt := float64(st.TServerNanos-lastNanos) / 1e9
			if dt > 0 {
				bps := uint64(float64(st.BytesRecv-lastBytes) * 8.0 / dt)
				l.mu.Lock()
				l.achieved = bps
				l.mu.Unlock()
			}
		}
		lastBytes, lastNanos = st.BytesRecv, st.TServerNanos
	}
}

// AchievedBps returns the most recent measured throughput at the server.
func (l *UDPLoad) AchievedBps() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.achieved
}

// Stop halts the flow and waits for the goroutines to exit.
func (l *UDPLoad) Stop() {
	l.mu.Lock()
	cancel := l.cancel
	l.cancel = nil
	l.mu.Unlock()
	if cancel != nil {
		cancel()
		l.wg.Wait()
	}
}

// Close stops the flow and closes the socket.
func (l *UDPLoad) Close() error {
	l.Stop()
	return l.conn.Close()
}

var _ LoadController = (*UDPLoad)(nil)
