package service

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/wyw14/solo-037/internal/model"
)

func standardConditions() model.Conditions {
	return model.Conditions{TowSpeed: 1.2, WaveCondition: "calm", Loading: "draft-0.3"}
}

func waveSeries(source string, start time.Time, n int, gap time.Duration, f func(k int) float64) []model.Evidence {
	out := make([]model.Evidence, n)
	for k := 0; k < n; k++ {
		out[k] = model.NewEvidence("", "", source, model.EvidenceWaveTime, f(k), start.Add(time.Duration(k)*gap))
	}
	return out
}

func forceSamples(source string, at time.Time, values ...float64) []model.Evidence {
	out := make([]model.Evidence, len(values))
	for i, v := range values {
		out[i] = model.NewEvidence("", "", source, model.EvidenceForce, v, at.Add(time.Duration(i)*time.Second))
	}
	return out
}

func measuredRun(name string, conditions model.Conditions, at time.Time, evidence ...model.Evidence) MeasuredRun {
	op := model.NewOperation(name, "hull-X", "section-a", "carriage-1", at).WithConditions(conditions)
	for i := range evidence {
		evidence[i].Run = op.ID
		evidence[i].Revision = op.Revision
	}
	return MeasuredRun{Operation: op, Evidence: evidence}
}

func runsMap(runs ...MeasuredRun) map[model.RunID]MeasuredRun {
	out := make(map[model.RunID]MeasuredRun, len(runs))
	for _, run := range runs {
		out[run.Operation.ID] = run
	}
	return out
}

func groupOf(runs ...MeasuredRun) TrialGroup {
	ids := make([]model.RunID, len(runs))
	for i, run := range runs {
		ids[i] = run.Operation.ID
	}
	return TrialGroup{ID: "group-1", Name: "g", RunIDs: ids, CreatedAt: runs[0].Operation.CreatedAt}
}

func sine() func(k int) float64 {
	return func(k int) float64 { return math.Sin(float64(k) / 40 * 2 * math.Pi) }
}

func findSource(comparisons []SourceComparison, source string) (SourceComparison, bool) {
	for _, c := range comparisons {
		if c.Source == source {
			return c, true
		}
	}
	return SourceComparison{}, false
}

func hasBlocker(report ConsistencyReport, code string) bool {
	for _, b := range report.Blockers {
		if b.Code == code {
			return true
		}
	}
	return false
}

func TestConsistencyPerfectRepetitionIsComparable(t *testing.T) {
	start := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	a := measuredRun("tow A", standardConditions(), start, waveSeries("left", start, 60, 100*time.Millisecond, sine())...)
	b := measuredRun("tow B", standardConditions(), start.Add(time.Hour), waveSeries("left", start.Add(2*time.Hour), 60, 250*time.Millisecond, sine())...)
	report := BuildConsistencyReport(groupOf(a, b), runsMap(a, b), start)
	if !report.Comparable || len(report.Blockers) != 0 {
		t.Fatalf("identical conditions blocked: %+v", report.Blockers)
	}
	cmp, ok := findSource(report.Wave, "left")
	if !ok || len(cmp.Pairs) != 1 {
		t.Fatalf("missing left probe pair: %+v", report.Wave)
	}
	metrics := cmp.Pairs[0].Metrics
	if metrics.Status != "ok" || metrics.Pearson == nil || math.Abs(*metrics.Pearson-1) > 1e-9 {
		t.Fatalf("wave correlation not ~1: %+v", metrics)
	}
	if metrics.NRMSD == nil || math.Abs(*metrics.NRMSD) > 1e-9 {
		t.Fatalf("wave nrmsd not ~0: %+v", metrics)
	}
	if len(report.RunSummaries) != 2 || report.RunSummaries[0].WaveCount != 60 {
		t.Fatalf("run summaries wrong: %+v", report.RunSummaries)
	}
}

