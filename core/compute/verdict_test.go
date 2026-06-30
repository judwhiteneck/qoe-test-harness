package compute

import "testing"

// fill returns a histogram with n samples all at value v.
func fill(v float64, n int) *Histogram {
	h := NewDefaultHistogram()
	for i := 0; i < n; i++ {
		h.Observe(v)
	}
	return h
}

// passingInputs is a clean, fully-valid run that should PASS. Individual tests
// mutate one field to exercise each branch.
func passingInputs() Inputs {
	return Inputs{
		CapacityOK:                 true,
		StandingQueueOK:            true,
		MarkingSurvival:            0.995,
		Down:                       DirectionInputs{LL: fill(5, 1000), Classic: fill(100, 1000)},
		Up:                         DirectionInputs{LL: fill(5, 1000), Classic: fill(100, 1000)},
		ClassicSoloThroughputBps:   100e6,
		ClassicWithLLThroughputBps: 95e6,
		ClassicSoloP99Ms:           60,
		ClassicWithLLP99Ms:         61,
	}
}

func TestEvaluatePass(t *testing.T) {
	r := Evaluate(passingInputs(), DefaultThresholds())
	if r.Verdict != Pass {
		t.Fatalf("verdict = %s, want pass (caveats=%v subs=%v)", r.Verdict, r.Caveats, r.SubResults)
	}
	if len(r.SubResults) != 3 {
		t.Fatalf("want 3 sub-results, got %d", len(r.SubResults))
	}
	for _, s := range r.SubResults {
		if s.Status != Pass {
			t.Errorf("sub %q = %s, want pass (%s)", s.Name, s.Status, s.Detail)
		}
	}
}

func TestEvaluateInconclusiveGates(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Inputs)
	}{
		{"wrong link", func(in *Inputs) { in.WrongLink = true }},
		{"capacity gate", func(in *Inputs) { in.CapacityOK = false }},
		{"no standing queue", func(in *Inputs) { in.StandingQueueOK = false }},
		{"baseline drift", func(in *Inputs) { in.BaselineDrifted = true }},
		{"noisy no-harm", func(in *Inputs) { in.NoHarmNoisy = true }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := passingInputs()
			c.mutate(&in)
			r := Evaluate(in, DefaultThresholds())
			if r.Verdict != Inconclusive {
				t.Fatalf("verdict = %s, want inconclusive", r.Verdict)
			}
			if len(r.Caveats) == 0 {
				t.Fatal("inconclusive result must carry a caveat")
			}
			if len(r.SubResults) != 0 {
				t.Fatal("inconclusive must not emit pass/fail sub-results")
			}
		})
	}
}

func TestEvaluateFailWorking(t *testing.T) {
	in := passingInputs()
	in.Down.LL = fill(15, 1000) // LL p99 ~ 15ms > 10ms threshold
	r := Evaluate(in, DefaultThresholds())
	if r.Verdict != Fail {
		t.Fatalf("verdict = %s, want fail", r.Verdict)
	}
	if subStatus(r, "working") != Fail {
		t.Fatalf("working = %s, want fail", subStatus(r, "working"))
	}
}

func TestEvaluateFailWhenClassicNotCongested(t *testing.T) {
	// Standing queue claimed but classic stayed low: separation not demonstrated.
	in := passingInputs()
	in.Down.Classic = fill(8, 1000) // classic p99 < 50ms
	r := Evaluate(in, DefaultThresholds())
	if subStatus(r, "working") != Fail {
		t.Fatalf("working = %s, want fail (classic not > 50ms)", subStatus(r, "working"))
	}
}

func TestEvaluateFailMarking(t *testing.T) {
	in := passingInputs()
	in.MarkingSurvival = 0.90
	r := Evaluate(in, DefaultThresholds())
	if r.Verdict != Fail || subStatus(r, "marking_survival") != Fail {
		t.Fatalf("verdict=%s marking=%s, want fail/fail", r.Verdict, subStatus(r, "marking_survival"))
	}
}

func TestEvaluateFailNoHarmThroughput(t *testing.T) {
	in := passingInputs()
	in.ClassicWithLLThroughputBps = 80e6 // 80% of solo < 90%
	r := Evaluate(in, DefaultThresholds())
	if subStatus(r, "no_harm") != Fail {
		t.Fatalf("no_harm = %s, want fail", subStatus(r, "no_harm"))
	}
}

func TestEvaluateFailNoHarmLatency(t *testing.T) {
	in := passingInputs()
	in.ClassicWithLLP99Ms = in.ClassicSoloP99Ms + 20 // +20ms > 5ms margin
	r := Evaluate(in, DefaultThresholds())
	if subStatus(r, "no_harm") != Fail {
		t.Fatalf("no_harm = %s, want fail", subStatus(r, "no_harm"))
	}
}

func TestEvaluateMissingSamplesFails(t *testing.T) {
	in := passingInputs()
	in.Up = DirectionInputs{} // nil histograms
	r := Evaluate(in, DefaultThresholds())
	if subStatus(r, "working") != Fail {
		t.Fatalf("working = %s, want fail on missing samples", subStatus(r, "working"))
	}
}

func subStatus(r Result, name string) Verdict {
	for _, s := range r.SubResults {
		if s.Name == name {
			return s.Status
		}
	}
	return ""
}
