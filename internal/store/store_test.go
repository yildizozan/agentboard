package store

import (
	"context"
	"database/sql"
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
	tk, err := s.Add(context.Background(), repo, body, status)
	if err != nil {
		t.Fatalf("Add(%q): %v", body, err)
	}
	return tk
}

func userVersion(t *testing.T, s *Store) int {
	t.Helper()
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	return v
}

func TestOpenMigratesOnce(t *testing.T) {
	s, path := openTemp(t)
	mustAdd(t, s, "/r", "keep me", task.Todo)
	s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if v := userVersion(t, s2); v != len(migrations) {
		t.Errorf("user_version = %d, want %d", v, len(migrations))
	}
	got, err := s2.List(context.Background(), "/r", nil)
	if err != nil || len(got) != 1 {
		t.Fatalf("List after reopen = %v, %v; want 1 task", got, err)
	}
}

func TestMigrationV2MovesTitleAndDescriptionIntoBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentboard.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schemaV1 + `
		INSERT INTO tasks (repo, title, description, status, created_at, updated_at) VALUES
			('/r', 'Fix login', 'token expiry', 'todo', 1, 2),
			('/r', 'No details', '', 'done', 3, 4);
		PRAGMA user_version = 1;`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	got, err := s.List(context.Background(), "/r", nil)
	if err != nil || len(got) != 2 {
		t.Fatalf("List = %v, %v; want 2 tasks", got, err)
	}
	if got[0].Body != "# Fix login\n\ntoken expiry" || got[0].Title() != "Fix login" || got[0].UpdatedAt.Unix() != 2 {
		t.Errorf("migrated task = %+v", got[0])
	}
	if got[1].Body != "# No details" {
		t.Errorf("migrated task without description = %+v", got[1])
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
	tk, err := s.Add(context.Background(), "/r", "  Fix login  \n\ndetails\n", task.Todo)
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
	if _, err := s.Add(ctx, "/r", "  ", task.Todo); !errors.Is(err, task.ErrNoTitle) {
		t.Errorf("blank body error = %v, want ErrNoTitle", err)
	}
	if _, err := s.Add(ctx, "/r", "x", task.Status("later")); err == nil {
		t.Error("invalid status accepted")
	}
	if _, err := s.Add(ctx, "", "x", task.Todo); err == nil {
		t.Error("empty repo accepted")
	}
}

func TestListIsolatesRepos(t *testing.T) {
	s, _ := openTemp(t)
	mustAdd(t, s, "/a", "in a", task.Todo)
	mustAdd(t, s, "/b", "in b", task.Todo)

	got, err := s.List(context.Background(), "/a", nil)
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

	all, err := s.List(ctx, "/r", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, all, backlog.ID, todo1.ID, todo2.ID, doing.ID, done.ID)

	active, err := s.List(ctx, "/r", task.ActiveStatuses())
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, active, backlog.ID, todo1.ID, todo2.ID, doing.ID)

	onlyDone, err := s.List(ctx, "/r", []task.Status{task.Done})
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
				if _, err := s.Add(context.Background(), "/r", fmt.Sprintf("s%d-w%d", i, j), task.Todo); err != nil {
					errs <- err
				}
			}()
		}
	}
	wg.Wait()
	checkErrs(t, errs)

	got, err := opened[0].List(context.Background(), "/r", nil)
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
	tasks, err := s.List(context.Background(), repo, nil)
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
