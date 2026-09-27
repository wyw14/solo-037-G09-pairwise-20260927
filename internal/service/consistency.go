package service

import (
	"math"
	"sort"
	"time"

	"github.com/wyw14/solo-037/internal/model"
	"github.com/wyw14/solo-037/internal/probe"
)

// Blocker codes describe why cross-run repeatability cannot be concluded.
const (
	BlockerMemberCount       = "member_count"
	BlockerMissingRun        = "missing_run"
	BlockerConditionsUnset   = "conditions_unconfigured"
	BlockerConditionMismatch = "condition_mismatch"
)

// Advisory codes flag things an operator should know but which do not by
// themselves invalidate a comparison.
const (
	AdvisoryAbortedRun          = "aborted_run"
	AdvisoryNoCommonWaveSource  = "no_common_wave_source"
	AdvisoryNoCommonForceSource = "no_common_force_source"
	AdvisoryMetricUndefined     = "metric_undefined"
)

// ConsistencyReport explains repeatability for one trial group. It deliberately
// has no single averaged score: blockers name condition differences, the
// coverage matrix names evidence gaps, and metrics stay per source/pair.
type ConsistencyReport struct {
	Group                TrialGroup            `json:"group"`
	GeneratedAt          time.Time             `json:"generated_at"`
	Comparable           bool                  `json:"comparable"`
	Blockers             []Blocker             `json:"blockers"`
	Advisories           []Advisory            `json:"advisories"`
	ConditionTable       []RunConditionView    `json:"condition_table"`
	ConditionDifferences []ConditionDifference `json:"condition_differences"`
	Coverage             CoverageMatrix        `json:"coverage"`
	Wave                 []SourceComparison    `json:"wave"`
	Force                []SourceComparison    `json:"force"`
	RunSummaries         []RunSummary          `json:"run_summaries"`
}

type Blocker struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	RunID   model.RunID `json:"run_id,omitempty"`
}

type Advisory struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	RunID   model.RunID `json:"run_id,omitempty"`
	Source  string      `json:"source,omitempty"`
}

// RunConditionView is one column of the condition table. Optional condition
// fields are pointers so an unconfigured run renders null instead of "0"/"".
type RunConditionView struct {
	RunID         model.RunID    `json:"run_id"`
	Name          string         `json:"name"`
	Stage         model.RunStage `json:"stage"`
	ModelName     string         `json:"model_name"`
	Section       string         `json:"section"`
	Carriage      string         `json:"carriage"`
	TowSpeed      *float64       `json:"tow_speed"`
	WaveCondition *string        `json:"wave_condition"`
	Loading       *string        `json:"loading"`
	Configured    bool           `json:"configured"`
}

type FieldValue struct {
	RunID      model.RunID `json:"run_id"`
	RunName    string      `json:"run_name"`
	Value      *string     `json:"value"`
	Configured bool        `json:"configured"`
}

type ConditionDifference struct {
	Field  string       `json:"field"`
	Values []FieldValue `json:"values"`
}

type CoverageMatrix struct {
	WaveSources        []string      `json:"wave_sources"`
	ForceSources       []string      `json:"force_sources"`
	CommonWaveSources  []string      `json:"common_wave_sources"`
	CommonForceSources []string      `json:"common_force_sources"`
	Runs               []RunCoverage `json:"runs"`
}

type RunCoverage struct {
	RunID model.RunID      `json:"run_id"`
	Name  string           `json:"name"`
	Stage model.RunStage   `json:"stage"`
	Wave  []SourceCoverage `json:"wave"`
	Force []SourceCoverage `json:"force"`
}

type SourceCoverage struct {
	SourceID string        `json:"source_id"`
	Present  bool          `json:"present"`
	Count    int           `json:"count"`
	First    time.Time     `json:"first,omitempty"`
	Last     time.Time     `json:"last,omitempty"`
	SpanMs   int64         `json:"span_ms"`
	Gap      probe.GapInfo `json:"gap"`
}