func TestConsistencyTowSpeedMismatchBlocksAndListsDifference(t *testing.T) {
	start := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	a := measuredRun("tow A", standardConditions(), start, waveSeries("left", start, 30, time.Second, sine())...)
	other := standardConditions()
	other.TowSpeed = 1.5
	b := measuredRun("tow B", other, start.Add(time.Hour), waveSeries("left", start.Add(2*time.Hour), 30, time.Second, sine())...)
	report := BuildConsistencyReport(groupOf(a, b), runsMap(a, b), start)
	if report.Comparable {
		t.Fatal("different tow speeds must not be comparable")
	}
	if !hasBlocker(report, BlockerConditionMismatch) || len(report.ConditionDifferences) != 1 {
		t.Fatalf("expected one condition mismatch, got %+v", report.Blockers)
	}
	diff := report.ConditionDifferences[0]
	if diff.Field != model.FieldTowSpeed || len(diff.Values) != 2 ||
		diff.Values[0].Value == nil || *diff.Values[0].Value != "1.2" ||
		diff.Values[1].Value == nil || *diff.Values[1].Value != "1.5" {
		t.Fatalf("tow speed difference detail wrong: %+v", diff)
	}
	// Cross-run metrics are withheld, but per-run summaries remain available.
	if len(report.Wave) != 0 {
		t.Fatal("cross-run metrics must be withheld on mismatch")
	}
	if len(report.RunSummaries) != 2 {
		t.Fatal("per-run summaries must still be returned")
	}
}

func TestConsistencyLegacyRunsWithoutConditionsAreBlocked(t *testing.T) {
	start := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	a := measuredRun("legacy A", model.Conditions{}, start, waveSeries("left", start, 20, time.Second, sine())...)
	b := measuredRun("legacy B", model.Conditions{}, start.Add(time.Hour), waveSeries("left", start.Add(2*time.Hour), 20, time.Second, sine())...)
	report := BuildConsistencyReport(groupOf(a, b), runsMap(a, b), start)
	if report.Comparable || !hasBlocker(report, BlockerConditionsUnset) {
		t.Fatalf("zero conditions must never count as shared: %+v", report.Blockers)
	}
	if len(report.ConditionDifferences) != 0 {
		t.Fatal("unconfigured runs must not produce field mismatch rows")
	}
}

func TestConsistencyMissingRunBlocks(t *testing.T) {
	start := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	a := measuredRun("tow A", standardConditions(), start)
	group := TrialGroup{ID: "g", Name: "g", RunIDs: []model.RunID{a.Operation.ID, "vanished"}}
	report := BuildConsistencyReport(group, runsMap(a), start)
	if report.Comparable || !hasBlocker(report, BlockerMissingRun) {
		t.Fatalf("missing run must block: %+v", report.Blockers)
	}
	if len(report.ConditionTable) != 1 {
		t.Fatal("present run should still appear in the condition table")
	}
}

func TestConsistencySingleMemberBlocks(t *testing.T) {
	start := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	a := measuredRun("lonely", standardConditions(), start)
	group := TrialGroup{ID: "g", Name: "g", RunIDs: []model.RunID{a.Operation.ID}}
	report := BuildConsistencyReport(group, runsMap(a), start)
	if !hasBlocker(report, BlockerMemberCount) || report.Comparable {
		t.Fatalf("single member must block: %+v", report.Blockers)
	}
}

func TestConsistencyAbortedRunIsAdvisoryOnly(t *testing.T) {
	start := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	a := measuredRun("tow A", standardConditions(), start, waveSeries("left", start, 30, time.Second, sine())...)
	b := measuredRun("tow B", standardConditions(), start.Add(time.Hour), waveSeries("left", start.Add(2*time.Hour), 30, time.Second, sine())...)
	b.Operation.Stage = model.StageAborted
	report := BuildConsistencyReport(groupOf(a, b), runsMap(a, b), start)
	if !report.Comparable {
		t.Fatalf("abort must not block: %+v", report.Blockers)
	}
	found := false
	for _, advisory := range report.Advisories {
		if advisory.Code == AdvisoryAbortedRun && advisory.RunID == b.Operation.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("aborted run advisory missing: %+v", report.Advisories)
	}
}

