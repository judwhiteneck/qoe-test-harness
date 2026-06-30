package protocol

import "encoding/binary"

const (
	probeBody = 8 + 1     // t_send_nanos, tos_intended
	echoBody  = 8 + 8 + 2 // t_send_nanos (echoed), t_recv_server_nanos, tos_observed, ce

	// ProbeSize is the encoded size of a probe packet.
	ProbeSize = HeaderSize + probeBody
	// EchoSize is the encoded size of an echo packet.
	EchoSize = HeaderSize + echoBody
)

// Probe is a client measurement packet. TOSIntended records the marking the
// client set on the IP header, so the server's echo can report intended vs
// observed and the analysis can measure marking survival.
type Probe struct {
	Header
	TSendNanos  int64
	TOSIntended uint8
}

// Marshal encodes the probe (forcing Type = MsgProbe).
func (p Probe) Marshal(dst []byte) (int, error) {
	if len(dst) < ProbeSize {
		return 0, ErrShort
	}
	h := p.Header
	h.Type = MsgProbe
	if _, err := h.Marshal(dst); err != nil {
		return 0, err
	}
	binary.BigEndian.PutUint64(dst[HeaderSize:], uint64(p.TSendNanos))
	dst[HeaderSize+8] = p.TOSIntended
	return ProbeSize, nil
}

// UnmarshalProbe parses a probe packet.
func UnmarshalProbe(b []byte) (Probe, error) {
	h, err := UnmarshalHeader(b)
	if err != nil {
		return Probe{}, err
	}
	if len(b) < ProbeSize {
		return Probe{}, ErrShort
	}
	return Probe{
		Header:      h,
		TSendNanos:  int64(binary.BigEndian.Uint64(b[HeaderSize:])),
		TOSIntended: b[HeaderSize+8],
	}, nil
}

// Echo is the server's reply. TOSObserved is the TOS byte the server actually
// received (the marking-survival signal); CE is 1 if ECN Congestion-Experienced
// was seen.
type Echo struct {
	Header
	TSendNanos       int64
	TRecvServerNanos int64
	TOSObserved      uint8
	CE               uint8
}

// Marshal encodes the echo (forcing Type = MsgEcho).
func (e Echo) Marshal(dst []byte) (int, error) {
	if len(dst) < EchoSize {
		return 0, ErrShort
	}
	h := e.Header
	h.Type = MsgEcho
	if _, err := h.Marshal(dst); err != nil {
		return 0, err
	}
	binary.BigEndian.PutUint64(dst[HeaderSize:], uint64(e.TSendNanos))
	binary.BigEndian.PutUint64(dst[HeaderSize+8:], uint64(e.TRecvServerNanos))
	dst[HeaderSize+16] = e.TOSObserved
	dst[HeaderSize+17] = e.CE
	return EchoSize, nil
}

// UnmarshalEcho parses an echo packet.
func UnmarshalEcho(b []byte) (Echo, error) {
	h, err := UnmarshalHeader(b)
	if err != nil {
		return Echo{}, err
	}
	if len(b) < EchoSize {
		return Echo{}, ErrShort
	}
	return Echo{
		Header:           h,
		TSendNanos:       int64(binary.BigEndian.Uint64(b[HeaderSize:])),
		TRecvServerNanos: int64(binary.BigEndian.Uint64(b[HeaderSize+8:])),
		TOSObserved:      b[HeaderSize+16],
		CE:               b[HeaderSize+17],
	}, nil
}
