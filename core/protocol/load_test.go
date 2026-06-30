package protocol

import "testing"

func TestLoadStatRoundTrip(t *testing.T) {
	in := LoadStat{
		Header:       Header{Session: 0xABCD, Seq: 7},
		BytesRecv:    123456789,
		TServerNanos: 1700000000123456789,
	}
	buf := make([]byte, LoadStatSize)
	n, err := in.Marshal(buf)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if n != LoadStatSize {
		t.Fatalf("Marshal wrote %d bytes, want %d", n, LoadStatSize)
	}

	out, err := UnmarshalLoadStat(buf)
	if err != nil {
		t.Fatalf("UnmarshalLoadStat: %v", err)
	}
	if out.Type != MsgLoadStat {
		t.Errorf("Type = %d, want MsgLoadStat (%d)", out.Type, MsgLoadStat)
	}
	if out.Session != in.Session || out.BytesRecv != in.BytesRecv || out.TServerNanos != in.TServerNanos {
		t.Errorf("round trip mismatch: got %+v, want %+v", out, in)
	}
}

func TestLoadStatShortBuffers(t *testing.T) {
	if _, err := (LoadStat{}).Marshal(make([]byte, LoadStatSize-1)); err != ErrShort {
		t.Errorf("Marshal short = %v, want ErrShort", err)
	}
	full := make([]byte, LoadStatSize)
	_, _ = (LoadStat{}).Marshal(full)
	if _, err := UnmarshalLoadStat(full[:LoadStatSize-1]); err != ErrShort {
		t.Errorf("Unmarshal short = %v, want ErrShort", err)
	}
}
