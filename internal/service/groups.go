package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wyw14/solo-037/internal/model"
)

// TrialGroup is a named, persisted set of measured tows that an operator wants
// checked for repeatability. Membership alone says nothing about whether the
// tows share conditions; the consistency report answers that.
type TrialGroup struct {
	ID        model.GroupID `json:"id"`
	Name      string        `json:"name"`
	RunIDs    []model.RunID `json:"run_ids"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// GroupArchive persists trial groups to trial-groups.json. An empty directory
// selects in-memory mode, mirroring MeasurementArchive.
type GroupArchive struct {
	mu     sync.Mutex
	path   string
	groups map[model.GroupID]TrialGroup
}

func OpenGroupArchive(directory string) (*GroupArchive, error) {
	a := &GroupArchive{groups: make(map[model.GroupID]TrialGroup)}
	if directory == "" {
		return a, nil
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return nil, err
	}
	a.path = filepath.Join(directory, "trial-groups.json")
	data, err := os.ReadFile(a.path)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &a.groups); err != nil {
		return nil, fmt.Errorf("read trial group archive: %w", err)
	}
	if a.groups == nil {
		return nil, errors.New("trial group archive must contain a group map")
	}
	return a, nil
}

func (a *GroupArchive) commit(next map[model.GroupID]TrialGroup) error {
	if err := writeAtomicJSON(a.path, "trial-groups-*.tmp", next); err != nil {
		return err
	}
	a.groups = next
	return nil
}

// normalizeRunIDs drops blanks and duplicates while preserving first-seen order.
func normalizeRunIDs(ids []model.RunID) []model.RunID {
	seen := make(map[model.RunID]struct{}, len(ids))
	out := make([]model.RunID, 0, len(ids))
	for _, id := range ids {
		id = model.RunID(strings.TrimSpace(string(id)))
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// Create stores a new group. Conditions are deliberately not validated here:
// the point of grouping is to let the report explain field-by-field why tows
// are or are not comparable. Run existence is checked by the Runtime layer.
func (a *GroupArchive) Create(name string, runIDs []model.RunID, at time.Time) (TrialGroup, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return TrialGroup{}, errors.New("group name is required")
	}
	members := normalizeRunIDs(runIDs)
	if len(members) < 2 {
		return TrialGroup{}, errors.New("a trial group needs at least two distinct runs")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := at.UTC()
	group := TrialGroup{
		ID:        model.NewGroupID(),
		Name:      name,
		RunIDs:    members,
		CreatedAt: now,
		UpdatedAt: now,
	}
	next := make(map[model.GroupID]TrialGroup, len(a.groups)+1)
	for id, existing := range a.groups {
		next[id] = existing
	}
	next[group.ID] = group
	if err := a.commit(next); err != nil {
		return TrialGroup{}, err
	}
	return group, nil
}

func cloneGroup(group TrialGroup) TrialGroup {
	group.RunIDs = append([]model.RunID(nil), group.RunIDs...)
	return group
}

func (a *GroupArchive) Get(id model.GroupID) (TrialGroup, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	group, ok := a.groups[id]
	return cloneGroup(group), ok
}

func (a *GroupArchive) List() []TrialGroup {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]TrialGroup, 0, len(a.groups))
	for _, group := range a.groups {
		result = append(result, cloneGroup(group))
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}