type SourceComparison struct {
	Kind   model.EvidenceKind `json:"kind"`
	Source string             `json:"source"`
	Pairs  []PairComparison   `json:"pairs"`
}

type PairComparison struct {
	RunA    model.RunID      `json:"run_a"`
	RunB    model.RunID      `json:"run_b"`
	Metrics probe.Comparison `json:"metrics"`
}

type RunSummary struct {
	RunID      model.RunID    `json:"run_id"`
	Name       string         `json:"name"`
	Stage      model.RunStage `json:"stage"`
	WaveCount  int            `json:"wave_count"`
	WaveMean   float64        `json:"wave_mean"`
	WaveRMS    float64        `json:"wave_rms"`
	ForceCount int            `json:"force_count"`
	ForceMean  float64        `json:"force_mean"`
	ForceStd   float64        `json:"force_std"`
}

// BuildConsistencyReport is a pure function: archives, locks and the clock
// stay at the Runtime boundary, so every blocking and coverage scenario can be
// driven directly from a table of runs.
func BuildConsistencyReport(group TrialGroup, runs map[model.RunID]MeasuredRun, at time.Time) ConsistencyReport {
	report := ConsistencyReport{
		Group:                group,
		GeneratedAt:          at.UTC(),
		Blockers:             []Blocker{},
		Advisories:           []Advisory{},
		ConditionTable:       []RunConditionView{},
		ConditionDifferences: []ConditionDifference{},
		Wave:                 []SourceComparison{},
		Force:                []SourceComparison{},
		RunSummaries:         []RunSummary{},
	}

	memberIDs := normalizeRunIDs(group.RunIDs)
	present := make([]MeasuredRun, 0, len(memberIDs))
	missing := make([]model.RunID, 0)
	for _, id := range memberIDs {
		if run, ok := runs[id]; ok {
			present = append(present, run)
		} else {
			missing = append(missing, id)
		}
	}
	sort.SliceStable(present, func(i, j int) bool {
		ti, tj := present[i].Operation.CreatedAt, present[j].Operation.CreatedAt
		if ti.Equal(tj) {
			return present[i].Operation.ID < present[j].Operation.ID
		}
		return ti.Before(tj)
	})

	if len(memberIDs) < 2 {
		report.Blockers = append(report.Blockers, Blocker{
			Code:    BlockerMemberCount,
			Message: "a repeatability group needs at least two distinct runs",
		})
	}
	for _, id := range missing {
		report.Blockers = append(report.Blockers, Blocker{
			Code:    BlockerMissingRun,
			Message: "group references a run that is not in the measurement archive",
			RunID:   id,
		})
	}

	for _, run := range present {
		report.ConditionTable = append(report.ConditionTable, conditionView(run))
		report.RunSummaries = append(report.RunSummaries, summarizeRun(run))
		if run.Operation.Stage == model.StageAborted {
			report.Advisories = append(report.Advisories, Advisory{
				Code:    AdvisoryAbortedRun,
				Message: "run was aborted; its evidence may only cover part of the tow",
				RunID:   run.Operation.ID,
			})
		}
	}

	// Field-by-field condition comparison only makes sense when every member
	// exists; a missing member already blocks the report.
	conditionsComparable := len(missing) == 0 && len(present) >= 2
	if conditionsComparable {
		for _, run := range present {
			if !run.Operation.Conditions.Configured() {
				report.Blockers = append(report.Blockers, Blocker{
					Code:    BlockerConditionsUnset,
					Message: "run has no complete tow conditions (tow speed, wave condition, loading); legacy/unconfigured runs cannot be assumed to share conditions",
					RunID:   run.Operation.ID,
				})
			}
		}
		if len(report.blockerCodes(BlockerConditionsUnset)) == 0 {
			report.ConditionDifferences = compareConditions(present)
			if len(report.ConditionDifferences) > 0 {
				report.Blockers = append(report.Blockers, Blocker{
					Code:    BlockerConditionMismatch,
					Message: "runs do not share the same physical conditions; cross-run metrics are withheld",
				})
			}
		}
	}

	report.Coverage = buildCoverage(present)
	if len(present) > 0 {
		if len(report.Coverage.CommonWaveSources) == 0 {
			report.Advisories = append(report.Advisories, Advisory{
				Code:    AdvisoryNoCommonWaveSource,
				Message: "no wave probe is present on every run; wave repeatability can only be read for the pairs sharing a source",
			})
		}
		if len(report.Coverage.CommonForceSources) == 0 {
			report.Advisories = append(report.Advisories, Advisory{
				Code:    AdvisoryNoCommonForceSource,
				Message: "no force sensor is present on every run; force repeatability can only be read for the pairs sharing a source",
			})
		}
	}

	// Cross-run metrics are only produced when nothing blocks the comparison.
	// Condition differences must surface as differences, never be averaged away.
	report.Comparable = len(report.Blockers) == 0
	if report.Comparable && len(present) >= 2 {
		var metricAdvisories []Advisory
		report.Wave, report.Force, metricAdvisories = compareAcrossRuns(present)
		report.Advisories = append(report.Advisories, metricAdvisories...)
	}
	// Re-sort the whole slice so JSON output is deterministic regardless of
	// collection order.
	sortAdvisories(report.Advisories)
	report.Advisories = dedupeAdvisories(report.Advisories)

	report.Coverage = ensureCoverageSlices(report.Coverage)
	return report
}

