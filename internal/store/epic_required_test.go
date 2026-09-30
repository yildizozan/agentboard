package store

import (
	"context"
	"errors"
	"testing"

	"github.com/yildizozan/agentboard/internal/task"
)

func TestTaskCannotBeUnlinked(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	epic := addDraft(t, s, "/r", task.Draft{Body: "# Epic", Kind: task.EpicKind})
	child := addDraft(t, s, "/r", task.Draft{Body: "# Child", EpicID: &epic.ID})
	_, err := s.Update(ctx, "/r", child.ID, task.Patch{Epic: ptr(int64(0)), Body: ptr("# Should not be saved")})
	if !errors.Is(err, task.ErrInvalid) {
		t.Fatalf("unlink error=%v, want validation error", err)
	}
	stored, err := s.Get(ctx, "/r", child.ID)
	if err != nil || !sameEpic(stored.EpicID, &epic.ID) || stored.Body != "# Child" {
		t.Fatalf("rejected update changed task: %+v %v", stored, err)
	}
}

func TestEpicCannotBeDeletedWithChildren(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	epic := addDraft(t, s, "/r", task.Draft{Body: "# Epic", Kind: task.EpicKind})
	child := addDraft(t, s, "/r", task.Draft{Body: "# Child", Status: task.Done, EpicID: &epic.ID})
	if err := s.Delete(ctx, "/r", epic.ID); !errors.Is(err, task.ErrInvalid) {
		t.Fatalf("delete error=%v, want validation error", err)
	}
	if _, err := s.Get(ctx, "/r", epic.ID); err != nil {
		t.Fatal("epic removed:", err)
	}
	if stored, err := s.Get(ctx, "/r", child.ID); err != nil || !sameEpic(stored.EpicID, &epic.ID) {
		t.Fatalf("child changed: %+v %v", stored, err)
	}
	if err := s.Delete(ctx, "/r", child.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "/r", epic.ID); err != nil {
		t.Fatal("empty epic rejected:", err)
	}
}

func TestMergeCannotWriteLegacyOrphan(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	epic := addDraft(t, s, "/r", task.Draft{Body: "# Epic", Kind: task.EpicKind})
	target := addDraft(t, s, "/r", task.Draft{Body: "# Legacy", EpicID: &epic.ID})
	source := addDraft(t, s, "/r", task.Draft{Body: "# Source", EpicID: &epic.ID})
	if _, err := s.db.Exec(`UPDATE tasks SET epic_id = NULL WHERE id = ?`, target.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Merge(ctx, "/r", target.ID, source.ID); !errors.Is(err, task.ErrInvalid) {
		t.Fatalf("legacy target merge error=%v, want validation error", err)
	}
	if stored, err := s.Get(ctx, "/r", target.ID); err != nil || stored.Body != "# Legacy" {
		t.Fatalf("target changed: %+v %v", stored, err)
	}
	if _, err := s.Get(ctx, "/r", source.ID); err != nil {
		t.Fatal("source deleted:", err)
	}
}
