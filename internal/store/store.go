// Package store persists tasks in a single SQLite file shared by all agentboard processes.
package store

import (
	"cmp"
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/yildizozan/agentboard/internal/task"
)

// dsnParams make concurrent writers from several processes safe:
// busy_timeout waits for locks instead of failing, and immediate transactions take
// the write lock up front so read-then-write steps cannot deadlock. foreign_keys protects
// references between tasks and epics.
// WAL is not set here: it persists in the file and is enabled once by enableWAL.
const dsnParams = "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate"

// lockWait bounds how long Open retries steps that SQLite does not cover with busy_timeout.
const lockWait = 5 * time.Second

//go:embed schema.sql
var schema string

// Store is a handle to the task database.
type Store struct {
	db *sql.DB
}

// Open creates the parent directory if needed, opens the database and upgrades the schema.
func Open(path string) (*Store, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve db path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	// SQLite uses a URI, so literal ?, # and % in filenames must be escaped.
	// The leading slash also keeps Windows drive letters in the URI path.
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath}
	db, err := sql.Open("sqlite", uri.String()+dsnParams)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// One connection per process: writers inside a process queue in database/sql
	// instead of competing for the SQLite lock; only processes compete, via busy_timeout.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	ctx := context.Background()
	if err := s.enableWAL(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// enableWAL switches the file to WAL so readers do not block writers.
// Changing the journal mode ignores busy_timeout and fails at once while another
// process holds a lock (only on a fresh file), so it is retried until lockWait.
func (s *Store) enableWAL(ctx context.Context) error {
	deadline := time.Now().Add(lockWait)
	for {
		var mode string
		err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&mode)
		switch {
		case err == nil && mode == "wal":
			return nil
		case err == nil:
			return fmt.Errorf("enable WAL: journal mode is %q", mode)
		case !isBusy(err) || time.Now().After(deadline):
			return fmt.Errorf("enable WAL: %w", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func isBusy(err error) bool {
	var sqlErr *sqlite.Error
	return errors.As(err, &sqlErr) && sqlErr.Code()&0xff == sqlite3.SQLITE_BUSY
}

// Add validates d and inserts it into repo. An epic link must name an epic of the same repo.
func (s *Store) Add(ctx context.Context, repo string, d task.Draft) (task.Task, error) {
	if repo == "" {
		return task.Task{}, task.Invalidf("repo must not be empty")
	}
	d, err := d.Validate()
	if err != nil {
		return task.Task{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return task.Task{}, fmt.Errorf("begin add: %w", err)
	}
	defer tx.Rollback()
	if d.EpicID != nil {
		if err := checkEpic(ctx, tx, repo, *d.EpicID); err != nil {
			return task.Task{}, err
		}
	}

	now := time.Now().Unix()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO tasks (repo, body, status, kind, priority, epic_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		repo, d.Body, string(d.Status), string(d.Kind), string(d.Priority), d.EpicID, now, now)
	if err != nil {
		return task.Task{}, fmt.Errorf("insert task: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return task.Task{}, fmt.Errorf("read task id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return task.Task{}, fmt.Errorf("commit add: %w", err)
	}
	return task.Task{
		ID:        id,
		Repo:      repo,
		Body:      d.Body,
		Status:    d.Status,
		Kind:      d.Kind,
		Priority:  d.Priority,
		EpicID:    d.EpicID,
		Revision:  1,
		CreatedAt: time.Unix(now, 0),
		UpdatedAt: time.Unix(now, 0),
	}, nil
}

// checkEpic returns task.ErrNotFound when epicID is not in repo, and a validation error
// when it is not an epic.
func checkEpic(ctx context.Context, q rowQuerier, repo string, epicID int64) error {
	epic, err := getTask(ctx, q, repo, epicID)
	if err != nil {
		return err
	}
	if epic.Kind != task.EpicKind {
		return task.Invalidf("#%d is not an epic", epicID)
	}
	return nil
}

// Update applies p to task id in repo inside one transaction.
// It returns task.ErrNotFound when the task is not in repo and *task.ConflictError when
// p.From is set and the task is currently in another status.
func (s *Store) Update(ctx context.Context, repo string, id int64, p task.Patch) (task.Task, error) {
	p, err := p.Validate()
	if err != nil {
		return task.Task{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return task.Task{}, fmt.Errorf("begin update: %w", err)
	}
	defer tx.Rollback()

	tk, err := getTask(ctx, tx, repo, id)
	if err != nil {
		return task.Task{}, err
	}
	if p.ExpectedRevision != nil && tk.Revision != *p.ExpectedRevision {
		return task.Task{}, &task.RevisionConflictError{ID: id, Expected: *p.ExpectedRevision, Current: tk.Revision}
	}
	if p.From != nil && tk.Status != *p.From {
		return task.Task{}, &task.ConflictError{ID: id, Current: tk.Status}
	}

	if p.Body != nil {
		tk.Body = *p.Body
	}
	if p.Status != nil {
		tk.Status = *p.Status
	}
	if p.Priority != nil {
		tk.Priority = *p.Priority
	}
	if err := linkEpic(ctx, tx, &tk, p.Epic); err != nil {
		return task.Task{}, err
	}
	now := time.Now().Unix()
	tk.UpdatedAt = time.Unix(now, 0)
	tk.Revision++
	if _, err := tx.ExecContext(ctx,
		`UPDATE tasks SET body = ?, status = ?, priority = ?, epic_id = ?, updated_at = ?, revision = ? WHERE id = ?`,
		tk.Body, string(tk.Status), string(tk.Priority), tk.EpicID, now, tk.Revision, id); err != nil {
		return task.Task{}, fmt.Errorf("update task: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return task.Task{}, fmt.Errorf("commit update: %w", err)
	}
	return tk, nil
}

// Get returns task id of repo or task.ErrNotFound.
func (s *Store) Get(ctx context.Context, repo string, id int64) (task.Task, error) {
	return getTask(ctx, s.db, repo, id)
}

// linkEpic keeps the parent or moves tk to another epic of the same board.
func linkEpic(ctx context.Context, q rowQuerier, tk *task.Task, epic *int64) error {
	if epic != nil {
		tk.EpicID = epic
	}
	if err := task.ValidateEpic(tk.Kind, tk.EpicID); err != nil {
		return err
	}
	if tk.EpicID == nil {
		return nil
	}
	return checkEpic(ctx, q, tk.Repo, *tk.EpicID)
}

// Merge appends task sourceID to task targetID of repo (task.MergeBody) and deletes the
// source, in one transaction. It returns task.ErrNotFound when either task is not in repo
// and *task.ConflictError when the source is doing, since an agent may be working on it.
func (s *Store) Merge(ctx context.Context, repo string, targetID, sourceID int64) (task.Task, error) {
	if targetID == sourceID {
		return task.Task{}, task.Invalidf("cannot merge task #%d into itself", targetID)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return task.Task{}, fmt.Errorf("begin merge: %w", err)
	}
	defer tx.Rollback()

	target, err := getTask(ctx, tx, repo, targetID)
	if err != nil {
		return task.Task{}, err
	}
	source, err := getTask(ctx, tx, repo, sourceID)
	if err != nil {
		return task.Task{}, err
	}
	if source.Status == task.Doing {
		return task.Task{}, &task.ConflictError{ID: sourceID, Current: source.Status}
	}
	if source.Kind == task.EpicKind && target.Kind != task.EpicKind {
		return task.Task{}, task.Invalidf("cannot merge epic #%d into task #%d: merge it into an epic", sourceID, targetID)
	}

	if err := task.ValidateEpic(target.Kind, target.EpicID); err != nil {
		return task.Task{}, err
	}

	target.Body = task.MergeBody(target, source)
	now := time.Now().Unix()
	target.UpdatedAt = time.Unix(now, 0)
	target.Revision++
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET body = ?, updated_at = ?, revision = ? WHERE id = ?`, target.Body, now, target.Revision, targetID); err != nil {
		return task.Task{}, fmt.Errorf("update merge target: %w", err)
	}
	// Move the source epic's tasks before deleting it, so every task keeps a parent.
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET epic_id = ?, updated_at = ?, revision = revision + 1 WHERE epic_id = ?`, targetID, now, sourceID); err != nil {
		return task.Task{}, fmt.Errorf("move merged epic's tasks: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, sourceID); err != nil {
		return task.Task{}, fmt.Errorf("delete merge source: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return task.Task{}, fmt.Errorf("commit merge: %w", err)
	}
	return target, nil
}

// Delete removes task id from repo. An epic must be empty before it can be deleted.
func (s *Store) Delete(ctx context.Context, repo string, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete: %w", err)
	}
	defer tx.Rollback()
	tk, err := getTask(ctx, tx, repo, id)
	if err != nil {
		return err
	}
	if tk.Kind == task.EpicKind {
		var hasTasks bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tasks WHERE epic_id = ?)`, id).Scan(&hasTasks); err != nil {
			return fmt.Errorf("check epic tasks: %w", err)
		}
		if hasTasks {
			return task.ErrEpicHasTasks
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE id = ? AND repo = ?`, id, repo); err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete: %w", err)
	}
	return nil
}

// Filter narrows List. Zero values match everything.
type Filter struct {
	Statuses []task.Status // only these statuses
	Epic     int64         // only tasks of this epic
}

// List returns the tasks of repo that match f, ordered by board column, then by priority
// (high first) and then by id.
func (s *Store) List(ctx context.Context, repo string, f Filter) ([]task.Task, error) {
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE repo = ?`
	args := []any{repo}
	if len(f.Statuses) > 0 {
		query += " AND status IN (?" + strings.Repeat(", ?", len(f.Statuses)-1) + ")"
		for _, st := range f.Statuses {
			args = append(args, string(st))
		}
	}
	if f.Epic != 0 {
		query += " AND epic_id = ?"
		args = append(args, f.Epic)
	}
	query += " ORDER BY id"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []task.Task
	for rows.Next() {
		tk, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, tk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}

	order := task.Statuses()
	slices.SortStableFunc(tasks, func(a, b task.Task) int {
		return cmp.Or(
			cmp.Compare(slices.Index(order, a.Status), slices.Index(order, b.Status)),
			cmp.Compare(a.Priority.Rank(), b.Priority.Rank()),
		)
	})
	return tasks, nil
}

// RepoSummary is one board and its number of tasks.
type RepoSummary struct {
	Path  string
	Count int
}

// Repos returns every repo that has at least one task, ordered by path.
func (s *Store) Repos(ctx context.Context) ([]RepoSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT repo, COUNT(*) FROM tasks GROUP BY repo ORDER BY repo`)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	defer rows.Close()
	var repos []RepoSummary
	for rows.Next() {
		var r RepoSummary
		if err := rows.Scan(&r.Path, &r.Count); err != nil {
			return nil, fmt.Errorf("scan repo: %w", err)
		}
		repos = append(repos, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	return repos, nil
}

// taskColumns is the column list scanTask expects.
const taskColumns = `id, repo, body, status, kind, priority, epic_id, created_at, updated_at, revision`

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// rowQuerier is satisfied by *sql.DB and *sql.Tx.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func getTask(ctx context.Context, q rowQuerier, repo string, id int64) (task.Task, error) {
	tk, err := scanTask(q.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = ? AND repo = ?`, id, repo))
	if errors.Is(err, sql.ErrNoRows) {
		return task.Task{}, fmt.Errorf("#%d: %w", id, task.ErrNotFound)
	}
	return tk, err
}

func scanTask(row scanner) (task.Task, error) {
	var (
		tk                     task.Task
		status, kind, priority string
		epic                   sql.NullInt64
		created, updated       int64
	)
	if err := row.Scan(&tk.ID, &tk.Repo, &tk.Body, &status, &kind, &priority, &epic, &created, &updated, &tk.Revision); err != nil {
		return task.Task{}, fmt.Errorf("scan task: %w", err)
	}
	tk.Status = task.Status(status)
	tk.Kind = task.Kind(kind)
	tk.Priority = task.Priority(priority)
	if epic.Valid {
		tk.EpicID = &epic.Int64
	}
	tk.CreatedAt = time.Unix(created, 0)
	tk.UpdatedAt = time.Unix(updated, 0)
	return tk, nil
}