func (r ConsistencyReport) blockerCodes(code string) []Blocker {
	var out []Blocker
	for _, b := range r.Blockers {
		if b.Code == code {
			out = append(out, b)
		}
	}
	return out
}

func conditionView(run MeasuredRun) RunConditionView {
	op := run.Operation
	view := RunConditionView{
		RunID:      op.ID,
		Name:       op.Name,
		Stage:      op.Stage,
		ModelName:  op.ModelName,
		Section:    op.Section,
		Carriage:   op.Carriage,
		Configured: op.Conditions.Configured(),
	}
	if op.Conditions.TowSpeed > 0 {
		speed := op.Conditions.TowSpeed
		view.TowSpeed = &speed
	}
	if v, ok := op.Conditions.RenderField(model.FieldWaveCondition); ok {
		view.WaveCondition = &v
	}
	if v, ok := op.Conditions.RenderField(model.FieldLoading); ok {
		view.Loading = &v
	}
	return view
}

func renderFieldValue(op model.Operation, field string) (*string, bool) {
	switch field {
	case model.FieldModelName:
		v := op.ModelName
		return &v, true
	case model.FieldSection:
		v := op.Section
		return &v, true
	case model.FieldCarriage:
		v := op.Carriage
		return &v, true
	default:
		v, ok := op.Conditions.RenderField(field)
		if !ok {
			return nil, false
		}
		return &v, true
	}
}

func compareConditions(present []MeasuredRun) []ConditionDifference {
	differences := []ConditionDifference{}
	for _, field := range model.ConditionFields() {
		values := make([]FieldValue, 0, len(present))
		first, firstOK := renderFieldValue(present[0].Operation, field)
		disagree := false
		for _, run := range present {
			value, configured := renderFieldValue(run.Operation, field)
			values = append(values, FieldValue{
				RunID:      run.Operation.ID,
				RunName:    run.Operation.Name,
				Value:      value,
				Configured: configured,
			})
			if configured != firstOK || (configured && firstOK && *value != *first) {
				disagree = true
			}
		}
		if disagree {
			differences = append(differences, ConditionDifference{Field: field, Values: values})
		}
	}
	return differences
}

func groupEvidenceBySource(run MeasuredRun, kind model.EvidenceKind) map[string][]model.Evidence {
	out := make(map[string][]model.Evidence)
	for _, ev := range run.Evidence {
		if ev.Kind == kind {
			out[ev.SourceID] = append(out[ev.SourceID], ev)
		}
	}
	return out
}

