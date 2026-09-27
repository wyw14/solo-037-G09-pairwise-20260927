package probe

import (
    "math"
    "testing"
    "time"

    "github.com/wyw14/solo-037/internal/model"
)

func makeSamples(values []float64, start time.Time, gap time.Duration) []model.Evidence {
    out := make([]model.Evidence, len(values))
    for i, v := range values {
        out[i] = model.NewEvidence("run", "rev", "src", model.EvidenceWaveTime, v, start.Add(time.Duration(i)*gap))
    }
    return out
}

func approx(x, y, tol float64) bool { return math.Abs(x-y) <= tol }

func TestCompareIdenticalSineShapeAcrossDifferentWindows(t *testing.T) {
    // Same shape sampled over two different absolute windows/durations must
    // correlate perfectly after per-side time normalization.
    var va, vb []float64
    startA := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
    startB := time.Date(2026, 1, 2, 9, 30, 0, 0, time.UTC)
    for k := 0; k < 60; k++ {
        phase := float64(k) / 60 * 2 * math.Pi
        va = append(va, math.Sin(phase))
        vb = append(vb, math.Sin(phase))
    }
    cmp := CompareSeries(makeSamples(va, startA, 100*time.Millisecond), makeSamples(vb, startB, 250*time.Millisecond), 101)
    if cmp.Status != StatusOK || cmp.Pearson == nil || cmp.NRMSD == nil {
        t.Fatalf("expected ok metrics, got %+v", cmp)
    }
    if !approx(*cmp.Pearson, 1, 1e-9) {
        t.Fatalf("pearson = %v, want 1", *cmp.Pearson)
    }
    if !approx(*cmp.NRMSD, 0, 1e-9) || !approx(*cmp.MeanDelta, 0, 1e-9) {
        t.Fatalf("nrmsd=%v meanDelta=%v, want ~0", *cmp.NRMSD, *cmp.MeanDelta)
    }
}

func TestPearsonHandCalculated(t *testing.T) {
    if r, ok := Pearson([]float64{1, 2, 3}, []float64{3, 2, 1}); !ok || !approx(r, -1, 1e-12) {
        t.Fatalf("reverse series r=%v ok=%v, want -1", r, ok)
    }
    if r, ok := Pearson([]float64{1, 2, 3}, []float64{2, 4, 6}); !ok || !approx(r, 1, 1e-12) {
        t.Fatalf("linear series r=%v ok=%v, want 1", r, ok)
    }
    if _, ok := Pearson([]float64{5, 5, 5}, []float64{1, 2, 3}); ok {
        t.Fatal("constant vector must yield undefined correlation")
    }
    if _, ok := Pearson([]float64{1}, []float64{2}); ok {
        t.Fatal("single point must not define correlation")
    }
}

func TestNRMSDHandCalculatedAndSymmetric(t *testing.T) {
    // grids: range([0,1])=1, range([0,0])=0 -> denom 0.5; rms diff = sqrt(0.5)
    d, ok := NRMSD([]float64{0, 1}, []float64{0, 0})
    if !ok || !approx(d, math.Sqrt(0.5)/0.5, 1e-12) {
        t.Fatalf("nrmsd = %v ok=%v", d, ok)
    }
    d2, _ := NRMSD([]float64{0, 0}, []float64{0, 1})
    if !approx(d, d2, 1e-12) {
        t.Fatalf("nrmsd must be symmetric: %v vs %v", d, d2)
    }
    if _, ok := NRMSD([]float64{3, 3}, []float64{5, 5}); ok {
        t.Fatal("two constant series have zero amplitude denominator")
    }
}

