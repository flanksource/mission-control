package sqlstats

import (
	"math"
	"testing"
	"time"
)

// Two snapshots ~68 seconds apart from the user's instance — delta puts SQL
// at ~99.99% of one scheduler. Locks the formula in case anyone refactors
// computeCPUDelta and forgets the cumulative-ratio denominator.
func TestComputeCPUDelta_RealWorldSample(t *testing.T) {
	t0 := time.Now()
	prev := cpuSnapshot{
		CapturedAt: t0,
		CPUTicks:   1697346292937280,
		MSTicks:    458743923,
	}
	current := cpuSnapshot{
		CapturedAt: t0.Add(time.Duration(68339) * time.Millisecond),
		CPUTicks:   1697599148055831,
		MSTicks:    458812262,
	}

	out := computeCPUDelta(prev, current)
	if out.Pending {
		t.Fatalf("expected non-pending result, got %+v", out)
	}
	want := 99.985 // (252810118551 / 68339) / (1697599148055831 / 458812262) * 100
	if math.Abs(out.ProcessPercent-want) > 0.05 {
		t.Errorf("ProcessPercent = %.4f, want %.4f ± 0.05", out.ProcessPercent, want)
	}
	if out.ElapsedSeconds < 68 || out.ElapsedSeconds > 69 {
		t.Errorf("ElapsedSeconds = %v, want ~68.3", out.ElapsedSeconds)
	}
}

func TestComputeCPUDelta_MissingBaselineIsPending(t *testing.T) {
	current := cpuSnapshot{CapturedAt: time.Now(), CPUTicks: 100, MSTicks: 1}
	out := computeCPUDelta(cpuSnapshot{}, current)
	if !out.Pending {
		t.Errorf("zero baseline should yield pending=true; got %+v", out)
	}
}

func TestComputeCPUDelta_NegativeDeltaIsPending(t *testing.T) {
	t0 := time.Now()
	prev := cpuSnapshot{CapturedAt: t0, CPUTicks: 200, MSTicks: 100}
	current := cpuSnapshot{CapturedAt: t0.Add(time.Second), CPUTicks: 100, MSTicks: 200}
	out := computeCPUDelta(prev, current)
	if !out.Pending {
		t.Errorf("ticks going backwards should yield pending=true; got %+v", out)
	}
}

func TestComputeCPUDelta_StaleBaselineIsPending(t *testing.T) {
	t0 := time.Now()
	prev := cpuSnapshot{CapturedAt: t0, CPUTicks: 100, MSTicks: 100}
	current := cpuSnapshot{
		CapturedAt: t0.Add(cpuSampleCacheMaxAge + time.Second),
		CPUTicks:   200,
		MSTicks:    200,
	}
	out := computeCPUDelta(prev, current)
	if !out.Pending {
		t.Errorf("baseline older than max age should yield pending=true; got %+v", out)
	}
}