type perRun struct {
	wave  map[string][]model.Evidence
	force map[string][]model.Evidence
}

func buildCoverage(present []MeasuredRun) CoverageMatrix {
	matrix := CoverageMatrix{
		WaveSources:        []string{},
		ForceSources:       []string{},
		CommonWaveSources:  []string{},
		CommonForceSources: []string{},
		Runs:               []RunCoverage{},
	}
	grouped := make([]perRun, len(present))
	waveUnion := map[string]struct{}{}
	forceUnion := map[string]struct{}{}
	for i, run := range present {
		grouped[i] = perRun{
			wave:  groupEvidenceBySource(run, model.EvidenceWaveTime),
			force: groupEvidenceBySource(run, model.EvidenceForce),
		}
		for source := range grouped[i].wave {
			waveUnion[source] = struct{}{}
		}
		for source := range grouped[i].force {
			forceUnion[source] = struct{}{}
		}
	}
	matrix.WaveSources = sortedKeysMap(waveUnion)
	matrix.ForceSources = sortedKeysMap(forceUnion)
	matrix.CommonWaveSources = commonSources(grouped, matrix.WaveSources, func(g perRun) map[string][]model.Evidence { return g.wave })
	matrix.CommonForceSources = commonSources(grouped, matrix.ForceSources, func(g perRun) map[string][]model.Evidence { return g.force })

	for i, run := range present {
		coverage := RunCoverage{
			RunID: run.Operation.ID,
			Name:  run.Operation.Name,
			Stage: run.Operation.Stage,
			Wave:  []SourceCoverage{},
			Force: []SourceCoverage{},
		}
		for _, source := range matrix.WaveSources {
			coverage.Wave = append(coverage.Wave, sourceCoverage(source, grouped[i].wave[source]))
		}
		for _, source := range matrix.ForceSources {
			coverage.Force = append(coverage.Force, sourceCoverage(source, grouped[i].force[source]))
		}
		matrix.Runs = append(matrix.Runs, coverage)
	}
	return matrix
}

func sortedKeysMap(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func commonSources(grouped []perRun, union []string, pick func(perRun) map[string][]model.Evidence) []string {
	if len(grouped) == 0 {
		return []string{}
	}
	out := []string{}
	for _, source := range union {
		common := true
		for _, g := range grouped {
			if len(pick(g)[source]) == 0 {
				common = false
				break
			}
		}
		if common {
			out = append(out, source)
		}
	}
	return out
}

func sourceCoverage(source string, samples []model.Evidence) SourceCoverage {
	if len(samples) == 0 {
		return SourceCoverage{SourceID: source, Present: false, Gap: probe.DetectGaps(nil)}
	}
	ordered := make([]model.Evidence, len(samples))
	copy(ordered, samples)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].RecordedAt.Before(ordered[j].RecordedAt)
	})
	first, last := ordered[0].RecordedAt, ordered[len(ordered)-1].RecordedAt
	return SourceCoverage{
		SourceID: source,
		Present:  true,
		Count:    len(ordered),
		First:    first,
		Last:     last,
		SpanMs:   last.Sub(first).Milliseconds(),
		Gap:      probe.DetectGaps(ordered),
	}
}

func compareAcrossRuns(present []MeasuredRun) ([]SourceComparison, []SourceComparison, []Advisory) {
	var advisories []Advisory
	wave := compareKind(present, model.EvidenceWaveTime, &advisories)
	force := compareKind(present, model.EvidenceForce, &advisories)
	return wave, force, advisories
}

