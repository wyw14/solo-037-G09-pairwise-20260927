package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/wyw14/solo-037/internal/model"
)

// CreateTrialGroup validates that every referenced run exists (existence
// only — condition agreement is the report's job) and persists the group.
// Measurement lookup happens before taking the group lock, so the lock order
// is always measurements-then-groups, never nested the other way round.
func (r *Runtime) CreateTrialGroup(name string, runIDs []model.RunID) (TrialGroup, error) {
	for _, id := range normalizeRunIDs(runIDs) {
		if _, ok := r.Measurements.Get(id); !ok {
			return TrialGroup{}, fmt.Errorf("run not found: %s", id)
		}
	}
	return r.Groups.Create(name, runIDs, time.Now().UTC())
}

func (r *Runtime) TrialGroups() []TrialGroup { return r.Groups.List() }

func (r *Runtime) TrialGroup(id model.GroupID) (TrialGroup, bool) {
	return r.Groups.Get(id)
}

// GroupConsistency builds the repeatability report for a stored group. A
// missing group is an error; runs that vanished after grouping are reported
// inside the body as missing_run blockers rather than as an error.
func (r *Runtime) GroupConsistency(id model.GroupID) (ConsistencyReport, error) {
	group, ok := r.Groups.Get(id)
	if !ok {
		return ConsistencyReport{}, errors.New("trial group not found")
	}
	runs := make(map[model.RunID]MeasuredRun, len(group.RunIDs))
	for _, runID := range group.RunIDs {
		if run, ok := r.Measurements.Get(runID); ok {
			runs[runID] = run
		}
	}
	return BuildConsistencyReport(group, runs, time.Now().UTC()), nil
}
