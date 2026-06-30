package protocol

import "encoding/binary"

// MsgStop ends a downstream flow early (client -> server). It carries the same
// cookie as the Start so a stranger cannot cancel another tester's flow.
const MsgStop MsgType = 8

const (
	startBody = CookieSize + 8 + 4 + 1 // cookie, rate_bps, duration_ms, marking
	// StartSize is the encoded size of a Start request.
	StartSize = HeaderSize + startBody
)

// Start asks the server to begin a paced downstream flow. The server generates
// traffic only after VerifyCookie passes for the request's source — the anti-
// amplification gate (spec G1). RateBps and DurationMs are bounded server-side so
// a valid cookie still cannot be turned into an unbounded blast. Marking is the
// IP TOS the server should set on the flow (0 = classic); on-wire marking
// fidelity is validated in M0/S2 on hardware.
type Start struct {
	Header
	Cookie     [CookieSize]byte
	RateBps    uint64
	DurationMs uint32
	Marking    uint8
}

// Marshal encodes the request (forcing Type = MsgStart).
func (s Start) Marshal(dst []byte) (int, error) {
	if len(dst) < StartSize {
		return 0, ErrShort
	}
	h := s.Header
	h.Type = MsgStart
	if _, err := h.Marshal(dst); err != nil {
		return 0, err
	}
	copy(dst[HeaderSize:], s.Cookie[:])
	o := HeaderSize + CookieSize
	binary.BigEndian.PutUint64(dst[o:], s.RateBps)
	binary.BigEndian.PutUint32(dst[o+8:], s.DurationMs)
	dst[o+12] = s.Marking
	return StartSize, nil
}

// UnmarshalStart parses a Start request.
func UnmarshalStart(b []byte) (Start, error) {
	h, err := UnmarshalHeader(b)
	if err != nil {
		return Start{}, err
	}
	if len(b) < StartSize {
		return Start{}, ErrShort
	}
	var s Start
	s.Header = h
	copy(s.Cookie[:], b[HeaderSize:])
	o := HeaderSize + CookieSize
	s.RateBps = binary.BigEndian.Uint64(b[o:])
	s.DurationMs = binary.BigEndian.Uint32(b[o+8:])
	s.Marking = b[o+12]
	return s, nil
}
