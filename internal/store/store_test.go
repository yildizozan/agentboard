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

func mustAdd(t *testing.T, s *Store, repo, title string, status task.Status) task.Task {
	t.Helper()
	tk, err := s.Add(context.Background(), repo, title, "", status)
	if err != nil {
		t.Fatalf("Add(%q): %v", title, err)
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
	tk, err := s.Add(context.Background(), "/r", "  Fix login  ", "details", task.Todo)
	if err != nil {
		t.Fatal(err)
	}
	if tk.ID == 0 || tk.Repo != "/r" || tk.Title != "Fix login" || tk.Description != "details" || tk.Status != task.Todo {
		t.Errorf("Add returned %+v", tk)
	}
	if tk.CreatedAt.IsZero() || !tk.UpdatedAt.Equal(tk.CreatedAt) {
		t.Errorf("timestamps not set: %+v", tk)
	}
}

func TestAddRejectsInvalidInput(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if _, err := s.Add(ctx, "/r", "  ", "", task.Todo); !errors.Is(err, task.ErrEmptyTitle) {
		t.Errorf("blank title error = %v, want ErrEmptyTitle", err)
	}
	if _, err := s.Add(ctx, "/r", "x", "", task.Status("later")); err == nil {
		t.Error("invalid status accepted")
	}
	if _, err := s.Add(ctx, "", "x", "", task.Todo); err == nil {
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
	if len(got) != 1 || got[0].Title != "in a" {
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
				if _, err := s.Add(context.Background(), "/r", fmt.Sprintf("s%d-w%d", i, j), "", task.Todo); err != nil {
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
