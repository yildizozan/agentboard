package store

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/yildizozan/agentboard/internal/task"
)

func TestUpdateUsesTaskRevisionToRejectStaleWrites(t *testing.T) {
	s, path := openTemp(t)
	ctx := context.Background()
	original := mustAdd(t, s, "/r", "# Original", task.Todo)
	if original.Revision != 1 {
		t.Fatalf("new task revision = %d, want 1", original.Revision)
	}
	updated, err := s.Update(ctx, "/r", original.ID, task.Patch{Body: ptr("# First edit"), ExpectedRevision: &original.Revision})
	if err != nil || updated.Revision != 2 {
		t.Fatalf("first edit = %+v, %v", updated, err)
	}
	_, err = s.Update(ctx, "/r", original.ID, task.Patch{Body: ptr("# Stale edit"), ExpectedRevision: &original.Revision})
	var conflict *task.RevisionConflictError
	if !errors.As(err, &conflict) || conflict.ID != original.ID || conflict.Expected != 1 || conflict.Current != 2 {
		t.Fatalf("stale edit error = %v, want revision conflict 1 -> 2", err)
	}
	stored, err := s.Get(ctx, "/r", original.ID)
	if err != nil || stored.Body != "# First edit" || stored.Revision != 2 {
		t.Fatalf("stale write changed task: %+v, %v", stored, err)
	}
	updated, err = s.Update(ctx, "/r", original.ID, task.Patch{Priority: ptr(task.High)})
	if err != nil || updated.Revision != 3 {
		t.Fatalf("unconditional legacy update = %+v, %v", updated, err)
	}
	s.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	stored, err = reopened.Get(ctx, "/r", original.ID)
	if err != nil || stored.Revision != 3 || stored.Body != "# First edit" || stored.Priority != task.High {
		t.Fatalf("reopening reset task contents or revision: %+v, %v", stored, err)
	}
}

func TestConcurrentUpdatesWithSameRevisionHaveOneWinner(t *testing.T) {
	first, path := openTemp(t)
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	original := mustAdd(t, first, "/r", "# Original", task.Todo)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i, s := range []*Store{first, second} {
		wg.Go(func() {
			<-start
			body := []string{"# Writer A", "# Writer B"}[i]
			_, err := s.Update(context.Background(), "/r", original.ID, task.Patch{Body: &body, ExpectedRevision: ptr(int64(1))})
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	wins, conflicts := 0, 0
	for err := range errs {
		var conflict *task.RevisionConflictError
		switch {
		case err == nil:
			wins++
		case errors.As(err, &conflict):
			conflicts++
		default:
			t.Errorf("unexpected update error: %v", err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("concurrent results: %d wins, %d conflicts; want one each", wins, conflicts)
	}
	got, err := first.Get(context.Background(), "/r", original.ID)
	if err != nil || got.Revision != 2 || (got.Body != "# Writer A" && got.Body != "# Writer B") {
		t.Fatalf("concurrent task = %+v, %v", got, err)
	}
}

func TestMergeBumpsTargetAndReparentedTaskRevisions(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	target := addDraft(t, s, "/r", task.Draft{Body: "# Target", Kind: task.EpicKind})
	source := addDraft(t, s, "/r", task.Draft{Body: "# Source", Kind: task.EpicKind})
	child := addDraft(t, s, "/r", task.Draft{Body: "# Child", EpicID: &source.ID})
	unrelated := addDraft(t, s, "/r", task.Draft{Body: "# Unrelated", EpicID: &target.ID})
	if _, err := s.db.Exec(`UPDATE tasks SET updated_at=1 WHERE id=?`, child.ID); err != nil {
		t.Fatal(err)
	}
	merged, err := s.Merge(ctx, "/r", target.ID, source.ID)
	if err != nil || merged.Revision != 2 {
		t.Fatalf("merged target = %+v, %v", merged, err)
	}
	for _, id := range []int64{target.ID, child.ID} {
		got, err := s.Get(ctx, "/r", id)
		if err != nil || got.Revision != 2 || got.UpdatedAt.Unix() <= 1 {
			t.Fatalf("merge did not update task #%d version/timestamp: %+v, %v", id, got, err)
		}
		_, err = s.Update(ctx, "/r", id, task.Patch{Body: ptr("# Stale edit"), ExpectedRevision: ptr(int64(1))})
		var conflict *task.RevisionConflictError
		if !errors.As(err, &conflict) {
			t.Errorf("stale edit after merge error = %v, want revision conflict", err)
		}
	}
	got, err := s.Get(ctx, "/r", unrelated.ID)
	if err != nil || got.Revision != 1 {
		t.Fatalf("merge changed unrelated task: %+v, %v", got, err)
	}
}