func TestCompareScalarForceReadings(t *testing.T) {
    at := time.Now().UTC()
    a := []model.Evidence{model.NewEvidence("r1", "rev", "cell", model.EvidenceForce, 12.0, at)}
    b := []model.Evidence{model.NewEvidence("r2", "rev", "cell", model.EvidenceForce, 12.4, at)}
    cmp := CompareSeries(a, b, DefaultGridPoints)
    if cmp.Status != StatusScalar || cmp.MeanDelta == nil || !approx(*cmp.MeanDelta, -0.4, 1e-12) || cmp.Pearson != nil {
        t.Fatalf("scalar comparison wrong: %+v", cmp)
    }
}

func TestCompareOneVsManyIsInsufficient(t *testing.T) {
    at := time.Now().UTC()
    one := []model.Evidence{model.NewEvidence("r1", "rev", "cell", model.EvidenceForce, 12, at)}
    many := makeSamples([]float64{1, 2, 3}, at, time.Second)
    if cmp := CompareSeries(one, many, DefaultGridPoints); cmp.Status != StatusInsufficientSample {
        t.Fatalf("got %s, want insufficient_sample", cmp.Status)
    }
}

func TestCompareZeroTimeSpan(t *testing.T) {
    at := time.Now().UTC()
    same := func(v float64) model.Evidence { return model.NewEvidence("r", "rev", "s", model.EvidenceWaveTime, v, at) }
    a := []model.Evidence{same(1), same(2), same(3)}
    b := []model.Evidence{same(2), same(3), same(4)}
    if cmp := CompareSeries(a, b, DefaultGridPoints); cmp.Status != StatusZeroSpan {
        t.Fatalf("got %s, want zero_span", cmp.Status)
    }
}

func TestCompareConstantButDifferentSeriesKeepsMeanDelta(t *testing.T) {
    at := time.Now().UTC()
    a := makeSamples([]float64{5, 5, 5, 5}, at, time.Second)
    b := makeSamples([]float64{7, 7, 7, 7}, at, time.Second)
    cmp := CompareSeries(a, b, DefaultGridPoints)
    if cmp.MeanDelta == nil || !approx(*cmp.MeanDelta, -2, 1e-12) {
        t.Fatalf("constant offset must still show mean delta: %+v", cmp)
    }
    if cmp.Pearson != nil || cmp.NRMSD != nil {
        t.Fatal("correlation and normalized amplitude are undefined for constant series")
    }
}

func TestCompareMissingSource(t *testing.T) {
    at := time.Now().UTC()
    a := makeSamples([]float64{1, 2, 3}, at, time.Second)
    if cmp := CompareSeries(a, nil, DefaultGridPoints); cmp.Status != StatusMissingSource {
        t.Fatalf("got %s, want missing_source", cmp.Status)
    }
}

func TestDetectGapsHeuristic(t *testing.T) {
    at := time.Now().UTC()
    even := make([]model.Evidence, 0, 6)
    for i := 0; i < 6; i++ {
        even = append(even, model.NewEvidence("r", "rev", "s", model.EvidenceWaveTime, 1, at.Add(time.Duration(i)*time.Second)))
    }
    if info := DetectGaps(even); info.Gapped {
        t.Fatalf("even cadence flagged as gapped: %+v", info)
    }
    gapped := append([]model.Evidence{}, even[:3]...)
    gapped = append(gapped,
        model.NewEvidence("r", "rev", "s", model.EvidenceWaveTime, 1, at.Add(8*time.Second)),
        model.NewEvidence("r", "rev", "s", model.EvidenceWaveTime, 1, at.Add(9*time.Second)),
        model.NewEvidence("r", "rev", "s", model.EvidenceWaveTime, 1, at.Add(10*time.Second)),
    )
    info := DetectGaps(gapped)
    if !info.Gapped || info.MaxGapMs != 6000 {
        t.Fatalf("6x gap not detected: %+v", info)
    }
    if DetectGaps(even[:2]).Gapped {
        t.Fatal("two points cannot establish a cadence")
    }
    if DetectGaps(nil).Samples != 0 {
        t.Fatal("empty series mishandled")
    }
}
