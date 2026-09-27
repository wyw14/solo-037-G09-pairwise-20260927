package service

import (
	"testing"
	"time"

	"github.com/wyw14/solo-037/internal/model"
)

func TestGroupArchivePersistsDedupesAndOrders(t *testing.T) {
	directory := t.TempDir()
	a, err := OpenGroupArchive(directory)
	if err != nil {
		t.Fatal(err)
	}
	ids := []model.RunID{"run-a", "run-b", "run-a", "  ", "run-c"}
	group, err := a.Create("repeat set", ids, time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got := group.RunIDs; len(got) != 3 || got[0] != "run-a" || got[1] != "run-b" || got[2] != "run-c" {
		t.Fatalf("membership not normalized: %v", got)
	}
	reloaded, err := OpenGroupArchive(directory)
	if err != nil {
		t.Fatal(err)
	}
	loaded, ok := reloaded.Get(group.ID)
	if !ok {
		t.Fatal("group missing after reload")
	}
	if loaded.Name != "repeat set" || len(loaded.RunIDs) != 3 {
		t.Fatalf("group did not persist: %+v", loaded)
	}
}

func TestGroupArchiveRejectsInvalidInput(t *testing.T) {
	a, err := OpenGroupArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Create("  ", []model.RunID{"a", "b"}, time.Now().UTC()); err == nil {
		t.Fatal("blank name accepted")
	}
	if _, err := a.Create("g", []model.RunID{"a"}, time.Now().UTC()); err == nil {
		t.Fatal("single-member group accepted")
	}
	if _, err := a.Create("g", []model.RunID{"a", "a"}, time.Now().UTC()); err == nil {
		t.Fatal("duplicate-only membership accepted")
	}
	if len(a.List()) != 0 {
		t.Fatal("rejected groups must not be stored")
	}
}

func TestGroupArchiveListSortedByCreation(t *testing.T) {
	a, err := OpenGroupArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	later, _ := a.Create("later", []model.RunID{"a", "b"}, base.Add(time.Hour))
	earlier, _ := a.Create("earlier", []model.RunID{"a", "b"}, base)
	groups := a.List()
	if len(groups) != 2 || groups[0].ID != earlier.ID || groups[1].ID != later.ID {
		t.Fatalf("list not creation ordered: %v", groups)
	}
}

func TestGroupArchiveInMemoryModeWritesNoFile(t *testing.T) {
	a, err := OpenGroupArchive("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Create("g", []model.RunID{"a", "b"}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if a.path != "" {
		t.Fatal("in-memory archive should keep an empty path")
	}
}
