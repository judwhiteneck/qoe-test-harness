package protocol

import "encoding/binary"

const (
	// MsgLoad is a client -> server bulk load packet (upstream saturation). The
	// body is padding; only its size matters. The server counts received bytes.
	MsgLoad MsgType = 6
	// MsgLoadStat is a server -> client running tally of bytes received on a load
	// flow, with the server's receive timestamp, so the client can compute the
	// achieved (bottleneck-limited) rate from two samples. It is small and rate-
	// limited, so it is never an amplification of the inbound load.
	MsgLoadStat MsgType = 7

	loadStatBody = 8 + 8 // bytes_recv, t_server_nanos
	// LoadStatSize is the encoded size of a load-stat packet.
	LoadStatSize = HeaderSize + loadStatBody
)

// LoadStat is the server's cumulative receive report for a load flow.
type LoadStat struct {
	Header
	BytesRecv    uint64
	TServerNanos int64
}

// Marshal encodes the load stat (forcing Type = MsgLoadStat).
func (s LoadStat) Marshal(dst []byte) (int, error) {
	if len(dst) < LoadStatSize {
		return 0, ErrShort
	}
	h := s.Header
	h.Type = MsgLoadStat
	if _, err := h.Marshal(dst); err != nil {
		return 0, err
	}
	binary.BigEndian.PutUint64(dst[HeaderSize:], s.BytesRecv)
	binary.BigEndian.PutUint64(dst[HeaderSize+8:], uint64(s.TServerNanos))
	return LoadStatSize, nil
}

// UnmarshalLoadStat parses a load-stat packet.
func UnmarshalLoadStat(b []byte) (LoadStat, error) {
	h, err := UnmarshalHeader(b)
	if err != nil {
		return LoadStat{}, err
	}
	if len(b) < LoadStatSize {
		return LoadStat{}, ErrShort
	}
	return LoadStat{
		Header:       h,
		BytesRecv:    binary.BigEndian.Uint64(b[HeaderSize:]),
		TServerNanos: int64(binary.BigEndian.Uint64(b[HeaderSize+8:])),
	}, nil
}
