package probe

import (
    "math"
    "sort"
    "time"

    "github.com/wyw14/solo-037/internal/model"
)

// Comparison statuses explain why a metric may be missing. Metrics that cannot
// be computed are emitted as null pointers rather than NaN, because encoding/json
// rejects NaN.
const (
    StatusOK                    = "ok"
    StatusScalar                = "scalar"
    StatusInsufficientSample    = "insufficient_sample"
    StatusZeroSpan              = "zero_span"
    StatusZeroVariance          = "undefined_zero_variance"
    StatusZeroAmplitude         = "undefined_zero_amplitude"
    StatusMissingSource         = "missing_source"
)

// DefaultGridPoints is the resampling grid used for repeatability comparisons.
const DefaultGridPoints = 101

// Comparison holds repeatability metrics for one physical source across two
// runs. Pointers are nil (JSON null) when a metric is undefined.
type Comparison struct {
    Status     string   `json:"status"`
    GridPoints int      `json:"grid_points"`
    Pearson    *float64 `json:"pearson"`
    NRMSD      *float64 `json:"nrmsd"`
    MeanDelta  *float64 `json:"mean_delta"`
    RMSDelta   *float64 `json:"rms_delta"`
    SampledA   *float64 `json:"std_a"`
    SampledB   *float64 `json:"std_b"`
}

// GapInfo describes the sampling continuity of one run/source series.
type GapInfo struct {
    Samples   int           `json:"samples"`
    MaxGapMs  int64         `json:"max_gap_ms"`
    MedianGapMs int64       `json:"median_gap_ms"`
    Gapped    bool          `json:"gapped"`
}

// CompareSeries compares two series from the same physical source measured in
// two different runs. Each side is time-normalized on its own window and
// resampled onto a shared fractional-time grid, so the comparison is about
// wave/force shape, not absolute clock time (absolute spans are shown in the
// coverage matrix).
func CompareSeries(rawA, rawB []model.Evidence, gridPoints int) Comparison {
    if gridPoints <= 0 {
        gridPoints = DefaultGridPoints
    }
    a := orderedCopy(rawA)
    b := orderedCopy(rawB)
    result := Comparison{Status: StatusOK, GridPoints: gridPoints}
    if len(a) == 0 || len(b) == 0 {
        result.Status = StatusMissingSource
        result.GridPoints = 0
        return result
    }
    if len(a) == 1 && len(b) == 1 {
        result.Status = StatusScalar
        result.GridPoints = 0
        delta := a[0].Value - b[0].Value
        result.MeanDelta = &delta
        return result
    }
    if len(a) == 1 || len(b) == 1 {
        result.Status = StatusInsufficientSample
        result.GridPoints = 0
        return result
    }
    va, okA, reasonA := NormalizedGrid(a, gridPoints)
    vb, okB, reasonB := NormalizedGrid(b, gridPoints)
    if !okA || !okB {
        result.Status = firstBadReason(reasonA, reasonB)
        result.GridPoints = 0
        return result
    }
    n := float64(len(va))
    var sumDeltaSq, sumDelta float64
    for i := range va {
        delta := va[i] - vb[i]
        sumDelta += delta
        sumDeltaSq += delta * delta
    }
    meanDelta := sumDelta / n
    result.MeanDelta = &meanDelta
    rmsDelta := math.Sqrt(sumDeltaSq / n)
    result.RMSDelta = &rmsDelta
    sa := populationStd(va)
    sb := populationStd(vb)
    result.SampledA = &sa
    result.SampledB = &sb

    if r, ok := Pearson(va, vb); ok {
        result.Pearson = &r
    } else {
        result.Status = StatusZeroVariance
    }
    if d, ok := NRMSD(va, vb); ok {
        result.NRMSD = &d
    } else if result.Status == StatusOK {
        result.Status = StatusZeroAmplitude
    }
    return result
}

