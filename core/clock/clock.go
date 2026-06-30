// Package clock provides an injectable time source so timing-sensitive code is
// deterministic under test (see docs/ENGINEERING.md §1). Production code outside
// the I/O layer takes a Clock rather than calling time.Now directly.
package clock

import "time"

// Clock is the time seam.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
}

// System is the real monotonic clock used in production.
type System struct{}

func (System) Now() time.Time                  { return time.Now() }
func (System) Since(t time.Time) time.Duration { return time.Since(t) }

// Fake is a deterministic clock for tests; it advances only when told to.
type Fake struct{ t time.Time }

// NewFake returns a Fake starting at start.
func NewFake(start time.Time) *Fake { return &Fake{t: start} }

func (f *Fake) Now() time.Time                  { return f.t }
func (f *Fake) Since(t time.Time) time.Duration { return f.t.Sub(t) }

// Advance moves the fake clock forward.
func (f *Fake) Advance(d time.Duration) { f.t = f.t.Add(d) }
