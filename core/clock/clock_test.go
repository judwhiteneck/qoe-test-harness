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

func TestFakeSleepAdvancesWithoutDelay(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	f := NewFake(start)
	wall := time.Now()
	f.Sleep(10 * time.Second) // huge fake sleep, must be instant in real time
	if took := time.Since(wall); took > 500*time.Millisecond {
		t.Fatalf("Fake.Sleep blocked for %v; should be instant", took)
	}
	if got := f.Since(start); got != 10*time.Second {
		t.Fatalf("after Sleep, Since = %v, want 10s", got)
	}
}

func TestSystemClock(t *testing.T) {
	var c Clock = System{}
	t0 := c.Now()
	c.Sleep(time.Millisecond)
	if d := c.Since(t0); d <= 0 {
		t.Fatalf("Since after Sleep = %v, want > 0", d)
	}
}

// System must satisfy the Clock interface.
var _ Clock = System{}
var _ Clock = (*Fake)(nil)
