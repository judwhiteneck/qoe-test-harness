// Package clock provides an injectable time source so timing-sensitive code is
// deterministic under test (see docs/ENGINEERING.md §1). Production code outside
// the I/O layer takes a Clock rather than calling time.Now directly.
package clock

import "time"

// Clock is the time seam.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
	// Sleep blocks for d. On the real clock this is a wall-clock wait (e.g. to let
	// an async load flow ramp before reading its achieved rate); on the Fake clock
	// it advances instantly, so settle waits do not slow tests.
	Sleep(d time.Duration)
}

// System is the real monotonic clock used in production.
type System struct{}

func (System) Now() time.Time                  { return time.Now() }
func (System) Since(t time.Time) time.Duration { return time.Since(t) }
func (System) Sleep(d time.Duration)           { time.Sleep(d) }

// Fake is a deterministic clock for tests; it advances only when told to.
type Fake struct{ t time.Time }

// NewFake returns a Fake starting at start.
func NewFake(start time.Time) *Fake { return &Fake{t: start} }

func (f *Fake) Now() time.Time                  { return f.t }
func (f *Fake) Since(t time.Time) time.Duration { return f.t.Sub(t) }

// Sleep advances the fake clock by d without any real delay.
func (f *Fake) Sleep(d time.Duration) { f.t = f.t.Add(d) }

// Advance moves the fake clock forward.
func (f *Fake) Advance(d time.Duration) { f.t = f.t.Add(d) }

// Set jumps the fake clock to t. Used by the loopback simulator to advance time
// to a packet's delivery instant.
func (f *Fake) Set(t time.Time) { f.t = t }
