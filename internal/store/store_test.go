package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/yildizozan/agentboard/internal/task"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nested", "agentboard.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func mustAdd(t *testing.T, s *Store, repo, body string, status task.Status) task.Task {
	t.Helper()
	tk, err := s.Add(context.Background(), repo, task.Draft{Body: body, Status: status})
	if err != nil {
		t.Fatalf("Add(%q): %v", body, err)
	}
	return tk
}

func TestReopenKeepsTasks(t *testing.T) {
	s, path := openTemp(t)
	mustAdd(t, s, "/r", "keep me", task.Todo)
	s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	got, err := s2.List(context.Background(), "/r", Filter{})
	if err != nil || len(got) != 1 {
		t.Fatalf("List after reopen = %v, %v; want 1 task", got, err)
	}
}

func TestOpenUsesWAL(t *testing.T) {
	s, _ := openTemp(t)
	var mode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

func TestAddReturnsStoredTask(t *testing.T) {
	s, _ := openTemp(t)
	tk, err := s.Add(context.Background(), "/r", task.Draft{Body: "  Fix login  \n\ndetails\n", Status: task.Todo})
	if err != nil {
		t.Fatal(err)
	}
	if tk.ID == 0 || tk.Repo != "/r" || tk.Body != "# Fix login\n\ndetails" || tk.Status != task.Todo {
		t.Errorf("Add returned %+v", tk)
	}
	if tk.CreatedAt.IsZero() || !tk.UpdatedAt.Equal(tk.CreatedAt) {
		t.Errorf("timestamps not set: %+v", tk)
	}
}

func TestAddRejectsInvalidInput(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if _, err := s.Add(ctx, "/r", task.Draft{Body: "  ", Status: task.Todo}); !errors.Is(err, task.ErrNoTitle) {
		t.Errorf("blank body error = %v, want ErrNoTitle", err)
	}
	if _, err := s.Add(ctx, "/r", task.Draft{Body: "x", Status: task.Status("later")}); err == nil {
		t.Error("invalid status accepted")
	}
	if _, err := s.Add(ctx, "", task.Draft{Body: "x", Status: task.Todo}); err == nil {
		t.Error("empty repo accepted")
	}
}

func TestListIsolatesRepos(t *testing.T) {
	s, _ := openTemp(t)
	mustAdd(t, s, "/a", "in a", task.Todo)
	mustAdd(t, s, "/b", "in b", task.Todo)

	got, err := s.List(context.Background(), "/a", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title() != "in a" {
		t.Errorf("List(/a) = %v", got)
	}
}

func TestListFiltersAndOrders(t *testing.T) {
	s, _ := openTemp(t)
	done := mustAdd(t, s, "/r", "done", task.Done)
	doing := mustAdd(t, s, "/r", "doing", task.Doing)
	backlog := mustAdd(t, s, "/r", "backlog", task.Backlog)
	todo1 := mustAdd(t, s, "/r", "todo 1", task.Todo)
	todo2 := mustAdd(t, s, "/r", "todo 2", task.Todo)
	ctx := context.Background()

	all, err := s.List(ctx, "/r", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, all, backlog.ID, todo1.ID, todo2.ID, doing.ID, done.ID)

	active, err := s.List(ctx, "/r", Filter{Statuses: task.ActiveStatuses()})
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, active, backlog.ID, todo1.ID, todo2.ID, doing.ID)

	onlyDone, err := s.List(ctx, "/r", Filter{Statuses: []task.Status{task.Done}})
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, onlyDone, done.ID)
}

func assertIDs(t *testing.T, got []task.Task, want ...int64) {
	t.Helper()
	ids := make([]int64, len(got))
	for i, tk := range got {
		ids[i] = tk.ID
	}
	if fmt.Sprint(ids) != fmt.Sprint(want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
}

// TestConcurrentWritersAcrossStores simulates several MCP processes writing to one DB file.
func TestConcurrentWritersAcrossStores(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentboard.db")
	const stores, writers = 2, 50

	opened := make([]*Store, stores)
	var wg sync.WaitGroup
	errs := make(chan error, stores*writers)
	for i := range opened {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Open(path)
			if err != nil {
				errs <- fmt.Errorf("open %d: %w", i, err)
				return
			}
			opened[i] = s
		}()
	}
	wg.Wait()
	checkErrs(t, errs)
	for _, s := range opened {
		t.Cleanup(func() { s.Close() })
	}

	for i, s := range opened {
		for j := range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := s.Add(context.Background(), "/r", task.Draft{Body: fmt.Sprintf("s%d-w%d", i, j), Status: task.Todo}); err != nil {
					errs <- err
				}
			}()
		}
	}
	wg.Wait()
	checkErrs(t, errs)

	got, err := opened[0].List(context.Background(), "/r", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != stores*writers {
		t.Errorf("stored %d tasks, want %d", len(got), stores*writers)
	}
}