func TestConsistencyCoverageUnionAndIntersection(t *testing.T) {
	start := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	a := measuredRun("two probes", standardConditions(), start,
		append(waveSeries("left", start, 30, time.Second, sine()), waveSeries("right", start, 30, time.Second, sine())...)...)
	b := measuredRun("one probe", standardConditions(), start.Add(time.Hour),
		waveSeries("left", start.Add(2*time.Hour), 30, time.Second, sine())...)
	report := BuildConsistencyReport(groupOf(a, b), runsMap(a, b), start)
	if !report.Comparable {
		t.Fatalf("coverage differences must not block: %+v", report.Blockers)
	}
	if len(report.Coverage.WaveSources) != 2 || len(report.Coverage.CommonWaveSources) != 1 ||
		report.Coverage.CommonWaveSources[0] != "left" {
		t.Fatalf("union/intersection wrong: %+v", report.Coverage)
	}
	// Second run's row must mark "right" absent.
	var rightCoverage SourceCoverage
	for _, c := range report.Coverage.Runs[1].Wave {
		if c.SourceID == "right" {
			rightCoverage = c
		}
	}
	if rightCoverage.Present || rightCoverage.Count != 0 {
		t.Fatalf("missing probe must show absent: %+v", rightCoverage)
	}
	// Pairs: "left" compares normally; "right" pair is missing_source.
	left, _ := findSource(report.Wave, "left")
	right, _ := findSource(report.Wave, "right")
	if len(left.Pairs) != 1 || left.Pairs[0].Metrics.Status != "ok" {
		t.Fatalf("common probe pair wrong: %+v", left)
	}
	if len(right.Pairs) != 1 || right.Pairs[0].Metrics.Status != "missing_source" {
		t.Fatalf("probe only on one run must be missing_source for the pair: %+v", right)
	}
}

func TestConsistencyScalarForceReadings(t *testing.T) {
	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	a := measuredRun("tow A", standardConditions(), at, forceSamples("load-cell", at, 12.0)...)
	b := measuredRun("tow B", standardConditions(), at.Add(time.Hour), forceSamples("load-cell", at.Add(2*time.Hour), 12.4)...)
	report := BuildConsistencyReport(groupOf(a, b), runsMap(a, b), at)
	if !report.Comparable {
		t.Fatalf("scalar force comparison blocked: %+v", report.Blockers)
	}
	force, ok := findSource(report.Force, "load-cell")
	if !ok || len(force.Pairs) != 1 || force.Pairs[0].Metrics.Status != "scalar" {
		t.Fatalf("expected scalar force pair: %+v", report.Force)
	}
	if math.Abs(*force.Pairs[0].Metrics.MeanDelta+0.4) > 1e-12 {
		t.Fatalf("force delta wrong: %+v", force.Pairs[0].Metrics)
	}
}

func TestConsistencyReportIsDeterministic(t *testing.T) {
	t1 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	r1 := measuredRun("first", standardConditions(), t1, waveSeries("left", t1, 20, time.Second, sine())...)
	r2 := measuredRun("second", standardConditions(), t1.Add(time.Hour), waveSeries("left", t1.Add(2*time.Hour), 20, time.Second, sine())...)
	r3 := measuredRun("third", standardConditions(), t1.Add(2*time.Hour), waveSeries("left", t1.Add(3*time.Hour), 20, time.Second, sine())...)
	// Members deliberately listed out of creation order.
	group := TrialGroup{ID: "g", Name: "g", RunIDs: []model.RunID{r3.Operation.ID, r1.Operation.ID, r2.Operation.ID}}
	runs := runsMap(r1, r2, r3)
	first := BuildConsistencyReport(group, runs, t1)
	second := BuildConsistencyReport(group, runs, t1)
	b1, _ := json.Marshal(first)
	b2, _ := json.Marshal(second)
	if string(b1) != string(b2) {
		t.Fatal("report JSON is not deterministic")
	}
	if first.ConditionTable[0].RunID != r1.Operation.ID ||
		first.ConditionTable[1].RunID != r2.Operation.ID ||
		first.ConditionTable[2].RunID != r3.Operation.ID {
		t.Fatalf("condition table not creation ordered: %+v", first.ConditionTable)
	}
}
