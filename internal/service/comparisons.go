package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wyw14/solo-037/internal/model"
	"github.com/wyw14/solo-037/internal/probe"
)

// ComparisonGroup names a set of measured runs that personnel selected for a
// repeatability check, typically repeated tows of the same hull model.
type ComparisonGroup struct {
	ID        model.GroupID `json:"id"`
	Name      string        `json:"name"`
	RunIDs    []model.RunID `json:"run_ids"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// ConditionDifference reports one condition field whose value is not
// identical across the grouped runs, with the actual value per run.
type ConditionDifference struct {
	Field  string                 `json:"field"`
	Values map[model.RunID]string `json:"values"`
}

type ForceStats struct {
	Count int     `json:"count"`
	Mean  float64 `json:"mean"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
}

// RunSummary places one member run's wave, force and coverage facts side by
// side with the other members. It never merges values across runs.
type RunSummary struct {
	Run           model.RunID    `json:"run"`
	Name          string         `json:"name"`
	ModelName     string         `json:"model_name"`
	Section       string         `json:"section"`
	Carriage      string         `json:"carriage"`
	Wave          probe.Analysis `json:"wave"`
	Force         ForceStats     `json:"force"`
	WaveSources   []string       `json:"wave_sources"`
	ForceSources  []string       `json:"force_sources"`
	EvidenceCount int            `json:"evidence_count"`
	AllDurable    bool           `json:"all_durable"`
}

type SourceWaveStat struct {
	Count int     `json:"count"`
	Mean  float64 `json:"mean"`
	RMS   float64 `json:"rms"`
}

// WaveSourceComparison compares one wave source across the runs that
// recorded it, and lists the runs that did not.
type WaveSourceComparison struct {
	Source  string                         `json:"source"`
	Runs    map[model.RunID]SourceWaveStat `json:"runs"`
	Missing []model.RunID                  `json:"missing,omitempty"`
}

type WaveComparison struct {
	MeanSpread    float64                `json:"mean_spread"`
	RMSSpread     float64                `json:"rms_spread"`
	CommonSources []string               `json:"common_sources"`
	Sources       []WaveSourceComparison `json:"sources"`
}

type ForceComparison struct {
	MeanSpread       float64       `json:"mean_spread"`
	RunsWithoutForce []model.RunID `json:"runs_without_force,omitempty"`
}

// CoverageGap lists where one run's evidence falls short of the coverage
// the other runs in the group provide.
type CoverageGap struct {
	Run                 model.RunID `json:"run"`
	MissingWaveSources  []string    `json:"missing_wave_sources,omitempty"`
	MissingForceSources []string    `json:"missing_force_sources,omitempty"`
	NonDurableEvidence  bool        `json:"non_durable_evidence,omitempty"`
}

// GroupAnalysis is a side-by-side repeatability report. It deliberately
// contains no aggregated score: personnel read the per-run values, spreads,
// coverage gaps and condition differences themselves.
type GroupAnalysis struct {
	Group                ComparisonGroup       `json:"group"`
	Comparable           bool                  `json:"comparable"`
	ConditionDifferences []ConditionDifference `json:"condition_differences"`
	Runs                 []RunSummary          `json:"runs"`
	Wave                 WaveComparison        `json:"wave"`
	Force                ForceComparison       `json:"force"`
	CoverageGaps         []CoverageGap         `json:"coverage_gaps"`
	Findings             []string              `json:"findings"`
	Evaluated            time.Time             `json:"evaluated_at"`
}

type ComparisonArchive struct {
	mu     sync.Mutex
	path   string
	runs   *MeasurementArchive
	groups map[model.GroupID]ComparisonGroup
}

func OpenComparisonArchive(directory string, runs *MeasurementArchive) (*ComparisonArchive, error) {
	a := &ComparisonArchive{runs: runs, groups: make(map[model.GroupID]ComparisonGroup)}
	if directory == "" {
		return a, nil
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return nil, err
	}
	a.path = filepath.Join(directory, "comparison-groups.json")
	data, err := os.ReadFile(a.path)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &a.groups); err != nil {
		return nil, fmt.Errorf("read comparison group archive: %w", err)
	}
	if a.groups == nil {
		return nil, errors.New("comparison group archive must contain a group map")
	}
	return a, nil
}

func cloneComparisonGroup(value ComparisonGroup) ComparisonGroup {
	value.RunIDs = append([]model.RunID(nil), value.RunIDs...)
	return value
}