func checkErrs(t *testing.T, errs chan error) {
	t.Helper()
	for {
		select {
		case err := <-errs:
			t.Fatal(err)
		default:
			return
		}
	}
}

func ptr[T any](v T) *T { return &v }

func get(t *testing.T, s *Store, repo string, id int64) task.Task {
	t.Helper()
	tasks, err := s.List(context.Background(), repo, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range tasks {
		if tk.ID == id {
			return tk
		}
	}
	t.Fatalf("task #%d not in %s", id, repo)
	return task.Task{}
}

func TestUpdateMovesWithMatchingFrom(t *testing.T) {
	s, _ := openTemp(t)
	tk := mustAdd(t, s, "/r", "claim me", task.Todo)
	got, err := s.Update(context.Background(), "/r", tk.ID, task.Patch{Status: ptr(task.Doing), From: ptr(task.Todo)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != task.Doing || get(t, s, "/r", tk.ID).Status != task.Doing {
		t.Errorf("status not updated: %+v", got)
	}
}

func TestUpdateConflictLeavesTaskUnchanged(t *testing.T) {
	s, _ := openTemp(t)
	tk := mustAdd(t, s, "/r", "taken", task.Doing)
	_, err := s.Update(context.Background(), "/r", tk.ID, task.Patch{Status: ptr(task.Done), From: ptr(task.Todo)})

	var conflict *task.ConflictError
	if !errors.As(err, &conflict) || conflict.Current != task.Doing {
		t.Fatalf("error = %v, want ConflictError with current doing", err)
	}
	if got := get(t, s, "/r", tk.ID); got.Status != task.Doing {
		t.Errorf("status changed to %s", got.Status)
	}
}

func TestUpdateChangesOnlyGivenFields(t *testing.T) {
	s, _ := openTemp(t)
	tk := mustAdd(t, s, "/r", "# old", task.Todo)
	got, err := s.Update(context.Background(), "/r", tk.ID, task.Patch{Body: ptr("  # new\n\n- [ ] step  ")})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := s.Get(context.Background(), "/r", tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []task.Task{got, stored} {
		if x.Body != "# new\n\n- [ ] step" || x.Status != task.Todo {
			t.Errorf("unexpected task after body patch: %+v", x)
		}
	}
}

func TestUpdateValidatesPatch(t *testing.T) {
	s, _ := openTemp(t)
	tk := mustAdd(t, s, "/r", "x", task.Todo)
	for _, p := range []task.Patch{{}, {From: ptr(task.Todo)}, {Body: ptr(" ")}} {
		if _, err := s.Update(context.Background(), "/r", tk.ID, p); err == nil {
			t.Errorf("Update(%+v) returned no error", p)
		}
	}
}

func TestUpdateAndDeleteAreRepoScoped(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	tk := mustAdd(t, s, "/a", "belongs to a", task.Todo)

	if _, err := s.Update(ctx, "/b", tk.ID, task.Patch{Status: ptr(task.Done)}); !errors.Is(err, task.ErrNotFound) {
		t.Errorf("cross-repo Update error = %v, want ErrNotFound", err)
	}
	if _, err := s.Get(ctx, "/b", tk.ID); !errors.Is(err, task.ErrNotFound) {
		t.Errorf("cross-repo Get error = %v, want ErrNotFound", err)
	}
	if err := s.Delete(ctx, "/b", tk.ID); !errors.Is(err, task.ErrNotFound) {
		t.Errorf("cross-repo Delete error = %v, want ErrNotFound", err)
	}
	if got := get(t, s, "/a", tk.ID); got.Status != task.Todo {
		t.Errorf("task changed by other repo: %+v", got)
	}
	if _, err := s.Update(ctx, "/a", 999, task.Patch{Status: ptr(task.Done)}); !errors.Is(err, task.ErrNotFound) {
		t.Errorf("missing id Update error = %v, want ErrNotFound", err)
	}
}

func TestDeleteDoesNotReuseIDs(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	mustAdd(t, s, "/r", "first", task.Todo)
	last := mustAdd(t, s, "/r", "last", task.Todo)
	if err := s.Delete(ctx, "/r", last.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "/r", last.ID); !errors.Is(err, task.ErrNotFound) {
		t.Errorf("second Delete error = %v, want ErrNotFound", err)
	}
	next := mustAdd(t, s, "/r", "next", task.Todo)
	if next.ID <= last.ID {
		t.Errorf("id %d reused after deleting %d", next.ID, last.ID)
	}
}

func TestRepos(t *testing.T) {
	s, _ := openTemp(t)
	mustAdd(t, s, "/b", "one", task.Todo)
	mustAdd(t, s, "/a", "one", task.Todo)
	mustAdd(t, s, "/a", "two", task.Done)
	got, err := s.Repos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []RepoSummary{{Path: "/a", Count: 2}, {Path: "/b", Count: 1}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Repos = %v, want %v", got, want)
	}
}

func TestUpdateBumpsUpdatedAt(t *testing.T) {
	s, _ := openTemp(t)
	tk := mustAdd(t, s, "/r", "x", task.Todo)
	if _, err := s.db.Exec(`UPDATE tasks SET created_at = 1, updated_at = 1 WHERE id = ?`, tk.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.Update(context.Background(), "/r", tk.ID, task.Patch{Status: ptr(task.Doing)})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := s.Get(context.Background(), "/r", tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []task.Task{got, stored} {
		if x.UpdatedAt.Unix() <= 1 || x.CreatedAt.Unix() != 1 {
			t.Errorf("timestamps after update: created %d, updated %d; want created 1 and updated now",
				x.CreatedAt.Unix(), x.UpdatedAt.Unix())
		}
	}
}

func TestMergeAppendsSourceAndDeletesIt(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	target := mustAdd(t, s, "/r", "# Fix login\n\nA", task.Todo)
	source := mustAdd(t, s, "/r", "# Login broken\n\n## Context\nB", task.Backlog)
	if _, err := s.db.Exec(`UPDATE tasks SET updated_at = 1 WHERE id = ?`, target.ID); err != nil {
		t.Fatal(err)
	}

	got, err := s.Merge(ctx, "/r", target.ID, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("# Fix login\n\nA\n\n## Merged from #%d: Login broken\n\n### Context\nB", source.ID)
	stored, err := s.Get(ctx, "/r", target.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []task.Task{got, stored} {
		if x.Body != want || x.Status != task.Todo || x.UpdatedAt.Unix() <= 1 {
			t.Errorf("merged target = %+v, want body %q, status todo and a new updated_at", x, want)
		}
	}
	if _, err := s.Get(ctx, "/r", source.ID); !errors.Is(err, task.ErrNotFound) {
		t.Errorf("source after merge: %v, want ErrNotFound", err)
	}
}

func TestMergeRefusesDoingSourceAndChangesNothing(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	target := mustAdd(t, s, "/r", "# target", task.Todo)
	source := mustAdd(t, s, "/r", "# claimed by an agent", task.Doing)

	_, err := s.Merge(ctx, "/r", target.ID, source.ID)
	var conflict *task.ConflictError
	if !errors.As(err, &conflict) || conflict.ID != source.ID || conflict.Current != task.Doing {
		t.Fatalf("Merge error = %v, want a conflict naming the doing source", err)
	}
	if got, _ := s.Get(ctx, "/r", target.ID); got.Body != "# target" {
		t.Errorf("target changed: %q", got.Body)
	}
	if _, err := s.Get(ctx, "/r", source.ID); err != nil {
		t.Errorf("source removed: %v", err)
	}
}

func TestMergeAllowsDoingTarget(t *testing.T) {
	s, _ := openTemp(t)
	target := mustAdd(t, s, "/r", "# in progress", task.Doing)
	source := mustAdd(t, s, "/r", "# duplicate", task.Todo)
	got, err := s.Merge(context.Background(), "/r", target.ID, source.ID)
	if err != nil || got.Status != task.Doing {
		t.Errorf("Merge into doing target = %+v, %v", got, err)
	}
}

func TestMergeRejectsSelfMissingAndOtherRepo(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	a := mustAdd(t, s, "/a", "# in a", task.Todo)
	b := mustAdd(t, s, "/b", "# in b", task.Todo)

	if _, err := s.Merge(ctx, "/a", a.ID, a.ID); !errors.Is(err, task.ErrInvalid) {
		t.Errorf("self merge error = %v, want ErrInvalid", err)
	}
	for name, ids := range map[string][2]int64{
		"source in other repo": {a.ID, b.ID},
		"target in other repo": {b.ID, a.ID},
		"missing source":       {a.ID, 999},
		"missing target":       {999, a.ID},
	} {
		if _, err := s.Merge(ctx, "/a", ids[0], ids[1]); !errors.Is(err, task.ErrNotFound) {
			t.Errorf("%s: error = %v, want ErrNotFound", name, err)
		}
	}
	for repo, tk := range map[string]task.Task{"/a": a, "/b": b} {
		if got, err := s.Get(ctx, repo, tk.ID); err != nil || got.Body != tk.Body {
			t.Errorf("%s changed after refused merges: %+v, %v", repo, got, err)
		}
	}
}

// addDraft adds d to repo and fails the test on error.
func addDraft(t *testing.T, s *Store, repo string, d task.Draft) task.Task {
	t.Helper()
	tk, err := s.Add(context.Background(), repo, d)
	if err != nil {
		t.Fatalf("Add(%+v): %v", d, err)
	}
	return tk
}

func TestAddStoresKindPriorityAndEpic(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	plain := addDraft(t, s, "/r", task.Draft{Body: "# plain"})
	epic := addDraft(t, s, "/r", task.Draft{Body: "# Auth rewrite", Kind: task.EpicKind, Priority: task.High})
	child := addDraft(t, s, "/r", task.Draft{Body: "# Login", Status: task.Todo, Priority: task.Low, EpicID: &epic.ID})

	for _, tc := range []struct {
		got      task.Task
		kind     task.Kind
		priority task.Priority
		epic     *int64
	}{
		{plain, task.TaskKind, task.Normal, nil},
		{epic, task.EpicKind, task.High, nil},
		{child, task.TaskKind, task.Low, &epic.ID},
	} {
		stored, err := s.Get(ctx, "/r", tc.got.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range []task.Task{tc.got, stored} {
			if x.Kind != tc.kind || x.Priority != tc.priority || !sameEpic(x.EpicID, tc.epic) {
				t.Errorf("#%d = kind %q, priority %q, epic %v; want %q, %q, %v",
					x.ID, x.Kind, x.Priority, x.EpicID, tc.kind, tc.priority, tc.epic)
			}
		}
	}
}

func sameEpic(a, b *int64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func TestEpicLinksMustNameAnEpicOfTheSameBoard(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	epic := addDraft(t, s, "/r", task.Draft{Body: "# epic", Kind: task.EpicKind})
	other := addDraft(t, s, "/r", task.Draft{Body: "# other epic", Kind: task.EpicKind})
	plain := addDraft(t, s, "/r", task.Draft{Body: "# plain"})
	foreign := addDraft(t, s, "/elsewhere", task.Draft{Body: "# foreign epic", Kind: task.EpicKind})
	missing := int64(999)

	for name, tc := range map[string]struct {
		epicID int64
		want   error
	}{
		"missing epic":    {missing, task.ErrNotFound},
		"epic of another": {foreign.ID, task.ErrNotFound},
		"not an epic":     {plain.ID, task.ErrInvalid},
	} {
		if _, err := s.Add(ctx, "/r", task.Draft{Body: "# x", EpicID: &tc.epicID}); !errors.Is(err, tc.want) {
			t.Errorf("Add with %s: error = %v, want %v", name, err, tc.want)
		}
		if _, err := s.Update(ctx, "/r", plain.ID, task.Patch{Epic: &tc.epicID}); !errors.Is(err, tc.want) {
			t.Errorf("Update with %s: error = %v, want %v", name, err, tc.want)
		}
	}
	if _, err := s.Update(ctx, "/r", epic.ID, task.Patch{Epic: &other.ID}); !errors.Is(err, task.ErrNestedEpic) {
		t.Errorf("epic into epic: error = %v, want ErrNestedEpic", err)
	}
	if got, _ := s.Get(ctx, "/r", plain.ID); got.EpicID != nil {
		t.Errorf("refused links changed the task: %+v", got)
	}
}

func TestUpdateLinksUnlinksAndReprioritizes(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	epic := addDraft(t, s, "/r", task.Draft{Body: "# epic", Kind: task.EpicKind})
	tk := addDraft(t, s, "/r", task.Draft{Body: "# task"})

	got, err := s.Update(ctx, "/r", tk.ID, task.Patch{Epic: &epic.ID, Priority: ptr(task.High)})
	if err != nil || !sameEpic(got.EpicID, &epic.ID) || got.Priority != task.High || got.Body != "# task" {
		t.Fatalf("link = %+v, %v", got, err)
	}
	got, err = s.Update(ctx, "/r", tk.ID, task.Patch{Epic: ptr(int64(0))})
	if err != nil || got.EpicID != nil || got.Priority != task.High {
		t.Fatalf("unlink = %+v, %v", got, err)
	}
	if stored, _ := s.Get(ctx, "/r", tk.ID); stored.EpicID != nil || stored.Priority != task.High {
		t.Errorf("stored after unlink = %+v", stored)
	}
}

func TestListOrdersByPriorityWithinStatus(t *testing.T) {
	s, _ := openTemp(t)
	low := addDraft(t, s, "/r", task.Draft{Body: "# low", Status: task.Todo, Priority: task.Low})
	high := addDraft(t, s, "/r", task.Draft{Body: "# high", Status: task.Todo, Priority: task.High})
	normal := addDraft(t, s, "/r", task.Draft{Body: "# normal", Status: task.Todo})
	high2 := addDraft(t, s, "/r", task.Draft{Body: "# high 2", Status: task.Todo, Priority: task.High})
	doingLow := addDraft(t, s, "/r", task.Draft{Body: "# doing low", Status: task.Doing, Priority: task.Low})
	backlogHigh := addDraft(t, s, "/r", task.Draft{Body: "# backlog high", Priority: task.High})
	got, err := s.List(context.Background(), "/r", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, got, backlogHigh.ID, high.ID, high2.ID, normal.ID, low.ID, doingLow.ID)
}

func TestListFiltersByEpic(t *testing.T) {
	s, _ := openTemp(t)
	epic := addDraft(t, s, "/r", task.Draft{Body: "# epic", Kind: task.EpicKind})
	a := addDraft(t, s, "/r", task.Draft{Body: "# a", EpicID: &epic.ID})
	addDraft(t, s, "/r", task.Draft{Body: "# unrelated"})
	b := addDraft(t, s, "/r", task.Draft{Body: "# b", Status: task.Done, EpicID: &epic.ID})
	got, err := s.List(context.Background(), "/r", Filter{Epic: epic.ID})
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, got, a.ID, b.ID)
	active, err := s.List(context.Background(), "/r", Filter{Epic: epic.ID, Statuses: task.ActiveStatuses()})
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, active, a.ID)
}

func TestDeletingAnEpicUnlinksItsTasks(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	epic := addDraft(t, s, "/r", task.Draft{Body: "# epic", Kind: task.EpicKind})
	child := addDraft(t, s, "/r", task.Draft{Body: "# child", EpicID: &epic.ID})
	if err := s.Delete(ctx, "/r", epic.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get(ctx, "/r", child.ID); err != nil || got.EpicID != nil {
		t.Errorf("child after epic delete = %+v, %v; want it kept without an epic", got, err)
	}
}

func TestMergingEpicsMovesTheirTasks(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	keep := addDraft(t, s, "/r", task.Draft{Body: "# keep", Kind: task.EpicKind})
	dup := addDraft(t, s, "/r", task.Draft{Body: "# duplicate", Kind: task.EpicKind})
	child := addDraft(t, s, "/r", task.Draft{Body: "# child", EpicID: &dup.ID})
	if _, err := s.Merge(ctx, "/r", keep.ID, dup.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, "/r", child.ID); !sameEpic(got.EpicID, &keep.ID) {
		t.Errorf("child epic after merge = %v, want #%d", got.EpicID, keep.ID)
	}
}

func TestMergingAnEpicIntoATaskIsRefused(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	plain := addDraft(t, s, "/r", task.Draft{Body: "# plain"})
	epic := addDraft(t, s, "/r", task.Draft{Body: "# epic", Kind: task.EpicKind})
	child := addDraft(t, s, "/r", task.Draft{Body: "# child", EpicID: &epic.ID})
	if _, err := s.Merge(ctx, "/r", plain.ID, epic.ID); !errors.Is(err, task.ErrInvalid) {
		t.Fatalf("epic into task: error = %v, want ErrInvalid", err)
	}
	if got, _ := s.Get(ctx, "/r", child.ID); !sameEpic(got.EpicID, &epic.ID) {
		t.Errorf("child changed after refused merge: %+v", got)
	}
	if got, _ := s.Get(ctx, "/r", plain.ID); got.Body != "# plain" {
		t.Errorf("target changed after refused merge: %q", got.Body)
	}
}

func TestMergingATaskIntoAnEpicKeepsTheEpic(t *testing.T) {
	s, _ := openTemp(t)
	epic := addDraft(t, s, "/r", task.Draft{Body: "# epic", Kind: task.EpicKind, Priority: task.High})
	tk := addDraft(t, s, "/r", task.Draft{Body: "# task"})
	got, err := s.Merge(context.Background(), "/r", epic.ID, tk.ID)
	if err != nil || got.Kind != task.EpicKind || got.Priority != task.High {
		t.Errorf("task into epic = %+v, %v", got, err)
	}
}