func compareKind(present []MeasuredRun, kind model.EvidenceKind, advisories *[]Advisory) []SourceComparison {
	union := map[string]struct{}{}
	groups := make([]map[string][]model.Evidence, len(present))
	for i, run := range present {
		groups[i] = groupEvidenceBySource(run, kind)
		for source := range groups[i] {
			union[source] = struct{}{}
		}
	}
	sources := sortedKeysMap(union)
	result := make([]SourceComparison, 0, len(sources))
	for _, source := range sources {
		comparison := SourceComparison{Kind: kind, Source: source, Pairs: []PairComparison{}}
		for i := 0; i < len(present); i++ {
			for j := i + 1; j < len(present); j++ {
				aSamples := groups[i][source]
				bSamples := groups[j][source]
				if len(aSamples) == 0 && len(bSamples) == 0 {
					continue // neither run carries this source; nothing to compare
				}
				metrics := probe.CompareSeries(aSamples, bSamples, probe.DefaultGridPoints)
				comparison.Pairs = append(comparison.Pairs, PairComparison{
					RunA:    present[i].Operation.ID,
					RunB:    present[j].Operation.ID,
					Metrics: metrics,
				})
				switch metrics.Status {
				case probe.StatusOK, probe.StatusScalar, probe.StatusMissingSource:
					// ok is a metric; scalar is the normal force case; a
					// missing source is already shown by the coverage matrix.
				default:
					*advisories = append(*advisories, Advisory{
						Code:    AdvisoryMetricUndefined,
						Message: "metrics for " + source + " are not fully defined on this pair: " + metrics.Status,
						RunID:   present[j].Operation.ID,
						Source:  source,
					})
				}
			}
		}
		result = append(result, comparison)
	}
	return result
}

func summarizeRun(run MeasuredRun) RunSummary {
	wave, force := make([]model.Evidence, 0), make([]model.Evidence, 0)
	for _, ev := range run.Evidence {
		switch ev.Kind {
		case model.EvidenceWaveTime:
			wave = append(wave, ev)
		case model.EvidenceForce:
			force = append(force, ev)
		}
	}
	waveAnalysis := probe.Analyze(wave)
	count, mean, std := numericStats(force)
	return RunSummary{
		RunID:      run.Operation.ID,
		Name:       run.Operation.Name,
		Stage:      run.Operation.Stage,
		WaveCount:  waveAnalysis.Count,
		WaveMean:   waveAnalysis.Mean,
		WaveRMS:    waveAnalysis.RMS,
		ForceCount: count,
		ForceMean:  mean,
		ForceStd:   std,
	}
}

func numericStats(samples []model.Evidence) (int, float64, float64) {
	if len(samples) == 0 {
		return 0, 0, 0
	}
	var sum float64
	for _, ev := range samples {
		sum += ev.Value
	}
	mean := sum / float64(len(samples))
	var squares float64
	for _, ev := range samples {
		d := ev.Value - mean
		squares += d * d
	}
	return len(samples), mean, math.Sqrt(squares / float64(len(samples)))
}

func sortAdvisories(advisories []Advisory) {
	sort.SliceStable(advisories, func(i, j int) bool {
		if advisories[i].Code != advisories[j].Code {
			return advisories[i].Code < advisories[j].Code
		}
		if advisories[i].RunID != advisories[j].RunID {
			return advisories[i].RunID < advisories[j].RunID
		}
		return advisories[i].Source < advisories[j].Source
	})
}

func dedupeAdvisories(advisories []Advisory) []Advisory {
	out := make([]Advisory, 0, len(advisories))
	for _, advisory := range advisories {
		if len(out) > 0 && out[len(out)-1].Code == advisory.Code &&
			out[len(out)-1].RunID == advisory.RunID && out[len(out)-1].Source == advisory.Source {
			continue
		}
		out = append(out, advisory)
	}
	return out
}

func ensureCoverageSlices(matrix CoverageMatrix) CoverageMatrix {
	if matrix.Runs == nil {
		matrix.Runs = []RunCoverage{}
	}
	for i := range matrix.Runs {
		if matrix.Runs[i].Wave == nil {
			matrix.Runs[i].Wave = []SourceCoverage{}
		}
		if matrix.Runs[i].Force == nil {
			matrix.Runs[i].Force = []SourceCoverage{}
		}
	}
	return matrix
}