// NormalizedGrid sorts samples, maps their timestamps onto [0,1] and linearly
// interpolates values onto n equally spaced grid points (inclusive endpoints).
func NormalizedGrid(samples []model.Evidence, n int) ([]float64, bool, string) {
    if n < 2 {
        n = 2
    }
    if len(samples) == 0 {
        return nil, false, StatusInsufficientSample
    }
    if len(samples) == 1 {
        return nil, false, StatusInsufficientSample
    }
    first := samples[0].RecordedAt
    last := samples[len(samples)-1].RecordedAt
    span := last.Sub(first)
    if span <= 0 {
        return nil, false, StatusZeroSpan
    }
    values := make([]float64, n)
    idx := 0
    for k := 0; k < n; k++ {
        target := first.Add(time.Duration(float64(k) / float64(n-1) * float64(span)))
        for idx+1 < len(samples) && samples[idx+1].RecordedAt.Before(target) {
            idx++
        }
        s0 := samples[idx]
        s1 := samples[min(idx+1, len(samples)-1)]
        if idx+1 >= len(samples) || s0.RecordedAt.Equal(target) || s1.RecordedAt.Equal(s0.RecordedAt) {
            values[k] = s0.Value
            continue
        }
        if !target.After(s0.RecordedAt) {
            values[k] = s0.Value
            continue
        }
        if !target.Before(s1.RecordedAt) {
            values[k] = s1.Value
            continue
        }
        fraction := float64(target.Sub(s0.RecordedAt)) / float64(s1.RecordedAt.Sub(s0.RecordedAt))
        values[k] = s0.Value + fraction*(s1.Value-s0.Value)
    }
    return values, true, StatusOK
}

// Pearson returns the Pearson product-moment correlation of two equally long
// vectors. ok is false when either vector has zero variance.
func Pearson(x, y []float64) (float64, bool) {
    n := float64(len(x))
    if n == 0 || len(x) != len(y) {
        return 0, false
    }
    var sx, sy float64
    for i := range x {
        sx += x[i]
        sy += y[i]
    }
    mx, my := sx/n, sy/n
    var cov, vx, vy float64
    for i := range x {
        dx, dy := x[i]-mx, y[i]-my
        cov += dx * dy
        vx += dx * dx
        vy += dy * dy
    }
    if vx == 0 || vy == 0 {
        return 0, false
    }
    return cov / math.Sqrt(vx*vy), true
}

// NRMSD normalizes the RMS difference by the mean amplitude of the two series:
// sqrt(mean((a-b)^2)) / ((range(a)+range(b))/2). Averaging both ranges keeps
// the metric symmetric in run order. ok is false when both sides are constant.
func NRMSD(x, y []float64) (float64, bool) {
    if len(x) == 0 || len(x) != len(y) {
        return 0, false
    }
    var sumSq float64
    for i := range x {
        d := x[i] - y[i]
        sumSq += d * d
    }
    rms := math.Sqrt(sumSq / float64(len(x)))
    denom := (rangeOf(x) + rangeOf(y)) / 2
    if denom == 0 {
        return 0, false
    }
    return rms / denom, true
}

// DetectGaps flags discontinuous sampling without a configured sample rate:
// a gap is flagged when the largest interval is more than twice the median.
// Series with fewer than three samples cannot establish a cadence.
func DetectGaps(samples []model.Evidence) GapInfo {
    ordered := orderedCopy(samples)
    info := GapInfo{Samples: len(ordered)}
    if len(ordered) < 2 {
        return info
    }
    gaps := make([]int64, 0, len(ordered)-1)
    var maxGap int64
    for i := 1; i < len(ordered); i++ {
        gap := ordered[i].RecordedAt.Sub(ordered[i-1].RecordedAt).Milliseconds()
        gaps = append(gaps, gap)
        if gap > maxGap {
            maxGap = gap
        }
    }
    info.MaxGapMs = maxGap
    sort.Slice(gaps, func(i, j int) bool { return gaps[i] < gaps[j] })
    middle := gaps[len(gaps)/2]
    if len(gaps)%2 == 0 {
        middle = (gaps[len(gaps)/2-1] + gaps[len(gaps)/2]) / 2
    }
    info.MedianGapMs = middle
    if len(ordered) >= 3 && middle > 0 && maxGap > 2*middle {
        info.Gapped = true
    }
    return info
}

func orderedCopy(samples []model.Evidence) []model.Evidence {
    ordered := append([]model.Evidence{}, samples...)
    sort.SliceStable(ordered, func(i, j int) bool {
        return ordered[i].RecordedAt.Before(ordered[j].RecordedAt)
    })
    return ordered
}

func firstBadReason(a, b string) string {
    if a != StatusOK {
        return a
    }
    return b
}

func populationStd(values []float64) float64 {
    var sum float64
    for _, v := range values {
        sum += v
    }
    mean := sum / float64(len(values))
    var squares float64
    for _, v := range values {
        d := v - mean
        squares += d * d
    }
    return math.Sqrt(squares / float64(len(values)))
}

func rangeOf(values []float64) float64 {
    min, max := values[0], values[0]
    for _, v := range values[1:] {
        if v < min {
            min = v
        }
        if v > max {
            max = v
        }
    }
    return max - min
}
