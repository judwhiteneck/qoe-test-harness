// Package protocol defines the UDP wire format shared by client and server: the
// common header, probe/echo packets, and the return-routability handshake. It is
// pure (no net/os, see docs/ARCHITECTURE.md): it turns structs into bytes and
// back, validating all input as untrusted. The actual sockets live in core/net.
package protocol

import (
	"encoding/binary"
	"errors"
)

const (
	// Magic identifies our packets ("L4").
	Magic uint16 = 0x4C34
	// Version is the wire-format version.
	Version uint8 = 1
	// HeaderSize is the fixed common-header length in bytes.
	HeaderSize = 2 + 1 + 1 + 8 + 8 // magic, version, type, session, seq
)

// MsgType is the packet kind.
type MsgType uint8

const (
	MsgHello  MsgType = 1 // client -> server: request a cookie (return routability)
	MsgCookie MsgType = 2 // server -> client: cookie to echo before any rated flow
	MsgProbe  MsgType = 3 // client -> server: timestamped measurement packet
	MsgEcho   MsgType = 4 // server -> client: reply carrying the observed TOS
	MsgStart  MsgType = 5 // client -> server: begin a rated flow; must carry a valid cookie
)

// Wire errors. All decode paths validate length and framing because packets
// arrive from the network and are untrusted.
var (
	ErrShort   = errors.New("protocol: buffer too short")
	ErrMagic   = errors.New("protocol: bad magic")
	ErrVersion = errors.New("protocol: unsupported version")
)

// Header is the common prefix of every packet.
type Header struct {
	Type    MsgType
	Session uint64
	Seq     uint64
}

// Marshal writes the header into dst and returns the number of bytes written.
func (h Header) Marshal(dst []byte) (int, error) {
	if len(dst) < HeaderSize {
		return 0, ErrShort
	}
	binary.BigEndian.PutUint16(dst[0:], Magic)
	dst[2] = Version
	dst[3] = byte(h.Type)
	binary.BigEndian.PutUint64(dst[4:], h.Session)
	binary.BigEndian.PutUint64(dst[12:], h.Seq)
	return HeaderSize, nil
}

// UnmarshalHeader parses a header, rejecting short buffers, bad magic, and
// unsupported versions.
func UnmarshalHeader(b []byte) (Header, error) {
	if len(b) < HeaderSize {
		return Header{}, ErrShort
	}
	if binary.BigEndian.Uint16(b[0:]) != Magic {
		return Header{}, ErrMagic
	}
	if b[2] != Version {
		return Header{}, ErrVersion
	}
	return Header{
		Type:    MsgType(b[3]),
		Session: binary.BigEndian.Uint64(b[4:]),
		Seq:     binary.BigEndian.Uint64(b[12:]),
	}, nil
}
