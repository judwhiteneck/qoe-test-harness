package clock

import (
	"testing"
	"time"
)

func TestFakeIsDeterministic(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	f := NewFake(start)
	if !f.Now().Equal(start) {
		t.Fatalf("Now = %v, want %v", f.Now(), start)
	}
	f.Advance(250 * time.Millisecond)
	if got := f.Since(start); got != 250*time.Millisecond {
		t.Fatalf("Since = %v, want 250ms", got)
	}
	f.Advance(750 * time.Millisecond)
	if got := f.Now().Sub(start); got != time.Second {
		t.Fatalf("elapsed = %v, want 1s", got)
	}
}

// System must satisfy the Clock interface.
var _ Clock = System{}
var _ Clock = (*Fake)(nil)
