package protocol

import "testing"

func TestStartRoundTrip(t *testing.T) {
	in := Start{
		Header:     Header{Session: 0x1234},
		RateBps:    500_000_000,
		DurationMs: 8000,
		Marking:    0xB5, // NQB|ECT(1) sample
	}
	for i := range in.Cookie {
		in.Cookie[i] = byte(i + 1)
	}
	buf := make([]byte, StartSize)
	n, err := in.Marshal(buf)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if n != StartSize {
		t.Fatalf("Marshal wrote %d, want %d", n, StartSize)
	}

	out, err := UnmarshalStart(buf)
	if err != nil {
		t.Fatalf("UnmarshalStart: %v", err)
	}
	if out.Type != MsgStart {
		t.Errorf("Type = %d, want MsgStart (%d)", out.Type, MsgStart)
	}
	if out.Session != in.Session || out.RateBps != in.RateBps ||
		out.DurationMs != in.DurationMs || out.Marking != in.Marking || out.Cookie != in.Cookie {
		t.Errorf("round trip mismatch: got %+v, want %+v", out, in)
	}
}

func TestStartShortBuffers(t *testing.T) {
	if _, err := (Start{}).Marshal(make([]byte, StartSize-1)); err != ErrShort {
		t.Errorf("Marshal short = %v, want ErrShort", err)
	}
	full := make([]byte, StartSize)
	_, _ = (Start{}).Marshal(full)
	if _, err := UnmarshalStart(full[:StartSize-1]); err != ErrShort {
		t.Errorf("Unmarshal short = %v, want ErrShort", err)
	}
}