func (a *ComparisonArchive) commit(next map[model.GroupID]ComparisonGroup) error {
	if a.path != "" {
		data, err := json.MarshalIndent(next, "", "  ")
		if err != nil {
			return err
		}
		file, err := os.CreateTemp(filepath.Dir(a.path), "comparison-groups-*.tmp")
		if err != nil {
			return err
		}
		name := file.Name()
		defer os.Remove(name)
		if _, err = file.Write(data); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if err := os.Rename(name, a.path); err != nil {
			return err
		}
	}
	a.groups = next
	return nil
}

func (a *ComparisonArchive) validate(name string, runIDs []model.RunID) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("name is required")
	}
	if len(runIDs) < 2 {
		return errors.New("at least two runs are required")
	}
	seen := make(map[model.RunID]struct{}, len(runIDs))
	for _, id := range runIDs {
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("run %s listed twice", id)
		}
		seen[id] = struct{}{}
		if _, ok := a.runs.Get(id); !ok {
			return fmt.Errorf("run %s not found", id)
		}
	}
	return nil
}

func (a *ComparisonArchive) Create(name string, runIDs []model.RunID) (ComparisonGroup, error) {
	if err := a.validate(name, runIDs); err != nil {
		return ComparisonGroup{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now().UTC()
	group := ComparisonGroup{
		ID:        model.NewGroupID(),
		Name:      strings.TrimSpace(name),
		RunIDs:    append([]model.RunID(nil), runIDs...),
		CreatedAt: now,
		UpdatedAt: now,
	}
	next := make(map[model.GroupID]ComparisonGroup, len(a.groups)+1)
	for id, value := range a.groups {
		next[id] = value
	}
	next[group.ID] = group
	if err := a.commit(next); err != nil {
		return ComparisonGroup{}, err
	}
	return group, nil
}

func (a *ComparisonArchive) Update(id model.GroupID, name string, runIDs []model.RunID) (ComparisonGroup, error) {
	if err := a.validate(name, runIDs); err != nil {
		return ComparisonGroup{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	group, exists := a.groups[id]
	if !exists {
		return ComparisonGroup{}, errors.New("comparison group not found")
	}
	group.Name = strings.TrimSpace(name)
	group.RunIDs = append([]model.RunID(nil), runIDs...)
	group.UpdatedAt = time.Now().UTC()
	next := make(map[model.GroupID]ComparisonGroup, len(a.groups))
	for key, value := range a.groups {
		next[key] = value
	}
	next[id] = group
	if err := a.commit(next); err != nil {
		return ComparisonGroup{}, err
	}
	return group, nil
}

func (a *ComparisonArchive) Get(id model.GroupID) (ComparisonGroup, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	group, exists := a.groups[id]
	return cloneComparisonGroup(group), exists
}

func (a *ComparisonArchive) List() []ComparisonGroup {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]ComparisonGroup, 0, len(a.groups))
	for _, group := range a.groups {
		result = append(result, cloneComparisonGroup(group))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result
}

func waveEvidence(run MeasuredRun) []model.Evidence {
	result := make([]model.Evidence, 0, len(run.Evidence))
	for _, evidence := range run.Evidence {
		if evidence.Kind == model.EvidenceWaveTime {
			result = append(result, evidence)
		}
	}
	return result
}

func summarizeRun(run MeasuredRun) RunSummary {
	summary := RunSummary{
		Run:       run.Operation.ID,
		Name:      run.Operation.Name,
		ModelName: run.Operation.ModelName,
		Section:   run.Operation.Section,
		Carriage:  run.Operation.Carriage,
	}
	waves := make([]model.Evidence, 0, len(run.Evidence))
	waveSources := make(map[string]struct{})
	forceSources := make(map[string]struct{})
	allDurable := len(run.Evidence) > 0
	for _, evidence := range run.Evidence {
		if !evidence.Durable {
			allDurable = false
		}
		switch evidence.Kind {
		case model.EvidenceWaveTime:
			waves = append(waves, evidence)
			waveSources[evidence.SourceID] = struct{}{}
		case model.EvidenceForce:
			forceSources[evidence.SourceID] = struct{}{}
			if summary.Force.Count == 0 {
				summary.Force.Min = evidence.Value
				summary.Force.Max = evidence.Value
			} else {
				summary.Force.Min = math.Min(summary.Force.Min, evidence.Value)
				summary.Force.Max = math.Max(summary.Force.Max, evidence.Value)
			}
			summary.Force.Count++
			summary.Force.Mean += evidence.Value
		}
	}
	if summary.Force.Count > 0 {
		summary.Force.Mean /= float64(summary.Force.Count)
	}
	summary.Wave = probe.Analyze(waves)
	summary.EvidenceCount = len(run.Evidence)
	summary.AllDurable = allDurable
	for source := range waveSources {
		summary.WaveSources = append(summary.WaveSources, source)
	}
	sort.Strings(summary.WaveSources)
	for source := range forceSources {
		summary.ForceSources = append(summary.ForceSources, source)
	}
	sort.Strings(summary.ForceSources)
	return summary
}

func conditionDifferences(summaries []RunSummary) []ConditionDifference {
	fields := []struct {
		name string
		get  func(RunSummary) string
	}{
		{"model_name", func(s RunSummary) string { return s.ModelName }},
		{"section", func(s RunSummary) string { return s.Section }},
		{"carriage", func(s RunSummary) string { return s.Carriage }},
	}
	var result []ConditionDifference
	for _, field := range fields {
		values := make(map[model.RunID]string, len(summaries))
		distinct := make(map[string]struct{}, len(summaries))
		for _, summary := range summaries {
			value := field.get(summary)
			values[summary.Run] = value
			distinct[value] = struct{}{}
		}
		if len(distinct) > 1 {
			result = append(result, ConditionDifference{Field: field.name, Values: values})
		}
	}
	return result
}

func compareWaves(runs []MeasuredRun, summaries []RunSummary) WaveComparison {
	result := WaveComparison{}
	haveWave := false
	var minMean, maxMean, minRMS, maxRMS float64
	for _, summary := range summaries {
		if summary.Wave.Count == 0 {
			continue
		}
		if !haveWave {
			haveWave = true
			minMean, maxMean = summary.Wave.Mean, summary.Wave.Mean
			minRMS, maxRMS = summary.Wave.RMS, summary.Wave.RMS
			continue
		}
		minMean = math.Min(minMean, summary.Wave.Mean)
		maxMean = math.Max(maxMean, summary.Wave.Mean)
		minRMS = math.Min(minRMS, summary.Wave.RMS)
		maxRMS = math.Max(maxRMS, summary.Wave.RMS)
	}
	if haveWave {
		result.MeanSpread = maxMean - minMean
		result.RMSSpread = maxRMS - minRMS
	}
	perSource := make(map[string]map[model.RunID]SourceWaveStat)
	for _, run := range runs {
		for source, samples := range probe.SameSourceGroups(waveEvidence(run)) {
			stats := probe.Analyze(samples)
			bucket, ok := perSource[source]
			if !ok {
				bucket = make(map[model.RunID]SourceWaveStat)
				perSource[source] = bucket
			}
			bucket[run.Operation.ID] = SourceWaveStat{Count: stats.Count, Mean: stats.Mean, RMS: stats.RMS}
		}
	}
	sources := make([]string, 0, len(perSource))
	for source := range perSource {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	for _, source := range sources {
		comparison := WaveSourceComparison{Source: source, Runs: perSource[source]}
		for _, run := range runs {
			if _, ok := perSource[source][run.Operation.ID]; !ok {
				comparison.Missing = append(comparison.Missing, run.Operation.ID)
			}
		}
		if len(comparison.Missing) == 0 {
			result.CommonSources = append(result.CommonSources, source)
		}
		result.Sources = append(result.Sources, comparison)
	}
	return result
}

func compareForce(summaries []RunSummary) ForceComparison {
	result := ForceComparison{}
	haveForce := false
	var minMean, maxMean float64
	for _, summary := range summaries {
		if summary.Force.Count == 0 {
			result.RunsWithoutForce = append(result.RunsWithoutForce, summary.Run)
			continue
		}
		if !haveForce {
			haveForce = true
			minMean, maxMean = summary.Force.Mean, summary.Force.Mean
			continue
		}
		minMean = math.Min(minMean, summary.Force.Mean)
		maxMean = math.Max(maxMean, summary.Force.Mean)
	}
	if haveForce {
		result.MeanSpread = maxMean - minMean
	}
	return result
}

func missingStrings(union []string, present []string) []string {
	owned := make(map[string]struct{}, len(present))
	for _, value := range present {
		owned[value] = struct{}{}
	}
	var result []string
	for _, value := range union {
		if _, ok := owned[value]; !ok {
			result = append(result, value)
		}
	}
	return result
}

func coverageGaps(summaries []RunSummary) []CoverageGap {
	waveUnion := make(map[string]struct{})
	forceUnion := make(map[string]struct{})
	for _, summary := range summaries {
		for _, source := range summary.WaveSources {
			waveUnion[source] = struct{}{}
		}
		for _, source := range summary.ForceSources {
			forceUnion[source] = struct{}{}
		}
	}
	waveSources := make([]string, 0, len(waveUnion))
	for source := range waveUnion {
		waveSources = append(waveSources, source)
	}
	sort.Strings(waveSources)
	forceSources := make([]string, 0, len(forceUnion))
	for source := range forceUnion {
		forceSources = append(forceSources, source)
	}
	sort.Strings(forceSources)
	var result []CoverageGap
	for _, summary := range summaries {
		gap := CoverageGap{
			Run:                 summary.Run,
			MissingWaveSources:  missingStrings(waveSources, summary.WaveSources),
			MissingForceSources: missingStrings(forceSources, summary.ForceSources),
			NonDurableEvidence:  summary.EvidenceCount > 0 && !summary.AllDurable,
		}
		if len(gap.MissingWaveSources) > 0 || len(gap.MissingForceSources) > 0 || gap.NonDurableEvidence {
			result = append(result, gap)
		}
	}
	return result
}

func (a *ComparisonArchive) Analyze(id model.GroupID) (GroupAnalysis, error) {
	group, exists := a.Get(id)
	if !exists {
		return GroupAnalysis{}, errors.New("comparison group not found")
	}
	result := GroupAnalysis{Group: group, Evaluated: time.Now().UTC()}
	runs := make([]MeasuredRun, 0, len(group.RunIDs))
	for _, runID := range group.RunIDs {
		run, ok := a.runs.Get(runID)
		if !ok {
			result.Findings = append(result.Findings, fmt.Sprintf("run %s is missing from the measurement archive", runID))
			continue
		}
		runs = append(runs, run)
	}
	summaries := make([]RunSummary, 0, len(runs))
	for _, run := range runs {
		summaries = append(summaries, summarizeRun(run))
	}
	result.Runs = summaries
	result.ConditionDifferences = conditionDifferences(summaries)
	result.Wave = compareWaves(runs, summaries)
	result.Force = compareForce(summaries)
	result.CoverageGaps = coverageGaps(summaries)

	names := make(map[model.RunID]string, len(summaries))
	withEvidence := 0
	for _, summary := range summaries {
		names[summary.Run] = summary.Name
		if summary.Wave.Count > 0 || summary.Force.Count > 0 {
			withEvidence++
		}
		if summary.Wave.Count == 0 {
			result.Findings = append(result.Findings, fmt.Sprintf("run %s (%s) has no wave evidence", summary.Run, summary.Name))
		}
	}
	for _, difference := range result.ConditionDifferences {
		pairs := make([]string, 0, len(difference.Values))
		for _, summary := range summaries {
			pairs = append(pairs, fmt.Sprintf("%s=%s", summary.Name, difference.Values[summary.Run]))
		}
		result.Findings = append(result.Findings, fmt.Sprintf("runs differ in %s (%s); quantitative comparison is not valid", difference.Field, strings.Join(pairs, ", ")))
	}
	for _, runID := range result.Force.RunsWithoutForce {
		result.Findings = append(result.Findings, fmt.Sprintf("run %s (%s) has no force evidence", runID, names[runID]))
	}
	for _, gap := range result.CoverageGaps {
		if len(gap.MissingWaveSources) > 0 {
			result.Findings = append(result.Findings, fmt.Sprintf("run %s (%s) is missing wave source(s) %s recorded in other runs", gap.Run, names[gap.Run], strings.Join(gap.MissingWaveSources, ", ")))
		}
		if len(gap.MissingForceSources) > 0 {
			result.Findings = append(result.Findings, fmt.Sprintf("run %s (%s) is missing force source(s) %s recorded in other runs", gap.Run, names[gap.Run], strings.Join(gap.MissingForceSources, ", ")))
		}
		if gap.NonDurableEvidence {
			result.Findings = append(result.Findings, fmt.Sprintf("run %s (%s) contains evidence that was not persisted", gap.Run, names[gap.Run]))
		}
	}
	if withEvidence < 2 {
		result.Findings = append(result.Findings, "fewer than two runs have wave or force evidence")
	}
	result.Comparable = len(result.ConditionDifferences) == 0 && withEvidence >= 2 && len(runs) == len(group.RunIDs)
	return result, nil
}

func (r *Runtime) CreateComparisonGroup(name string, runIDs []model.RunID) (ComparisonGroup, error) {
	return r.Comparisons.Create(name, runIDs)
}

func (r *Runtime) UpdateComparisonGroup(id model.GroupID, name string, runIDs []model.RunID) (ComparisonGroup, error) {
	return r.Comparisons.Update(id, name, runIDs)
}

func (r *Runtime) ComparisonGroups() []ComparisonGroup { return r.Comparisons.List() }

func (r *Runtime) ComparisonGroup(id model.GroupID) (ComparisonGroup, bool) {
	return r.Comparisons.Get(id)
}

func (r *Runtime) AnalyzeComparisonGroup(id model.GroupID) (GroupAnalysis, error) {
	return r.Comparisons.Analyze(id)
}
