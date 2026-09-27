package service

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wyw14/solo-037/internal/model"
)

func openComparisonArchives(t *testing.T, directory string) (*MeasurementArchive, *ComparisonArchive) {
	t.Helper()
	measurements, err := OpenMeasurementArchive(directory)
	if err != nil {
		t.Fatal(err)
	}
	comparisons, err := OpenComparisonArchive(directory, measurements)
	if err != nil {
		t.Fatal(err)
	}
	return measurements, comparisons
}

func createRun(t *testing.T, a *MeasurementArchive, name, section string) MeasuredRun {
	t.Helper()
	run, err := a.Create(name, "hull X", section, "carriage-1")
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func recordEvidence(t *testing.T, a *MeasurementArchive, run model.RunID, kind model.EvidenceKind, source string, values ...float64) {
	t.Helper()
	at := time.Now().Add(-time.Minute).UTC()
	for index, value := range values {
		input := MeasurementInput{Kind: kind, SourceID: source, Value: value, RecordedAt: at.Add(time.Duration(index) * time.Second)}
		if _, err := a.Record(run, input); err != nil {
			t.Fatal(err)
		}
	}
}

func TestComparisonGroupPersistsAndAnalyzesRepeatableRuns(t *testing.T) {
	directory := t.TempDir()
	measurements, comparisons := openComparisonArchives(t, directory)
	first := createRun(t, measurements, "tow 1", "section-a")
	second := createRun(t, measurements, "tow 2", "section-a")
	recordEvidence(t, measurements, first.Operation.ID, model.EvidenceWaveTime, "probe-a", 0.1, 0.3)
	recordEvidence(t, measurements, second.Operation.ID, model.EvidenceWaveTime, "probe-a", 0.2, 0.4)
	recordEvidence(t, measurements, first.Operation.ID, model.EvidenceForce, "load-cell", 12)
	recordEvidence(t, measurements, second.Operation.ID, model.EvidenceForce, "load-cell", 14)
	group, err := comparisons.Create("repeat tows", []model.RunID{first.Operation.ID, second.Operation.ID})
	if err != nil {
		t.Fatal(err)
	}
	reloadedMeasurements, reloaded := openComparisonArchives(t, directory)
	if len(reloadedMeasurements.List()) != 2 {
		t.Fatal("measurement archive did not reload")
	}
	groups := reloaded.List()
	if len(groups) != 1 || len(groups[0].RunIDs) != 2 || groups[0].ID != group.ID {
		t.Fatalf("group did not persist: %+v", groups)
	}
	analysis, err := reloaded.Analyze(group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !analysis.Comparable || len(analysis.ConditionDifferences) != 0 {
		t.Fatalf("matching runs should be comparable: %+v", analysis)
	}
	if len(analysis.Runs) != 2 {
		t.Fatalf("expected two run summaries: %+v", analysis.Runs)
	}
	if math.Abs(analysis.Wave.MeanSpread-0.1) > 1e-9 {
		t.Fatalf("wave mean spread = %v, want 0.1", analysis.Wave.MeanSpread)
	}
	if math.Abs(analysis.Force.MeanSpread-2) > 1e-9 {
		t.Fatalf("force mean spread = %v, want 2", analysis.Force.MeanSpread)
	}
	if len(analysis.Wave.CommonSources) != 1 || analysis.Wave.CommonSources[0] != "probe-a" {
		t.Fatalf("common sources: %+v", analysis.Wave.CommonSources)
	}
	if len(analysis.CoverageGaps) != 0 {
		t.Fatalf("unexpected coverage gaps: %+v", analysis.CoverageGaps)
	}
}

func TestComparisonGroupRejectsInvalidSelections(t *testing.T) {
	measurements, comparisons := openComparisonArchives(t, t.TempDir())
	first := createRun(t, measurements, "tow 1", "section-a")
	second := createRun(t, measurements, "tow 2", "section-a")
	cases := []struct {
		name   string
		runIDs []model.RunID
	}{
		{"", []model.RunID{first.Operation.ID, second.Operation.ID}},
		{"single", []model.RunID{first.Operation.ID}},
		{"duplicate", []model.RunID{first.Operation.ID, first.Operation.ID}},
		{"unknown", []model.RunID{first.Operation.ID, "missing"}},
	}
	for _, tc := range cases {
		if _, err := comparisons.Create(tc.name, tc.runIDs); err == nil {
			t.Fatalf("case %q accepted", tc.name)
		}
	}
	if len(comparisons.List()) != 0 {
		t.Fatal("rejected selections changed archive")
	}
}

func TestComparisonAnalysisReportsConditionDifferences(t *testing.T) {
	measurements, comparisons := openComparisonArchives(t, t.TempDir())
	first := createRun(t, measurements, "tow 1", "section-a")
	other := createRun(t, measurements, "tow 3", "section-b")
	recordEvidence(t, measurements, first.Operation.ID, model.EvidenceWaveTime, "probe-a", 0.2)
	recordEvidence(t, measurements, other.Operation.ID, model.EvidenceWaveTime, "probe-a", 0.2)
	group, err := comparisons.Create("mixed sections", []model.RunID{first.Operation.ID, other.Operation.ID})
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := comparisons.Analyze(group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Comparable {
		t.Fatal("runs with different sections must not be comparable")
	}
	if len(analysis.ConditionDifferences) != 1 || analysis.ConditionDifferences[0].Field != "section" {
		t.Fatalf("condition differences: %+v", analysis.ConditionDifferences)
	}
	values := analysis.ConditionDifferences[0].Values
	if values[first.Operation.ID] != "section-a" || values[other.Operation.ID] != "section-b" {
		t.Fatalf("difference values: %+v", values)
	}
	joined := strings.Join(analysis.Findings, "\n")
	if !strings.Contains(joined, "section") || !strings.Contains(joined, "not valid") {
		t.Fatalf("findings should explain the section difference: %v", analysis.Findings)
	}
}

func TestComparisonAnalysisReportsCoverageGaps(t *testing.T) {
	measurements, comparisons := openComparisonArchives(t, t.TempDir())
	full := createRun(t, measurements, "tow 1", "section-a")
	partial := createRun(t, measurements, "tow 2", "section-a")
	recordEvidence(t, measurements, full.Operation.ID, model.EvidenceWaveTime, "probe-a", 0.2)
	recordEvidence(t, measurements, full.Operation.ID, model.EvidenceForce, "load-cell", 12)
	recordEvidence(t, measurements, partial.Operation.ID, model.EvidenceWaveTime, "probe-b", 0.2)
	group, err := comparisons.Create("uneven coverage", []model.RunID{full.Operation.ID, partial.Operation.ID})
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := comparisons.Analyze(group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Force.RunsWithoutForce) != 1 || analysis.Force.RunsWithoutForce[0] != partial.Operation.ID {
		t.Fatalf("runs without force: %+v", analysis.Force.RunsWithoutForce)
	}
	if len(analysis.Wave.CommonSources) != 0 {
		t.Fatalf("no wave source is common: %+v", analysis.Wave.CommonSources)
	}
	gaps := make(map[model.RunID]CoverageGap)
	for _, gap := range analysis.CoverageGaps {
		gaps[gap.Run] = gap
	}
	if len(gaps[full.Operation.ID].MissingWaveSources) != 1 || gaps[full.Operation.ID].MissingWaveSources[0] != "probe-b" {
		t.Fatalf("full run gaps: %+v", gaps[full.Operation.ID])
	}
	if len(gaps[partial.Operation.ID].MissingWaveSources) != 1 || gaps[partial.Operation.ID].MissingWaveSources[0] != "probe-a" {
		t.Fatalf("partial run wave gaps: %+v", gaps[partial.Operation.ID])
	}
	if len(gaps[partial.Operation.ID].MissingForceSources) != 1 || gaps[partial.Operation.ID].MissingForceSources[0] != "load-cell" {
		t.Fatalf("partial run force gaps: %+v", gaps[partial.Operation.ID])
	}
	joined := strings.Join(analysis.Findings, "\n")
	if !strings.Contains(joined, "no force evidence") || !strings.Contains(joined, "missing wave source") {
		t.Fatalf("findings should describe gaps: %v", analysis.Findings)
	}
}

func TestComparisonGroupUpdateChangesMembership(t *testing.T) {
	measurements, comparisons := openComparisonArchives(t, t.TempDir())
	first := createRun(t, measurements, "tow 1", "section-a")
	second := createRun(t, measurements, "tow 2", "section-a")
	third := createRun(t, measurements, "tow 3", "section-a")
	group, err := comparisons.Create("repeat tows", []model.RunID{first.Operation.ID, second.Operation.ID})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := comparisons.Update(group.ID, "repeat tows", []model.RunID{first.Operation.ID, third.Operation.ID})
	if err != nil {
		t.Fatal(err)
	}
	if updated.RunIDs[1] != third.Operation.ID || updated.ID != group.ID {
		t.Fatalf("update did not change membership: %+v", updated)
	}
	if _, err := comparisons.Update("missing", "repeat tows", []model.RunID{first.Operation.ID, second.Operation.ID}); err == nil {
		t.Fatal("update of unknown group accepted")
	}
	loaded, ok := comparisons.Get(group.ID)
	if !ok || loaded.RunIDs[1] != third.Operation.ID {
		t.Fatalf("stored membership: %+v", loaded)
	}
}
