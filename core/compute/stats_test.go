package compute

import (
	"math"
	"testing"
)

func TestBaseRTTEmpty(t *testing.T) {
	if got := BaseRTT(nil, 0.05); got != 0 {
		t.Fatalf("BaseRTT(nil) = %v, want 0", got)
	}
}

func TestBaseRTTPercentiles(t *testing.T) {
	// 0,1,2,...,100
	s := make([]float64, 0, 101)
	for i := 0; i <= 100; i++ {
		s = append(s, float64(i))
	}
	cases := []struct {
		p    float64
		want float64
	}{
		{0, 0},
		{1, 100},
		{0.5, 50},
		{0.05, 5},
		{0.01, 1},
	}
	for _, c := range cases {
		if got := BaseRTT(s, c.p); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("BaseRTT(p=%v) = %v, want %v", c.p, got, c.want)
		}
	}
}

func TestBaseRTTDoesNotMutate(t *testing.T) {
	s := []float64{5, 1, 3, 2, 4}
	_ = BaseRTT(s, 0.5)
	if s[0] != 5 || s[1] != 1 {
		t.Fatalf("BaseRTT mutated input: %v", s)
	}
}

func TestWorkingDelta(t *testing.T) {
	cases := []struct{ rtt, base, want float64 }{
		{20, 12, 8},
		{12, 12, 0},
		{10, 12, 0}, // negative clamps to 0
	}
	for _, c := range cases {
		if got := WorkingDelta(c.rtt, c.base); got != c.want {
			t.Errorf("WorkingDelta(%v,%v) = %v, want %v", c.rtt, c.base, got, c.want)
		}
	}
}
